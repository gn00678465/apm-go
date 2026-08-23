//go:build unix

package main

import "testing"

func TestIsHelpCase(t *testing.T) {
	cases := []struct {
		argv []string
		want bool
	}{
		{[]string{"doctor", "--help"}, true},
		{[]string{"--help"}, true},
		{[]string{"doctor", "-h"}, false},
		{[]string{"doctor"}, false},
		{nil, false},
	}
	for _, c := range cases {
		if got := isHelpCase(c.argv); got != c.want {
			t.Errorf("isHelpCase(%v) = %v, want %v", c.argv, got, c.want)
		}
	}
}

// clickDoctorHelp and cobraDoctorHelp are the two sides' actual doctor
// --help output (pinned oracle.pin commit c8d6cdec / apm-go's own doctor
// command), captured verbatim so the parser is exercised against real
// framework output rather than a hand-simplified stand-in.
const clickDoctorHelp = `Usage: apm doctor [OPTIONS]

  Run environment diagnostics (git, network, auth, marketplace config).
  Reports a pass/fail table and exits non-zero if a critical check fails.

Options:
  -v, --verbose  Show detailed output
  --help         Show this message and exit.
`

const cobraDoctorHelp = `Run environment diagnostics (git, network, auth, marketplace config). Reports a pass/fail table and exits non-zero if a critical check fails.

Usage:
  apm doctor [flags]

Flags:
  -h, --help      help for doctor
  -v, --verbose   Show detailed output
`

func TestParseHelpFlags_ClickAndCobraAgreeOnSameSemanticFlags(t *testing.T) {
	clickFlags := parseHelpFlags(clickDoctorHelp)
	cobraFlags := parseHelpFlags(cobraDoctorHelp)

	if !helpFlagsEqual(clickFlags, cobraFlags) {
		t.Fatalf("Click flags = %+v, Cobra flags = %+v, want equal despite different layout/ordering", clickFlags, cobraFlags)
	}
	if len(clickFlags) != 1 {
		t.Fatalf("parsed %d flags, want 1 (verbose; the framework-injected --help flag is excluded): %+v", len(clickFlags), clickFlags)
	}
}

func TestParseHelpFlags_ShortAliasAndDescription(t *testing.T) {
	flags := parseHelpFlags(cobraDoctorHelp)
	byLong := make(map[string]helpFlagInfo, len(flags))
	for _, f := range flags {
		byLong[f.LongFlag] = f
	}

	verbose, ok := byLong["verbose"]
	if !ok {
		t.Fatal("expected a \"verbose\" flag")
	}
	if verbose.ShortAlias != "v" || verbose.Description != "Show detailed output" {
		t.Errorf("verbose = %+v", verbose)
	}

	if _, ok := byLong["help"]; ok {
		t.Error(`the framework-injected "help" flag must be excluded from parseHelpFlags' result`)
	}
}

func TestParseHelpFlags_DefaultAnnotationExtractedFromEitherStyle(t *testing.T) {
	click := parseHelpFlags("  --limit INTEGER  Maximum results to show  [default: 20]\n")
	if len(click) != 1 || click[0].DefaultIfShown != "20" {
		t.Errorf("Click default parse = %+v, want DefaultIfShown \"20\"", click)
	}

	cobra := parseHelpFlags("  --limit int   Maximum results to show (default 20)\n")
	if len(cobra) != 1 || cobra[0].DefaultIfShown != "20" {
		t.Errorf("Cobra default parse = %+v, want DefaultIfShown \"20\"", cobra)
	}
}

func TestParseHelpDescriptionParagraph_ClickAndCobraAgree(t *testing.T) {
	want := "Run environment diagnostics (git, network, auth, marketplace config). Reports a pass/fail table and exits non-zero if a critical check fails."

	if got := parseHelpDescriptionParagraph(clickDoctorHelp); got != want {
		t.Errorf("Click paragraph = %q, want %q", got, want)
	}
	if got := parseHelpDescriptionParagraph(cobraDoctorHelp); got != want {
		t.Errorf("Cobra paragraph = %q, want %q", got, want)
	}
}

func TestDiffHelpSemantic_LayoutOnlyDifferenceIsNotADiff(t *testing.T) {
	outDir := t.TempDir()
	writeBinFiles(t, outDir, "oracle", "c1", clickDoctorHelp, "")
	writeBinFiles(t, outDir, "target", "c1", cobraDoctorHelp, "")

	hs, differ, err := diffHelpSemantic(
		outDir+"/oracle/c1", outDir+"/target/c1",
		"", "", "", "", "", "", false,
	)
	if err != nil {
		t.Fatalf("diffHelpSemantic: %v", err)
	}
	if differ {
		t.Errorf("diffHelpSemantic reported a diff for semantically identical Click/Cobra output: %+v", hs)
	}
}

func TestDiffHelpSemantic_DescriptionDriftIsDetected(t *testing.T) {
	outDir := t.TempDir()
	driftedCobra := `Run environment diagnostics (git, network, auth, marketplace config). Reports a pass/fail table and exits non-zero if a critical check fails.

Usage:
  apm doctor [flags]

Flags:
  -h, --help      help for doctor
  -v, --verbose   Enable verbose logging
`
	writeBinFiles(t, outDir, "oracle", "c1", clickDoctorHelp, "")
	writeBinFiles(t, outDir, "target", "c1", driftedCobra, "")

	hs, differ, err := diffHelpSemantic(
		outDir+"/oracle/c1", outDir+"/target/c1",
		"", "", "", "", "", "", false,
	)
	if err != nil {
		t.Fatalf("diffHelpSemantic: %v", err)
	}
	if !differ {
		t.Fatal("diffHelpSemantic did not detect a genuine flag description drift")
	}
	if hs.Flags == nil {
		t.Fatal("hs.Flags = nil, want the differing verbose description")
	}
}

// TestDiffCase_HelpCaseSeparatesLayoutFromSemanticDrift is the integration
// proof for ticket 02 attempt 2's help_semantic requirement: a --help case
// where the two frameworks render the same semantic content differently
// gets "stdout" in Fields (the layout difference) but NOT "help_semantic";
// a --help case with an actual flag description drift gets BOTH.
func TestDiffCase_HelpCaseSeparatesLayoutFromSemanticDrift(t *testing.T) {
	t.Run("layout only", func(t *testing.T) {
		outDir := t.TempDir()
		c := Case{ID: "doctor-help", Argv: []string{"doctor", "--help"}, RewriteBinaryName: true}
		writeBinFiles(t, outDir, "oracle", "doctor-help", clickDoctorHelp, "")
		writeBinFiles(t, outDir, "target", "doctor-help", cobraDoctorHelp, "")

		cd, detail := mustDiffCase(t, outDir, c, Record{}, Record{})
		if !fieldsEqual(cd.Fields, []string{"stdout"}) {
			t.Fatalf("Fields = %v, want [stdout] only (layout differs, semantics match)", cd.Fields)
		}
		if detail.HelpSemantic != nil {
			t.Errorf("detail.HelpSemantic = %+v, want nil", detail.HelpSemantic)
		}
	})

	t.Run("semantic drift", func(t *testing.T) {
		outDir := t.TempDir()
		c := Case{ID: "doctor-help", Argv: []string{"doctor", "--help"}, RewriteBinaryName: true}
		driftedCobra := `Run environment diagnostics (git, network, auth, marketplace config). Reports a pass/fail table and exits non-zero if a critical check fails.

Usage:
  apm doctor [flags]

Flags:
  -h, --help      help for doctor
  -v, --verbose   Enable verbose logging
`
		writeBinFiles(t, outDir, "oracle", "doctor-help", clickDoctorHelp, "")
		writeBinFiles(t, outDir, "target", "doctor-help", driftedCobra, "")

		cd, detail := mustDiffCase(t, outDir, c, Record{}, Record{})
		if !fieldsEqual(cd.Fields, []string{"stdout", "help_semantic"}) {
			t.Fatalf("Fields = %v, want [stdout help_semantic]: a stdout waiver must not be able to hide this", cd.Fields)
		}
		if detail.HelpSemantic == nil || detail.HelpSemantic.Flags == nil {
			t.Errorf("detail.HelpSemantic = %+v, want a populated Flags diff", detail.HelpSemantic)
		}
		if !containsStr(cd.Taxonomy.Heuristic, "F01") {
			t.Errorf("Heuristic = %v, want to contain F01 for a help_semantic diff (code-review finding: heuristicTaxonomy had no case for it)", cd.Taxonomy.Heuristic)
		}
	})
}

// TestDiffCase_NonHelpCaseNeverComputesHelpSemantic proves help_semantic is
// only ever computed for a case whose argv ends in --help, per acceptance --
// an ordinary case's stdout happening to look flag-like must never trigger
// it.
func TestDiffCase_NonHelpCaseNeverComputesHelpSemantic(t *testing.T) {
	outDir := t.TempDir()
	c := Case{ID: "c1", Argv: []string{"doctor"}}
	writeBinFiles(t, outDir, "oracle", "c1", "  -v, --verbose  Show detailed output\n", "")
	writeBinFiles(t, outDir, "target", "c1", "  -v, --verbose  Enable verbose logging\n", "")

	_, detail := mustDiffCase(t, outDir, c, Record{}, Record{})
	if detail.HelpSemantic != nil {
		t.Errorf("detail.HelpSemantic = %+v, want nil for a non---help case", detail.HelpSemantic)
	}
}
