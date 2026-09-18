package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// globalDirOverride is the test seam: when non-empty, enterGlobalScope uses
// this path instead of ~/.apm/. Production code never sets it.
var globalDirOverride string

// globalDeployDirOverride is the test seam for the deploy root. When
// non-empty, enterGlobalScope returns this instead of HOME.
var globalDeployDirOverride string

// enterGlobalScope switches the working directory to ~/.apm/ and returns
// HOME as the deploy root. Package management files (apm.yml, lockfile,
// apm_modules) resolve under ~/.apm/ via cwd. Deploy targets (.claude/,
// .agents/) resolve under HOME so globally installed skills are visible
// to AI agents regardless of which project is active.
func enterGlobalScope() (deployDir string, err error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}

	dir := globalDirOverride
	if dir == "" {
		dir = filepath.Join(home, ".apm")
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("create %s: %w", dir, err)
	}
	if err := os.Chdir(dir); err != nil {
		return "", fmt.Errorf("enter %s: %w", dir, err)
	}

	deployDir = globalDeployDirOverride
	if deployDir == "" {
		deployDir = home
	}
	return deployDir, nil
}

// ensureGlobalManifest creates a minimal apm.yml under the current directory
// if one does not already exist, so that a first `install --global <pkg>`
// succeeds without requiring `init --global` first.
func ensureGlobalManifest() error {
	if _, err := os.Stat("apm.yml"); err == nil {
		return nil
	}
	return os.WriteFile("apm.yml", []byte("name: global\nversion: 0.0.0\n"), 0644)
}
