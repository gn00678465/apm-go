package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestInstall_RootPromptZeroTarget: with no dependency and no target, a
// *.prompt.md beside a project's .apm/ is content to integrate, so the
// install exits 2 as Oracle 0.33.0 does (No harness detected). A --global
// install does not deploy ~/.apm/*.prompt.md, so there it is nothing to
// integrate and the install exits 0 with nothing changed.
func TestInstall_RootPromptZeroTarget(t *testing.T) {
	t.Run("project", func(t *testing.T) {
		root := bootstrapProject(t, "proj")
		writeCompileFile(t, root, "proj/apm.yml", "name: p\nversion: 1.0.0\n")
		writeCompileFile(t, root, "proj/top.prompt.md", "---\ndescription: t\n---\nt\n")
		if err := os.Mkdir(filepath.Join(root, "proj", ".apm"), 0o755); err != nil {
			t.Fatal(err)
		}
		stdout, stderr, code := runCompileCLI(t, "install")
		if code != 2 || !strings.Contains(stdout, " x no deployment target detected") {
			t.Errorf("exit %d, stdout:\n%s\nstderr:\n%s\nwant exit 2 and the no-target error", code, stdout, stderr)
		}
		assertNoFile(t, filepath.Join(root, "proj", "apm.lock.yaml"))
		assertNoFile(t, filepath.Join(root, "proj", ".claude"))
	})
	t.Run("global", func(t *testing.T) {
		root := bootstrapProject(t, "proj")
		writeCompileFile(t, root, "home/.apm/apm.yml", "name: g\nversion: 1.0.0\n")
		writeCompileFile(t, root, "home/.apm/x.prompt.md", "---\ndescription: x\n---\nx\n")
		if err := os.Mkdir(filepath.Join(root, "home", ".apm", ".apm"), 0o755); err != nil {
			t.Fatal(err)
		}
		stdout, stderr, code := runCompileCLI(t, "install", "-g")
		// The scope line comes first (install/manifest_helpers.py:63-74).
		// The user scope deploys to copilot when nothing is detected at ~,
		// and integrates ~/.apm's own content, here nothing (measured,
		// issue125 impl1/gempty).
		if want := " i Installing to user scope (~/.apm/)\n > Installing dependencies from apm.yml...\n   + <project root> (local)\n  |-- (files unchanged)\n\n i No changes -- install state already up to date in <t>s.\n"; code != 0 || elapsed86.ReplaceAllString(stdout, "in <t>s.") != want || stderr != "" {
			t.Errorf("exit %d, stdout:\n%s\nstderr:\n%s\nwant exit 0, stdout:\n%s", code, stdout, stderr, want)
		}
		assertNoFile(t, filepath.Join(root, "home", ".apm", "apm.lock.yaml"))
	})
}
