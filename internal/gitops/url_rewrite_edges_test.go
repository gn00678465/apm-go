package gitops

import (
	"context"
	"errors"
	"os/exec"
	"testing"
	"time"
)

func TestExecConfigProbe_ExpiredDeadlineIsATimeout(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	_, _, err := execConfigProbe(ctx, ConfigProbe{Args: []string{"--version"}})
	if err != context.DeadlineExceeded {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
}

func TestExecConfigProbe_MissingGitIsNotAnExitCode(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	_, code, err := execConfigProbe(context.Background(), ConfigProbe{Args: []string{"--version"}})
	if !errors.Is(err, exec.ErrNotFound) || code != 0 {
		t.Fatalf("code %d, err %v; want exec.ErrNotFound", code, err)
	}
}

// CPython's urllib.parse rejects these in _check_bracketed_netloc and
// _check_bracketed_host ("Invalid IPv6 URL", "IPvFuture address is invalid").
func TestPyURLSplit_RejectsMalformedBracketedHost(t *testing.T) {
	for _, raw := range []string{
		"https://[::1]x/o/r.git",
		"https://x[::1]/o/r.git",
		"https://[vx]/o/r.git",
		// Brackets in the userinfo only: the host is then checked as an IP
		// literal, which a name is not.
		"https://us[er]@example.com/o/r.git",
	} {
		if parsed, err := pyURLSplit(raw); err == nil {
			t.Errorf("pyURLSplit(%q) = %+v, want an error", raw, parsed)
		}
	}
	parsed, err := pyURLSplit("https://[v1.a]/o/r.git")
	if err != nil || parsed.netloc != "[v1.a]" {
		t.Errorf("IPvFuture host: netloc %q, err %v; want [v1.a]", parsed.netloc, err)
	}
}

func TestValidateResolvedURLRewrite_UnparsableURL(t *testing.T) {
	const good, bad = "https://github.com/o/r.git", "https://[::1]x/o/r.git"
	// A port that is not a number splits, and fails when the origin is read.
	const badPort = "https://github.com:abc/o/r.git"
	for _, tc := range [][2]string{{bad, good}, {good, bad}, {badPort, good}, {good, badPort}} {
		err := validateResolvedURLRewrite(tc[0], tc[1], false)
		if err != (urlRewriteParseError{}) {
			t.Errorf("rewrite %q -> %q: err = %v, want the parse error", tc[0], tc[1], err)
		}
	}
}

func TestGitURLHost_SchemeWithNoHostRule(t *testing.T) {
	const raw = "ftp://files.example.com/o/r.git"
	parsed, err := pyURLSplit(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got := gitURLHost(raw, parsed); got != "" {
		t.Errorf("gitURLHost(%q) = %q, want none", raw, got)
	}
}
