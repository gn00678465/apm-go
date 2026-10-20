package deploy

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// .config/opencode is a target root at user scope alone: in a project .config
// stays the target root, as before issue #125, so a .config/opencode linked
// out of .config is refused there and passes in HOME.
func TestCheckDeployPath_ConfigOpencodeRootAtUserScopeOnly(t *testing.T) {
	setup := func(t *testing.T) string {
		root := t.TempDir()
		outside := filepath.Join(t.TempDir(), "opencode")
		if err := os.MkdirAll(filepath.Join(outside, "agents"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(root, ".config"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(root, ".config", "opencode")); err != nil {
			t.Fatal(err)
		}
		return root
	}
	const rel = ".config/opencode/agents/a.md"
	t.Run("project", func(t *testing.T) {
		root := setup(t)
		t.Setenv("HOME", t.TempDir())
		var outside *DeployPathError
		if err := CheckDeployPath(root, rel); !errors.As(err, &outside) || outside.Link != ".config/opencode" {
			t.Errorf("CheckDeployPath = %v, want the symlink .config/opencode refused", err)
		}
		if !withinTargetRoot(root, filepath.Join(root, ".config")) {
			t.Error("a project's .config is its own target root, which an empty-parents cleanup may remove")
		}
	})
	t.Run("user scope", func(t *testing.T) {
		root := setup(t)
		t.Setenv("HOME", root)
		t.Setenv("USERPROFILE", root)
		if err := CheckDeployPath(root, rel); err != nil {
			t.Errorf("CheckDeployPath = %v, want nil", err)
		}
		if withinTargetRoot(root, filepath.Join(root, ".config")) {
			t.Error("~/.config is the user's and must not be removed")
		}
	})
}
