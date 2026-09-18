package deploy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCleanupDanglingSymlinks_RemovesDangling(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, ".claude", "skills")
	if err := os.MkdirAll(parent, 0755); err != nil {
		t.Fatal(err)
	}

	link := filepath.Join(parent, "show-me")
	if err := os.Symlink(filepath.Join(root, "nonexistent-target"), link); err != nil {
		t.Fatal(err)
	}

	cleanupDanglingSymlinks(root, link)

	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Errorf("dangling symlink %s should have been removed", link)
	}
}

func TestCleanupDanglingSymlinks_PreservesValidSymlink(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, ".claude", "skills")
	if err := os.MkdirAll(parent, 0755); err != nil {
		t.Fatal(err)
	}

	target := filepath.Join(root, "real-target")
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatal(err)
	}

	link := filepath.Join(parent, "valid-skill")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	cleanupDanglingSymlinks(root, link)

	info, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("valid symlink %s should still exist: %v", link, err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("expected %s to remain a symlink", link)
	}
}

func TestCleanupDanglingSymlinks_PreservesRegularDir(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".claude", "skills", "keep-me")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}

	cleanupDanglingSymlinks(root, dir)

	if _, err := os.Stat(dir); err != nil {
		t.Errorf("regular directory with content should still exist: %v", err)
	}
}

func TestCleanupDanglingSymlinks_StopsAtProjectDir(t *testing.T) {
	root := t.TempDir()

	cleanupDanglingSymlinks(root, root)

	if _, err := os.Stat(root); err != nil {
		t.Errorf("projectDir itself must never be removed: %v", err)
	}
}
