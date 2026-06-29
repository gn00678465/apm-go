package lockfile

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestComputeTreeSHA256(t *testing.T) {
	dir := t.TempDir()

	// Create a fixture git repo with known content
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s\n%s", args, err, out)
		}
	}

	git("init")
	git("config", "user.name", "test")
	git("config", "user.email", "test@test.com")

	// Create files with known content
	os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello\n"), 0644)
	os.MkdirAll(filepath.Join(dir, "sub"), 0755)
	os.WriteFile(filepath.Join(dir, "sub", "world.txt"), []byte("world\n"), 0644)

	git("add", ".")
	git("commit", "-m", "initial")

	// Get the commit SHA
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = dir
	commitBytes, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	commit := string(bytes.TrimSpace(commitBytes))

	// Compute tree_sha256 via our function
	actual, err := ComputeTreeSHA256(dir, commit)
	if err != nil {
		t.Fatalf("ComputeTreeSHA256: %v", err)
	}

	// Independently compute expected hash (anti-tautology: NOT using our function)
	// hello.txt: mode 100644, content "hello\n"
	helloHash := sha256sum([]byte("hello\n"))
	// sub/world.txt: mode 100644, content "world\n"
	worldHash := sha256sum([]byte("world\n"))

	// sub/ tree: "100644 world.txt <worldHash>\n"
	subTreeContent := fmt.Sprintf("100644 world.txt %s\n", worldHash)
	subTreeHash := sha256sum([]byte(subTreeContent))

	// root tree: "040000 sub <subTreeHash>\n100644 hello.txt <helloHash>\n"
	// Sorted by name: hello.txt < sub
	rootTreeContent := fmt.Sprintf("100644 hello.txt %s\n040000 sub %s\n", helloHash, subTreeHash)
	rootHash := sha256sum([]byte(rootTreeContent))
	expected := HashEnvelope("sha256", rootHash)

	if actual != expected {
		t.Errorf("tree_sha256 mismatch:\n  actual:   %s\n  expected: %s", actual, expected)
	}
}

func TestVerifyTreeSHA256_Match(t *testing.T) {
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s\n%s", args, err, out)
		}
	}

	git("init")
	git("config", "user.name", "test")
	git("config", "user.email", "test@test.com")
	os.WriteFile(filepath.Join(dir, "file.txt"), []byte("content"), 0644)
	git("add", ".")
	git("commit", "-m", "init")

	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = dir
	commitBytes, _ := cmd.Output()
	commit := string(bytes.TrimSpace(commitBytes))

	hash, err := ComputeTreeSHA256(dir, commit)
	if err != nil {
		t.Fatal(err)
	}

	if err := VerifyTreeSHA256(hash, dir, commit); err != nil {
		t.Errorf("verify should pass: %v", err)
	}
}

func TestVerifyTreeSHA256_Mismatch(t *testing.T) {
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s\n%s", args, err, out)
		}
	}

	git("init")
	git("config", "user.name", "test")
	git("config", "user.email", "test@test.com")
	os.WriteFile(filepath.Join(dir, "file.txt"), []byte("content"), 0644)
	git("add", ".")
	git("commit", "-m", "init")

	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = dir
	commitBytes, _ := cmd.Output()
	commit := string(bytes.TrimSpace(commitBytes))

	fakeHash := "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	err := VerifyTreeSHA256(fakeHash, dir, commit)
	if err == nil {
		t.Fatal("expected integrity violation")
	}
}

func sha256sum(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}
