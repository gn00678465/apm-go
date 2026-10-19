package deploy

import (
	"io/fs"
	"path"
	"runtime"
	"strings"
)

// skillCopyRules is what a deploy of one skill leaves out, as the Oracle copies
// a skill with shutil.copytree and an ignore callback
// (integration/skill_integrator.py:73-94,370-374,877-885,1203-1212) and lists
// it in the lock from what that copy wrote (install/deployed_paths.py:85-106).
type skillCopyRules struct {
	// rootGit leaves out the .git of a cloned package's root (#113).
	rootGit bool
	// root is a package's root skill (Primitive.RootSkill).
	root bool
	// plugin is a Claude plugin's root skill (Primitive.PluginRoot).
	plugin bool
}

func skillCopyRulesOf(p Primitive) skillCopyRules {
	return skillCopyRules{rootGit: p.SkipRootGit, root: p.RootSkill, plugin: p.PluginRoot}
}

// ignores is the ignore callback, applied to every directory of the copy:
// the cache marker and the Python bytecode of every skill
// (security/gate.py:309-311,332-353), and for a root skill every .apm, and for
// a Claude plugin's every apm.yml (shutil.ignore_patterns, which compares as
// the file system does: without case on Windows). A symlink, which the
// callback also drops, is walked as skillLinks says (issue #89, #118).
func (f skillCopyRules) ignores(name string) bool {
	if name == ".apm-pin" || name == "__pycache__" {
		return true
	}
	if s := pySuffix(name); s == ".pyc" || s == ".pyo" {
		return true
	}
	return f.root && patternName(name, ".apm") || f.plugin && patternName(name, "apm.yml")
}

// authorizes tells whether the file rel, below the skill, is in the Oracle's
// deployable source plan (install/deployable_source_plan.py:187-205,242-255):
// for a root skill every file but those below the first .apm, every .apm-pin,
// and a plugin's apm.yml beside SKILL.md; for any other skill every file. The
// copy makes only the directories that hold such a file
// (DeployableSourcePlan.copy_ignore, :284-297), so an empty directory is not
// made, and one whose only file the callback drops is.
func (f skillCopyRules) authorizes(rel string) bool {
	if !f.root {
		return true
	}
	first, _, _ := strings.Cut(rel, "/")
	return first != ".apm" && path.Base(rel) != ".apm-pin" && !(f.plugin && rel == "apm.yml")
}

// pySuffix is pathlib's PurePath.suffix of a file name.
func pySuffix(name string) string {
	i := strings.LastIndexByte(name, '.')
	if 0 < i && i < len(name)-1 {
		return name[i:]
	}
	return ""
}

func patternName(name, pattern string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(name, pattern)
	}
	return name == pattern
}

// walkSkillDir calls visit for each entry of the skill at srcDir that its
// deploy writes, a directory before what is in it, with its slash-separated
// path below srcDir, and a symlink as links says.
func walkSkillDir(srcDir string, links skillLinks, f skillCopyRules, visit func(src, rel string, isDir bool) error) error {
	planned := map[string]bool{}
	err := walkSkillTree(srcDir, "", links, f.rootGit, func(_, rel string, isDir bool) error {
		if !isDir && f.authorizes(rel) {
			for p := rel; p != "."; p = path.Dir(p) {
				planned[p] = true
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	return walkSkillTree(srcDir, "", links, f.rootGit, func(src, rel string, isDir bool) error {
		if !planned[rel] || f.ignores(path.Base(rel)) {
			return fs.SkipDir
		}
		return visit(src, rel, isDir)
	})
}
