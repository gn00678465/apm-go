package main

import (
	"bytes"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// defaultTimeout is the production per-run timeout (acceptance: 60s). Tests
// call runProcess directly with a short timeout to exercise the same code
// path without waiting a full minute.
const defaultTimeout = 60 * time.Second

// execResult is the raw outcome of one subprocess run: nothing here is
// normalised or compared, only captured.
type execResult struct {
	ExitCode int
	TimedOut bool
	Stdout   []byte
	Stderr   []byte
}

// runProcess runs argv[0] with argv[1:], the given env and stdin, in cwd,
// under its own process group. If the process (or any descendant it spawns)
// is still running after timeout, the whole group is SIGKILLed — a
// descendant cannot outlive its parent's timeout by detaching stdout/stderr
// and sleeping. Whatever stdout/stderr bytes were written before the kill
// are still returned, and TimedOut is set so the caller can mark the record
// accordingly. The process never has a TTY on any fd: stdin/stdout/stderr
// are all in-memory buffers, never the runner's own fds.
func runProcess(argv []string, env map[string]string, stdin string, cwd string, timeout time.Duration) execResult {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = cwd
	cmd.Env = envSlice(env)
	cmd.Stdin = strings.NewReader(stdin)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		return execResult{
			ExitCode: -1,
			Stderr:   []byte("parity: failed to start process: " + err.Error()),
		}
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	timedOut := false
	select {
	case <-done:
		// Process finished within the timeout; stdoutBuf/stderrBuf are
		// fully written because Wait() only returns once the internal
		// copy goroutines have finished draining the pipes.
	case <-time.After(timeout):
		timedOut = true
		// Negative pid signals the whole process group, not just the
		// direct child, so a subprocess it spawned cannot survive the kill.
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		<-done // block until Wait() actually returns so the buffers are final
	}

	exitCode := -1
	if cmd.ProcessState != nil {
		exitCode = cmd.ProcessState.ExitCode()
	}

	return execResult{
		ExitCode: exitCode,
		TimedOut: timedOut,
		Stdout:   stdoutBuf.Bytes(),
		Stderr:   stderrBuf.Bytes(),
	}
}
