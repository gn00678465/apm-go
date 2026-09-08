//go:build windows

package authoring

import (
	"bytes"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"testing"
)

// TestPathWithinRoot_RealACLDeniedComponent_FailsClosed is B-MAJOR-1's live,
// end-to-end repro using a REAL Windows ACL denial (external audit round 8,
// 2026-07-31 follow-up), independent of the injected-osLstat unit tests in
// lstaterror_test.go: an ancestor component this process is genuinely denied
// permission to Lstat must make pathWithinRoot fail closed (reject), not
// silently treat "access is denied" the same as "doesn't exist" and accept.
//
// Visibly t.Skip (never silently) at each step this cannot be constructed:
// determining the current user, denying this process's own read-attributes
// access via icacls, or observing that denial actually affect os.Lstat (a
// deny ACE can have no visible effect for a sufficiently privileged process,
// e.g. one holding SeBackupPrivilege/SeRestorePrivilege or already running
// elevated in a way that bypasses ordinary DACL checks). The ACL is always
// restored in cleanup, regardless of which path this test takes, so
// t.TempDir()'s own cleanup can still remove the directory afterward.
func TestPathWithinRoot_RealACLDeniedComponent_FailsClosed(t *testing.T) {
	u, err := user.Current()
	if err != nil {
		t.Skipf("SKIPPED: cannot determine current user (%v); the real-ACL-denial guard is untested by this run", err)
	}

	root := t.TempDir()
	denied := filepath.Join(root, "denied")
	if err := os.Mkdir(denied, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(denied, "pkg")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}

	denyCmd := exec.Command("icacls", denied, "/deny", u.Username+":(RA)")
	if out, err := denyCmd.CombinedOutput(); err != nil {
		t.Skipf("SKIPPED: icacls could not deny this process's own read-attributes access to %q (%v: %s); the real-ACL-denial guard is untested by this run", denied, err, bytes.TrimSpace(out))
	}
	t.Cleanup(func() {
		_ = exec.Command("icacls", denied, "/remove:d", u.Username).Run()
	})

	if _, statErr := os.Lstat(denied); statErr == nil {
		t.Skip("SKIPPED: the icacls deny ACE had no observable effect on this process's own os.Lstat (e.g. a privilege level that bypasses ordinary DACL checks) -- the real-ACL-denial guard is untested by this run")
	}

	if pathWithinRoot(root, target) {
		t.Fatal(`pathWithinRoot(root, target) = true, want false: a real ACL-denied ancestor component must fail closed (reject), not be silently treated as "doesn't exist" (B-MAJOR-1, external audit round 8)`)
	}
}
