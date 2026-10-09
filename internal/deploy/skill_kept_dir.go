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
	_ = filepath.WalkDir(destDir, func(p string, d fs.DirEntry, err error) error {
		rel, relErr := filepath.Rel(destDir, p)
		if relErr != nil {
			rel = p
		}
		if err == nil && d.IsDir() {
			return nil
		}
		if err == nil && d.Type().IsRegular() && sameRegularFile(p, filepath.Join(srcDir, rel)) {
			return nil
		}
		mismatch, ok = filepath.ToSlash(rel), false
		return filepath.SkipAll
	})
	return mismatch, ok
}

// sameRegularFile reports whether a and b are regular files with the same
// bytes. Neither path is followed when it is a symlink.
func sameRegularFile(a, b string) bool {
	infoA, err := os.Lstat(a)
	if err != nil || !infoA.Mode().IsRegular() {
		return false
	}
	infoB, err := os.Lstat(b)
	if err != nil || !infoB.Mode().IsRegular() || infoA.Size() != infoB.Size() {
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
	for {
		nA, errA := io.ReadFull(fa, bufA)
		nB, errB := io.ReadFull(fb, bufB)
		if nA != nB || !bytes.Equal(bufA[:nA], bufB[:nB]) {
			return false
		}
		endA := errA == io.EOF || errA == io.ErrUnexpectedEOF
		endB := errB == io.EOF || errB == io.ErrUnexpectedEOF
		if endA && endB {
			return true
		}
		if errA != nil || errB != nil {
			return false
		}
	}
}
