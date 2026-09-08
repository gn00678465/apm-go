// This file (resolveref_test.go) is R5/AC40's direct, per-branch coverage
// of resolveRef's decision tree (design.md §6): each of the 6 branches
// prd.md's AC40 names -- implicit HEAD, explicit HEAD, --version,
// --no-verify, a concrete 40-hex SHA, and a local source -- gets its own
// test, calling resolveRef directly rather than only exercising it
// indirectly through AddPackage/SetPackage. AC21 additionally requires a
// LOCAL-source variant of every one of those 6 scenarios (mkt-046's "local
// source never touches the network" contract must hold no matter which
// other flag/ref shape is combined with it) -- those are the
// TestResolveRef_LocalSource_* tests below, every one of them using
// panicLister to prove zero network I/O.
//
// All of these use resolveRef's add-mode call shape (implicitHeadOnEmpty
// and skipLocalSource both true, mirroring AddPackage's own call) unless a
// test name says otherwise (the LocalSource_SetMode_* test below proves the
// skipLocalSource=false side of BLOCKING 2's fix -- see editor.go's
// resolveRef doc comment).
//
// MAJOR 2 (external audit, 2026-07-30) found two problems with the
// original 14 tests here, both addressed in this revision:
//   - TestResolveRef_LocalSource_ImplicitHead_NeverTouchesNetwork and
//     TestResolveRef_LocalSource_ZeroFlag_NeverTouchesNetwork called
//     resolveRef with byte-identical arguments -- not two distinct paths.
//     Merged into one test whose name still matches both of verify.ps1's
//     AC21 `-list` patterns (`LocalSource.*ImplicitHead` and
//     `LocalSource.*ZeroFlag`).
//   - TestResolveRef_LocalSource_Version_NeverTouchesNetwork's own
//     assertions are satisfied by the version!="" branch alone (resolveRef
//     returns before ever reaching the isLocalPackageSource check), so it
//     does not independently exercise the local-source branch the way its
//     name implies -- its comment below says so explicitly rather than
//     overclaiming coverage it doesn't have.
//   - Five missing boundary cases for the SHA/HEAD matching were added:
//     a 40-char non-hex string, a 41-char string, an uppercase SHA, a
//     mixed-case "Head", and a "refs/heads/HEAD" ref.
package authoring

import (
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/semver"
)

const resolveRefTestSHA = "0123456789abcdef0123456789abcdef01234567"

// ── AC40 branch 1: implicit HEAD (no --ref given at all) ────────────────

func TestResolveRef_ImplicitHead_RemoteSource_ResolvesViaLister(t *testing.T) {
	// Arrange
	lister := mapRefLister{refs: []semver.TagInfo{{Name: "HEAD", Commit: resolveRefTestSHA}}}

	// Act
	got, err := resolveRef("owner/repo", "", "", lister, false, true, true, nil, nil)

	// Assert
	if err != nil {
		t.Fatalf("resolveRef returned error for an implicit HEAD: %v", err)
	}
	if got != resolveRefTestSHA {
		t.Errorf("resolveRef = %q, want the resolved HEAD SHA %q", got, resolveRefTestSHA)
	}
}

// TestResolveRef_ImplicitHeadDisabled_EmptyRefShortCircuits_NoListerCall
// covers SetPackage's call shape: implicitHeadOnEmpty=false means an empty
// ref is simply "nothing to resolve", exactly the pre-fix short-circuit --
// SetPackage must never start treating an unrelated field update as an
// implicit-HEAD pin.
func TestResolveRef_ImplicitHeadDisabled_EmptyRefShortCircuits_NoListerCall(t *testing.T) {
	// Act
	got, err := resolveRef("owner/repo", "", "", panicLister{}, false, false, false, nil, nil)

	// Assert
	if err != nil {
		t.Fatalf("resolveRef returned error: %v", err)
	}
	if got != "" {
		t.Errorf("resolveRef = %q, want empty (nothing to resolve)", got)
	}
}

// ── AC40 branch 2: explicit HEAD (--ref HEAD, any case) ─────────────────

func TestResolveRef_ExplicitHead_RemoteSource_ResolvesViaLister(t *testing.T) {
	// Arrange
	lister := mapRefLister{refs: []semver.TagInfo{{Name: "HEAD", Commit: resolveRefTestSHA}}}

	// Act
	got, err := resolveRef("owner/repo", "HEAD", "", lister, false, true, true, nil, nil)

	// Assert
	if err != nil {
		t.Fatalf("resolveRef returned error for an explicit --ref HEAD: %v", err)
	}
	if got != resolveRefTestSHA {
		t.Errorf("resolveRef = %q, want the resolved HEAD SHA %q", got, resolveRefTestSHA)
	}
}

// TestResolveRef_ExplicitHead_CaseInsensitive proves "head"/"Head" are
// recognized the same as "HEAD" (mirroring upstream's ref.upper() == "HEAD").
func TestResolveRef_ExplicitHead_CaseInsensitive(t *testing.T) {
	// Arrange
	lister := mapRefLister{refs: []semver.TagInfo{{Name: "HEAD", Commit: resolveRefTestSHA}}}

	// Act
	got, err := resolveRef("owner/repo", "head", "", lister, false, true, true, nil, nil)

	// Assert
	if err != nil {
		t.Fatalf("resolveRef returned error for a lowercase 'head': %v", err)
	}
	if got != resolveRefTestSHA {
		t.Errorf("resolveRef = %q, want the resolved HEAD SHA %q", got, resolveRefTestSHA)
	}
}

// TestResolveRef_ExplicitHead_MixedCase_TitleCase is MAJOR 2's missing
// mixed-case boundary: "Head" (neither all-lower nor all-upper) must still
// match -- a mutation that narrows the EqualFold check to only literal
// "HEAD"/"head" would fail this while passing the two tests above.
func TestResolveRef_ExplicitHead_MixedCase_TitleCase(t *testing.T) {
	// Arrange
	lister := mapRefLister{refs: []semver.TagInfo{{Name: "HEAD", Commit: resolveRefTestSHA}}}

	// Act
	got, err := resolveRef("owner/repo", "Head", "", lister, false, true, true, nil, nil)

	// Assert
	if err != nil {
		t.Fatalf("resolveRef returned error for a mixed-case 'Head': %v", err)
	}
	if got != resolveRefTestSHA {
		t.Errorf("resolveRef = %q, want the resolved HEAD SHA %q", got, resolveRefTestSHA)
	}
}

// TestResolveRef_RefsHeadsHEAD_NotTreatedAsHeadKeyword is MAJOR 2's missing
// "refs/heads/HEAD" boundary: this string contains "HEAD" but is not
// case-insensitively EQUAL to "HEAD", so it must fall through to the plain
// tag/branch exact-name lookup (branch 6), not the special HEAD-keyword
// branch (5) -- a mutation that turned the HEAD check into a substring
// match would incorrectly treat this as a HEAD alias.
func TestResolveRef_RefsHeadsHEAD_NotTreatedAsHeadKeyword(t *testing.T) {
	// Arrange
	weirdRef := "refs/heads/HEAD"
	lister := mapRefLister{refs: []semver.TagInfo{{Name: weirdRef, Commit: resolveRefTestSHA}}}

	// Act
	got, err := resolveRef("owner/repo", weirdRef, "", lister, false, true, true, nil, nil)

	// Assert
	if err != nil {
		t.Fatalf("resolveRef returned error for ref %q: %v", weirdRef, err)
	}
	if got != resolveRefTestSHA {
		t.Errorf("resolveRef = %q, want the resolved SHA %q via an exact-name lookup", got, resolveRefTestSHA)
	}
}

// ── AC40 branch 3: --version given (no ref resolution at all) ───────────

func TestResolveRef_VersionGiven_SkipsResolution_NoListerCall(t *testing.T) {
	// Act: panicLister proves a --version range never triggers ref
	// resolution, even though ref itself is empty (which would otherwise
	// be treated as an implicit HEAD).
	got, err := resolveRef("owner/repo", "", "^1.0.0", panicLister{}, false, true, true, nil, nil)

	// Assert
	if err != nil {
		t.Fatalf("resolveRef returned error: %v", err)
	}
	if got != "" {
		t.Errorf("resolveRef = %q, want empty when --version is given", got)
	}
}

// ── AC40 branch 4: --no-verify + HEAD (implicit or explicit) errors ─────

func TestResolveRef_NoVerify_ImplicitHead_ReturnsOfflineError(t *testing.T) {
	// Act: panicLister proves the failure itself never touches the network.
	_, err := resolveRef("owner/repo", "", "", panicLister{}, true, true, true, nil, nil)

	// Assert
	if err == nil {
		t.Fatal("expected resolveRef to fail: --no-verify cannot resolve an implicit HEAD offline")
	}
	if !strings.Contains(err.Error(), "Cannot resolve HEAD ref without network access. Provide an explicit --ref SHA.") {
		t.Errorf("error = %v, want the exact upstream-parity message", err)
	}
}

func TestResolveRef_NoVerify_ExplicitHead_ReturnsOfflineError(t *testing.T) {
	// Act
	_, err := resolveRef("owner/repo", "HEAD", "", panicLister{}, true, true, true, nil, nil)

	// Assert
	if err == nil {
		t.Fatal("expected resolveRef to fail: --no-verify cannot resolve an explicit --ref HEAD offline")
	}
	if !strings.Contains(err.Error(), "Cannot resolve HEAD ref without network access. Provide an explicit --ref SHA.") {
		t.Errorf("error = %v, want the exact upstream-parity message", err)
	}
}

// ── BLOCKING 2 (external audit round 3, 2026-07-30): onExplicitHeadWillResolve
// must fire exactly once, and only immediately before the real lister call
// for an explicitly-given HEAD ref -- never for an implicit one (no --ref
// given at all), and never when --no-verify makes resolution impossible
// (the hook must not fire before the offline error is returned). ──────────

func TestResolveRef_ExplicitHead_InvokesOnExplicitHeadWillResolve(t *testing.T) {
	// Arrange
	lister := mapRefLister{refs: []semver.TagInfo{{Name: "HEAD", Commit: resolveRefTestSHA}}}
	calls := 0

	// Act
	got, err := resolveRef("owner/repo", "HEAD", "", lister, false, true, true, func() { calls++ }, nil)

	// Assert
	if err != nil {
		t.Fatalf("resolveRef returned error: %v", err)
	}
	if got != resolveRefTestSHA {
		t.Errorf("resolveRef = %q, want %q", got, resolveRefTestSHA)
	}
	if calls != 1 {
		t.Errorf("onExplicitHeadWillResolve called %d times, want exactly 1", calls)
	}
}

func TestResolveRef_ImplicitHead_DoesNotInvokeOnExplicitHeadWillResolve(t *testing.T) {
	// Arrange: ref == "" (no --ref given at all) is implicit HEAD, not
	// explicit -- the hook must stay silent for it.
	lister := mapRefLister{refs: []semver.TagInfo{{Name: "HEAD", Commit: resolveRefTestSHA}}}
	calls := 0

	// Act
	if _, err := resolveRef("owner/repo", "", "", lister, false, true, true, func() { calls++ }, nil); err != nil {
		t.Fatalf("resolveRef returned error: %v", err)
	}

	// Assert
	if calls != 0 {
		t.Errorf("onExplicitHeadWillResolve called %d times for an implicit HEAD, want 0", calls)
	}
}

func TestResolveRef_NoVerify_ExplicitHead_DoesNotInvokeOnExplicitHeadWillResolve(t *testing.T) {
	// Arrange: --no-verify makes resolution impossible -- the hook must not
	// fire before resolveRef returns errCannotResolveHeadOffline. panicLister
	// additionally proves the network is never reached either.
	calls := 0

	// Act
	_, err := resolveRef("owner/repo", "HEAD", "", panicLister{}, true, true, true, func() { calls++ }, nil)

	// Assert
	if err == nil {
		t.Fatal("expected resolveRef to fail offline")
	}
	if calls != 0 {
		t.Errorf("onExplicitHeadWillResolve called %d times under --no-verify, want 0", calls)
	}
}

func TestResolveRef_LocalSource_ExplicitHead_DoesNotInvokeOnExplicitHeadWillResolve(t *testing.T) {
	// Arrange: mkt-046's local-source short-circuit (add mode) must resolve
	// nothing at all, so the hook must never fire either. panicLister proves
	// the network is never reached.
	calls := 0

	// Act
	got, err := resolveRef("./pkgs/tool", "HEAD", "", panicLister{}, false, true, true, func() { calls++ }, nil)

	// Assert
	if err != nil {
		t.Fatalf("resolveRef returned error: %v", err)
	}
	if got != "" {
		t.Errorf("resolveRef = %q, want empty for a local source", got)
	}
	if calls != 0 {
		t.Errorf("onExplicitHeadWillResolve called %d times for a local source, want 0", calls)
	}
}

// ── AC40 branch 5: a concrete 40-hex SHA is stored as-is ─────────────────

func TestResolveRef_ConcreteSHA_ReturnedAsIs_NoListerCall(t *testing.T) {
	// Act: panicLister proves an already-concrete SHA never triggers a
	// lister call, even with --no-verify unset.
	got, err := resolveRef("owner/repo", resolveRefTestSHA, "", panicLister{}, false, true, true, nil, nil)

	// Assert
	if err != nil {
		t.Fatalf("resolveRef returned error: %v", err)
	}
	if got != resolveRefTestSHA {
		t.Errorf("resolveRef = %q, want the SHA stored verbatim %q", got, resolveRefTestSHA)
	}
}

// TestResolveRef_ShaPattern_40CharNonHex_ResolvesViaLister is MAJOR 2's
// missing 40-char-non-hex boundary: shaRefPattern requires every one of the
// 40 characters to be a hex digit ([0-9a-f]), so a 40-character string
// containing a non-hex letter must NOT be treated as an already-concrete
// SHA -- it must instead go through the normal tag/branch lookup. A
// mutation that widens the pattern to `^.{40}$` (any 40 characters) would
// return this ref verbatim instead of resolving it, so this test asserts
// the *resolved* commit, not the input ref, comes back.
func TestResolveRef_ShaPattern_40CharNonHex_ResolvesViaLister(t *testing.T) {
	// Arrange: 40 characters, but "g" is not a valid hex digit.
	nonHexRef := strings.Repeat("g", 40)
	if len(nonHexRef) != 40 {
		t.Fatalf("test fixture bug: nonHexRef has length %d, want 40", len(nonHexRef))
	}
	lister := mapRefLister{refs: []semver.TagInfo{{Name: nonHexRef, Commit: resolveRefTestSHA}}}

	// Act
	got, err := resolveRef("owner/repo", nonHexRef, "", lister, false, true, true, nil, nil)

	// Assert
	if err != nil {
		t.Fatalf("resolveRef returned error for a 40-char non-hex ref: %v", err)
	}
	if got != resolveRefTestSHA {
		t.Errorf("resolveRef = %q, want the resolved commit %q (a 40-char non-hex string must not be treated as a concrete SHA)", got, resolveRefTestSHA)
	}
}

// TestResolveRef_ShaPattern_41Char_ResolvesViaLister is MAJOR 2's missing
// 41-char boundary: one character longer than a real SHA must not match
// shaRefPattern either.
func TestResolveRef_ShaPattern_41Char_ResolvesViaLister(t *testing.T) {
	// Arrange
	ref41 := resolveRefTestSHA + "0"
	if len(ref41) != 41 {
		t.Fatalf("test fixture bug: ref41 has length %d, want 41", len(ref41))
	}
	lister := mapRefLister{refs: []semver.TagInfo{{Name: ref41, Commit: resolveRefTestSHA}}}

	// Act
	got, err := resolveRef("owner/repo", ref41, "", lister, false, true, true, nil, nil)

	// Assert
	if err != nil {
		t.Fatalf("resolveRef returned error for a 41-char ref: %v", err)
	}
	if got != resolveRefTestSHA {
		t.Errorf("resolveRef = %q, want the resolved commit %q (a 41-char string must not be treated as a concrete SHA)", got, resolveRefTestSHA)
	}
}

// TestResolveRef_ShaPattern_39CharValidHex_ResolvesViaLister is MAJOR 3's
// missing 39-char boundary (external audit round 2, 2026-07-30, REGR-M2): a
// mutation widening shaRefPattern from `^[0-9a-f]{40}$` to
// `^[0-9a-f]{39,40}$` would misread a 39-char *valid* lowercase-hex string
// as an already-concrete SHA (the existing 41Char/40CharNonHex/UppercaseSHA
// tests each vary a different axis -- length-too-long, non-hex character,
// wrong case -- none of them is one character short of 40 valid hex
// digits, so none of them catches this specific widening).
func TestResolveRef_ShaPattern_39CharValidHex_ResolvesViaLister(t *testing.T) {
	// Arrange
	ref39 := strings.TrimSuffix(resolveRefTestSHA, "7")
	if len(ref39) != 39 {
		t.Fatalf("test fixture bug: ref39 has length %d, want 39", len(ref39))
	}
	lister := mapRefLister{refs: []semver.TagInfo{{Name: ref39, Commit: resolveRefTestSHA}}}

	// Act
	got, err := resolveRef("owner/repo", ref39, "", lister, false, true, true, nil, nil)

	// Assert
	if err != nil {
		t.Fatalf("resolveRef returned error for a 39-char hex ref: %v", err)
	}
	if got != resolveRefTestSHA {
		t.Errorf("resolveRef = %q, want the resolved commit %q (a 39-char string must not be treated as a concrete SHA, even if every character is valid hex)", got, resolveRefTestSHA)
	}
}

// TestResolveRef_ShaPattern_UppercaseSHA_ResolvesViaLister is MAJOR 2's
// missing uppercase-SHA boundary: shaRefPattern only matches lowercase hex
// ([0-9a-f]), mirroring git's own lowercase SHA convention, so an uppercase
// 40-hex string must not be treated as already-concrete either. A mutation
// that made the pattern case-insensitive would return this verbatim.
func TestResolveRef_ShaPattern_UppercaseSHA_ResolvesViaLister(t *testing.T) {
	// Arrange
	upperSHA := strings.ToUpper(resolveRefTestSHA)
	lister := mapRefLister{refs: []semver.TagInfo{{Name: upperSHA, Commit: resolveRefTestSHA}}}

	// Act
	got, err := resolveRef("owner/repo", upperSHA, "", lister, false, true, true, nil, nil)

	// Assert
	if err != nil {
		t.Fatalf("resolveRef returned error for an uppercase SHA-shaped ref: %v", err)
	}
	if got != resolveRefTestSHA {
		t.Errorf("resolveRef = %q, want the resolved commit %q (an uppercase 40-hex string must not be treated as a concrete SHA)", got, resolveRefTestSHA)
	}
}

// ── AC40 branch 6 / AC21: a local source never resolves, in every
// scenario -- each variant below combines "local source" with one of the
// other 5 branches above, proving mkt-046's contract holds regardless of
// what else is given. All use resolveRef's add-mode call shape
// (skipLocalSource=true). ────────────────────────────────────────────────

// TestResolveRef_LocalSource_ImplicitHead_NeverTouchesNetwork also covers
// AC21's "zero flag" regression scenario (an add-mode call with every
// resolveRef input at its zero value is, for a local source, identical to
// "implicit HEAD" -- there is no third distinct argument shape to give it).
// The name matches both of verify.ps1's `-list` patterns
// (`LocalSource.*ImplicitHead` and `LocalSource.*ZeroFlag`) so this single
// test satisfies both without the byte-for-byte duplicate the external
// audit (MAJOR 2, 2026-07-30) flagged in the previous revision.
func TestResolveRef_LocalSource_ImplicitHead_ZeroFlag_NeverTouchesNetwork(t *testing.T) {
	// Act
	got, err := resolveRef("./pkgs/tool", "", "", panicLister{}, false, true, true, nil, nil)

	// Assert
	if err != nil {
		t.Fatalf("resolveRef returned error for a local source: %v", err)
	}
	if got != "" {
		t.Errorf("resolveRef = %q, want empty for a local source", got)
	}
}

func TestResolveRef_LocalSource_ExplicitHead_NeverTouchesNetwork(t *testing.T) {
	// Act
	got, err := resolveRef("./pkgs/tool", "HEAD", "", panicLister{}, false, true, true, nil, nil)

	// Assert
	if err != nil {
		t.Fatalf("resolveRef returned error for a local source: %v", err)
	}
	if got != "" {
		t.Errorf("resolveRef = %q, want empty for a local source (never resolved, even for explicit HEAD)", got)
	}
}

// TestResolveRef_LocalSource_Version_NeverTouchesNetwork: MAJOR 2 (external
// audit, 2026-07-30) correctly points out that version != "" returns before
// resolveRef ever reaches the isLocalPackageSource check, so this test's
// panicLister proof is fully satisfied by the version branch alone (see
// TestResolveRef_VersionGiven_SkipsResolution_NoListerCall, which asserts
// the identical thing for a remote source) -- it does NOT independently
// exercise the local-source branch's mutation surface the way its name
// might suggest. It is kept only because verify.ps1's AC21 gate requires a
// `LocalSource.*Version` test to exist for completeness; the tests that DO
// independently exercise the local-source check (order-sensitive against
// the branches it runs before) are ConcreteSHA below and ImplicitHead/
// ExplicitHead above, where removing the local check would make panicLister
// panic or return the SHA verbatim instead of "".
func TestResolveRef_LocalSource_Version_NeverTouchesNetwork(t *testing.T) {
	// Act
	got, err := resolveRef("./pkgs/tool", "", "^1.0.0", panicLister{}, false, true, true, nil, nil)

	// Assert
	if err != nil {
		t.Fatalf("resolveRef returned error for a local source: %v", err)
	}
	if got != "" {
		t.Errorf("resolveRef = %q, want empty for a local source", got)
	}
}

func TestResolveRef_LocalSource_NoVerify_NeverTouchesNetwork(t *testing.T) {
	// Act: a local source must succeed (not error) even with --no-verify
	// and an implicit HEAD -- unlike the remote case, there is nothing to
	// resolve at all, so the offline-HEAD error must never fire here.
	got, err := resolveRef("./pkgs/tool", "", "", panicLister{}, true, true, true, nil, nil)

	// Assert
	if err != nil {
		t.Fatalf("resolveRef returned error for a local source with --no-verify: %v", err)
	}
	if got != "" {
		t.Errorf("resolveRef = %q, want empty for a local source", got)
	}
}

// TestResolveRef_LocalSource_ConcreteSHA_NeverTouchesNetwork is order-
// sensitive: the local check (branch 2) runs before the SHA check
// (branch 4), so a mutation that removed/reordered the local check would
// make this test fail by returning the SHA verbatim instead of "".
func TestResolveRef_LocalSource_ConcreteSHA_NeverTouchesNetwork(t *testing.T) {
	// Act
	got, err := resolveRef("./pkgs/tool", resolveRefTestSHA, "", panicLister{}, false, true, true, nil, nil)

	// Assert
	if err != nil {
		t.Fatalf("resolveRef returned error for a local source: %v", err)
	}
	if got != "" {
		t.Errorf("resolveRef = %q, want empty for a local source (the local check runs before the SHA check)", got)
	}
}

// TestResolveRef_LocalSource_OrdinaryMutableRef_NeverTouchesNetwork is
// MAJOR 1's missing local-matrix case (external audit round 2,
// 2026-07-30): AC21 claims "all scenarios" for a local source, but no
// existing test gave a local source an *ordinary* mutable ref (a plain
// branch name, neither "", "HEAD", nor a concrete SHA). This mutation
// survived every test above it without this one:
//
//	if skipLocalSource && isLocalPackageSource(source) &&
//	    (ref == "" || strings.EqualFold(ref, "HEAD") || shaRefPattern.MatchString(ref)) {
//
// -- i.e. narrowing classifyRefResolution's local short-circuit to only
// fire for ref shapes that already have their own no-op reason, so
// `add ./pkg --ref main` falls through to the plain named-ref branch and
// calls lister.ListRefs on a local source: a direct mkt-046 violation.
// panicLister proves this test would catch it.
func TestResolveRef_LocalSource_OrdinaryMutableRef_NeverTouchesNetwork(t *testing.T) {
	// Act
	got, err := resolveRef("./pkgs/tool", "main", "", panicLister{}, false, true, true, nil, nil)

	// Assert
	if err != nil {
		t.Fatalf("resolveRef returned error for a local source: %v", err)
	}
	if got != "" {
		t.Errorf("resolveRef = %q, want empty for a local source (an ordinary mutable ref like \"main\" must never reach the lister for a local source)", got)
	}
}

// ── BLOCKING 2 (external audit, 2026-07-30): the skipLocalSource=false
// (set-mode) call shape must NOT short-circuit a local source -- an
// explicitly-given ref must still resolve via lister.ListRefs, exactly like
// a remote source. Before this fix, resolveRef applied its local
// short-circuit unconditionally, so `package set NAME --ref main` on a
// local package silently cleared Ref back to "" instead of resolving it
// (install-marketplace-contracts.md:87: "`set` always resolves a given
// ref"). See editor_test.go's TestSetPackage_LocalSource_* tests for the
// SetPackage-level regression. ──────────────────────────────────────────

func TestResolveRef_LocalSource_SetMode_DoesNotShortCircuit_ResolvesViaLister(t *testing.T) {
	// Arrange
	lister := mapRefLister{refs: []semver.TagInfo{{Name: "main", Commit: resolveRefTestSHA}}}

	// Act: skipLocalSource=false, implicitHeadOnEmpty=false -- SetPackage's
	// own call shape.
	got, err := resolveRef("./pkgs/tool", "main", "", lister, false, false, false, nil, nil)

	// Assert
	if err != nil {
		t.Fatalf("resolveRef returned error: %v", err)
	}
	if got != resolveRefTestSHA {
		t.Errorf("resolveRef = %q, want the resolved SHA %q (set-mode must resolve a local source's explicitly-given ref, not short-circuit it to empty)", got, resolveRefTestSHA)
	}
}

func TestResolveRef_LocalSource_SetMode_ConcreteSHA_StoredVerbatim_NoListerCall(t *testing.T) {
	// Act: panicLister proves an already-concrete SHA never triggers a
	// lister call for set-mode either, local source or not.
	got, err := resolveRef("./pkgs/tool", resolveRefTestSHA, "", panicLister{}, false, false, false, nil, nil)

	// Assert
	if err != nil {
		t.Fatalf("resolveRef returned error: %v", err)
	}
	if got != resolveRefTestSHA {
		t.Errorf("resolveRef = %q, want the SHA stored verbatim %q", got, resolveRefTestSHA)
	}
}

// ── BLOCKING 1 (external audit round 2, 2026-07-30): WillResolveMutableRefForAdd
// must not silently drift from resolveRef's own behavior -- classifyRefResolution
// is now the one function both call (editor.go). This cross-product test
// proves that behaviorally, not just structurally: for every (source
// local/remote) x (ref ""/HEAD/head/<concrete SHA>/main) x (version ""/set)
// combination, WillResolveMutableRefForAdd's prediction is compared against
// what resolveRef, given the exact same inputs and add's own call shape
// (skipLocalSource=true, implicitHeadOnEmpty=true), actually does: whether
// it reaches the network-resolving HEAD branch (as opposed to never calling
// the lister at all, storing a SHA verbatim, or resolving an ordinary named
// ref) -- distinguished via a lister that maps "HEAD" and "main" to two
// different, distinguishable commits. ───────────────────────────────────────

// BLOCKING 2 (external audit round 3, 2026-07-30) found this test's original
// two gaps: it pinned noVerify=false unconditionally (so
// classifyRefResolution's missing noVerify awareness went uncaught here),
// and refs never included the mixed-case "Head" (title case) -- both are
// covered below. "noVerify=true" makes actuallyResolvedViaHead false
// unconditionally (resolveRef must hard-fail via refKindHeadOffline before
// ever reaching spy.called), which is exactly what WillResolveMutableRefForAdd
// must now also predict.
func TestWillResolveMutableRefForAdd_MatchesResolveRefAcrossCrossProduct(t *testing.T) {
	const headSHA = "1111111111111111111111111111111111111a"
	const mainSHA = "2222222222222222222222222222222222222b"
	concreteSHARef := strings.Repeat("3", 39) + "c"
	if len(concreteSHARef) != 40 {
		t.Fatalf("test fixture bug: concreteSHARef has length %d, want 40", len(concreteSHARef))
	}

	sources := []struct {
		name    string
		source  string
		isLocal bool
	}{
		{"remote", "owner/repo", false},
		{"local", "./pkgs/tool", true},
	}
	// MAJOR (external audit round 3, 2026-07-30, ROUND2-B1): "Head" (title
	// case -- neither all-upper "HEAD" nor all-lower "head") must be
	// included here, or a predictor mutation carving out an exception for
	// this exact spelling (e.g. `... == refKindHead && ref != "Head"`)
	// survives undetected.
	refs := []string{"", "HEAD", "head", "Head", concreteSHARef, "main"}
	versions := []string{"", "^1.0.0"}
	noVerifies := []bool{false, true}

	for _, s := range sources {
		for _, ref := range refs {
			for _, version := range versions {
				for _, noVerify := range noVerifies {
					name := s.name + "/ref=" + ref + "/version=" + version + "/noVerify=" + strconvBool(noVerify)
					if ref == "" {
						name = s.name + "/ref=<empty>/version=" + version + "/noVerify=" + strconvBool(noVerify)
					}
					t.Run(name, func(t *testing.T) {
						spy := &spyRefLister{inner: mapRefLister{refs: []semver.TagInfo{
							{Name: "HEAD", Commit: headSHA},
							{Name: "main", Commit: mainSHA},
						}}}

						predicted := WillResolveMutableRefForAdd(s.source, ref, version, noVerify)
						got, _ := resolveRef(s.source, ref, version, spy, noVerify, true, true, nil, nil)
						actuallyResolvedViaHead := spy.called && got == headSHA

						if predicted != actuallyResolvedViaHead {
							t.Errorf("WillResolveMutableRefForAdd(%q, %q, %q, %v) = %v, but resolveRef's actual HEAD-branch resolution = %v (lister called: %v, got: %q)",
								s.source, ref, version, noVerify, predicted, actuallyResolvedViaHead, spy.called, got)
						}
					})
				}
			}
		}
	}
}

// TestResolveRef_CrossProduct_MatchesDirectSpecOracle is MAJOR 1 (external
// audit round 4, 2026-07-30)'s fix for the cross-product test above being a
// COUPLED ORACLE: WillResolveMutableRefForAdd and resolveRef both call the
// exact same classifyRefResolution, so a mutation to that shared classifier
// (the report's own example: adding `&& ref != "Head"` to the noVerify
// check inside classifyRefResolution) makes both the predictor and
// resolveRef's actual behavior wrong IN THE SAME WAY -- predicted still
// equals actuallyResolvedViaHead, so the test above stays green even though
// production behavior is broken for `--ref Head --no-verify`.
//
// This test computes its expected outcome (was the lister called at all,
// what value/error resolveRef returns) directly from add's documented
// decision table (this file's own package doc comment, and editor.go's
// classifyRefResolution doc comment) -- WITHOUT calling
// classifyRefResolution, WillResolveMutableRefForAdd, or any other
// production helper to derive the expectation -- so a bug inside the shared
// classifier itself has nowhere left to hide from both sides of the
// comparison agreeing.
func TestResolveRef_CrossProduct_MatchesDirectSpecOracle(t *testing.T) {
	const headSHA = "1111111111111111111111111111111111111a"
	const mainSHA = "2222222222222222222222222222222222222b"
	concreteSHARef := strings.Repeat("3", 39) + "c"
	if len(concreteSHARef) != 40 {
		t.Fatalf("test fixture bug: concreteSHARef has length %d, want 40", len(concreteSHARef))
	}

	sources := []struct {
		name    string
		source  string
		isLocal bool
	}{
		{"remote", "owner/repo", false},
		{"local", "./pkgs/tool", true},
	}
	refs := []string{"", "HEAD", "head", "Head", concreteSHARef, "main"}
	versions := []string{"", "^1.0.0"}
	noVerifies := []bool{false, true}
	isHeadShaped := func(ref string) bool {
		return ref == "" || strings.EqualFold(ref, "HEAD")
	}

	for _, s := range sources {
		for _, ref := range refs {
			for _, version := range versions {
				for _, noVerify := range noVerifies {
					name := s.name + "/ref=" + ref + "/version=" + version + "/noVerify=" + strconvBool(noVerify)
					if ref == "" {
						name = s.name + "/ref=<empty>/version=" + version + "/noVerify=" + strconvBool(noVerify)
					}
					t.Run(name, func(t *testing.T) {
						spy := &spyRefLister{inner: mapRefLister{refs: []semver.TagInfo{
							{Name: "HEAD", Commit: headSHA},
							{Name: "main", Commit: mainSHA},
						}}}

						// Directly-computed oracle -- add's own call shape is
						// implicitHeadOnEmpty=true, skipLocalSource=true.
						var wantListerCalled, wantErr bool
						var wantResult string
						switch {
						case version != "":
							// AC40 branch 3: a version range pins nothing here.
							wantResult, wantListerCalled, wantErr = "", false, false
						case s.isLocal:
							// mkt-046: add never resolves a local source's ref,
							// regardless of what ref/noVerify says.
							wantResult, wantListerCalled, wantErr = "", false, false
						case ref == concreteSHARef:
							// AC40 branch 5: an already-concrete SHA is stored
							// verbatim, no lister call.
							wantResult, wantListerCalled, wantErr = concreteSHARef, false, false
						case isHeadShaped(ref):
							if noVerify {
								// AC18: HEAD cannot be resolved offline.
								wantResult, wantListerCalled, wantErr = "", false, true
							} else {
								wantResult, wantListerCalled, wantErr = headSHA, true, false
							}
						case ref == "main":
							wantResult, wantListerCalled, wantErr = mainSHA, true, false
						default:
							t.Fatalf("test fixture bug: unhandled ref %q", ref)
						}

						got, err := resolveRef(s.source, ref, version, spy, noVerify, true, true, nil, nil)

						if spy.called != wantListerCalled {
							t.Errorf("lister called = %v, want %v (source=%q, ref=%q, version=%q, noVerify=%v)", spy.called, wantListerCalled, s.source, ref, version, noVerify)
						}
						if wantErr {
							if err == nil {
								t.Errorf("resolveRef(%q, %q, %q, noVerify=%v) returned no error, want one", s.source, ref, version, noVerify)
							}
							return
						}
						if err != nil {
							t.Errorf("resolveRef(%q, %q, %q, noVerify=%v) returned error %v, want none", s.source, ref, version, noVerify, err)
						}
						if got != wantResult {
							t.Errorf("resolveRef(%q, %q, %q, noVerify=%v) = %q, want %q", s.source, ref, version, noVerify, got, wantResult)
						}
					})
				}
			}
		}
	}
}

func strconvBool(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// TestResolveRefForKind_UnrecognizedKind_FailsClosed_NeverTouchesLister is
// MAJOR 5 (external audit round 4, 2026-07-30): resolveRef's kind-dispatch
// switch used to fall back to refKindNamed's own network-resolving branch
// for anything it didn't recognize by name (a bare `default:` with a
// "// refKindNamed" comment). A future refResolutionKind value added to the
// enum without also adding an explicit case to that switch would silently
// touch the network with whatever ref happens to hold, instead of failing
// closed. Drives resolveRefForKind directly with a kind value
// classifyRefResolution can never actually produce today, proving the
// `default` case (a) returns an error and (b) never calls the lister at
// all -- panicLister enforces (b).
func TestResolveRefForKind_UnrecognizedKind_FailsClosed_NeverTouchesLister(t *testing.T) {
	const unrecognizedKind = refResolutionKind(999)

	got, err := resolveRefForKind(unrecognizedKind, "owner/repo", "some-ref", panicLister{}, nil, nil)

	if err == nil {
		t.Fatal("resolveRefForKind with an unrecognized kind returned no error, want a fail-closed error")
	}
	if got != "" {
		t.Errorf("resolveRefForKind = %q, want empty on the fail-closed path", got)
	}
}

// spyRefLister wraps another RefLister and records whether ListRefs was
// ever called -- used by the BLOCKING 1 cross-product test above to
// distinguish "resolveRef never touched the lister" from "resolveRef called
// the lister for an ordinary named-ref lookup" (both leave predicted=false,
// but only the former is what WillResolveMutableRefForAdd claims).
type spyRefLister struct {
	inner  RefLister
	called bool
}

func (s *spyRefLister) ListRefs(source string) ([]semver.TagInfo, error) {
	s.called = true
	return s.inner.ListRefs(source)
}
