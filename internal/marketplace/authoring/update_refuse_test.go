package authoring

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The write is a rename, which replaces the whole file. A config file a
// rename cannot stand in for is refused before anything is replaced, by a
// real run and by a dry run alike.

const refusedConfig = "    - name: tool\n      source: owner/tool\n      ref: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n"

func refusedPlan() []PackageUpdate {
	return []PackageUpdate{{
		Index:   0,
		Package: PackageEntry{Name: "tool", Source: "owner/tool", Ref: shaA},
		Action:  UpdateApply,
		NewRef:  shaB,
	}}
}

// symlinkTo makes dir/name a symbolic link to target (a name in dir).
func symlinkTo(t *testing.T, dir, name, target string) {
	t.Helper()
	if err := os.Symlink(target, filepath.Join(dir, name)); err != nil {
		t.Skipf("cannot create a symbolic link here: %v", err)
	}
}

// assertBothRunsRefuse runs the plan as a dry run and as a real run and
// wants the same literal error from both.
func assertBothRunsRefuse(t *testing.T, dir, want string) {
	t.Helper()
	for _, dryRun := range []bool{true, false} {
		err := ApplyPackageUpdates(dir, refusedPlan(), dryRun)
		if err == nil || err.Error() != want {
			t.Errorf("dryRun=%v: error = %v\nwant %s", dryRun, err, want)
		}
	}
	assertNoTempFiles(t, dir)
}

func assertStillSymlink(t *testing.T, path, target string) {
	t.Helper()
	got, err := os.Readlink(path)
	if err != nil || got != target {
		t.Errorf("Readlink(%s) = %q, %v; want the link to %q to be left as it was", path, got, err, target)
	}
}

func TestApplyPackageUpdates_SymlinkedConfig_Refused(t *testing.T) {
	dir := t.TempDir()
	content := apmYML(refusedConfig)
	writeFile(t, dir, "shared.yml", content)
	mtime := backdate(t, dir, "shared.yml")
	symlinkTo(t, dir, "apm.yml", "shared.yml")
	path := filepath.Join(dir, "apm.yml")

	assertBothRunsRefuse(t, dir, "cannot update "+path+" in place: it is a symbolic link; edit the file it points to by hand")

	assertStillSymlink(t, path, "shared.yml")
	assertNotWritten(t, dir, "shared.yml", content, mtime)
}

func TestApplyPackageUpdates_SymlinkedLegacyConfig_Refused(t *testing.T) {
	dir := t.TempDir()
	content := "name: demo\nversion: 0.1.0\nowner:\n  name: me\npackages:\n  - name: tool\n    source: owner/tool\n    ref: " + shaA + "\n"
	writeFile(t, dir, "shared.yml", content)
	mtime := backdate(t, dir, "shared.yml")
	symlinkTo(t, dir, "marketplace.yml", "shared.yml")
	path := filepath.Join(dir, "marketplace.yml")

	assertBothRunsRefuse(t, dir, "cannot update "+path+" in place: it is a symbolic link; edit the file it points to by hand")

	assertStillSymlink(t, path, "shared.yml")
	assertNotWritten(t, dir, "shared.yml", content, mtime)
}

func TestApplyPackageUpdates_SymlinkedConfig_NothingToUpdate_NoError(t *testing.T) {
	dir := t.TempDir()
	content := apmYML(refusedConfig)
	writeFile(t, dir, "shared.yml", content)
	mtime := backdate(t, dir, "shared.yml")
	symlinkTo(t, dir, "apm.yml", "shared.yml")
	skipped := []PackageUpdate{{Index: 0, Package: PackageEntry{Name: "tool", Source: "owner/tool", Ref: shaA}, Note: "already up to date"}}

	for _, dryRun := range []bool{true, false} {
		if err := ApplyPackageUpdates(dir, skipped, dryRun); err != nil {
			t.Errorf("dryRun=%v: error = %v, want nil", dryRun, err)
		}
	}

	assertStillSymlink(t, filepath.Join(dir, "apm.yml"), "shared.yml")
	assertNotWritten(t, dir, "shared.yml", content, mtime)
}

func TestApplyPackageUpdates_ReadOnlyConfig_Refused(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can open a 0444 file for writing, so the file is not read-only to this process")
	}
	dir := t.TempDir()
	content := apmYML(refusedConfig)
	writeFile(t, dir, "apm.yml", content)
	path := filepath.Join(dir, "apm.yml")
	if err := os.Chmod(path, 0o444); err != nil {
		t.Fatal(err)
	}
	mtime := backdate(t, dir, "apm.yml")

	for _, dryRun := range []bool{true, false} {
		err := ApplyPackageUpdates(dir, refusedPlan(), dryRun)
		// The reason is the operating system's text for the failed open.
		if prefix := "cannot update " + path + " in place: open " + path + ": "; err == nil || !strings.HasPrefix(err.Error(), prefix) {
			t.Errorf("dryRun=%v: error = %v\nwant one that starts with %s", dryRun, err, prefix)
		}
	}

	assertNotWritten(t, dir, "apm.yml", content, mtime)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o444 {
		t.Errorf("mode = %v, want -r--r--r--", got)
	}
}

func TestCheckReplaceableByRename_NotARegularFile(t *testing.T) {
	dir := t.TempDir()
	t.Run("a directory", func(t *testing.T) {
		err := checkReplaceableByRename(dir)
		if want := "cannot update " + dir + " in place: it is not a regular file"; err == nil || err.Error() != want {
			t.Errorf("error = %v\nwant %s", err, want)
		}
	})
	t.Run("nothing at the path", func(t *testing.T) {
		path := filepath.Join(dir, "apm.yml")
		err := checkReplaceableByRename(path)
		// The operation name in the reason is the operating system's (lstat,
		// GetFileAttributesEx on Windows), so only the cause is compared.
		if prefix := "cannot update " + path + " in place: "; !errors.Is(err, fs.ErrNotExist) || !strings.HasPrefix(err.Error(), prefix) {
			t.Errorf("error = %v\nwant a not-exist error that starts with %s", err, prefix)
		}
	})
}
