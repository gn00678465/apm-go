package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/gitops"
)

// Issue #154: update --global. The expected values are those of Oracle
// 0.33.0's `update -g` (issue154 runs r1-or and r6-or).

// updateGlobalScope points ~/.apm at <home>/.apm and the deploy root at
// <home>, and returns home and the project directory the test starts in.
func updateGlobalScope(t *testing.T) (home, project string) {
	t.Helper()
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	project = chdirTemp(t)
	home = t.TempDir()
	oldDir, oldDeploy := globalDirOverride, globalDeployDirOverride
	globalDirOverride, globalDeployDirOverride = filepath.Join(home, ".apm"), home
	t.Cleanup(func() { globalDirOverride, globalDeployDirOverride = oldDir, oldDeploy })
	return home, project
}

// updateGlobalPkg writes a local package with one agent and returns its path.
func updateGlobalPkg(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	writeGlobalStaleFile(t, filepath.Join(dir, "apm.yml"), "name: "+name+"\nversion: 1.0.0\n")
	writeGlobalStaleFile(t, filepath.Join(dir, ".apm", "agents", "ag-"+name+".agent.md"), "---\ndescription: a\n---\nagent "+name+"\n")
	return dir
}

// runUpdateGlobalCmd runs `update <args>` as a user types it and returns its
// stdout and exit code.
func runUpdateGlobalCmd(t *testing.T, args ...string) (string, int) {
	t.Helper()
	var err error
	out := captureStdout(t, func() {
		cmd := updateCmd()
		cmd.SilenceErrors, cmd.SilenceUsage = true, true
		cmd.SetArgs(args)
		err = cmd.Execute()
	})
	if err == nil {
		return out, 0
	}
	return out, exitCodeOf(err)
}

func writeGlobalManifest(t *testing.T, home, body string) {
	t.Helper()
	writeGlobalStaleFile(t, filepath.Join(home, ".apm", "apm.yml"), "name: g\nversion: 1.0.0\n"+body)
}

// listTree returns every path below root, slash-separated.
func listTree(t *testing.T, root string) []string {
	t.Helper()
	var paths []string
	err := filepath.Walk(root, func(p string, _ os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if rel, _ := filepath.Rel(root, p); rel != "." {
			paths = append(paths, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return paths
}

func TestUpdateGlobal_FlagWired(t *testing.T) {
	f := updateCmd().Flags().ShorthandLookup("g")
	if f == nil || f.Name != "global" {
		t.Fatalf("update -g = %v, want the --global flag", f)
	}
}

func TestUpdateGlobal_NoManifest(t *testing.T) {
	home, project := updateGlobalScope(t)
	out, code := runUpdateGlobalCmd(t, "-g")
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if want := "x No apm.yml found in ~/.apm/. Run 'apm-go install -g <org/repo>' to create one."; strings.TrimSpace(out) != want {
		t.Errorf("stdout = %q, want %q", out, want)
	}
	// Oracle 0.33.0 leaves an empty ~/.apm and nothing else (r1-or/n1).
	if got := listTree(t, home); len(got) != 1 || got[0] != ".apm" {
		t.Errorf("home holds %v, want only .apm", got)
	}
	if got := listTree(t, project); len(got) != 0 {
		t.Errorf("project holds %v, want nothing", got)
	}
}

func TestUpdateGlobal_HandWrittenManifestNoLock(t *testing.T) {
	home, project := updateGlobalScope(t)
	projectYML := "name: proj\nversion: 1.0.0\n"
	writeGlobalStaleFile(t, filepath.Join(project, "apm.yml"), projectYML)
	dep := updateGlobalPkg(t, "dep")
	writeGlobalManifest(t, home, "targets: [claude]\ndependencies:\n  apm:\n    - "+dep+"\n")

	if out, code := runUpdateGlobalCmd(t, "-g"); code != 0 {
		t.Fatalf("exit code = %d\n%s", code, out)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "agents", "ag-dep.md")); err != nil {
		t.Errorf("agent not deployed below the home directory: %v", err)
	}
	assertGlobalStaleLock(t, home, true, ".claude/agents/ag-dep.md")
	if _, err := os.Lstat(filepath.Join(home, ".apm", ".gitignore")); !os.IsNotExist(err) {
		t.Errorf("~/.apm/.gitignore exists (err %v), the user scope keeps none", err)
	}
	if got := listTree(t, project); len(got) != 1 || got[0] != "apm.yml" {
		t.Errorf("project holds %v, want apm.yml alone", got)
	}
	if got := globalStaleRead(t, filepath.Join(project, "apm.yml")); got != projectYML {
		t.Errorf("project apm.yml changed:\n%s", got)
	}
}

func TestUpdateGlobal_RemovedPackageIsCleaned(t *testing.T) {
	home, _ := updateGlobalScope(t)
	dep, dep2 := updateGlobalPkg(t, "dep"), updateGlobalPkg(t, "dep2")
	globalStaleInstall(t, "claude", dep)
	globalStaleInstall(t, "claude", dep2)
	if _, err := os.Lstat(filepath.Join(home, ".claude", "agents", "ag-dep2.md")); err != nil {
		t.Fatalf("install -g did not deploy dep2: %v", err)
	}
	ymlPath := filepath.Join(home, ".apm", "apm.yml")
	var kept []string
	for _, line := range strings.SplitAfter(globalStaleRead(t, ymlPath), "\n") {
		if !strings.Contains(line, dep2) {
			kept = append(kept, line)
		}
	}
	writeGlobalStaleFile(t, ymlPath, strings.Join(kept, ""))

	if out, code := runUpdateGlobalCmd(t, "-g"); code != 0 {
		t.Fatalf("exit code = %d\n%s", code, out)
	}
	if _, err := os.Lstat(filepath.Join(home, ".claude", "agents", "ag-dep2.md")); !os.IsNotExist(err) {
		t.Errorf("dep2's agent is still there (err %v)", err)
	}
	if _, err := os.Lstat(filepath.Join(home, ".claude", "agents", "ag-dep.md")); err != nil {
		t.Errorf("dep's agent is gone: %v", err)
	}
	assertGlobalStaleLock(t, home, false, "dep2")
	assertGlobalStaleLock(t, home, true, ".claude/agents/ag-dep.md")
}

const updateGlobalMCP = "  mcp:\n    - name: srv\n      registry: false\n      transport: stdio\n      command: echo\n      args: [hi]\n"

func TestUpdateGlobal_NoTargetTakesMCP(t *testing.T) {
	home, _ := updateGlobalScope(t)
	dep := updateGlobalPkg(t, "dep")
	writeGlobalManifest(t, home, "targets: [opencode]\ndependencies:\n  apm:\n    - "+dep+"\n"+updateGlobalMCP)

	// Oracle 0.33.0 writes the lock and exits 1, also on a second run that
	// changes nothing (r6-or/o).
	for run := 1; run <= 2; run++ {
		out, code := runUpdateGlobalCmd(t, "-g")
		if code != 1 {
			t.Errorf("run %d: exit code = %d, want 1\n%s", run, code, out)
		}
		for _, want := range []string{
			"+- MCP Servers (1)",
			"Skipped workspace-only runtimes at user scope: opencode -- omit --global to install these",
			"No runtimes support user-scope MCP installation (supported: Copilot CLI, Claude Code, Codex CLI, Gemini CLI, Antigravity CLI, Hermes, Kiro, Windsurf, JetBrains Copilot)",
			" x MCP dependencies are declared, but no effective target can accept their configuration. Choose a supported --target and retry.",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("run %d: stdout lacks %q\n%s", run, want, out)
			}
		}
		for _, unwanted := range []string{"Already up to date", "Install failed after"} {
			if strings.Contains(out, unwanted) {
				t.Errorf("run %d: stdout has %q\n%s", run, unwanted, out)
			}
		}
		assertGlobalStaleLock(t, home, true, ".config/opencode/agents/ag-dep.md")
	}
	if _, err := os.Lstat(filepath.Join(home, ".apm", ".gitignore")); !os.IsNotExist(err) {
		t.Errorf("~/.apm/.gitignore exists (err %v)", err)
	}
}

func TestUpdateGlobal_SkippedTargetWarns(t *testing.T) {
	home, _ := updateGlobalScope(t)
	dep := updateGlobalPkg(t, "dep")
	writeGlobalManifest(t, home, "targets: [opencode, claude]\ndependencies:\n  apm:\n    - "+dep+"\n"+updateGlobalMCP)

	out, code := runUpdateGlobalCmd(t, "-g")
	if code != 0 {
		t.Fatalf("exit code = %d\n%s", code, out)
	}
	if want := "Skipped workspace-only runtimes at user scope: opencode -- omit --global to install these"; !strings.Contains(out, want) {
		t.Errorf("stdout lacks %q\n%s", want, out)
	}
	if mcp := globalStaleRead(t, filepath.Join(home, ".mcp.json")); !strings.Contains(mcp, `"srv"`) {
		t.Errorf("claude's MCP config lacks srv:\n%s", mcp)
	}
}

// TestUpdateGlobal_ProjectUpdateWritesGitignore keeps the project update as
// it was: it still writes .gitignore.
func TestUpdateGlobal_ProjectUpdateWritesGitignore(t *testing.T) {
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	project := chdirTemp(t)
	dep := updateGlobalPkg(t, "dep")
	writeGlobalStaleFile(t, filepath.Join(project, "apm.yml"), "name: proj\nversion: 1.0.0\ntargets: [claude]\ndependencies:\n  apm:\n    - "+dep+"\n")
	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &gitops.RealPackageLoader{ModulesDir: "apm_modules"}}
	captureStdout(t, func() {
		if err := runUpdate(deps, false, true, "", false, ""); err != nil {
			t.Fatalf("update: %v", err)
		}
	})
	if got := globalStaleRead(t, filepath.Join(project, ".gitignore")); !strings.Contains(got, "apm_modules/") {
		t.Errorf(".gitignore = %q, want apm_modules/", got)
	}
}
