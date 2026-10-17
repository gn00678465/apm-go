package deploy

import (
	"os"
	"path/filepath"
	"testing"
)

// A skill whose directories hold a symlink to their parent is equal to
// another of the same files: once the path holds 40 symlinks stat fails with
// ELOOP, which dircmp files under common_funny. Oracle 0.33.0's
// is_skill_dir_identical_to_source returns True for these two directories
// (review122-step27/loopprobe).
func TestSkillDirsEqual_SymlinkLoop(t *testing.T) {
	root := t.TempDir()
	for _, side := range []string{"a", "b"} {
		if err := os.MkdirAll(filepath.Join(root, side, "sub"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, side, "f"), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("..", filepath.Join(root, side, "sub", "loop")); err != nil {
			t.Skipf("symlink: %v", err)
		}
	}
	if !skillDirsEqual(filepath.Join(root, "a"), filepath.Join(root, "b")) {
		t.Error("skillDirsEqual = false, want true")
	}
}
