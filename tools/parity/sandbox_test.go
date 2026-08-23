//go:build unix

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewSandbox_MaterialisesFixtureIntoCwd(t *testing.T) {
	fixtureDir := t.TempDir()
	mustWriteFile(t, filepath.Join(fixtureDir, "apm.yml"), "name: demo\n")
	mustWriteFile(t, filepath.Join(fixtureDir, "sub", "nested.txt"), "nested content")
	mustWriteFile(t, filepath.Join(fixtureDir, "empty.txt"), "")

	sb, err := newSandbox(fixtureDir)
	if err != nil {
		t.Fatalf("newSandbox: %v", err)
	}
	defer sb.cleanup()

	assertFileContent(t, filepath.Join(sb.Cwd, "apm.yml"), "name: demo\n")
	assertFileContent(t, filepath.Join(sb.Cwd, "sub", "nested.txt"), "nested content")
	assertFileContent(t, filepath.Join(sb.Cwd, "empty.txt"), "")

	// Home must exist and start empty: no fixture materialises into it.
	entries, err := os.ReadDir(sb.Home)
	if err != nil {
		t.Fatalf("reading %s: %v", sb.Home, err)
	}
	if len(entries) != 0 {
		t.Errorf("%s expected empty, has %d entries", sb.Home, len(entries))
	}
}

func TestNewSandbox_NoFixtureLeavesCwdEmpty(t *testing.T) {
	sb, err := newSandbox("")
	if err != nil {
		t.Fatalf("newSandbox: %v", err)
	}
	defer sb.cleanup()

	entries, err := os.ReadDir(sb.Cwd)
	if err != nil {
		t.Fatalf("reading cwd: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("cwd expected empty, has %d entries", len(entries))
	}
}

func TestSandbox_CleanupRemovesEverything(t *testing.T) {
	sb, err := newSandbox("")
	if err != nil {
		t.Fatalf("newSandbox: %v", err)
	}
	root := sb.root
	sb.cleanup()

	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Errorf("sandbox root %s still exists after cleanup", root)
	}
}

// TestSandbox_NeverUsesRealHomeOrConfigDir is the direct proof ticket 01
// AC1 requires, re-verified after ticket 15 stopped forcing APM_CONFIG_DIR:
// a case that writes to $HOME/.apm/marker (following the client's own
// resolution) must land inside the sandbox, and the invoking user's real
// ~/.apm must stay untouched. HOME alone carries the isolation guarantee
// now; there is no separate sandbox APM_CONFIG_DIR to prove isolated.
func TestSandbox_NeverUsesRealHomeOrConfigDir(t *testing.T) {
	realHome, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("cannot determine real home dir: %v", err)
	}
	realMarker := filepath.Join(realHome, ".apm", "parity-test-marker")
	if _, err := os.Stat(realMarker); err == nil {
		t.Fatalf("pre-existing marker %s would make this test meaningless", realMarker)
	}

	scriptDir := t.TempDir()
	stub := writeStubScript(t, scriptDir, "stub.sh", `
mkdir -p "$HOME/.apm"
touch "$HOME/.apm/parity-test-marker"
exit 0
`)

	// Drive the sandbox directly (not via runCaseSide, which cleans up
	// before returning) so the marker can still be inspected afterward.
	sb, err := newSandbox("")
	if err != nil {
		t.Fatalf("newSandbox: %v", err)
	}
	defer sb.cleanup()

	env := buildEnv(nil, sb.Home, sb.LauncherCache)
	res := runProcess([]string{stub}, env, "", sb.Cwd, defaultTimeout)
	if res.ExitCode != 0 {
		t.Fatalf("stub exited %d, stderr=%q", res.ExitCode, res.Stderr)
	}

	// Proof 1: the marker went into the sandbox HOME, not the real one.
	sandboxMarker := filepath.Join(sb.Home, ".apm", "parity-test-marker")
	if _, err := os.Stat(sandboxMarker); err != nil {
		t.Errorf("expected marker at sandbox HOME %s: %v", sandboxMarker, err)
	}

	// Proof 2: the real ~/.apm was never touched.
	if _, err := os.Stat(realMarker); !os.IsNotExist(err) {
		t.Errorf("real config %s was touched by the run (err=%v)", realMarker, err)
	}
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

func assertFileContent(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	if string(got) != want {
		t.Errorf("%s content = %q, want %q", path, got, want)
	}
}
