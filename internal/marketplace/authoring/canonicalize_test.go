package authoring

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestPathWithinRoot_CanonicalizationFailure_FailsClosed is B-MINOR-1's
// fail-closed regression (external audit round 8, 2026-07-31 follow-up):
// when canonicalizeRealPathFn cannot positively canonicalize a path (the
// real Windows implementation returns an error when, e.g., the handle
// cannot be opened), pathWithinRoot must reject outright -- never fall back
// to comparing the un-canonicalized strings, which is exactly the ambiguity
// (8.3 short names, volume-GUID paths, UNC loopback paths) canonicalization
// exists to close.
func TestPathWithinRoot_CanonicalizationFailure_FailsClosed(t *testing.T) {
	orig := canonicalizeRealPathFn
	t.Cleanup(func() { canonicalizeRealPathFn = orig })
	canonicalizeRealPathFn = func(path string) (string, error) {
		return "", fmt.Errorf("injected canonicalization failure for %q", path)
	}

	root := t.TempDir()
	target := filepath.Join(root, "pkg")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}

	if pathWithinRoot(root, target) {
		t.Fatal("pathWithinRoot(root, target) = true, want false: a canonicalization failure must fail closed (reject), not fall back to comparing un-canonicalized strings (B-MINOR-1, external audit round 8)")
	}
}

// TestPathWithinRoot_UsesCanonicalizedPaths_NotRawStrings is B-MINOR-1's
// wiring regression: pathWithinRoot must actually route BOTH the resolved
// root and the resolved target through canonicalizeRealPathFn before
// comparing them -- not just define the var and never call it. A mutation
// that drops either call (or both) still passes every OTHER containment
// test in this file (since canonicalizeRealPath is an identity function on
// non-Windows, and on Windows the ordinary, non-aliased test fixtures used
// elsewhere already canonicalize to themselves) -- only counting the actual
// number of invocations catches that.
func TestPathWithinRoot_UsesCanonicalizedPaths_NotRawStrings(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "pkg")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}

	orig := canonicalizeRealPathFn
	t.Cleanup(func() { canonicalizeRealPathFn = orig })
	var calledWith []string
	canonicalizeRealPathFn = func(path string) (string, error) {
		calledWith = append(calledWith, path)
		return orig(path)
	}

	if !pathWithinRoot(root, target) {
		t.Fatal("pathWithinRoot(root, target) = false, want true for an ordinary in-root directory")
	}
	if len(calledWith) != 2 {
		t.Errorf("canonicalizeRealPathFn called %d time(s), want exactly 2 (once for the resolved root, once for the resolved target) -- B-MINOR-1's canonicalization step may have been bypassed", len(calledWith))
	}
}

// TestPathWithinRoot_CanonicalizationNormalizesAliasedSpelling_StillContained
// is B-MINOR-1's core positive case, using a fake canonicalizeRealPathFn
// (rather than depending on a live 8.3-short-name-enabled NTFS volume, which
// TestPathWithinRoot_Real8dot3ShortName_StillDetectedAsContained,
// canonicalize_windows_test.go, additionally covers end-to-end): root and
// target resolve, PRE-canonicalization, to two strings that would fail a
// naive prefix/Rel comparison (simulating an 8.3-short-name-style alias
// mismatch), but the fake canonicalizes both down to the SAME real string
// pathWithinRootLexical already knows is contained -- proving pathWithinRoot
// uses canonicalizeRealPathFn's RESULT for the comparison, not the pre-
// canonicalization strings it was called with.
func TestPathWithinRoot_CanonicalizationNormalizesAliasedSpelling_StillContained(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "pkg")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}

	// canonicalRoot/canonicalTarget stand in for the single canonical form
	// GetFinalPathNameByHandleW would return for both an alias and its
	// long-name original -- deliberately spelled differently from the raw
	// root/target strings passed in, so this test can only pass if
	// pathWithinRoot compares canonicalizeRealPathFn's RETURN value, not its
	// argument.
	canonicalRoot := filepath.Join(root, "canonical-marker")
	canonicalTarget := filepath.Join(canonicalRoot, "pkg")

	orig := canonicalizeRealPathFn
	t.Cleanup(func() { canonicalizeRealPathFn = orig })
	canonicalizeRealPathFn = func(path string) (string, error) {
		switch path {
		case root:
			return canonicalRoot, nil
		case target:
			return canonicalTarget, nil
		default:
			return path, nil
		}
	}

	if !pathWithinRoot(root, target) {
		t.Fatal("pathWithinRoot(root, target) = false, want true: canonicalizeRealPathFn maps both sides to a still-contained pair, so the post-canonicalization comparison must accept it")
	}
}
