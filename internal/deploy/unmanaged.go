package deploy

import (
	"bytes"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/apm-go/apm/internal/lockfile"
)

// Managed is what a project deploy may write over (issue #144): Paths, every
// deploy-root-relative path the lock before the run records, for any bucket,
// as the Oracle's managed set (install/pipeline.py:723-728), and Root, the
// deploy root whose files decide whether a path is taken. Root is the real
// one also for a trial deploy into a temporary directory, so the trial skips
// what the deploy in place skips.
type Managed struct {
	Paths map[string]bool
	Root  string
	// NoAdopt leaves what would be adopted as a path left to the user: a
	// frozen install writes no lock, so an adopted path would be one the
	// lock does not have, and its check of the deploy against the lock
	// would fail on a file the user put there.
	NoAdopt bool
}

// placement is what a project deploy does at a path it is about to write.
type placement int

const (
	placeWrite placement = iota
	// placeAdopt leaves the file or skill directory there, which is what the
	// Oracle compares with, and records it as deployed.
	placeAdopt
	// placeSkip leaves a file the user put there and records nothing.
	placeSkip
)

// unmanagedGuard is the Oracle's _check_adopt_or_skip for one Run
// (integration/base_integrator.py:198-241,429-495).
type unmanagedGuard struct {
	managed Managed
	// decided memoizes each path's placement, so a directory that several
	// targets share (sharedSkillRoot) and a path this run already wrote,
	// which the Oracle adds to its managed set (install/phases/
	// integrate.py:328-336), are decided once.
	decided map[string]placement
	// hashes holds the lock hash of each adopted file, read below Root.
	hashes map[string]string
	// skipped holds, by bucket, each path left to the user.
	skipped map[string][]string
}

func newUnmanagedGuard(m *Managed) *unmanagedGuard {
	if m == nil {
		return nil
	}
	return &unmanagedGuard{managed: *m, decided: map[string]placement{}, hashes: map[string]string{}}
}

func (g *unmanagedGuard) abs(rel string) string {
	return filepath.Join(g.managed.Root, filepath.FromSlash(rel))
}

// managedDir tells whether the lock records dir or a path below it: a skill
// directory one package deployed.
func (g *unmanagedGuard) managedDir(dir string) bool {
	if g.managed.Paths[dir] {
		return true
	}
	for p := range g.managed.Paths {
		if strings.HasPrefix(p, dir+"/") {
			return true
		}
	}
	return false
}

// file decides the placement of the single file destPath for the bucket
// depKey. want is what the Oracle compares a file there with: the source
// with each CRLF as LF; nil for a hook, which it never adopts
// (hook_integrator.py:1070). A symlink there is never the same
// (base_integrator.py:342-343).
func (g *unmanagedGuard) file(depKey, destPath string, want []byte) placement {
	if g == nil {
		return placeWrite
	}
	if d, ok := g.decided[destPath]; ok {
		return d
	}
	d := placeWrite
	abs := g.abs(destPath)
	if _, err := os.Stat(abs); err == nil && !g.managed.Paths[destPath] {
		d = placeSkip
		if info, err := os.Lstat(abs); want != nil && err == nil && info.Mode()&os.ModeSymlink == 0 {
			got, err := os.ReadFile(abs)
			if h, herr := lockfile.HashDeployedFile(abs); err == nil && herr == nil && bytes.Equal(got, want) {
				g.hashes[destPath] = h
				d = placeAdopt
			}
		}
	}
	return g.note(depKey, destPath, d)
}

// skill decides the placement of the skill directory destDir, whose files
// below it are rels: adopted when same finds it the same (skillSameOnDisk),
// each file then recorded with its hash below Root.
func (g *unmanagedGuard) skill(depKey, destDir string, rels []string, same func(abs string) bool) placement {
	if g == nil {
		return placeWrite
	}
	if d, ok := g.decided[destDir]; ok {
		return d
	}
	d := placeWrite
	abs := g.abs(destDir)
	if _, err := os.Stat(abs); err == nil && !g.managedDir(destDir) {
		d = placeSkip
		if info, err := os.Lstat(abs); err == nil && info.Mode()&os.ModeSymlink == 0 && same(abs) {
			hashes := map[string]string{}
			for _, rel := range rels {
				f := path.Join(destDir, rel)
				h, err := lockfile.HashDeployedFile(g.abs(f))
				if err != nil {
					hashes = nil
					break
				}
				hashes[f] = h
			}
			if hashes != nil {
				for f, h := range hashes {
					g.hashes[f] = h
				}
				d = placeAdopt
			}
		}
	}
	return g.note(depKey, destDir, d)
}

func (g *unmanagedGuard) note(depKey, rel string, d placement) placement {
	if d == placeAdopt && g.managed.NoAdopt {
		d = placeSkip
	}
	g.decided[rel] = d
	if d == placeSkip {
		if g.skipped == nil {
			g.skipped = map[string][]string{}
		}
		g.skipped[depKey] = append(g.skipped[depKey], rel)
	}
	return d
}

// hash is the lock hash of the deployed file rel: the adopted file's below
// Root, else the file's below deployDir.
func (g *unmanagedGuard) hash(deployDir, rel string) (string, error) {
	if g != nil {
		if h, ok := g.hashes[rel]; ok {
			return h, nil
		}
	}
	return lockfile.HashDeployedFile(filepath.Join(deployDir, rel))
}

// placeFile decides, for a project deploy, whether p's deploy writes the
// single file destPath. A hook that is not copilot's own file
// (.codex/hooks.json, antigravity's hooks.json) is a file several primitives
// merge into, never one the user put there, and is always written. An
// adopted agent, prompt or instruction counts as adopted (noteAdopted).
func placeFile(p Primitive, destPath string) placement {
	g := activeDeployMode.unmanaged
	if g == nil || p.Type == TypeHooks && !strings.HasPrefix(destPath, ".github/hooks/") {
		return placeWrite
	}
	var want []byte
	if p.Type != TypeHooks {
		src, err := os.ReadFile(p.SrcPath)
		if err != nil {
			// The deploy reports it.
			return placeWrite
		}
		want = comparedContent(p, destPath, src)
	}
	d := g.file(p.DepKey, destPath, want)
	if d == placeAdopt && copiedAsText(p.Type) {
		activeDeployMode.adopted[destPath] = true
	}
	return d
}
