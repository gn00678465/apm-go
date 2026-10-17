package deploy

import (
	"bytes"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"
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
			if len(s.Paths) == 0 {
				continue
			}
			s.ManagedAs = s.Paths
			if p.Type == TypeSkills {
				s.Same = skillDirsEqual(p.SrcPath, filepath.Join(deployDir, filepath.FromSlash(s.Paths[0])))
				s.Deployed = s.Same
			} else if same, deployed, text, err := shadowedContent(adapter, p); err == nil {
				s.Same = onDisk(same, s.Paths, deployDir, text)
				s.Deployed = onDisk(deployed, s.Paths, deployDir, text)
				if text {
					s.ManagedAs = oracleRels(s.Paths, deployDir)
				}
			}
			out = append(out, s)
		}
	}
	return out
}

// shadowedContent is, for the shadowed primitive p of a single file, what
// the Oracle compares the winner's file on disk with before it writes p
// (same), and what p's deploy writes there (deployed), computed as the
// deploy computes it. The Oracle compares a hook's source bytes, a claude
// rule as converted (integration/instruction_integrator.py:188,360), and any
// other primitive's source with each CRLF as LF when it is UTF-8
// (integration/base_integrator.py:355-373); a command it converts after the
// comparison (command_integrator.py:581), and a codex agent it writes as TOML
// (agent_integrator.py:262-283). text tells that p is an agent, prompt or
// instruction deployFileToPath writes (copiedAsText).
func shadowedContent(adapter TargetAdapter, p Primitive) (same, deployed []byte, text bool, err error) {
	src, err := os.ReadFile(p.SrcPath)
	if err != nil {
		return nil, nil, false, err
	}
	switch _, commander := adapter.(promptCommander); {
	case p.Type == TypeHooks:
		return src, src, false, nil
	case p.Type == TypeInstructions && adapter.Name() == "claude":
		rule, err := claudeRule(p)
		return rule, rule, false, err
	case p.Type == TypePrompts && commander:
		deployed, _, err = convertPromptToCommand(p.Name, adapter.Name(), src)
	case p.Type == TypeAgents && adapter.Name() == "codex":
		deployed, err = codexAgentTOML(p)
	case activeDeployMode.symlink:
		deployed, text = src, copiedAsText(p.Type)
	default:
		deployed, text = copiedBytes(p, src), copiedAsText(p.Type)
	}
	if text && activeDeployMode.global {
		// What the Oracle's user scope writes over the winner's copy.
		deployed = oracleText(src)
	}
	return crlfToLF(src), deployed, text, err
}

// copiedAsText tells whether deployFileToPath writes a primitive of type t
// as oracleText gives it in a project, where the Oracle's user scope also
// copies it so and a --global deploy links it or copies it with a bare CR
// kept: an agent, a prompt or an instruction.
func copiedAsText(t PrimitiveType) bool {
	return t == TypeAgents || t == TypePrompts || t == TypeInstructions
}

// crlfToLF is the Oracle's normalize_crlf_to_lf of content that decodes as
// UTF-8, which leaves a bare CR (utils/atomic_io.py:28-37), and the content
// as it is otherwise (integration/base_integrator.py:366-372).
func crlfToLF(b []byte) []byte {
	if !utf8.Valid(b) {
		return b
	}
	return bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n"))
}

// onDisk tells whether each of paths below deployDir holds want, as
// oracleFinds reads it.
func onDisk(want []byte, paths []string, deployDir string, text bool) bool {
	for _, rel := range paths {
		got, ok := oracleFinds(filepath.Join(deployDir, filepath.FromSlash(rel)), text)
		if !ok || !bytes.Equal(got, want) {
			return false
		}
	}
	return true
}

// oracleFinds is what the Oracle's comparison reads at abs, through a
// symlink, and false where it returns False: nothing there, or a file it
// cannot read (integration/base_integrator.py:337-374). For a file copied
// as text (copiedAsText) a symlink there is also False (:342-343), but for
// the link a --global deploy makes, since the Oracle's user scope copies
// the file. That copy is oracleText of the source, so at --global the
// Oracle's file is oracleText of what the link or apm-go's copy holds; in a
// project the Oracle compares the bytes on disk as they are, so a file the
// user turned to CRLF is not the same.
func oracleFinds(abs string, text bool) ([]byte, bool) {
	if text {
		info, err := os.Lstat(abs)
		if err != nil || info.Mode()&os.ModeSymlink != 0 && !(activeDeployMode.global && isDeployLink(abs)) {
			return nil, false
		}
	}
	got, err := os.ReadFile(abs)
	if err != nil {
		return nil, false
	}
	if text && activeDeployMode.global {
		return oracleText(got), true
	}
	return got, true
}

// oracleRels are paths below deployDir as the Oracle looks them up in its
// managed set: a symlink resolved, as portable_relpath resolves it
// (utils/paths.py:14-29), and the absolute path, which no lock records,
// when it leads out of deployDir. The link a --global deploy makes stays,
// since the Oracle copies the file there.
func oracleRels(paths []string, deployDir string) []string {
	out := make([]string, len(paths))
	for i, rel := range paths {
		out[i] = rel
		abs := filepath.Join(deployDir, filepath.FromSlash(rel))
		if info, err := os.Lstat(abs); err != nil || info.Mode()&os.ModeSymlink == 0 || activeDeployMode.global && isDeployLink(abs) {
			continue
		}
		real, err := filepath.EvalSymlinks(abs)
		if err != nil {
			continue
		}
		out[i] = filepath.ToSlash(real)
		if root, err := filepath.EvalSymlinks(deployDir); err == nil {
			if r, err := filepath.Rel(root, real); err == nil && r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator)) {
				out[i] = filepath.ToSlash(r)
			}
		}
	}
	return out
}

// dircmpIgnores is Python 3.12's filecmp.DEFAULT_IGNORES.
var dircmpIgnores = []string{"RCS", "CVS", "tags", ".git", ".hg", ".bzr", "_darcs", "__pycache__"}

// skillDirsEqual is the Oracle's is_skill_dir_identical_to_source
// (integration/skill_integrator.py:531-550) over Python 3.12's
// filecmp.dircmp: at every level, the names of a and b less dircmpIgnores
// are the same, files of one name have the same bytes, and directories of
// one name are equal, each entry followed through a symlink (os.stat). A
// name that is a file on one side and a directory on the other, or that
// cannot be stat'ed, is dircmp's common_funny, which _dircmp_equal does not
// look at, so it does not make the two unequal. That also ends a symlink
// loop: once the path holds 40 symlinks, stat fails with ELOOP, so the
// Oracle returns True for loopprobe (review122-step27). A directory that
// cannot be listed or read makes them unequal, where the Oracle raises.
func skillDirsEqual(a, b string) bool {
	left, errA := dircmpNames(a)
	right, errB := dircmpNames(b)
	if errA != nil || errB != nil || !slices.Equal(left, right) {
		return false
	}
	for _, name := range left {
		pa, pb := filepath.Join(a, name), filepath.Join(b, name)
		ia, errA := os.Stat(pa)
		ib, errB := os.Stat(pb)
		if errA != nil || errB != nil || ia.Mode().Type() != ib.Mode().Type() {
			continue
		}
		switch {
		case ia.IsDir():
			if !skillDirsEqual(pa, pb) {
				return false
			}
		case ia.Mode().IsRegular():
			da, errA := os.ReadFile(pa)
			db, errB := os.ReadFile(pb)
			if errA != nil || errB != nil || !bytes.Equal(da, db) {
				return false
			}
		}
	}
	return true
}

// dircmpNames lists dir as dircmp's phase0 does, sorted, less dircmpIgnores.
func dircmpNames(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if !slices.Contains(dircmpIgnores, e.Name()) {
			names = append(names, e.Name())
		}
	}
	return names, nil
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
