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

// S2: a non-sha256 envelope whose hex happens to equal the file's SHA-256 must
// still fail closed — the algorithm claim is unvalidated, so it is a violation.
func TestVerifyDeployedState_RejectsUnsupportedHashAlgorithm(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("test"), 0644)
	const sha256Hex = "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"

	lock := &Lockfile{
		Dependencies: []LockedDep{{
			RepoURL:        "github.com/demo/pkg",
			DeployedHashes: map[string]string{"f.txt": "sha512:" + sha256Hex},
		}},
	}
	viol := VerifyDeployedState(lock, dir)
	if len(viol) != 1 || viol[0].Path != "f.txt" {
		t.Fatalf("expected 1 violation for non-sha256 envelope, got %+v", viol)
	}
}

func TestVerifyDeployedState_ViolationOrderIsFixed(t *testing.T) {
	const testHash = "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"
	lock := &Lockfile{
		Dependencies: []LockedDep{
			{
				RepoURL:        "github.com/demo/second-in-name",
				DeployedHashes: map[string]string{"z/4.txt": testHash, "z/1.txt": testHash, "z/3.txt": testHash, "z/2.txt": testHash},
			},
			{
				RepoURL:        "github.com/demo/first-in-name",
				DeployedHashes: map[string]string{"a/3.txt": testHash, "a/1.txt": testHash, "a/2.txt": testHash},
			},
		},
		LocalDeployedHashes: map[string]string{"m/c.txt": testHash, "m/a.txt": testHash, "m/b.txt": testHash},
	}
	want := []string{
		"z/1.txt", "z/2.txt", "z/3.txt", "z/4.txt",
		"a/1.txt", "a/2.txt", "a/3.txt",
		"m/a.txt", "m/b.txt", "m/c.txt",
	}
	root := t.TempDir()

	for i := 0; i < 50; i++ {
		viol := VerifyDeployedState(lock, root)

		if len(viol) != len(want) {
			t.Fatalf("run %d: %d violations, want %d", i, len(viol), len(want))
		}
		for j, v := range viol {
			if v.Path != want[j] {
				t.Fatalf("run %d: violation %d is %s, want %s", i, j, v.Path, want[j])
			}
		}
	}
}
