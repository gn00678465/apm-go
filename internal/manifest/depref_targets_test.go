package manifest

import (
	"slices"
	"testing"

	"go.yaml.in/yaml/v4"
)

// Issue #69: a dependency's `targets:`. The reason texts are what Oracle
// 0.33.0 printed for the same entry.

func parseDepEntry(t *testing.T, text string) (*DependencyReference, error) {
	t.Helper()
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(text), &doc); err != nil {
		t.Fatal(err)
	}
	return ParseDepDict(doc.Content[0], 0)
}

func TestParseDepDict_Targets_ReadForEachForm(t *testing.T) {
	for _, tc := range []struct {
		name, entry string
		want        []string
	}{
		{"path", "path: ./dep\ntargets: [claude]\n", []string{"claude"}},
		{"git", "git: https://127.0.0.1/o/a.git\ntargets: [copilot]\n", []string{"copilot"}},
		{"registry", "id: acme/pkg\nversion: 1.0.0\ntargets: [codex, claude]\n", []string{"claude", "codex"}},
		{"trimmed, lowercased, deduplicated", "path: ./dep\ntargets: [' Claude ', claude]\n", []string{"claude"}},
		{"null is no restriction", "path: ./dep\ntargets: null\n", nil},
		{"absent", "path: ./dep\n", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, err := parseDepEntry(t, tc.entry)
			if err != nil {
				t.Fatalf("ParseDepDict: %v", err)
			}
			if !slices.Equal(d.TargetSubset, tc.want) {
				t.Errorf("TargetSubset = %q, want %q", d.TargetSubset, tc.want)
			}
		})
	}
}

func TestParseDepDict_Targets_RejectedWithOracleReason(t *testing.T) {
	const valid = "Valid targets: agent-skills, antigravity, claude, codex, copilot, copilot-app, copilot-cowork, cursor, gemini, grok-build, grok-cloud, hermes, kiro, openclaw, opencode, windsurf."
	for _, tc := range []struct {
		name, entry, want string
	}{
		{"empty", "path: ./dep\ntargets: []\n",
			"dependency entry 0: targets: must contain at least one target; remove the field to route primitives to all active install targets."},
		{"scalar", "path: ./dep\ntargets: claude\n",
			"dependency entry 0: 'targets' field must be a list of target names"},
		{"integer", "path: ./dep\ntargets: [1]\n",
			"dependency entry 0: Each entry in 'targets' must be a non-empty string"},
		{"YAML 1.1 bool", "path: ./dep\ntargets: [yes]\n",
			"dependency entry 0: Each entry in 'targets' must be a non-empty string"},
		{"typo", "path: ./dep\ntargets: [claud]\n",
			"dependency entry 0: Unknown target 'claud'. " + valid + " Did you mean 'claude'?"},
		{"typo, lowercased", "path: ./dep\ntargets: [Coplot]\n",
			"dependency entry 0: Unknown target 'coplot'. " + valid + " Did you mean 'copilot'?"},
		{"alias", "path: ./dep\ntargets: [vscode]\n",
			"dependency entry 0: Unknown target 'vscode'. " + valid},
		{"all", "path: ./dep\ntargets: [all]\n",
			"dependency entry 0: Unknown target 'all'. " + valid},
		{"git: parent", "git: parent\npath: x\ntargets: [claude]\n",
			"dependency entry 0: Unsupported field(s) for parent git dependency: 'targets'"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseDepEntry(t, tc.entry)
			if err == nil || err.Error() != tc.want {
				t.Errorf("err = %v\nwant %s", err, tc.want)
			}
		})
	}
}
