package deploy

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeStaleFixture(t *testing.T, root, rel, content string) string {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRemoveStaleMCPServers_RemovesOnlyTheNamesAndReturnsThemSorted(t *testing.T) {
	root := t.TempDir()
	path := writeStaleFixture(t, root, "opencode.json", `{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "a": {"type": "local"},
    "b": {"type": "local"},
    "mine": {"type": "local"}
  }
}`)
	removed, err := RemoveStaleMCPServers(root, "opencode", []string{"b", "a", "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(removed, []string{"a", "b"}) {
		t.Errorf("removed = %v", removed)
	}
	data, _ := os.ReadFile(path)
	want := `{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "mine": {
      "type": "local"
    }
  }
}`
	if string(data) != want {
		t.Errorf("opencode.json:\n%s", data)
	}
}

func TestRemoveStaleMCPServers_NothingToRemoveLeavesTheFile(t *testing.T) {
	root := t.TempDir()
	const content = "{\"mcpServers\":{\"mine\":{}}}"
	path := writeStaleFixture(t, root, ".mcp.json", content)
	removed, err := RemoveStaleMCPServers(root, "claude", []string{"gone"})
	if err != nil || removed != nil {
		t.Fatalf("removed = %v, err = %v", removed, err)
	}
	if data, _ := os.ReadFile(path); string(data) != content {
		t.Errorf(".mcp.json rewritten:\n%s", data)
	}

	removed, err = RemoveStaleMCPServers(root, "codex", []string{"gone"})
	if err != nil || removed != nil {
		t.Fatalf("missing file: removed = %v, err = %v", removed, err)
	}
	if _, err := os.Stat(filepath.Join(root, ".codex")); !os.IsNotExist(err) {
		t.Errorf("a missing config was created: %v", err)
	}
}

func TestRemoveStaleMCPServers_TargetWithoutMCPConfig(t *testing.T) {
	removed, err := RemoveStaleMCPServers(t.TempDir(), "cursor", []string{"x"})
	if err != nil || removed != nil {
		t.Errorf("removed = %v, err = %v", removed, err)
	}
	if MCPConfigLabel("cursor") != "" || MCPConfigLabel("agent-skills") != "" {
		t.Error("a target with no MCP writer has a label")
	}
}

func TestRemoveStaleMCPServers_Labels(t *testing.T) {
	want := map[string]string{
		"claude":      ".mcp.json",
		"codex":       "Codex CLI config",
		"copilot":     "Copilot CLI config",
		"antigravity": "Antigravity MCP config",
		"opencode":    "opencode.json",
	}
	for target, label := range want {
		if got := MCPConfigLabel(target); got != label {
			t.Errorf("MCPConfigLabel(%q) = %q, want %q", target, got, label)
		}
	}
}

func TestRemoveStaleMCPServers_InvalidFileIsAnError(t *testing.T) {
	root := t.TempDir()
	path := writeStaleFixture(t, root, ".github/mcp-config.json", "{not json")
	_, err := RemoveStaleMCPServers(root, "copilot", []string{"x"})
	if err == nil || !strings.HasPrefix(err.Error(), "MCP cleanup failed for Copilot CLI config: existing ") ||
		!strings.HasSuffix(err.Error(), ". Check the config path and permissions, then retry.") {
		t.Errorf("err = %v", err)
	}
	if data, _ := os.ReadFile(path); string(data) != "{not json" {
		t.Errorf("file changed: %s", data)
	}
}

func TestRemoveStaleMCPServers_SymlinkIsRefused(t *testing.T) {
	for _, tc := range []struct {
		name string
		link func(t *testing.T, root, outside string)
	}{
		{"file", func(t *testing.T, root, outside string) {
			if err := os.MkdirAll(filepath.Join(root, ".codex"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join(outside, "config.toml"), filepath.Join(root, ".codex", "config.toml")); err != nil {
				t.Fatal(err)
			}
		}},
		{"parent directory", func(t *testing.T, root, outside string) {
			if err := os.Symlink(outside, filepath.Join(root, ".codex")); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, outside := t.TempDir(), t.TempDir()
			const content = "[mcp_servers.x]\ncommand = \"npx\"\n"
			target := writeStaleFixture(t, outside, "config.toml", content)
			tc.link(t, root, outside)

			_, err := RemoveStaleMCPServers(root, "codex", []string{"x"})
			want := "Refusing to clean symlinked MCP config: Codex CLI config (" + filepath.Join(root, ".codex", "config.toml") +
				"). Replace the symlink with a regular file or directory, then retry."
			if err == nil || err.Error() != want {
				t.Errorf("err = %v, want %q", err, want)
			}
			if data, _ := os.ReadFile(target); string(data) != content {
				t.Errorf("link target changed: %s", data)
			}
		})
	}
}
