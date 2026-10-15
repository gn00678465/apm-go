package manifest

import (
	"testing"

	"github.com/apm-go/apm/internal/yamlcore"
	"go.yaml.in/yaml/v4"
)

// The wanted bytes are the enabled line of mcp_configs in the lock Oracle
// 0.33.0 writes for the same apm.yml entry (issue #68).
func TestParseMCPEntry_Enabled(t *testing.T) {
	cases := []struct {
		yaml string
		want string
	}{
		{"enabled: false", "enabled: false\n"},
		{"enabled: False", "enabled: false\n"},
		{"enabled: no", "enabled: false\n"},
		{"enabled: off", "enabled: false\n"},
		{"enabled: YES", "enabled: true\n"},
		{"enabled: On", "enabled: true\n"},
		{"enabled: true", "enabled: true\n"},
		{`enabled: "no"`, "enabled: 'no'\n"},
		{"enabled: 'yes'", "enabled: 'yes'\n"},
		{`enabled: "false"`, "enabled: 'false'\n"},
		{"enabled: 0", "enabled: 0\n"},
		{"enabled: +5", "enabled: 5\n"},
		{"enabled: 1_000", "enabled: 1000\n"},
		{"enabled: null", "enabled: null\n"},
		{"enabled: ~", "enabled: null\n"},
		{"enabled:", "enabled: null\n"},
		{"enabled: [1, 2]", "enabled:\n- 1\n- 2\n"},
		{"enabled: {a: 1}", "enabled:\n  a: 1\n"},
	}
	for _, c := range cases {
		t.Run(c.yaml, func(t *testing.T) {
			m := parseMCPEntryText(t, "name: example\n"+c.yaml+"\n")
			if m.Enabled == nil {
				t.Fatal("Enabled = nil, want a value")
			}
			got, err := yamlcore.SafeDumpManifest(&yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
				{Kind: yaml.ScalarNode, Tag: "!!str", Value: "enabled"}, m.Enabled,
			}})
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != c.want {
				t.Errorf("lock line = %q, want %q", got, c.want)
			}
		})
	}
}

func TestParseMCPEntry_EnabledAbsent(t *testing.T) {
	if m := parseMCPEntryText(t, "name: example\n"); m.Enabled != nil {
		t.Errorf("Enabled = %#v, want nil", m.Enabled)
	}
}

func parseMCPEntryText(t *testing.T, text string) *MCPDependency {
	t.Helper()
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(text), &doc); err != nil {
		t.Fatal(err)
	}
	m, err := ParseMCPEntry(doc.Content[0])
	if err != nil {
		t.Fatal(err)
	}
	return m
}
