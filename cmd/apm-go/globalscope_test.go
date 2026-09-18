package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnterGlobalScope_ChdirsAndReturnsDeployDir(t *testing.T) {
	chdirTemp(t)
	gDir := t.TempDir()
	dDir := t.TempDir()

	old := globalDirOverride
	oldDeploy := globalDeployDirOverride
	globalDirOverride = gDir
	globalDeployDirOverride = dDir
	t.Cleanup(func() {
		globalDirOverride = old
		globalDeployDirOverride = oldDeploy
	})

	deployDir, err := enterGlobalScope()
	if err != nil {
		t.Fatalf("enterGlobalScope: %v", err)
	}

	cwd, _ := os.Getwd()
	if cwd != gDir {
		t.Errorf("cwd = %q, want %q", cwd, gDir)
	}
	if deployDir != dDir {
		t.Errorf("deployDir = %q, want %q", deployDir, dDir)
	}
}

func TestEnterGlobalScope_CreatesDir(t *testing.T) {
	chdirTemp(t)
	base := t.TempDir()
	target := filepath.Join(base, "new-subdir", "nested")

	old := globalDirOverride
	oldDeploy := globalDeployDirOverride
	globalDirOverride = target
	globalDeployDirOverride = base
	t.Cleanup(func() {
		globalDirOverride = old
		globalDeployDirOverride = oldDeploy
	})

	if _, err := enterGlobalScope(); err != nil {
		t.Fatalf("enterGlobalScope: %v", err)
	}

	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("target dir not created: %v", err)
	}
	if !info.IsDir() {
		t.Error("target is not a directory")
	}
}

func TestEnsureGlobalManifest_CreatesMinimalFile(t *testing.T) {
	chdirTemp(t)

	if err := ensureGlobalManifest(); err != nil {
		t.Fatalf("ensureGlobalManifest: %v", err)
	}

	data, err := os.ReadFile("apm.yml")
	if err != nil {
		t.Fatalf("apm.yml not created: %v", err)
	}
	want := "name: global\nversion: 0.0.0\n"
	if string(data) != want {
		t.Errorf("apm.yml content = %q, want %q", data, want)
	}
}

func TestEnsureGlobalManifest_SkipsExisting(t *testing.T) {
	chdirTemp(t)

	custom := "name: my-project\nversion: 1.0.0\n"
	if err := os.WriteFile("apm.yml", []byte(custom), 0644); err != nil {
		t.Fatal(err)
	}

	if err := ensureGlobalManifest(); err != nil {
		t.Fatalf("ensureGlobalManifest: %v", err)
	}

	data, err := os.ReadFile("apm.yml")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != custom {
		t.Errorf("apm.yml content changed to %q, want %q", data, custom)
	}
}
