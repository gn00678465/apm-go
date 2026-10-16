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
