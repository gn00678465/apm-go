package deploy

import (
	"bytes"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
)

// primitiveID tells apart two primitives of one name and type: those of two
// packages, and two staged files of one plugin.
type primitiveID struct {
	depKey, name, srcPath string
	typ                   PrimitiveType
}

func idOf(p Primitive) primitiveID {
	return primitiveID{depKey: p.DepKey, name: p.Name, srcPath: p.SrcPath, typ: p.Type}
}

// shadowedPrimitives lists, target by target, each dependency primitive the
// target allows that lost its name there to another dependency primitive, as
// the Oracle writes the first and finds the file there when it writes the
// next (integration/base_integrator.py:198-241). Each one says whether the
// Oracle would find on disk what it compares, and what the primitive
// deploys (Shadowed.Same, Shadowed.Deployed). A primitive the project's own
// content shadowed is left out: the Oracle writes the project's content
// last, over the dependency's file.
func shadowedPrimitives(targets []string, ordered []Primitive, targetSubsets map[string][]string, winnersByTarget map[string][]Primitive, written map[string]map[primitiveID][]string, deployDir string) []Shadowed {
	var out []Shadowed
	scratch := &renderDir{}
	defer scratch.remove()
	for _, target := range targets {
		adapter, ok := Adapters[target]
		if !ok {
			continue
		}
		winners := map[conflictKey]Primitive{}
		won := map[primitiveID]bool{}
		for _, w := range winnersByTarget[target] {
			winners[conflictKey{Name: w.Name, Type: w.Type}] = w
			won[idOf(w)] = true
		}
		for _, p := range ordered {
			if p.Source == "local" || p.Type == TypeMCP || won[idOf(p)] || !adapterSupports(adapter, p.Type) {
				continue
			}
			if subset, ok := targetSubsets[p.DepKey]; ok && !slices.Contains(subset, target) {
				continue
			}
			w, ok := winners[conflictKey{Name: p.Name, Type: p.Type}]
			if !ok || w.Source == "local" {
				continue
			}
			s := Shadowed{DepKey: p.DepKey, Type: p.Type, Name: p.Name}
			if p.Type == TypeSkills {
				if r, ok := adapter.(skillRooter); ok {
					s.Paths = []string{path.Join(r.skillRoot(w), w.Name)}
				}
			} else {
				s.Paths = written[target][idOf(w)]
			}
			if len(s.Paths) > 0 {
				s.Deployed = scratch.deploysAsOnDisk(adapter, p, s.Paths, deployDir)
				s.Same = s.Deployed
				if comparesSource(adapter, p) {
					s.Same = sourceOnDisk(p, s.Paths, deployDir)
				}
				out = append(out, s)
			}
		}
	}
	return out
}

// comparesSource tells whether the Oracle compares the file on disk with
// p's source read as text, not with what p deploys: a command, which it
// converts after the comparison (command_integrator.py:581), and a codex
// agent, which it writes as TOML (agent_integrator.py:262-283).
func comparesSource(adapter TargetAdapter, p Primitive) bool {
	if _, ok := adapter.(promptCommander); ok && (p.Type == TypePrompts || p.Type == TypeCommands) {
		return true
	}
	return adapter.Name() == "codex" && p.Type == TypeAgents
}

func sourceOnDisk(p Primitive, paths []string, deployDir string) bool {
	src, err := os.ReadFile(p.SrcPath)
	if err != nil {
		return false
	}
	want := oracleText(src)
	for _, rel := range paths {
		got, err := os.ReadFile(filepath.Join(deployDir, filepath.FromSlash(rel)))
		if err != nil || !bytes.Equal(got, want) {
			return false
		}
	}
	return true
}

// renderDir is a scratch directory a shadowed primitive is deployed into,
// with the conversions of a real deploy, to compare what it would write with
// what the winner wrote.
type renderDir struct {
	dir    string
	failed bool
}

func (r *renderDir) remove() {
	if r.dir != "" {
		_ = os.RemoveAll(r.dir)
	}
}

// deploysAsOnDisk deploys p into a fresh directory below the scratch one and
// tells whether each of paths there has the bytes, or for a skill the tree,
// of the same path below deployDir.
func (r *renderDir) deploysAsOnDisk(adapter TargetAdapter, p Primitive, paths []string, deployDir string) bool {
	if r.dir == "" && !r.failed {
		dir, err := os.MkdirTemp("", "apm-go-shadowed-")
		if err != nil {
			r.failed = true
			return false
		}
		r.dir = dir
	}
	if r.failed {
		return false
	}
	dir, err := os.MkdirTemp(r.dir, "p")
	if err != nil {
		return false
	}
	if _, _, err := deployOne(adapter, p, dir); err != nil {
		return false
	}
	for _, rel := range paths {
		rel = filepath.FromSlash(rel)
		if !sameTree(filepath.Join(dir, rel), filepath.Join(deployDir, rel)) {
			return false
		}
	}
	return true
}

// sameTree tells whether a and b, each followed through a symlink, are files
// with the same bytes, or directories with the same relative paths of files,
// each with the same bytes.
func sameTree(a, b string) bool {
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	if errA != nil || errB != nil {
		return false
	}
	fa, fb := treeFiles(ra), treeFiles(rb)
	if fa == nil || fb == nil || len(fa) != len(fb) {
		return false
	}
	for rel, data := range fa {
		other, ok := fb[rel]
		if !ok || !bytes.Equal(data, other) {
			return false
		}
	}
	return true
}

// treeFiles reads root, a file or a directory, as its files' bytes by path
// relative to it; nil when something in it cannot be read.
func treeFiles(root string) map[string][]byte {
	files := map[string][]byte{}
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(rel)] = data
		return nil
	})
	if err != nil {
		return nil
	}
	return files
}

// rootSkillsCreated tells, for each package whose root is a skill and wins
// on a target, whether the skill's directory on the first of those targets
// by name was absent before anything is written.
func rootSkillsCreated(targets []string, winnersByTarget map[string][]Primitive, deployDir string) map[string]RootSkill {
	sorted := slices.Clone(targets)
	sort.Strings(sorted)
	created := map[string]RootSkill{}
	for _, target := range sorted {
		r, ok := Adapters[target].(skillRooter)
		if !ok {
			continue
		}
		for _, w := range winnersByTarget[target] {
			if _, seen := created[w.DepKey]; seen || !w.RootSkill {
				continue
			}
			_, err := os.Lstat(filepath.Join(deployDir, filepath.FromSlash(path.Join(r.skillRoot(w), w.Name))))
			created[w.DepKey] = RootSkill{Name: w.Name, Created: os.IsNotExist(err)}
		}
	}
	return created
}
