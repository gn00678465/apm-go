package lockfile

import (
	"testing"

	"go.yaml.in/yaml/v4"
)

// Issue #68: a changed type of an MCP server's enabled is a changed config,
// and of no other field. false against 0 is the Oracle's 0 == False
// deliberately not followed.
func TestMCPConfigEqual(t *testing.T) {
	parse := func(text string) *yaml.Node {
		var doc yaml.Node
		if err := yaml.Unmarshal([]byte("name: s\n"+text+"\n"), &doc); err != nil {
			t.Fatal(err)
		}
		return doc.Content[0]
	}
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"enabled: false", "enabled: false", true},
		{"enabled: false", "enabled: 'false'", false},
		{"enabled: false", "enabled: 0", false},
		{"enabled: '0'", "enabled: 0", false},
		{"enabled: null", "enabled: 'null'", false},
		{"enabled: [1]", "enabled: ['1']", false},
		{"enabled: false", "", false},
		{"args: [-p, 8080]", "args: [-p, '8080']", true},
		{"env: {PORT: 8080}", "env: {PORT: '8080'}", true},
	} {
		if got := MCPConfigEqual(parse(c.a), parse(c.b)); got != c.want {
			t.Errorf("MCPConfigEqual(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}
