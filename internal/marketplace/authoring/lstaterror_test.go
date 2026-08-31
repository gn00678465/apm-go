package authoring

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// injectedStatError is a *fs.PathError-shaped error that is deliberately NOT
// fs.ErrNotExist (errors.Is(injectedStatError{...}, fs.ErrNotExist) is
// false), standing in for an ACL/permission denial or any other I/O failure
// osLstat might return for a component this process cannot inspect.
type injectedStatError struct{ path string }

func (e injectedStatError) Error() string {
	return fmt.Sprintf("injected non-ErrNotExist stat failure for %q", e.path)
}

// TestLongestExistingAncestor_NonNotExistLstatError_FailsClosed is
// B-MAJOR-1's regression (external audit round 8, 2026-07-31 follow-up):
// longestExistingAncestor used to treat ANY os.Lstat failure -- not just
// "this component doesn't exist" -- identically, silently walking up to the
// parent. An ACL/permission-denied component (which might itself be the
// very reparse point pathWithinRoot needs to inspect) must instead surface
// as an error, not be silently skipped as if it simply didn't exist.
func TestLongestExistingAncestor_NonNotExistLstatError_FailsClosed(t *testing.T) {
	root := t.TempDir()
	denied := filepath.Join(root, "denied")
	if err := os.Mkdir(denied, 0o755); err != nil {
		t.Fatal(err)
	}

	orig := osLstat
	t.Cleanup(func() { osLstat = orig })
	osLstat = func(name string) (os.FileInfo, error) {
		if name == denied {
			return nil, injectedStatError{path: name}
		}
		return orig(name)
	}

	_, err := longestExistingAncestor(denied)
	if err == nil {
		t.Fatal("longestExistingAncestor(denied) returned a nil error, want the injected non-ErrNotExist stat failure surfaced rather than silently treated as \"doesn't exist\" (B-MAJOR-1, external audit round 8)")
	}
}

// TestResolveRealPathJunctionAware_NonNotExistLstatError_FailsClosed is
// B-MAJOR-1's second call site (external audit round 8, 2026-07-31
// follow-up): resolveRealPathJunctionAware's per-component walk had the
// exact same bug -- an os.Lstat failure other than "doesn't exist" (e.g. an
// ACL-protected component) used to be treated as "nothing left to resolve,
// stop here and accept," which is unsafe: the component this process could
// not inspect might itself be a reparse point escaping root.
func TestResolveRealPathJunctionAware_NonNotExistLstatError_FailsClosed(t *testing.T) {
	root := t.TempDir()
	denied := filepath.Join(root, "denied")
	if err := os.Mkdir(denied, 0o755); err != nil {
		t.Fatal(err)
	}

	orig := osLstat
	t.Cleanup(func() { osLstat = orig })
	osLstat = func(name string) (os.FileInfo, error) {
		if name == denied {
			return nil, injectedStatError{path: name}
		}
		return orig(name)
	}

	_, err := resolveRealPathJunctionAware(denied)
	if err == nil {
		t.Fatal("resolveRealPathJunctionAware(denied) returned a nil error, want the injected non-ErrNotExist stat failure to fail closed rather than being silently treated as \"doesn't exist, nothing more to resolve\" (B-MAJOR-1, external audit round 8)")
	}
}

// TestPathWithinRoot_NonNotExistLstatError_FailsClosed proves the fix closes
// the loop end-to-end at the pathWithinRoot level (not just the two internal
// helpers in isolation): an in-root-looking target whose containment check
// hits a non-ErrNotExist stat error along the way must be REJECTED, not
// silently accepted because the walk gave up early.
func TestPathWithinRoot_NonNotExistLstatError_FailsClosed(t *testing.T) {
	root := t.TempDir()
	denied := filepath.Join(root, "denied")
	target := filepath.Join(denied, "pkg")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}

	orig := osLstat
	t.Cleanup(func() { osLstat = orig })
	osLstat = func(name string) (os.FileInfo, error) {
		if name == denied {
			return nil, injectedStatError{path: name}
		}
		return orig(name)
	}

	if pathWithinRoot(root, target) {
		t.Fatal("pathWithinRoot(root, target) = true, want false: a non-ErrNotExist stat error on an ancestor component must fail closed (reject), since that component's true nature (possibly a reparse point escaping root) could not be positively determined (B-MAJOR-1, external audit round 8)")
	}
}
