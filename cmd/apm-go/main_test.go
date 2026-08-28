package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/manifest"
	"github.com/apm-go/apm/internal/yamlcore"
)

func oraclePath(t *testing.T, rel string) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(wd, "..", "..")
	candidates := []string{
		filepath.Join(root, "conformance-kit", "oracle", rel),
		filepath.Join(root, "..", "conformance-kit", "oracle", rel),
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	t.Skipf("oracle fixture not found: %s", rel)
	return ""
}

// ── init tests (non-interactive via --yes) ──

func TestInitCmd_YesMode(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	cmd := initCmd()
	cmd.SetArgs([]string{"--yes", "--target", "claude"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("init --yes failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "apm.yml"))
	if err != nil {
		t.Fatal(err)
	}

	node, err := yamlcore.SafeLoad(data)
	if err != nil {
		t.Fatalf("SafeLoad failed: %v", err)
	}
	m, _, err := manifest.ParseManifest(node)
	if err != nil {
		t.Fatalf("ParseManifest failed: %v", err)
	}
	if m.Name == "" {
		t.Error("name should not be empty")
	}
	if m.Version != "1.0.0" {
		t.Errorf("version = %q, want 1.0.0", m.Version)
	}
	if m.Description == "" {
		t.Error("description should not be empty")
	}
	if m.Author == "" {
		t.Error("author should not be empty")
	}

	content := string(data)
	if strings.Contains(content, "minimal") {
		t.Error("init output must not contain 'minimal'")
	}
	// R1/AC1: init must write the plural targets: key, never the singular
	// target: key (this assertion used to require the opposite).
	if !strings.Contains(content, "targets:") {
		t.Error("init output must contain 'targets:' (plural)")
	}
	if regexp.MustCompile(`(?m)^target:`).MatchString(content) {
		t.Error("init output must not contain singular 'target:'")
	}
}

func TestInitCmd_GeneratedFieldsComplete(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	cmd := initCmd()
	cmd.SetArgs([]string{"--yes"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("init --yes failed: %v", err)
	}

	data, _ := os.ReadFile("apm.yml")
	content := string(data)

	requiredFields := []string{"name:", "version:", "description:", "author:", "dependencies:", "includes:"}
	for _, f := range requiredFields {
		if !strings.Contains(content, f) {
			t.Errorf("init output missing field %q", f)
		}
	}
}

func TestInitCmd_ExistingYmlYesOverwrites(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	os.WriteFile("apm.yml", []byte("existing"), 0644)

	cmd := initCmd()
	cmd.SetArgs([]string{"--yes"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("init --yes should overwrite: %v", err)
	}

	data, _ := os.ReadFile("apm.yml")
	if string(data) == "existing" {
		t.Error("apm.yml should have been overwritten")
	}
}

func TestInitCmd_TargetFlagValidation(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	cmd := initCmd()
	cmd.SetArgs([]string{"--yes", "--target", "notarealtool"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for invalid target")
	}
}

func TestInitCmd_OutputPassesSelfValidation(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	cmd := initCmd()
	cmd.SetArgs([]string{"--yes", "--target", "claude,copilot"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile("apm.yml")
	node, err := yamlcore.SafeLoad(data)
	if err != nil {
		t.Fatalf("init output fails SafeLoad: %v", err)
	}
	_, _, err = manifest.ParseManifest(node)
	if err != nil {
		t.Fatalf("init output fails ParseManifest: %v", err)
	}
}

func TestInitCmd_ProjectNameArg(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	cmd := initCmd()
	cmd.SetArgs([]string{"my-new-project", "--yes"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("init with project name failed: %v", err)
	}

	// Should have created the directory and apm.yml inside it
	if _, err := os.Stat(filepath.Join(dir, "my-new-project", "apm.yml")); err != nil {
		t.Error("apm.yml should be in the new project directory")
	}
}

// TestInitCmd_SuggestsOracleNextSteps locks the pinned Oracle's consumer
// guidance: the success panel includes both the valid install command and
// the Oracle's run-script line, with apm-go's sanctioned binary-name rewrite.
//
// The next-step lines print via ux.Info, which (ticket 10 attempt 3) now
// redirects os.Stderr to os.Stdout the same way Warn/Error already did --
// so this asserts against captureStdout, not stderr.
func TestInitCmd_SuggestsOracleNextSteps(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	stdout := captureStdout(t, func() {
		cmd := initCmd()
		cmd.SetArgs([]string{"--yes"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("init --yes failed: %v", err)
		}
	})

	if !strings.Contains(stdout, "apm-go install") {
		t.Errorf("init output = %q, want the valid 'apm-go install' next-step to remain", stdout)
	}
	if !strings.Contains(stdout, "apm-go run <script>") {
		t.Errorf("init output = %q, want the Oracle's 'apm-go run <script>' next-step", stdout)
	}
}

// ── validate dispatch tests ──

func TestValidateCmd_LockfileBypass(t *testing.T) {
	p := oraclePath(t, filepath.Join("lockfile", "v1-git-only.yml"))
	cmd := validateCmd()
	cmd.SetArgs([]string{p})
	if err := cmd.Execute(); err != nil {
		t.Errorf("lockfile should be accepted via content-sniff: %v", err)
	}
}

func TestValidateCmd_InvalidManifest(t *testing.T) {
	p := oraclePath(t, filepath.Join("manifest", "invalid-missing-name.yml"))
	cmd := validateCmd()
	cmd.SetArgs([]string{p})
	if err := cmd.Execute(); err == nil {
		t.Error("expected error for invalid manifest")
	}
}

func TestValidateCmd_ValidManifest(t *testing.T) {
	p := oraclePath(t, filepath.Join("manifest", "valid-minimal.yml"))
	cmd := validateCmd()
	cmd.SetArgs([]string{p})
	if err := cmd.Execute(); err != nil {
		t.Errorf("valid manifest should be accepted: %v", err)
	}
}

// ── helper tests ──

// TestBuildManifestNode_KeyShapeAndContent replaces the old
// TestBuildManifestData: buildManifestData (a map[string]any, sorted
// alphabetically by go-yaml on marshal) was replaced by buildManifestNode (an
// ordered *yaml.Node tree, R2) so the produced apm.yml can have a semantic
// key order and a HeadComment above targets:. This asserts the node's
// content still carries the same information the old map-based test did.
func TestBuildManifestNode_KeyShapeAndContent(t *testing.T) {
	node := buildManifestNode(manifestSpec{
		Name: "test", Version: "1.0.0", Description: "desc", Author: "author",
		Targets: []string{"claude"},
	})

	out, err := yamlcore.SafeDump(node)
	if err != nil {
		t.Fatalf("SafeDump: %v", err)
	}
	content := string(out)

	for _, want := range []string{"name: test", "includes: auto", "dependencies:", "apm: []", "mcp: []"} {
		if !strings.Contains(content, want) {
			t.Errorf("buildManifestNode output missing %q:\n%s", want, content)
		}
	}
}

// TestReadExistingTargets covers the singular target: key (back-compat).
// The plural targets: key is covered separately by
// TestReadExistingTargets_BothKeys (AC2/AC3).
func TestReadExistingTargets(t *testing.T) {
	t.Run("sequence form", func(t *testing.T) {
		dir := t.TempDir()
		origDir, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(origDir)

		os.WriteFile("apm.yml", []byte("name: p\nversion: \"1.0.0\"\ntarget:\n  - claude\n  - copilot\n"), 0644)
		targets := readExistingTargets()
		if len(targets) != 2 || targets[0] != "claude" || targets[1] != "copilot" {
			t.Errorf("got %v, want [claude copilot]", targets)
		}
	})

	t.Run("scalar form", func(t *testing.T) {
		dir := t.TempDir()
		origDir, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(origDir)

		os.WriteFile("apm.yml", []byte("name: p\nversion: \"1.0.0\"\ntarget: claude\n"), 0644)
		targets := readExistingTargets()
		if len(targets) != 1 || targets[0] != "claude" {
			t.Errorf("got %v, want [claude]", targets)
		}
	})

	t.Run("no file", func(t *testing.T) {
		dir := t.TempDir()
		origDir, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(origDir)

		targets := readExistingTargets()
		if targets != nil {
			t.Errorf("got %v, want nil", targets)
		}
	})
}

// TestReadExistingTargets_BothKeys is the R1.1/AC2/AC3 regression:
// readExistingTargets used to only look at the singular target: key, so a
// project already on the plural targets: key silently lost its MultiSelect
// preselection (readExistingTargets returned nil for it). Both keys must
// round-trip; a file declaring both (invalid apm.yml, rejected by
// manifest.ParseManifest's hasConflictingTargetKeys check) must resolve to
// nil rather than guessing a winner, since readExistingTargets goes through
// that same validated parse (see its doc comment) instead of a second,
// looser ad hoc parser.
func TestReadExistingTargets_BothKeys(t *testing.T) {
	t.Run("plural sequence form", func(t *testing.T) {
		dir := t.TempDir()
		origDir, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(origDir)

		os.WriteFile("apm.yml", []byte("name: p\nversion: \"1.0.0\"\ntargets:\n  - claude\n  - copilot\n"), 0644)
		targets := readExistingTargets()
		if len(targets) != 2 || targets[0] != "claude" || targets[1] != "copilot" {
			t.Errorf("got %v, want [claude copilot]", targets)
		}
	})

	t.Run("plural scalar form", func(t *testing.T) {
		dir := t.TempDir()
		origDir, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(origDir)

		os.WriteFile("apm.yml", []byte("name: p\nversion: \"1.0.0\"\ntargets: claude\n"), 0644)
		targets := readExistingTargets()
		if len(targets) != 1 || targets[0] != "claude" {
			t.Errorf("got %v, want [claude]", targets)
		}
	})

	t.Run("singular still works after plural support added", func(t *testing.T) {
		dir := t.TempDir()
		origDir, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(origDir)

		os.WriteFile("apm.yml", []byte("name: p\nversion: \"1.0.0\"\ntarget:\n  - codex\n"), 0644)
		targets := readExistingTargets()
		if len(targets) != 1 || targets[0] != "codex" {
			t.Errorf("got %v, want [codex]", targets)
		}
	})

	t.Run("both keys present is invalid, no preselection guessed", func(t *testing.T) {
		dir := t.TempDir()
		origDir, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(origDir)

		// manifest.ParseManifest rejects an apm.yml declaring both keys
		// (manifest.go's hasConflictingTargetKeys check) -- readExistingTargets
		// goes through that same validation (it is not a second, ad hoc
		// parser, see its doc comment), so it must not guess a winner for a
		// file the real parser would reject; nil (no preselection) is
		// correct here, not a crash and not a silently-picked key.
		os.WriteFile("apm.yml", []byte("name: p\nversion: \"1.0.0\"\ntarget:\n  - codex\ntargets:\n  - claude\n"), 0644)
		targets := readExistingTargets()
		if targets != nil {
			t.Errorf("got %v, want nil (conflicting target/targets keys are invalid apm.yml)", targets)
		}
	})

	t.Run("CSV sugar on the singular scalar key", func(t *testing.T) {
		dir := t.TempDir()
		origDir, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(origDir)

		// manifest.go's parseTargetField splits a singular scalar target:
		// value on "," and trims each token (CSV sugar). The codex-audit
		// regression: an earlier readExistingTargets implementation
		// type-switched on the raw decoded YAML value without this
		// splitting, so "claude,copilot" round-tripped as one bogus
		// "claude,copilot" token instead of two real targets, and neither
		// would be preselected in the MultiSelect prompt (AC2).
		os.WriteFile("apm.yml", []byte("name: p\nversion: \"1.0.0\"\ntarget: claude,copilot\n"), 0644)
		targets := readExistingTargets()
		if len(targets) != 2 || targets[0] != "claude" || targets[1] != "copilot" {
			t.Errorf("got %v, want [claude copilot]", targets)
		}
	})

	t.Run("target alias is normalized", func(t *testing.T) {
		dir := t.TempDir()
		origDir, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(origDir)

		// manifest.ValidateTarget normalizes aliases (vscode -> copilot,
		// TargetAliases in target.go) -- the codex-audit regression: the
		// prior ad hoc parser returned the alias token verbatim, which
		// never matches any canonical MultiSelect option value, so it was
		// never preselected (AC2/AC3).
		os.WriteFile("apm.yml", []byte("name: p\nversion: \"1.0.0\"\ntargets:\n  - vscode\n"), 0644)
		targets := readExistingTargets()
		if len(targets) != 1 || targets[0] != "copilot" {
			t.Errorf("got %v, want [copilot] (vscode alias normalized)", targets)
		}
	})
}

// TestReadExistingTargets_LenientOnUnrelatedParseError is the 2026-07-30
// codex Tier 2 B5 fix: readExistingTargets used to go through
// manifest.ParseManifest ONLY, so an apm.yml that fails to parse for a
// reason UNRELATED to targets (e.g. a missing required version: field)
// silently lost its entire, otherwise-legal target selection -- the user is
// still running interactive init, but the MultiSelect preselection for an
// existing, readable target: value vanishes. Before this task introduced
// the ParseManifest-only pipeline, a bare type-switch read this value fine;
// this is a behavior regression this task introduced, not a pre-existing
// gap. version: is deliberately omitted here to trigger the parse failure.
func TestReadExistingTargets_LenientOnUnrelatedParseError(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	os.WriteFile("apm.yml", []byte("name: p\ntarget: claude\n"), 0644)
	targets := readExistingTargets()
	if len(targets) != 1 || targets[0] != "claude" {
		t.Errorf("got %v, want [claude] (an unrelated parse error must not drop a legal target selection)", targets)
	}
}

// TestReadExistingTargets_LenientDoesNotSplitCSVOnPluralKey is the 2026-07-30
// codex Tier 2 fix to lenientReadTargets: the singular target: scalar and
// the plural targets: scalar are NOT parsed the same way by the real parser.
// manifest.go's parseTargetField (target:, manifest.go:265) splits a scalar
// on "," -- CSV sugar for a list. manifest.go's parseTargetsField (targets:,
// manifest.go:312-313) does NOT split a scalar on "," -- the whole scalar is
// one (here invalid) token. lenientReadTargets used to CSV-split every
// scalar unconditionally, so a plural "targets: claude,copilot" was
// silently manufactured into two preselected targets an interactive init()
// run never actually wrote for that key. version: is omitted from both
// fixtures to force manifest.ParseManifest to fail and exercise the
// lenientReadTargets fallback specifically (see
// TestReadExistingTargets_LenientOnUnrelatedParseError).
func TestReadExistingTargets_LenientDoesNotSplitCSVOnPluralKey(t *testing.T) {
	t.Run("plural key CSV-shaped scalar is one invalid token, not split", func(t *testing.T) {
		dir := t.TempDir()
		origDir, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(origDir)

		os.WriteFile("apm.yml", []byte("name: p\ntargets: claude,copilot\n"), 0644)
		targets := readExistingTargets()
		if targets != nil {
			t.Errorf("got %v, want nil (targets: scalar is a single token per parseTargetsField, not CSV sugar; \"claude,copilot\" fails ValidateTarget as one token and must be dropped, not split into two)", targets)
		}
	})

	t.Run("singular key CSV-shaped scalar is still split", func(t *testing.T) {
		dir := t.TempDir()
		origDir, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(origDir)

		os.WriteFile("apm.yml", []byte("name: p\ntarget: claude,copilot\n"), 0644)
		targets := readExistingTargets()
		if len(targets) != 2 || targets[0] != "claude" || targets[1] != "copilot" {
			t.Errorf("got %v, want [claude copilot] (target: scalar is CSV sugar per parseTargetField)", targets)
		}
	})
}

// TestInitCmd_NonInitTargetRejected covers targets init deliberately doesn't
// support via --target. agent-skills used to be in this list, but AC24
// (R8.1) requires init to accept it now that it has a deploy adapter.
func TestInitCmd_NonInitTargetRejected(t *testing.T) {
	for _, bad := range []string{"gemini", "cursor", "windsurf"} {
		t.Run(bad, func(t *testing.T) {
			dir := t.TempDir()
			origDir, _ := os.Getwd()
			os.Chdir(dir)
			defer os.Chdir(origDir)

			cmd := initCmd()
			cmd.SetArgs([]string{"--yes", "--target", bad})
			if err := cmd.Execute(); err == nil {
				t.Errorf("--target %s should be rejected by init", bad)
			}
		})
	}
}

// TestInitCmd_TargetFlag_AcceptsExplicitOnly is the --target half of the
// 2026-08-02 explicit-only-targets parity fix: even though antigravity and
// agent-skills are excluded from the interactive MultiSelect menu (see
// TestTargetSelectOptions_ExcludesExplicitOnly), `--target` must still
// accept them explicitly -- upstream's EXPLICIT_ONLY_TARGETS only gates the
// prompt menu (commands/init.py:629), never the --target flag path
// (manifest_targets_from_target_option has no such filter).
func TestInitCmd_TargetFlag_AcceptsExplicitOnly(t *testing.T) {
	for _, explicitOnly := range []string{"antigravity", "agent-skills"} {
		t.Run(explicitOnly, func(t *testing.T) {
			dir := t.TempDir()
			origDir, _ := os.Getwd()
			os.Chdir(dir)
			defer os.Chdir(origDir)

			cmd := initCmd()
			cmd.SetArgs([]string{"--yes", "--target", explicitOnly})
			if err := cmd.Execute(); err != nil {
				t.Errorf("--target %s should be accepted by init, got error: %v", explicitOnly, err)
			}
		})
	}
}

func TestInitCmd_ProjectNameWithDotDotRejected(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	cmd := initCmd()
	cmd.SetArgs([]string{"..", "--yes"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for project name '..'")
	}
}

func TestInitCmd_ForceOverwrites(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	os.WriteFile("apm.yml", []byte("existing"), 0644)

	cmd := initCmd()
	cmd.SetArgs([]string{"--force"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("init --force should succeed: %v", err)
	}

	data, _ := os.ReadFile("apm.yml")
	if string(data) == "existing" {
		t.Error("apm.yml should have been overwritten")
	}
}

func TestBuildManifestNode_NoTargets(t *testing.T) {
	node := buildManifestNode(manifestSpec{Name: "test", Version: "1.0.0", Description: "desc", Author: "author"})
	out, err := yamlcore.SafeDump(node)
	if err != nil {
		t.Fatalf("SafeDump: %v", err)
	}
	content := string(out)
	if regexp.MustCompile(`(?m)^targets:`).MatchString(content) {
		t.Errorf("targets: should not be present when no targets selected:\n%s", content)
	}
	if regexp.MustCompile(`(?m)^target:`).MatchString(content) {
		t.Errorf("target: should not be present when no targets selected:\n%s", content)
	}
}
