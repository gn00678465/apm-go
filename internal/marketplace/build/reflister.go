// This file (reflister.go) implements ResolvePackages' `git ls-remote
// --tags --heads` access: the RefLister abstraction plus its production
// implementation, gitRefLister.
//
// Unlike internal/marketplace/authoring/refcheck.go's RefLister -- which
// strips both the "refs/tags/" and "refs/heads/" prefixes uniformly because
// `check`/`outdated` never need to tell a tag from a branch -- ResolvePackages
// needs the untouched prefix to implement mkt-055's "a tag beats a
// same-named branch" priority rule and to detect a branch/HEAD match for
// HeadNotAllowedError. That difference in what the caller needs back is why
// this package has its own small listing helper instead of importing
// authoring's.
package build

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/apm-go/apm/internal/gitops"
)

// RemoteRef is one line of `git ls-remote --tags --heads`: the ref's full
// name (e.g. "refs/tags/v1.0.0" or "refs/heads/main") and its commit SHA.
type RemoteRef struct {
	Name   string
	Commit string
}

// RefLister abstracts `git ls-remote --tags --heads` for ResolvePackages, so
// tests can substitute a fake -- in particular one that panics on any call,
// proving a local package's resolution never touches the network (mkt-051).
type RefLister interface {
	// ListRemoteRefs returns every tag and branch head advertised by
	// source (a marketplace.packages[].source string for a remote
	// package).
	ListRemoteRefs(source string) ([]RemoteRef, error)
}

// DefaultRefLister is ResolvePackages' production RefLister: a real `git
// ls-remote` subprocess, hardened against interactive credential prompts via
// gitops.ApplySecureGitEnv.
var DefaultRefLister RefLister = gitRefLister{}

type gitRefLister struct{}

// listRefsTimeout bounds how long a single `git ls-remote` subprocess may
// run before being killed, so an unreachable or private remote can never
// hang ResolvePackages indefinitely. A var, not a const, so tests can shrink
// it to prove the timeout actually fires (mirroring
// authoring/refcheck.go's review finding F3 HIGH, the same class of issue).
var listRefsTimeout = 30 * time.Second

func (gitRefLister) ListRemoteRefs(source string) ([]RemoteRef, error) {
	cloneURL := resolveCloneURL(source)
	safeURL := gitops.SanitizeGitOutput(cloneURL)

	ctx, cancel := context.WithTimeout(context.Background(), listRefsTimeout)
	defer cancel()

	cmd := newListRefsCmd(ctx, cloneURL)
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("git ls-remote %s: timed out after %s", safeURL, listRefsTimeout)
		}
		if ee, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("git ls-remote %s: %s", safeURL, gitops.SanitizeGitOutput(strings.TrimSpace(string(ee.Stderr))))
		}
		return nil, fmt.Errorf("git ls-remote %s: %w", safeURL, err)
	}
	return parseRemoteRefs(string(out)), nil
}

// newListRefsCmd builds the `git ls-remote --tags --heads <cloneURL>`
// subprocess command, hardened against interactive credential prompts via
// gitops.ApplySecureGitEnv. Split out from ListRemoteRefs so tests can
// assert on the constructed command without spawning a subprocess.
func newListRefsCmd(ctx context.Context, cloneURL string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "git", "ls-remote", "--tags", "--heads", cloneURL)
	gitops.ApplySecureGitEnv(cmd)
	return cmd
}

// parseRemoteRefs parses `git ls-remote --tags --heads` output into
// RemoteRef entries, deliberately keeping each ref's full "refs/tags/..." /
// "refs/heads/..." name intact (see this file's package doc comment for
// why).
func parseRemoteRefs(output string) []RemoteRef {
	var refs []RemoteRef
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		refs = append(refs, RemoteRef{Commit: parts[0], Name: parts[1]})
	}
	return refs
}
