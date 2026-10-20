package deploy

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

// junction makes an NTFS junction at link pointing at target, or skips the
// test where mklink cannot make one. A junction needs no privilege.
func junction(t *testing.T, link, target string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
		t.Skipf("mklink /J %s %s: %v: %s", link, target, err, out)
	}
}

// A junctioned .claude/rules gets the refusal TestCheckDeployWrite expects
// for a symlinked one, and nothing outside changes. Issue #116.
func TestCheckDeployWrite_JunctionParentLinksOutside(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{"home/.claude", "outside/rules"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeDeployFile(t, filepath.Join(root, "outside/rules/r.md"), "user rule")
	junction(t, filepath.Join(root, "home/.claude/rules"), filepath.Join(root, "outside/rules"))
	before := deployTree(t, filepath.Join(root, "outside"))

	err = CheckDeployWrite(filepath.Join(root, "home"), ".claude/rules/r.md")

	want := &DeployPathError{Path: ".claude/rules/r.md", Link: ".claude/rules", Resolved: filepath.Join(root, "outside/rules"), Root: filepath.Join(root, "home/.claude")}
	if !reflect.DeepEqual(err, want) {
		t.Errorf("CheckDeployWrite = %v, want %v", err, want)
	}
	if got := deployTree(t, filepath.Join(root, "outside")); !reflect.DeepEqual(got, before) {
		t.Errorf("outside changed: %v, want %v", got, before)
	}
}

// A junctioned .codex gets the refusal TestRemoveStaleMCPServers_SymlinkIsRefused
// expects for a symlinked one, and the file it leads to stays. Issue #116.
func TestRemoveStaleMCPServers_JunctionIsRefused(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	const content = "[mcp_servers.x]\ncommand = \"npx\"\n"
	target := writeStaleFixture(t, outside, "config.toml", content)
	junction(t, filepath.Join(root, ".codex"), outside)

	_, err := RemoveStaleMCPServers(root, "codex", []string{"x"}, false)
	want := "Refusing to clean symlinked MCP config: Codex CLI config (" + filepath.Join(root, ".codex", "config.toml") +
		"). Replace the symlink with a regular file or directory, then retry."
	if err == nil || err.Error() != want {
		t.Errorf("err = %v, want %q", err, want)
	}
	if data, _ := os.ReadFile(target); string(data) != content {
		t.Errorf("link target changed: %s", data)
	}
}
