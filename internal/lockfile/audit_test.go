package lockfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVerifyDeployedState(t *testing.T) {
	dir := t.TempDir()
	// good file: hash matches sha256("test")
	os.WriteFile(filepath.Join(dir, "ok.txt"), []byte("test"), 0644)
	// tampered file: recorded hash is for "test" but content differs
	os.WriteFile(filepath.Join(dir, "bad.txt"), []byte("tampered"), 0644)
	const testHash = "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"

	lock := &Lockfile{
		Dependencies: []LockedDep{{
			RepoURL:        "github.com/demo/pkg",
			DeployedHashes: map[string]string{"ok.txt": testHash, "bad.txt": testHash},
		}},
		LocalDeployedHashes: map[string]string{"missing.txt": testHash},
	}

	viol := VerifyDeployedState(lock, dir)
	if len(viol) != 2 {
		t.Fatalf("expected 2 violations (tampered + missing), got %d: %+v", len(viol), viol)
	}
	paths := map[string]bool{}
	for _, v := range viol {
		paths[v.Path] = true
	}
	if !paths["bad.txt"] || !paths["missing.txt"] {
		t.Errorf("expected bad.txt and missing.txt violations, got %v", paths)
	}
	if paths["ok.txt"] {
		t.Errorf("ok.txt should not be a violation")
	}
}
