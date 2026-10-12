package main

import (
	"strings"
	"testing"
)

// TestInstallAndUpdate_RuntimeErrorPrintedOnce: a runtime failure of install
// or update is one error line on stdout and nothing on stderr (issue #50
// ruling R15; cobra printed the usage text after it). A usage error still
// gets the usage text.
func TestInstallAndUpdate_RuntimeErrorPrintedOnce(t *testing.T) {
	tests := []struct {
		name     string
		manifest string
		args     []string
		wantLine string
	}{
		{
			name:     "install: local source missing",
			manifest: "name: p\nversion: 1.0.0\ntarget: claude\ndependencies:\n  apm:\n    - ./nope\n",
			args:     []string{"install"},
			wantLine: " x resolve: loading package _local/nope: local dependency source ",
		},
		{
			name:     "update: no lockfile",
			manifest: "name: p\nversion: 1.0.0\n",
			args:     []string{"update"},
			wantLine: " x apm-go update requires an existing apm.lock.yaml: ",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := chdirTemp(t)
			writeCompileFile(t, dir, "apm.yml", tt.manifest)

			stdout, stderr, exitCode := runCompileCLI(t, tt.args...)

			if exitCode != 1 {
				t.Errorf("exit code = %d, want 1", exitCode)
			}
			if got := strings.Count(stdout, " x "); got != 1 || !strings.Contains(stdout, tt.wantLine) {
				t.Errorf("stdout: %d error lines, want exactly one starting %q:\n%s", got, tt.wantLine, stdout)
			}
			if stderr != "" {
				t.Errorf("stderr = %q, want empty", stderr)
			}
		})
	}

	t.Run("install: unknown flag keeps the usage text", func(t *testing.T) {
		chdirTemp(t)
		_, stderr, exitCode := runCompileCLI(t, "install", "--no-such-flag")
		if exitCode == 0 || !strings.Contains(stderr, "Usage:") {
			t.Errorf("exit = %d, stderr = %q; want a usage error with the usage text", exitCode, stderr)
		}
	})
}
