package build

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// createEscapeLink makes link point at target using whichever link kind the
// platform lets an unprivileged process create. On Windows that is a
// junction: a real symlink needs Developer Mode or admin, while `mklink /J`
// needs neither, which makes junctions the reachable attack shape there.
func createEscapeLink(t *testing.T, link, target string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput()
		if err != nil {
			t.Skipf("cannot create a junction here: %v: %s", err, out)
		}
		return
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("cannot create a symlink here: %v", err)
	}
}

func TestRootWriter_RefusesToWriteThroughALinkPlantedBeforeItOpened(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "project")
	outside := filepath.Join(base, "outside")
	for _, d := range []string{root, outside} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	createEscapeLink(t, filepath.Join(root, "escape"), outside)

	rw, err := OpenRootWriter(root)
	if err != nil {
		t.Fatal(err)
	}
	defer rw.Close()

	if err := rw.WriteFileAtomic("escape/pwned.json", []byte("{}")); err == nil {
		t.Error("WriteFileAtomic wrote through a link pointing outside the root")
	}
	if err := rw.WriteFile("escape/pwned2.json", []byte("{}"), 0o644); err == nil {
		t.Error("WriteFile wrote through a link pointing outside the root")
	}
	assertNothingEscaped(t, outside)
}

// TestRootWriter_RefusesAfterAnAncestorIsSwappedForALink is the defect this
// type exists for. EnsureWithinRoot checks a path and returns a string; the
// swap happens after that check, and every later os.* call on the string
// walks the new link. The RootWriter holds a handle taken before the swap,
// so the swapped directory is no longer reachable through it.
func TestRootWriter_RefusesAfterAnAncestorIsSwappedForALink(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "project")
	outside := filepath.Join(base, "outside")
	target := filepath.Join(root, "out")
	for _, d := range []string{root, outside, target} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	rw, err := OpenRootWriter(root)
	if err != nil {
		t.Fatal(err)
	}
	defer rw.Close()

	// The window: a real directory at check time, a link by write time.
	if err := os.Remove(target); err != nil {
		t.Skipf("cannot remove the target directory to swap it: %v", err)
	}
	createEscapeLink(t, target, outside)

	if err := rw.WriteFileAtomic("out/marketplace.json", []byte("{}")); err == nil {
		t.Error("WriteFileAtomic wrote through a directory swapped for a link after the handle was taken")
	}
	if err := rw.WriteFile("out/plugin.json", []byte("{}"), 0o644); err == nil {
		t.Error("WriteFile wrote through a directory swapped for a link after the handle was taken")
	}

	// Control: the old shape -- a plain path-string write -- really does
	// escape here. Without this the test could pass on a platform where the
	// link was never followed by anything, proving nothing about RootWriter.
	if err := os.WriteFile(filepath.Join(target, "control.json"), []byte("{}"), 0o644); err != nil {
		t.Skipf("path-string control write did not reach outside either (%v); nothing to compare against", err)
	}
	entries, err := os.ReadDir(outside)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if len(names) != 1 || names[0] != "control.json" {
		t.Errorf("files outside the root = %v, want only the control write", names)
	}
}

func assertNothingEscaped(t *testing.T, outside string) {
	t.Helper()
	entries, err := os.ReadDir(outside)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("files escaped the root: %v", names)
	}
}

func TestRootWriter_RefusesParentTraversalAndAbsolutePaths(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "project")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	rw, err := OpenRootWriter(root)
	if err != nil {
		t.Fatal(err)
	}
	defer rw.Close()

	for _, rel := range []string{"../escaped.json", "a/../../escaped.json", filepath.Join(base, "absolute.json")} {
		if err := rw.WriteFileAtomic(rel, []byte("{}")); err == nil {
			t.Errorf("WriteFileAtomic(%q) succeeded; it must stay inside the root", rel)
		}
	}
	if _, err := os.Stat(filepath.Join(base, "escaped.json")); !os.IsNotExist(err) {
		t.Errorf("a traversal write landed outside the root (stat err = %v)", err)
	}
}

func TestRootWriter_WriteFileAtomicCreatesParentsAndRoundTrips(t *testing.T) {
	rw, err := OpenRootWriter(filepath.Join(t.TempDir(), "made-on-demand"))
	if err != nil {
		t.Fatal(err)
	}
	defer rw.Close()

	if err := rw.WriteFileAtomic("nested/deep/plugin.json", []byte(`{"n":1}`)); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(rw.Dir(), "nested", "deep", "plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"n":1}` {
		t.Errorf("content = %q, want %q", got, `{"n":1}`)
	}
}

// TestRootWriter_WriteFileAtomicKeepsAnExistingFilesMode is the overwrite
// half of the 2026-08-13 audit's finding #7: os.WriteFile applies its mode
// argument only when it CREATES the file, so an existing manifest kept the
// permissions it already had, and the atomic write must not widen them.
//
// Unix only. Windows records exactly one permission bit through Go's
// FileMode -- read-only -- and a read-only target is precisely what blocks
// the rename this write is built on (see the Windows test below), so there
// is no mode on Windows that is both non-default and renameable over.
func TestRootWriter_WriteFileAtomicKeepsAnExistingFilesMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows exposes only the read-only bit, which blocks the rename; see TestRootWriter_WriteFileAtomicFailsOnAReadOnlyTarget")
	}
	rw, err := OpenRootWriter(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer rw.Close()

	if err := rw.WriteFileAtomic("plugin.json", []byte("{}")); err != nil {
		t.Fatal(err)
	}
	abs := filepath.Join(rw.Dir(), "plugin.json")
	if err := os.Chmod(abs, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := rw.WriteFileAtomic("plugin.json", []byte(`{"v":2}`)); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(abs)
	if err != nil {
		t.Fatal(err)
	}
	if after.Mode().Perm() != 0o600 {
		t.Errorf("mode after overwrite = %v, want 0600: an overwrite must not widen permissions", after.Mode().Perm())
	}
}

// TestRootWriter_WriteFileAtomicCreatesAWritableFile is what the mode path
// CAN be checked for on every platform: a new file must not inherit
// os.CreateTemp's 0600 -- on Windows that would surface as a read-only
// manifest, on Unix as an owner-only one. It does not prove umask is applied
// (that needs a Unix run with a non-default umask, which this machine cannot
// execute -- see the commit body).
func TestRootWriter_WriteFileAtomicCreatesAWritableFile(t *testing.T) {
	rw, err := OpenRootWriter(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer rw.Close()

	if err := rw.WriteFileAtomic("plugin.json", []byte("{}")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(rw.Dir(), "plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o200 == 0 {
		t.Errorf("new file mode = %v, want the owner-write bit set", info.Mode().Perm())
	}
	// Deliberately no group/other-readable assertion: 0644 is what reaches
	// open(2), and the kernel then narrows it by the process umask, so a
	// developer running under umask 077 would correctly get 0600 and a test
	// demanding 0644 would fail on a correct implementation (external audit,
	// 2026-08-13). Proving umask is actually applied needs a subprocess with
	// a known umask, which this machine cannot run (no Go under WSL).
}

// TestRootWriter_WriteFileAtomicFailsOnAReadOnlyTarget pins a real Windows
// behaviour rather than papering over it: MoveFileEx refuses to replace a
// read-only file, so `pack --force` over a read-only manifest fails instead
// of overwriting. This is not new -- it has held since the plugin manifest
// became an atomic write (86ada0b) -- but nothing tested the full write path
// with a read-only target, so it was never written down.
func TestRootWriter_WriteFileAtomicFailsOnAReadOnlyTarget(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("on Unix the directory's write bit governs rename, not the file's")
	}
	rw, err := OpenRootWriter(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer rw.Close()

	if err := rw.WriteFileAtomic("plugin.json", []byte("{}")); err != nil {
		t.Fatal(err)
	}
	abs := filepath.Join(rw.Dir(), "plugin.json")
	if err := os.Chmod(abs, 0o444); err != nil {
		t.Fatal(err)
	}

	err = rw.WriteFileAtomic("plugin.json", []byte(`{"v":2}`))
	if err == nil {
		t.Fatal("WriteFileAtomic replaced a read-only file; if that becomes possible, this test and the doc comment must change together")
	}
	if !strings.Contains(err.Error(), "plugin.json") {
		t.Errorf("error %q does not name the file it failed to write", err)
	}
	// The original content must survive a failed write.
	got, rerr := os.ReadFile(abs)
	if rerr != nil {
		t.Fatal(rerr)
	}
	if string(got) != "{}" {
		t.Errorf("content after a failed write = %q, want the original", got)
	}
}

// TestRootWriter_WriteFileAtomicDoesNotWriteThroughAHardLink is the property
// the temp-file + rename shape exists for: truncating in place writes through
// every name the inode has, and Lstat cannot tell a planted hard link from an
// ordinary file.
func TestRootWriter_WriteFileAtomicDoesNotWriteThroughAHardLink(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "project")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(base, "victim.txt")
	if err := os.WriteFile(victim, []byte("do not lose me"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "plugin.json")
	if err := os.Link(victim, link); err != nil {
		t.Skipf("cannot create a hard link here: %v", err)
	}

	rw, err := OpenRootWriter(root)
	if err != nil {
		t.Fatal(err)
	}
	defer rw.Close()
	if err := rw.WriteFileAtomic("plugin.json", []byte(`{"replaced":true}`)); err != nil {
		t.Fatal(err)
	}

	survived, err := os.ReadFile(victim)
	if err != nil {
		t.Fatal(err)
	}
	if string(survived) != "do not lose me" {
		t.Errorf("the file outside the root was overwritten through a hard link: %q", survived)
	}
	written, err := os.ReadFile(link)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(written), "replaced") {
		t.Errorf("the in-root path was not updated: %q", written)
	}
}

func TestRootWriter_SubNarrowsTheBoundary(t *testing.T) {
	base := t.TempDir()
	rw, err := OpenRootWriter(filepath.Join(base, "project"))
	if err != nil {
		t.Fatal(err)
	}
	defer rw.Close()

	sub, err := rw.Sub("deploy/.claude")
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()

	if err := sub.WriteFile("agents/a.md", []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(rw.Dir(), "deploy", ".claude", "agents", "a.md")); err != nil {
		t.Errorf("Sub did not write inside the narrowed boundary: %v", err)
	}
	// The narrowed boundary is what confines writes now: an entry that
	// escapes .claude but stays in the project must still be refused,
	// otherwise Sub would be a wider boundary than the base it replaced.
	if err := sub.WriteFile("../escaped.md", []byte("x"), 0o644); err == nil {
		t.Error("Sub allowed a write that escapes the narrowed boundary")
	}
}

// TestRootWriter_RelAcceptsAnAbsolutePathInsideTheBoundary pins the shape
// conversion os.Root forces on every caller. EnsureWithinRoot accepts an
// ABSOLUTE output path that resolves inside the project -- output.go's own
// `if !filepath.IsAbs(joined)` branch exists for exactly that -- so a
// --marketplace-path override written as an absolute path passes containment.
// Handing that string straight to os.Root fails, which would refuse a path the
// containment gate had just approved. This test is what keeps Rel between them.
func TestRootWriter_RelAcceptsAnAbsolutePathInsideTheBoundary(t *testing.T) {
	root := t.TempDir()
	rw, err := OpenRootWriter(root)
	if err != nil {
		t.Fatal(err)
	}
	defer rw.Close()

	abs := filepath.Join(root, "dist", "marketplace.json")
	got, err := rw.Rel(abs)
	if err != nil {
		t.Fatalf("Rel(%q) error = %v; an absolute path inside the boundary must convert, not fail", abs, err)
	}
	if want := filepath.Join("dist", "marketplace.json"); got != want {
		t.Errorf("Rel() = %q, want %q", got, want)
	}

	// A relative path is already inside the boundary; it is only normalised,
	// so that a caller's "./build" reaches Sub as the single component os.Root
	// expects rather than a leading-dot path.
	if got, err := rw.Rel("dist/marketplace.json"); err != nil || got != filepath.Clean("dist/marketplace.json") {
		t.Errorf("Rel(relative) = %q, %v; want %q", got, err, filepath.Clean("dist/marketplace.json"))
	}
	if got, err := rw.Rel("./build"); err != nil || got != "build" {
		t.Errorf("Rel(\"./build\") = %q, %v; want %q", got, err, "build")
	}

	// Outside the boundary fails closed rather than producing a "..".
	outside := filepath.Join(filepath.Dir(root), "elsewhere", "marketplace.json")
	if _, err := rw.Rel(outside); err == nil {
		t.Errorf("Rel(%q) returned no error; a path outside the boundary must fail closed", outside)
	}
}

// TestWriteOutput_WritesToAnAbsolutePathInsideTheRoot is the end-to-end form
// of the same contract: `pack --marketplace-path claude=<abs>` must land bytes
// on disk. Before Rel existed, WriteOutput passed the absolute string to
// os.Root and every such run failed after containment had already passed.
func TestWriteOutput_WritesToAnAbsolutePathInsideTheRoot(t *testing.T) {
	root := t.TempDir()
	rw, err := OpenRootWriter(root)
	if err != nil {
		t.Fatal(err)
	}
	defer rw.Close()

	abs := filepath.Join(root, "dist", "marketplace.json")
	if err := WriteOutput(rw, abs, map[string]any{"name": "demo"}); err != nil {
		t.Fatalf("WriteOutput(absolute path inside root) error = %v", err)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		t.Fatalf("read %s: %v", abs, err)
	}
	if !strings.Contains(string(data), `"demo"`) {
		t.Errorf("file = %s, want the composed document", data)
	}
}
