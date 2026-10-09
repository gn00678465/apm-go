package deploy

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// keptSkillDirError is symlinkSkillTo's refusal to replace a real directory
// (issue #43). It is not a deploy failure: the source was read in full, so
// Run records the directory in DeployResult.KeptDirs and not in FailedBuckets.
type keptSkillDirError struct {
	dir      string // deploy-root-relative, slash-separated
	mismatch string // first entry the source does not match, relative to dir
}

func (e *keptSkillDirError) Error() string {
	return fmt.Sprintf("keeping %q: the directory holds %q, which this package does not have with the same content", e.dir, e.mismatch)
}

// UnderKeptDir reports whether relPath is one of keptDirs
// (DeployResult.KeptDirs) or a path below one.
func UnderKeptDir(keptDirs []string, relPath string) bool {
	p := normalizeRelPath(relPath)
	for _, dir := range keptDirs {
		dir = normalizeRelPath(dir)
		if p == dir || strings.HasPrefix(p, dir+"/") {
			return true
		}
	}
	return false
}

// firstEntryNotInSource returns the first entry of destDir, in WalkDir order
// and slash-separated relative to destDir, that is not a regular file srcDir
// has at the same relative path with the same bytes. ok is true when there is
// none, so deleting destDir loses nothing the source does not have.
// Directories and file modes are not compared. Anything that cannot be read
// counts as a mismatch: the caller deletes destDir on ok.
func firstEntryNotInSource(destDir, srcDir string) (mismatch string, ok bool) {
	ok = true
	_ = fs.WalkDir(os.DirFS(destDir), ".", func(rel string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			return nil
		}
		name := filepath.FromSlash(rel)
		if err == nil && sameRegularFile(filepath.Join(destDir, name), filepath.Join(srcDir, name)) {
			return nil
		}
		mismatch, ok = rel, false
		return fs.SkipAll
	})
	return mismatch, ok
}

// sameRegularFile reports whether a and b are regular files with the same
// bytes. Neither path is followed when it is a symlink.
func sameRegularFile(a, b string) bool {
	infoA, errA := os.Lstat(a)
	infoB, errB := os.Lstat(b)
	if errA != nil || errB != nil || !infoA.Mode().IsRegular() || !infoB.Mode().IsRegular() || infoA.Size() != infoB.Size() {
		return false
	}
	fa, err := os.Open(a)
	if err != nil {
		return false
	}
	defer fa.Close()
	fb, err := os.Open(b)
	if err != nil {
		return false
	}
	defer fb.Close()

	const chunk = 64 * 1024
	bufA, bufB := make([]byte, chunk), make([]byte, chunk)
	atEnd := func(err error) bool { return err == io.EOF || err == io.ErrUnexpectedEOF }
	for {
		nA, errA := io.ReadFull(fa, bufA)
		nB, errB := io.ReadFull(fb, bufB)
		if nA != nB || !bytes.Equal(bufA[:nA], bufB[:nB]) {
			return false
		}
		if errA != nil || errB != nil {
			return atEnd(errA) && atEnd(errB)
		}
	}
}
