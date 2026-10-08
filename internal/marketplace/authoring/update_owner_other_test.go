//go:build !windows

package authoring

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/apm-go/apm/internal/semver"
)

// otherGroup returns a group the process belongs to that is not the group
// new files get, which is what a config file shared through a group has.
func otherGroup(t *testing.T) int {
	t.Helper()
	groups, err := os.Getgroups()
	if err != nil {
		t.Fatal(err)
	}
	for _, gid := range groups {
		if gid != os.Getegid() {
			return gid
		}
	}
	t.Skip("the process belongs to one group only, so no file can have a group that differs from a new file's")
	return 0
}

func ownerOf(t *testing.T, path string) (uid, gid uint32, perm os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	st := info.Sys().(*syscall.Stat_t)
	return st.Uid, st.Gid, info.Mode().Perm()
}

// groupSharedConfig writes a one-entry apm.yml in a new directory, owned by
// otherGroup with mode 0640, and returns the directory and the content.
func groupSharedConfig(t *testing.T) (dir, content string, gid int) {
	t.Helper()
	gid = otherGroup(t)
	dir = t.TempDir()
	content = apmYML("    - name: tool\n      source: owner/tool\n      ref: " + shaA + "\n")
	writeFile(t, dir, "apm.yml", content)
	path := filepath.Join(dir, "apm.yml")
	if err := os.Chown(path, -1, gid); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	return dir, content, gid
}

func TestPackageUpdate_KeepsTheConfigFileGroup(t *testing.T) {
	dir, _, gid := groupSharedConfig(t)
	lister := &sourceLister{refs: map[string][]semver.TagInfo{"owner/tool": {headRef(shaB)}}}

	if _, err := updatePackages(t, dir, nil, false, lister); err != nil {
		t.Fatalf("update: %v", err)
	}

	assertFile(t, dir, "apm.yml", apmYML("    - name: tool\n      source: owner/tool\n      ref: "+shaB+"\n"))
	uid, gotGid, perm := ownerOf(t, filepath.Join(dir, "apm.yml"))
	if int(uid) != os.Geteuid() || int(gotGid) != gid || perm != 0o640 {
		t.Errorf("after the update: uid %d gid %d mode %v, want uid %d gid %d mode -rw-r-----", uid, gotGid, perm, os.Geteuid(), gid)
	}
	assertNoTempFiles(t, dir)
}

// recordChown replaces the chown seam for the test and returns the calls it
// saw; each call returns err without touching the file.
func recordChown(t *testing.T, err error) *[][2]int {
	t.Helper()
	calls := &[][2]int{}
	orig := chownFile
	chownFile = func(_ *os.File, uid, gid int) error {
		*calls = append(*calls, [2]int{uid, gid})
		return err
	}
	t.Cleanup(func() { chownFile = orig })
	return calls
}

func TestPackageUpdate_OwnerAndGroupCannotBeKept_NothingWritten(t *testing.T) {
	dir, content, gid := groupSharedConfig(t)
	mtime := backdate(t, dir, "apm.yml")
	calls := recordChown(t, errors.New("operation not permitted"))
	lister := &sourceLister{refs: map[string][]semver.TagInfo{"owner/tool": {headRef(shaB)}}}

	_, err := updatePackages(t, dir, nil, false, lister)

	path := filepath.Join(dir, "apm.yml")
	if want := "keep owner and group of " + path + ": operation not permitted"; err == nil || err.Error() != want {
		t.Fatalf("error = %v, want %s", err, want)
	}
	if want := [][2]int{{os.Geteuid(), gid}}; len(*calls) != 1 || (*calls)[0] != want[0] {
		t.Errorf("chown calls = %v, want %v", *calls, want)
	}
	assertNotWritten(t, dir, "apm.yml", content, mtime)
	uid, gotGid, perm := ownerOf(t, path)
	if int(uid) != os.Geteuid() || int(gotGid) != gid || perm != 0o640 {
		t.Errorf("after the failed update: uid %d gid %d mode %v, want uid %d gid %d mode -rw-r-----", uid, gotGid, perm, os.Geteuid(), gid)
	}
}

func TestPackageUpdate_OwnerAndGroupAlreadyMatch_NoChown(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", apmYML("    - name: tool\n      source: owner/tool\n      ref: "+shaA+"\n"))
	calls := recordChown(t, errors.New("chown must not be called"))
	lister := &sourceLister{refs: map[string][]semver.TagInfo{"owner/tool": {headRef(shaB)}}}

	if _, err := updatePackages(t, dir, nil, false, lister); err != nil {
		t.Fatalf("update: %v", err)
	}

	if len(*calls) != 0 {
		t.Errorf("chown calls = %v, want none", *calls)
	}
	assertFile(t, dir, "apm.yml", apmYML("    - name: tool\n      source: owner/tool\n      ref: "+shaB+"\n"))
}
