package deploy

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

// writeMCPFixture writes a minimal existing config file for one target,
// pre-populated with a "keep" and a "remove" server plus a foreign top-level
// key -- the shared fixture every 7a test starts from.
func writeMCPFixtureJSON(t *testing.T, path, topKey string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	content := `{"` + topKey + `":{"keep":{"type":"stdio","command":"kept"},"remove":{"type":"stdio","command":"gone"}},"otherTopLevelKey":"survives"}`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func writeMCPFixtureTOML(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	content := "[mcp_servers.keep]\ncommand = \"kept\"\n\n[mcp_servers.remove]\ncommand = \"gone\"\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestRemoveMCPServersFromTargets_RemovesAcrossAllFiveTargets_PreservesOtherServerAndForeignKeys(t *testing.T) {
	dir := t.TempDir()
	writeMCPFixtureJSON(t, filepath.Join(dir, ".mcp.json"), "mcpServers")
	writeMCPFixtureTOML(t, filepath.Join(dir, ".codex", "config.toml"))
	writeMCPFixtureJSON(t, filepath.Join(dir, ".github", "mcp-config.json"), "mcpServers")
	writeMCPFixtureJSON(t, filepath.Join(dir, ".agents", "mcp_config.json"), "mcpServers")
	writeMCPFixtureJSON(t, filepath.Join(dir, "opencode.json"), "mcp")

	diags := RemoveMCPServersFromTargets(dir, []string{"remove"})
	if len(diags) != 0 {
		t.Fatalf("unexpected diags: %v", diags)
	}

	jsonCases := []struct {
		relPath, topKey string
	}{
		{".mcp.json", "mcpServers"},
		{filepath.Join(".github", "mcp-config.json"), "mcpServers"},
		{filepath.Join(".agents", "mcp_config.json"), "mcpServers"},
		{"opencode.json", "mcp"},
	}
	for _, c := range jsonCases {
		root := readJSON(t, filepath.Join(dir, c.relPath))
		servers, ok := root[c.topKey].(map[string]any)
		if !ok {
			t.Fatalf("%s: %s missing or not a map: %v", c.relPath, c.topKey, root)
		}
		if _, ok := servers["remove"]; ok {
			t.Errorf("%s: expected 'remove' server gone, got %v", c.relPath, servers)
		}
		if _, ok := servers["keep"]; !ok {
			t.Errorf("%s: expected 'keep' server to survive, got %v", c.relPath, servers)
		}
		if root["otherTopLevelKey"] != "survives" {
			t.Errorf("%s: foreign top-level key lost: %v", c.relPath, root)
		}
	}

	tomlData, err := os.ReadFile(filepath.Join(dir, ".codex", "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	var tomlRoot map[string]any
	if err := toml.Unmarshal(tomlData, &tomlRoot); err != nil {
		t.Fatalf("invalid TOML after removal: %v\n%s", err, tomlData)
	}
	servers, ok := tomlRoot["mcp_servers"].(map[string]any)
	if !ok {
		t.Fatalf("mcp_servers missing or not a map: %v", tomlRoot)
	}
	if _, ok := servers["remove"]; ok {
		t.Errorf("codex: expected 'remove' server gone, got %v", servers)
	}
	if _, ok := servers["keep"]; !ok {
		t.Errorf("codex: expected 'keep' server to survive, got %v", servers)
	}
}

func TestRemoveMCPServersFromTargets_NoExistingFiles_CreatesNothing(t *testing.T) {
	dir := t.TempDir()

	diags := RemoveMCPServersFromTargets(dir, []string{"anything"})
	if len(diags) != 0 {
		t.Fatalf("unexpected diags: %v", diags)
	}

	for _, rel := range []string{".mcp.json", filepath.Join(".codex", "config.toml"), filepath.Join(".github", "mcp-config.json"), filepath.Join(".agents", "mcp_config.json"), "opencode.json"} {
		if _, err := os.Stat(filepath.Join(dir, rel)); !os.IsNotExist(err) {
			t.Errorf("%s should not have been created", rel)
		}
	}
}

// TestRemoveMCPServersFromTargets_TopKeyAbsent_LeavesFileByteExact covers the
// empty-behavior guard (design.md N7 §7a): a target file that exists but has
// none of serverNames under its topKey must be left completely untouched --
// not even rewritten with an equivalent empty map -- mirroring install's own
// "nothing to write, don't touch the file" convention.
func TestRemoveMCPServersFromTargets_TopKeyAbsent_LeavesFileByteExact(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".mcp.json")
	original := `{"mcpServers":{"unrelated":{"type":"stdio","command":"x"}}}`
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	diags := RemoveMCPServersFromTargets(dir, []string{"not-present"})
	if len(diags) != 0 {
		t.Fatalf("unexpected diags: %v", diags)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != original {
		t.Errorf("file should be byte-exact unchanged, got:\n%s", after)
	}
}

func TestRemoveMCPServersFromTargets_MalformedExistingFile_DiagAndSkip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".mcp.json")
	if err := os.WriteFile(path, []byte("{not valid json"), 0644); err != nil {
		t.Fatal(err)
	}
	// A second, valid target file so we can prove the malformed one doesn't
	// abort removal for everything else.
	writeMCPFixtureJSON(t, filepath.Join(dir, "opencode.json"), "mcp")

	diags := RemoveMCPServersFromTargets(dir, []string{"remove"})
	if len(diags) == 0 {
		t.Fatal("expected a diagnostic for the malformed .mcp.json")
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != "{not valid json" {
		t.Errorf("malformed file must be left untouched, got: %s", after)
	}

	root := readJSON(t, filepath.Join(dir, "opencode.json"))
	servers := root["mcp"].(map[string]any)
	if _, ok := servers["remove"]; ok {
		t.Errorf("opencode.json removal should still have happened despite claude's malformed file: %v", servers)
	}
}

func TestRemoveMCPServersFromTargets_EmptyServerNames_NoOp(t *testing.T) {
	dir := t.TempDir()
	writeMCPFixtureJSON(t, filepath.Join(dir, ".mcp.json"), "mcpServers")

	diags := RemoveMCPServersFromTargets(dir, nil)
	if len(diags) != 0 {
		t.Fatalf("unexpected diags: %v", diags)
	}

	root := readJSON(t, filepath.Join(dir, ".mcp.json"))
	servers := root["mcpServers"].(map[string]any)
	if _, ok := servers["keep"]; !ok {
		t.Error("expected keep to survive a no-op call")
	}
	if _, ok := servers["remove"]; !ok {
		t.Error("expected remove to survive a no-op call (empty serverNames)")
	}
}
