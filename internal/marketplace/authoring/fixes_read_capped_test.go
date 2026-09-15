package authoring

import (
	"context"
	"io"
	"testing"
	"time"
)

// SPEC marketplace-check-outdated-fixes §B: the manifest read is bounded
// while it streams (SC-F3), and the `git show` command keeps the secure
// environment (SC-F16).

// countingReader hands out size bytes, then io.EOF, counting what it gave.
type countingReader struct {
	size int
	read int
}

func (r *countingReader) Read(p []byte) (int, error) {
	if r.read >= r.size {
		return 0, io.EOF
	}
	n := len(p)
	if rest := r.size - r.read; n > rest {
		n = rest
	}
	for i := 0; i < n; i++ {
		p[i] = 'x'
	}
	r.read += n
	return n, nil
}

func TestReadCapped_StopsAtLimit(t *testing.T) {
	const limit = 4096
	type outcome struct {
		data []byte
		err  error
	}
	run := func(t *testing.T, r *countingReader) outcome {
		t.Helper()
		done := make(chan outcome, 1)
		go func() {
			data, err := readCapped(r, limit)
			done <- outcome{data, err}
		}()
		select {
		case o := <-done:
			return o
		case <-time.After(5 * time.Second):
			t.Fatal("readCapped did not return within 5s")
			return outcome{}
		}
	}

	r := &countingReader{size: limit + 1000}
	o := run(t, r)
	if o.err == nil {
		t.Errorf("err = nil, want an over-limit error")
	}
	if r.read > limit+1 {
		t.Errorf("read %d bytes, want at most %d", r.read, limit+1)
	}

	t.Run("ExactlyAtLimit", func(t *testing.T) {
		r := &countingReader{size: limit}
		o := run(t, r)
		if o.err != nil || len(o.data) != limit {
			t.Errorf("got %d bytes, err %v; want %d bytes and no error", len(o.data), o.err, limit)
		}
	})
}

func TestNewShowCmd_ShapeAndSecureEnv(t *testing.T) {
	dir := t.TempDir()
	cmd := newShowCmd(context.Background(), dir, ".claude-plugin/plugin.json")

	want := []string{"-C", dir, "show", "FETCH_HEAD:.claude-plugin/plugin.json"}
	if len(cmd.Args) < len(want) {
		t.Fatalf("args = %v, want suffix %v", cmd.Args, want)
	}
	got := cmd.Args[len(cmd.Args)-len(want):]
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("args = %v, want suffix %v", cmd.Args, want)
		}
	}
	for _, e := range []string{"GIT_TERMINAL_PROMPT=0", "LC_ALL=C", "LANGUAGE=C"} {
		found := false
		for _, env := range cmd.Env {
			if env == e {
				found = true
			}
		}
		if !found {
			t.Errorf("env lacks %q", e)
		}
	}
	if cmd.WaitDelay != subprocessWaitDelay {
		t.Errorf("WaitDelay = %s, want %s", cmd.WaitDelay, subprocessWaitDelay)
	}
}
