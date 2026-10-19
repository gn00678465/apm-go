package deploy

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
)

// junkSkill writes into base/rel every kind of entry the Oracle's skill copy
// leaves out, as ~/.cache/apm-go-verify/issue126/skex/scripts/mkskill.sh did
// for the Oracle runs the tests below take their expected trees from.
func junkSkill(t *testing.T, base, rel string) {
	t.Helper()
	for name, body := range map[string]string{
		"__pycache__/m.cpython-312.pyc": "pyc\n",
		"__pycache__/notes.txt":         "txt\n",
		"loose.pyc":                     "pyc\n",
		"loose.pyo":                     "pyo\n",
		".apm-pin":                      `{"schema_version": 1, "resolved_commit": "x"}` + "\n",
		"onlypin/.apm-pin":              "pin\n",
		"dir.pyc/inside.txt":            "in dir named .pyc\n",
		"x.pyc.d/keep.txt":              "keep\n",
		".apm/nested.txt":               "nested apm\n",
		"keep.txt":                      "keep\n",
		"apm.yml":                       "name: inner\nversion: 1.0.0\n",
	} {
		mkFile(t, base, rel+"/"+name, body)
	}
	if err := os.MkdirAll(filepath.Join(base, filepath.FromSlash(rel), "emptydir"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func skillMD(name string) string {
	return "---\nname: " + name + "\ndescription: " + name + "\n---\n" + name + "\n"
}

// entriesBelow lists every file and directory at and below dir/rel, sorted.
func entriesBelow(t *testing.T, dir, rel string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(filepath.Join(dir, rel), func(p string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		r, _ := filepath.Rel(dir, p)
		out = append(out, filepath.ToSlash(r))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(out)
	return out
}

// skexProject is the project of the Oracle run skex/p2 less rs/skills/rsk,
// which apm-go also deploys as a skill of its own (a difference outside
// issue #126): the root skill rs with a sub-skill in .apm/skills, the APM
// package ap with .apm/skills/s2, and the project's own skill.
func skexProject(t *testing.T, dir string) {
	rs := filepath.Join(dir, "apm_modules", "_local", "rs")
	mkFile(t, rs, "SKILL.md", skillMD("rs"))
	junkSkill(t, rs, "scripts")
	mkFile(t, rs, ".apm/skills/rsub/SKILL.md", skillMD("rsub"))
	junkSkill(t, rs, ".apm/skills/rsub")
	mkFile(t, rs, "top.pyc", "pyc\n")
	mkFile(t, rs, ".apm-pin", "pin\n")
	mkFile(t, rs, "__pycache__/t.pyc", "pyc\n")
	mkFile(t, rs, ".apm/top.txt", "top apm file\n")
	if err := os.MkdirAll(filepath.Join(rs, "topempty"), 0o755); err != nil {
		t.Fatal(err)
	}
	ap := filepath.Join(dir, "apm_modules", "_local", "ap")
	mkFile(t, ap, "apm.yml", "name: ap\nversion: 1.0.0\n")
	mkFile(t, ap, ".apm/skills/s2/SKILL.md", skillMD("s2"))
	junkSkill(t, ap, ".apm/skills/s2")
	mkFile(t, dir, ".apm/skills/own/SKILL.md", skillMD("own"))
	junkSkill(t, dir, ".apm/skills/own")
}

// The tree is Oracle 0.33.0's (skex/p2/or/tree.txt, its .claude part): the
// cache marker and Python bytecode are left out of every skill, .apm out of
// a root skill at every level, and a directory is made only when it holds a
// file of the Oracle's source plan, so emptydir and topempty are not and a
// sub-skill's onlypin, whose .apm-pin is in the plan, is.
func TestRun_SkillCopyLeavesOutWhatTheOracleDoes(t *testing.T) {
	dir := t.TempDir()
	skexProject(t, dir)
	m, resolved := depManifest("_local/rs", "_local/ap")

	result, err := Run([]string{"claude"}, dir, m, resolved, nil, "", false, nil)
	if err != nil {
		t.Fatal(err)
	}

	want := []string{
		".claude",
		".claude/skills",
		".claude/skills/own",
		".claude/skills/own/.apm",
		".claude/skills/own/.apm/nested.txt",
		".claude/skills/own/SKILL.md",
		".claude/skills/own/apm.yml",
		".claude/skills/own/keep.txt",
		".claude/skills/own/onlypin",
		".claude/skills/own/x.pyc.d",
		".claude/skills/own/x.pyc.d/keep.txt",
		".claude/skills/rs",
		".claude/skills/rs/SKILL.md",
		".claude/skills/rs/scripts",
		".claude/skills/rs/scripts/apm.yml",
		".claude/skills/rs/scripts/keep.txt",
		".claude/skills/rs/scripts/x.pyc.d",
		".claude/skills/rs/scripts/x.pyc.d/keep.txt",
		".claude/skills/rsub",
		".claude/skills/rsub/.apm",
		".claude/skills/rsub/.apm/nested.txt",
		".claude/skills/rsub/SKILL.md",
		".claude/skills/rsub/apm.yml",
		".claude/skills/rsub/keep.txt",
		".claude/skills/rsub/onlypin",
		".claude/skills/rsub/x.pyc.d",
		".claude/skills/rsub/x.pyc.d/keep.txt",
		".claude/skills/s2",
		".claude/skills/s2/.apm",
		".claude/skills/s2/.apm/nested.txt",
		".claude/skills/s2/SKILL.md",
		".claude/skills/s2/apm.yml",
		".claude/skills/s2/keep.txt",
		".claude/skills/s2/onlypin",
		".claude/skills/s2/x.pyc.d",
		".claude/skills/s2/x.pyc.d/keep.txt",
	}
	if got := entriesBelow(t, dir, ".claude"); !slices.Equal(got, want) {
		t.Errorf("tree:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	// The lock lists the files the copy wrote (skex/p2/or/proj/apm.lock.yaml).
	var files []string
	for _, f := range want {
		if info, err := os.Stat(filepath.Join(dir, f)); err == nil && !info.IsDir() {
			files = append(files, f)
		}
	}
	var recorded []string
	for _, key := range []string{"", "_local/rs", "_local/ap"} {
		if dr := result.PerDep[key]; dr != nil {
			recorded = append(recorded, dr.Files...)
		}
	}
	sort.Strings(recorded)
	if !slices.Equal(recorded, files) {
		t.Errorf("recorded:\n%s\nwant:\n%s", strings.Join(recorded, "\n"), strings.Join(files, "\n"))
	}
}

// A Claude plugin's root skill also leaves out apm.yml at every level, and a
// directory whose only files the copy drops is still made (skex/pl1: scripts
// holds apm.yml and a.pyc). A skill below it deploys on its own with the
// rules of any other skill, so its apm.yml stays.
func TestRun_SkillCopyOfAPluginRootSkill(t *testing.T) {
	dir := t.TempDir()
	pl := filepath.Join(dir, "apm_modules", "_local", "pl")
	mkFile(t, pl, "SKILL.md", "---\nname: pl\ndescription: pl\n---\nb\n")
	mkFile(t, pl, ".apm-pin", "p\n")
	mkFile(t, pl, ".claude-plugin/plugin.json", `{"name":"pl"}`+"\n")
	mkFile(t, pl, "scripts/apm.yml", "name: x\nversion: 1.0.0\n")
	mkFile(t, pl, "scripts/a.pyc", "p\n")
	mkFile(t, pl, "skills/ps/SKILL.md", "---\nname: ps\ndescription: ps\n---\nb\n")
	mkFile(t, pl, "skills/ps/b.pyo", "p\n")
	mkFile(t, pl, "skills/ps/apm.yml", "name: x\nversion: 1.0.0\n")
	m, resolved := depManifest("_local/pl")

	if _, err := Run([]string{"claude"}, dir, m, resolved, nil, "", false, nil); err != nil {
		t.Fatal(err)
	}

	want := []string{
		".claude",
		".claude/skills",
		".claude/skills/pl",
		".claude/skills/pl/.claude-plugin",
		".claude/skills/pl/.claude-plugin/plugin.json",
		".claude/skills/pl/SKILL.md",
		".claude/skills/pl/scripts",
		".claude/skills/pl/skills",
		".claude/skills/pl/skills/ps",
		".claude/skills/pl/skills/ps/SKILL.md",
		".claude/skills/ps",
		".claude/skills/ps/SKILL.md",
		".claude/skills/ps/apm.yml",
	}
	if got := entriesBelow(t, dir, ".claude"); !slices.Equal(got, want) {
		t.Errorf("tree:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// A --global deploy links the skill directory, so the link still shows what
// the copy leaves out, and records the files the copy writes; the copy it
// falls back to on a Windows account that cannot link records the same
// files, so one lock serves both (issue #126 comment 6267289737 item 1). The
// files are those Oracle 0.33.0 recorded for the same --global install of rs
// (skex/g1/or/home/.apm/apm.lock.yaml).
func TestRun_GlobalSkillRecordsTheCopyFiles(t *testing.T) {
	want := []string{
		".claude/skills/rs/SKILL.md",
		".claude/skills/rs/scripts/apm.yml",
		".claude/skills/rs/scripts/keep.txt",
		".claude/skills/rs/scripts/x.pyc.d/keep.txt",
		".claude/skills/rsub/.apm/nested.txt",
		".claude/skills/rsub/SKILL.md",
		".claude/skills/rsub/apm.yml",
		".claude/skills/rsub/keep.txt",
		".claude/skills/rsub/x.pyc.d/keep.txt",
	}
	own := map[bool][]string{}
	for _, link := range []bool{true, false} {
		t.Run(map[bool]string{true: "symlink", false: "copy"}[link], func(t *testing.T) {
			defer SetSymlinkProbeForTest(func() bool { return link })()
			apmDir, home := t.TempDir(), t.TempDir()
			skexProject(t, apmDir)
			m, resolved := depManifest("_local/rs")

			result, err := Run([]string{"claude"}, apmDir, m, resolved, nil, home, true, nil)
			if err != nil {
				t.Fatal(err)
			}

			got := slices.Clone(result.PerDep["_local/rs"].Files)
			sort.Strings(got)
			if !slices.Equal(got, want) {
				t.Errorf("recorded:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
			}
			own[link] = slices.Clone(result.PerDep[""].Files)
			sort.Strings(own[link])
		})
	}
	if !slices.Equal(own[true], own[false]) || len(own[true]) == 0 {
		t.Errorf("own skill recorded by link:\n%s\nby copy:\n%s", strings.Join(own[true], "\n"), strings.Join(own[false], "\n"))
	}
}
