//go:build unix

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestRunProcess_TimeoutCapturesPartialOutputAndMarksTimedOut(t *testing.T) {
	scriptDir := t.TempDir()
	stub := writeStubScript(t, scriptDir, "stub.sh", `
echo "partial output before the kill"
sleep 30
echo "should never be reached"
`)

	cwd := t.TempDir()
	env := buildEnv(nil, t.TempDir(), t.TempDir(), t.TempDir())

	start := time.Now()
	res := runProcess([]string{stub}, env, "", cwd, 200*time.Millisecond)
	elapsed := time.Since(start)

	if !res.TimedOut {
		t.Error("TimedOut = false, want true")
	}
	if elapsed > 5*time.Second {
		t.Errorf("runProcess took %s after a 200ms timeout; the kill did not take effect promptly", elapsed)
	}
	if !strings.Contains(string(res.Stdout), "partial output before the kill") {
		t.Errorf("stdout = %q, want it to contain the pre-sleep line", res.Stdout)
	}
	if strings.Contains(string(res.Stdout), "should never be reached") {
		t.Errorf("stdout = %q, contains output from after the sleep", res.Stdout)
	}
}

// TestRunProcess_TimeoutKillsWholeProcessGroup proves the whole group is
// killed, not just the direct child: the stub backgrounds a grandchild that
// would otherwise outlive the timeout by sleeping past it.
func TestRunProcess_TimeoutKillsWholeProcessGroup(t *testing.T) {
	scriptDir := t.TempDir()
	pidFile := filepath.Join(scriptDir, "child.pid")
	stub := writeStubScript(t, scriptDir, "stub.sh", `
sleep 30 &
echo $! > `+pidFile+`
sleep 30
`)

	cwd := t.TempDir()
	env := buildEnv(nil, t.TempDir(), t.TempDir(), t.TempDir())

	res := runProcess([]string{stub}, env, "", cwd, 200*time.Millisecond)
	if !res.TimedOut {
		t.Fatal("TimedOut = false, want true")
	}

	// Give the background sleep's pid file a moment to be visible, then
	// confirm that pid is dead — proof the whole group was signalled.
	deadline := time.Now().Add(2 * time.Second)
	var childPID int
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(pidFile)
		if err == nil && len(data) > 0 {
			var n int
			if _, scanErr := fmt.Sscan(string(data), &n); scanErr == nil {
				childPID = n
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if childPID == 0 {
		t.Fatal("never observed the background child's pid")
	}

	if err := syscall.Kill(childPID, 0); err == nil {
		t.Errorf("background child pid %d is still alive after group kill", childPID)
	}
}

func TestRunProcess_NoTimeoutReturnsFullOutputAndExitCode(t *testing.T) {
	scriptDir := t.TempDir()
	stub := writeStubScript(t, scriptDir, "stub.sh", `
echo "out line"
echo "err line" 1>&2
exit 7
`)

	cwd := t.TempDir()
	env := buildEnv(nil, t.TempDir(), t.TempDir(), t.TempDir())

	res := runProcess([]string{stub}, env, "", cwd, defaultTimeout)
	if res.TimedOut {
		t.Error("TimedOut = true, want false")
	}
	if res.ExitCode != 7 {
		t.Errorf("ExitCode = %d, want 7", res.ExitCode)
	}
	if strings.TrimSpace(string(res.Stdout)) != "out line" {
		t.Errorf("stdout = %q", res.Stdout)
	}
	if strings.TrimSpace(string(res.Stderr)) != "err line" {
		t.Errorf("stderr = %q", res.Stderr)
	}
}

func TestRunProcess_StdinIsPassedAndNoTTY(t *testing.T) {
	scriptDir := t.TempDir()
	stub := writeStubScript(t, scriptDir, "stub.sh", `
cat
if [ -t 0 ]; then echo "stdin-is-a-tty"; fi
if [ -t 1 ]; then echo "stdout-is-a-tty"; fi
`)

	cwd := t.TempDir()
	env := buildEnv(nil, t.TempDir(), t.TempDir(), t.TempDir())

	res := runProcess([]string{stub}, env, "input-from-case", cwd, defaultTimeout)
	out := string(res.Stdout)
	if !strings.Contains(out, "input-from-case") {
		t.Errorf("stdout = %q, want it to echo stdin", out)
	}
	if strings.Contains(out, "-is-a-tty") {
		t.Errorf("stdout = %q, a fd was reported as a TTY", out)
	}
}

func TestRunProcess_StartFailureReturnsSyntheticResult(t *testing.T) {
	cwd := t.TempDir()
	env := buildEnv(nil, t.TempDir(), t.TempDir(), t.TempDir())

	res := runProcess([]string{filepath.Join(t.TempDir(), "does-not-exist")}, env, "", cwd, defaultTimeout)
	if res.ExitCode != -1 {
		t.Errorf("ExitCode = %d, want -1", res.ExitCode)
	}
	if res.TimedOut {
		t.Error("TimedOut = true, want false")
	}
	if len(res.Stderr) == 0 {
		t.Error("expected a stderr message describing the start failure")
	}
}
