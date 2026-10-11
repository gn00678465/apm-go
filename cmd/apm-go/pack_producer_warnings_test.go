package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/marketplace/build"
)

const packWarningsRoot = "name: warnings-fixture\nversion: 1.0.0\nlicense: MIT\n"

const packWarningsLegacyYML = `name: legacy-marketplace
description: Legacy marketplace
version: 1.0.0
owner:
  name: APM Tests
packages:
  - name: remote-tool
    source: acme/remote-tool
    ref: 0123456789abcdef0123456789abcdef01234567
    category: Productivity
`

const packWarningsPluginRootYML = packWarningsRoot + `marketplace:
  owner:
    name: APM Tests
  metadata:
    pluginRoot: ./plugins
  packages:
    - name: pkg-a
      source: ./other/pkg-a
      category: Productivity
`

const (
	// With apm-go's command name; the Oracle's text is
	// marketplace/migration.py:45-48 (v0.33.0).
	wantLegacyWarning  = "marketplace.yml is deprecated. Run 'apm-go marketplace migrate' to fold it into apm.yml's 'marketplace:' block."
	pluginRootLineText = `source "./other/pkg-a" is outside pluginRoot "./plugins"`
)

func writePackFixtureFile(t *testing.T, rel, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(rel), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rel, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func setPackMetadataOutcome(t *testing.T, outcome string) {
	t.Helper()
	prev := build.DefaultMetadataFetcher
	build.DefaultMetadataFetcher = &countingMetadataFetcher{outcomes: map[string]string{"*": outcome}}
	t.Cleanup(func() { build.DefaultMetadataFetcher = prev })
}

func packJSONWarnings(t *testing.T, args ...string) []string {
	t.Helper()
	stdout, stderr, exit := runPackLikeMain(t, append(args, "--json"))
	if exit != 0 {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", exit, stdout, stderr)
	}
	var env struct {
		Warnings []string `json:"warnings"`
	}
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	return env.Warnings
}

// The Oracle logs a local apm.yml it cannot read at debug level only
// (v0.33.0 marketplace/builder.py:1145-1149), so the JSON warnings stay
// empty; the line apm-go prints ends in the raw parse error.
func TestPackJSON_Warnings_OmitUnreadableLocalMetadata(t *testing.T) {
	chdirTemp(t)
	writePackApmYML(t, packWarningsRoot+`marketplace:
  owner:
    name: APM Tests
  packages:
    - name: pkg-a
      source: ./pkgs/pkg-a
      category: Productivity
`)
	writePackFixtureFile(t, "pkgs/pkg-a/apm.yml", "name: pkg-a\ndescription: [unclosed\n")

	if got := packJSONWarnings(t); !reflect.DeepEqual(got, []string{}) {
		t.Errorf("warnings = %q, want none", got)
	}
}

// The Oracle's pluginRoot line is a BuildDiagnostic (v0.33.0
// marketplace/output_mappers.py:229-238), which is not in the build's
// warnings (marketplace/builder.py:316-320).
func TestPackJSON_Warnings_OmitPluginRootDiagnostic(t *testing.T) {
	chdirTemp(t)
	writePackApmYML(t, packWarningsPluginRootYML)
	writePackFixtureFile(t, "other/pkg-a/apm.yml", "name: pkg-a\ndescription: A local package\nversion: 1.0.0\n")

	if got := packJSONWarnings(t); !reflect.DeepEqual(got, []string{}) {
		t.Errorf("warnings = %q, want none", got)
	}
}

// The legacy-file deprecation is the Oracle's plan.warnings (v0.33.0
// core/build_orchestrator.py:172-180), reported ahead of the build's own
// warnings (:295), also when -m selects no output (:224-229).
func TestPackJSON_Warnings_LegacyDeprecation(t *testing.T) {
	chdirTemp(t)
	writePackApmYML(t, packWarningsRoot)
	writePackFixtureFile(t, "marketplace.yml", packWarningsLegacyYML)
	setPackMetadataOutcome(t, "fetched")

	want := []string{wantLegacyWarning}
	if got := packJSONWarnings(t); !reflect.DeepEqual(got, want) {
		t.Errorf("warnings = %q, want %q", got, want)
	}
	if got := packJSONWarnings(t, "-m", "none"); !reflect.DeepEqual(got, want) {
		t.Errorf("-m none: warnings = %q, want %q", got, want)
	}
}

// The Oracle prints the producer's warnings only in
// _render_marketplace_result (v0.33.0 commands/pack.py:811-819), and a
// --check-clean run skips that call when the dry run is implied
// (:642-647) or the drift metadata cannot be certified (:663-668).
func TestPack_CheckClean_ProducerWarnings(t *testing.T) {
	tests := []struct {
		name     string
		outcome  string
		args     []string
		wantExit int
		// wantAfter is the line the legacy warning must follow; empty means
		// the warning must not be printed.
		wantAfter string
	}{
		{"implied dry run", "fetched", []string{"--check-clean"}, 4, ""},
		{"explicit dry run, metadata not certifiable", "failed:request timed out", []string{"--check-clean", "--dry-run"}, 4, ""},
		{"explicit dry run, metadata certifiable", "fetched", []string{"--check-clean", "--dry-run"}, 4, "[missing on disk"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chdirTemp(t)
			writePackApmYML(t, packWarningsRoot)
			writePackFixtureFile(t, "marketplace.yml", packWarningsLegacyYML)
			setPackMetadataOutcome(t, tt.outcome)

			out, _, exit := runPackLikeMain(t, tt.args)
			if exit != tt.wantExit {
				t.Errorf("exit %d, want %d\n%s", exit, tt.wantExit, out)
			}
			at := strings.Index(out, " ! "+wantLegacyWarning+"\n")
			if tt.wantAfter == "" {
				if at >= 0 {
					t.Errorf("the producer's warning was printed:\n%s", out)
				}
				return
			}
			gate := strings.Index(out, tt.wantAfter)
			if gate < 0 || at < gate {
				t.Errorf("want the producer's warning after the gate's %q line:\n%s", tt.wantAfter, out)
			}
		})
	}
}

// The pluginRoot line follows the same rule as the producer's warnings.
func TestPack_CheckClean_HoldsPluginRootLine(t *testing.T) {
	chdirTemp(t)
	writePackApmYML(t, packWarningsPluginRootYML)
	writePackFixtureFile(t, "other/pkg-a/apm.yml", "name: pkg-a\ndescription: A local package\nversion: 1.0.0\n")

	if out, _, _ := runPackLikeMain(t, nil); !strings.Contains(out, pluginRootLineText) {
		t.Fatalf("a plain run must print the pluginRoot line:\n%s", out)
	}
	if out, _, _ := runPackLikeMain(t, []string{"--check-clean"}); strings.Contains(out, pluginRootLineText) {
		t.Errorf("--check-clean printed the pluginRoot line:\n%s", out)
	}
}
