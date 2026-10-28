package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMCPOnlyProjectWithNoTarget follows Oracle 0.33.0 (issue #123 runs
// c57_3 and c57_3dev): a project that declares MCP servers and has no
// target ends install, update and install --mcp with exit 2 and writes
// nothing. The text is apm-go's own no-target error.
func TestMCPOnlyProjectWithNoTarget(t *testing.T) {
	for _, section := range []string{"dependencies", "devDependencies"} {
		for _, args := range [][]string{{"install"}, {"update"}, {"install", "--mcp", "s2", "--", "echo", "x"}} {
			t.Run(section+"/"+strings.Join(args, " "), func(t *testing.T) {
				dir := t.TempDir()
				t.Chdir(dir)
				setTestHome(t, t.TempDir())
				manifest := "name: p\nversion: 1.0.0\n" + section + ":\n  mcp:\n  - name: s1\n    registry: false\n    transport: stdio\n    command: echo\n"
				if err := os.WriteFile("apm.yml", []byte(manifest), 0o644); err != nil {
					t.Fatal(err)
				}
				stdout, _, code := runCompileCLI(t, args...)
				if code != 2 {
					t.Fatalf("exit %d, want 2\n%s", code, stdout)
				}
				if !strings.Contains(stdout, "no deployment target detected") {
					t.Fatalf("stdout has no no-target error:\n%s", stdout)
				}
				entries, err := os.ReadDir(dir)
				if err != nil {
					t.Fatal(err)
				}
				if len(entries) != 1 || readText(t, filepath.Join(dir, "apm.yml")) != manifest {
					t.Fatalf("files changed: %v", entries)
				}
			})
		}
	}
}
