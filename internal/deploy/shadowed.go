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
// next (integration/base_integrator.py:198-241). A primitive the project's
// own content shadowed is left out: the Oracle writes the project's content
// last, over the dependency's file.
func shadowedPrimitives(targets []string, ordered []Primitive, targetSubsets map[string][]string, winnersByTarget map[string][]Primitive, written map[string]map[primitiveID][]string) []Shadowed {
	var out []Shadowed
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
				// codex writes an agent as TOML, which never has its
				// source's bytes, so the Oracle never adopts it there.
				s.Same = target != "codex" && sameBytes(p.SrcPath, w.SrcPath)
			}
			if len(s.Paths) > 0 {
				out = append(out, s)
			}
		}
	}
	return out
}

func sameBytes(a, b string) bool {
	da, errA := os.ReadFile(a)
	db, errB := os.ReadFile(b)
	return errA == nil && errB == nil && bytes.Equal(da, db)
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
