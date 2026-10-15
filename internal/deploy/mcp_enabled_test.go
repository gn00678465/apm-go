package deploy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/apm-go/apm/internal/manifest"
	"go.yaml.in/yaml/v4"
)

// The wanted values are what Oracle 0.33.0 writes to opencode.json for the
// same apm.yml entry (issue #68).
func TestWriteMCP_Opencode_Enabled(t *testing.T) {
	entries := map[string]string{
		"http":  "name: example\nregistry: false\ntransport: http\nurl: https://example.com/mcp\n",
		"stdio": "name: example\nregistry: false\ntransport: stdio\ncommand: my-server\n",
	}
	cases := []struct {
		line string
		want string
	}{
		{"enabled: false", "false"},
		{"", "true"},
		{"enabled: null", "null"},
		{`enabled: "false"`, `"false"`},
	}
	for kind, entry := range entries {
		for _, c := range cases {
			t.Run(kind+"/"+c.line, func(t *testing.T) {
				dir := t.TempDir()
				dep := parseMCPEntryText(t, entry+c.line+"\n")
				if _, _, diags, err := (&opencodeAdapter{}).WriteMCP([]Primitive{mcpPrim("local", dep)}, dir); err != nil {
					t.Fatalf("WriteMCP: %v (diags=%v)", err, diags)
				}
				if got := rawEnabled(t, filepath.Join(dir, "opencode.json"), "mcp", "example"); got != c.want {
					t.Errorf("enabled = %s, want %s", got, c.want)
				}
			})
		}
	}
}

func TestWriteMCP_Opencode_RegistryServerKeepsEnabled(t *testing.T) {
	srv := mockMCPRegistryServer(t, "io.example/demo", []map[string]any{
		{"type": "streamable-http", "url": "https://example.com/mcp"},
	})
	dep := parseMCPEntryText(t, "name: io.example/demo\nregistry: "+srv.URL+"\nenabled: false\n")
	prims, diags := collectMCPPrimitives([]*manifest.MCPDependency{dep}, "local", "")
	if len(prims) != 1 {
		t.Fatalf("prims = %+v, diags = %v", prims, diags)
	}
	dir := t.TempDir()
	if _, _, diags, err := (&opencodeAdapter{}).WriteMCP(prims, dir); err != nil {
		t.Fatalf("WriteMCP: %v (diags=%v)", err, diags)
	}
	if got := rawEnabled(t, filepath.Join(dir, "opencode.json"), "mcp", "io.example/demo"); got != "false" {
		t.Errorf("enabled = %s, want false", got)
	}
}

func TestWriteMCP_Claude_LeavesOutEnabled(t *testing.T) {
	dir := t.TempDir()
	dep := parseMCPEntryText(t, "name: example\nregistry: false\ntransport: http\nurl: https://example.com/mcp\nenabled: false\n")
	if _, _, diags, err := (&claudeAdapter{}).WriteMCP([]Primitive{mcpPrim("local", dep)}, dir); err != nil {
		t.Fatalf("WriteMCP: %v (diags=%v)", err, diags)
	}
	if got := rawEnabled(t, filepath.Join(dir, ".mcp.json"), "mcpServers", "example"); got != "" {
		t.Errorf(".mcp.json has enabled = %s, want no enabled key", got)
	}
}

func parseMCPEntryText(t *testing.T, text string) *manifest.MCPDependency {
	t.Helper()
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(text), &doc); err != nil {
		t.Fatal(err)
	}
	dep, err := manifest.ParseMCPEntry(doc.Content[0])
	if err != nil {
		t.Fatal(err)
	}
	return dep
}

// rawEnabled returns the JSON text of the server's enabled value, "" when the
// entry has none.
func rawEnabled(t *testing.T, path, topKey, server string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]map[string]map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatal(err)
	}
	entry, ok := root[topKey][server]
	if !ok {
		t.Fatalf("%s has no %s.%s: %s", path, topKey, server, data)
	}
	return string(entry["enabled"])
}
