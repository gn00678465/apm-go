package main

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/yamlcore"
)

// A refresh of a shared local slot leaves the disk and the lock that an
// install with the same declarations and targets gives. settled checks it
// after an uninstall: (a) an install then changes neither the lock nor any
// deployed file; (b) with the target directories and apm_modules/ deleted, a
// frozen install exits 0 and gives the files the lock records as (a) left
// them; (c) the uninstall wrote nothing outside the target roots install
// writes to.
type settled struct {
	deployRoot, manifestDir string
	global                  bool
	// args are the install flags the project is installed with
	// (--target claude,copilot when no targets: names them).
	args []string
	// roots are the target roots install writes to, by slash prefix.
	roots []string
	// restored are recorded paths the user removed before the uninstall and
	// the refresh does not decide, which (a)'s install puts back.
	restored []string
	before   map[string]string
}

// settledTargetDirs are the directories a deploy writes below the deploy
// root, every target's.
var settledTargetDirs = []string{".claude", ".github", ".agents", ".codex", ".opencode", ".copilot", ".config/opencode"}

// start records the deployed entries before the uninstall.
func (c *settled) start(t *testing.T) {
	t.Helper()
	if c.roots == nil {
		c.roots = []string{".claude/"}
	}
	c.before = deployedEntries(t, c.deployRoot)
}

// deployedEntries maps each file and symlink below the target directories of
// root to its content hash or its link target; a symlink is not followed.
func deployedEntries(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, dir := range settledTargetDirs {
		base := filepath.Join(root, filepath.FromSlash(dir))
		if _, err := os.Lstat(base); os.IsNotExist(err) {
			continue
		}
		err := filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(root, p)
			rel = filepath.ToSlash(rel)
			switch {
			case d.Type()&fs.ModeSymlink != 0:
				target, err := os.Readlink(p)
				if err != nil {
					return err
				}
				out[rel] = "link " + target
			case d.Type().IsRegular():
				data, err := os.ReadFile(p)
				if err != nil {
					return err
				}
				sum := sha256.Sum256(data)
				out[rel] = hex.EncodeToString(sum[:])
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return out
}

func (c *settled) install(t *testing.T, extra ...string) {
	t.Helper()
	args := []string{"install"}
	if c.global {
		args = append(args, "-g")
	}
	args = append(append(args, c.args...), extra...)
	if stdout, stderr, code := runCompileCLI(t, args...); code != 0 {
		t.Fatalf("%v: exit %d, stderr %q, stdout:\n%s", args, code, stderr, stdout)
	}
}

func (c *settled) lockText(t *testing.T) string {
	t.Helper()
	return readFileString(t, filepath.Join(c.manifestDir, "apm.lock.yaml"))
}

// check runs (a), (b) and (c) after the uninstall.
func (c *settled) check(t *testing.T) {
	t.Helper()
	after := deployedEntries(t, c.deployRoot)
	for p := range after {
		if _, ok := c.before[p]; !ok && !slices.ContainsFunc(c.roots, func(r string) bool { return strings.HasPrefix(p, r) }) {
			t.Errorf("(c) the uninstall wrote %s, outside the target roots %v of install", p, c.roots)
		}
	}
	lock := c.lockText(t)
	c.install(t)
	if got := c.lockText(t); got != lock {
		t.Errorf("(a) install changed apm.lock.yaml:\n%s\nwas:\n%s", got, lock)
	}
	restoredAt := func(p string) bool {
		return slices.ContainsFunc(c.restored, func(r string) bool { return p == r || strings.HasPrefix(p, r+"/") })
	}
	got := deployedEntries(t, c.deployRoot)
	maps.DeleteFunc(got, func(p, _ string) bool { return restoredAt(p) })
	if !maps.Equal(got, after) {
		t.Errorf("(a) install changed the deployed files:\n%v\nwas:\n%v", got, after)
	}
	recorded := recordedContent(t, lock, c.deployRoot)
	for _, dir := range append(settledTargetDirs, "apm_modules") {
		base := c.deployRoot
		if dir == "apm_modules" {
			base = c.manifestDir
		}
		if err := os.RemoveAll(filepath.Join(base, filepath.FromSlash(dir))); err != nil {
			t.Fatal(err)
		}
	}
	c.install(t, "--frozen")
	if got := c.lockText(t); got != lock {
		t.Errorf("(b) install --frozen changed apm.lock.yaml:\n%s\nwas:\n%s", got, lock)
	}
	if got := recordedContent(t, lock, c.deployRoot); !maps.Equal(got, recorded) {
		t.Errorf("(b) install --frozen gave the recorded files:\n%v\nwant:\n%v", got, recorded)
	}
	paths := settledLockPaths(t, lock)
	for p := range deployedEntries(t, c.deployRoot) {
		if !slices.ContainsFunc(paths, func(q string) bool { return p == q || strings.HasPrefix(p, q+"/") }) {
			t.Errorf("(b) install --frozen wrote %s, which the lock does not record", p)
		}
	}
}

// settledLockPaths is every deployed path the lock records.
func settledLockPaths(t *testing.T, lock string) []string {
	t.Helper()
	node, err := yamlcore.SafeLoad([]byte(lock))
	if err != nil {
		t.Fatal(err)
	}
	lf, err := lockfile.ParseLockfile(node)
	if err != nil {
		t.Fatal(err)
	}
	paths := slices.Clone(lf.LocalDeployedFiles)
	for i := range lf.Dependencies {
		paths = append(paths, lf.Dependencies[i].DeployedFiles...)
	}
	return paths
}

// recordedContent maps each file the lock records below root to the hash of
// its content, read through any symlink, or "missing".
func recordedContent(t *testing.T, lock, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, p := range settledLockPaths(t, lock) {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(p)))
		switch {
		case err == nil:
			sum := sha256.Sum256(data)
			out[p] = hex.EncodeToString(sum[:])
		case os.IsNotExist(err):
			out[p] = "missing"
		}
	}
	return out
}
