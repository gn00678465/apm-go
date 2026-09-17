package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// globalDirOverride is the test seam: when non-empty, enterGlobalScope uses
// this path instead of ~/.apm/. Production code never sets it.
var globalDirOverride string

// enterGlobalScope switches the working directory to ~/.apm/ so that every
// relative-path operation (apm.yml, apm_modules/, apm.lock.yaml, deploy)
// resolves under the user-scope root instead of the project-local cwd.
// The directory is created if absent (mode 0755, matching the Oracle's
// ensure_config_exists).
func enterGlobalScope() error {
	dir := globalDirOverride
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("cannot determine home directory: %w", err)
		}
		dir = filepath.Join(home, ".apm")
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	if err := os.Chdir(dir); err != nil {
		return fmt.Errorf("enter %s: %w", dir, err)
	}
	return nil
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
