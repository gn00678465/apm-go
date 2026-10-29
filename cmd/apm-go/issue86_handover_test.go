package main

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/apm-go/apm/internal/deploy"
)

// Issue #86, the uninstall hand-over of a skill and the directories a copy
// deploy makes: a symlink inside a skill directory that leads out of its
// target root is never written or made through.

const (
	handoverASkill = "---\nname: demo\ndescription: demo\n---\nA\n"
	handoverBSkill = "---\nname: demo\ndescription: demo\n---\nB\n"
)

// handoverFiles are the packages a and b of the owner's review: both have
// the skill demo, b also sub/b.md, which a does not have.
var handoverFiles = map[string]string{
	"a/apm.yml":                   "name: a\nversion: 1.0.0\n",
	"a/.apm/skills/demo/SKILL.md": handoverASkill,
	"b/apm.yml":                   "name: b\nversion: 1.0.0\n",
	"b/.apm/skills/demo/SKILL.md": handoverBSkill,
	"b/.apm/skills/demo/sub/b.md": "from b\n",
}

// handover86 installs ../a and ../b from root/proj, a's skill winning, and
// replaces .claude/skills/demo/sub by a symlink to target(root).
func handover86(t *testing.T, target func(root string) string) string {
	t.Helper()
	root := t.TempDir()
	writeCompileFile(t, root, "proj/apm.yml", "name: consumer\nversion: 1.0.0\ntarget: claude\ndependencies:\n  apm:\n    - ../a\n    - ../b\n")
	for rel, content := range handoverFiles {
		writeCompileFile(t, root, rel, content)
	}
	if err := os.MkdirAll(filepath.Join(root, "outside", "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(filepath.Join(root, "proj"))
	mustRunCLI(t, "install")
	sub := filepath.Join(".claude", "skills", "demo", "sub")
	if err := os.RemoveAll(sub); err != nil {
		t.Fatal(err)
	}
	link86(t, target(root), sub)
	return root
}

// Feedback test 1: handover-skill. a has no path below sub, but the skill
// passes to b as a whole, b/sub/b.md included, so the uninstall is refused
// before its first write and lists the skill directory, as Oracle 0.33.0
// lists it.
func TestUninstall_SkillHandoverThroughSymlinkOutside_Refused(t *testing.T) {
	root := handover86(t, func(root string) string { return filepath.Join(root, "outside", "sub") })
	before := tree86(t, root)

	stdout, code := runCLI86(t, "uninstall", "../a")

	want := " > Uninstalling 1 package(s)...\n + _local/a - found in apm.yml\n" + uninstallTargetRefused86(".claude/skills/demo")
	if code != 1 || stdout != want {
		t.Errorf("exit %d, stdout:\n%s\nwant exit 1, stdout:\n%s", code, stdout, want)
	}
	assertTree86(t, root, before)
	if got := tree86(t, filepath.Join(root, "outside", "sub")); !reflect.DeepEqual(got, map[string]string{".": "dir"}) {
		t.Errorf("outside/sub = %v, want empty", got)
	}
}

// Feedback test 2: sub linked to another directory of .claude stays inside
// the target root, so the uninstall goes on, but it leads out of the skill's
// directory, so the hand-over does not write b/sub/b.md through it (issue
// #112): one warning, and the lock does not record the path.
func TestUninstall_SkillHandoverThroughSymlinkInside_NotWritten(t *testing.T) {
	root := handover86(t, func(string) string { return filepath.Join("..", "..", "elsewhere") })
	if err := os.Mkdir(filepath.Join(".claude", "elsewhere"), 0o755); err != nil {
		t.Fatal(err)
	}

	stdout, code := runCLI86(t, "uninstall", "../a")

	real := realDir86(t, root)
	b := filepath.Join(".claude", "skills", "demo", "sub", "b.md")
	reason := containment86(filepath.Join(cwd86(t), b), filepath.Join(real, "proj", ".claude", "skills", "demo"), filepath.Join(real, "proj", ".claude", "elsewhere", "b.md"))
	want := " > Uninstalling 1 package(s)...\n" +
		" + _local/a - found in apm.yml\n" +
		" ! Re-integration skipped: restore .claude/skills/demo/sub/b.md: " + reason + ". Run 'apm-go install' to rebuild integrated files.\n" +
		" i Removed _local/a from apm_modules/\n" +
		" i Removed _local/a from dependencies.apm in apm.yml\n" +
		" + Updated " + filepath.Join(cwd86(t), "apm.yml") + " (removed 1 package(s))\n" +
		"  |-- 1 skill(s) integrated -> .claude/skills/\n" +
		" + Uninstall complete: Removed 1 package(s) from apm.yml, Removed 1 package(s) from apm_modules/\n"
	if code != 0 || stdout != want {
		t.Errorf("exit %d, stdout:\n%s\nwant exit 0, stdout:\n%s", code, stdout, want)
	}
	wantClaude := map[string]string{
		".": "dir", "elsewhere": "dir",
		"skills": "dir", "skills/demo": "dir", "skills/demo/SKILL.md": handoverBSkill, "skills/demo/sub": "-> ../../elsewhere",
	}
	if got := tree86(t, ".claude"); !reflect.DeepEqual(got, wantClaude) {
		t.Errorf(".claude = %v, want %v", got, wantClaude)
	}
	if got := tree86(t, filepath.Join(root, "outside")); !reflect.DeepEqual(got, map[string]string{".": "dir", "sub": "dir"}) {
		t.Errorf("outside = %v, want only the empty sub", got)
	}
	assertTransferRecord(t, transferLockDep(t, "apm.lock.yaml", "_local/b"),
		[]string{".claude/skills/demo", ".claude/skills/demo/SKILL.md"},
		map[string]string{
			".claude/skills/demo/SKILL.md": transferSHA(handoverBSkill),
		})
}

// Feedback test 3: the same shape for --global, with ~/.claude/skills/demo a
// real directory (the layout Oracle's copy deploy gives; apm-go's own is a
// symlink into apm_modules, where the user's sub link stays inside the
// package copy).
func TestUninstallGlobal_SkillHandoverThroughSymlinkOutside_Refused(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	setTestHome(t, home)
	writeCompileFile(t, root, "home/.apm/apm.yml", "name: global\nversion: 0.0.0\ntarget: claude\ndependencies:\n  apm:\n    - "+filepath.Join(home, "a")+"\n    - "+filepath.Join(home, "b")+"\n")
	for rel, content := range handoverFiles {
		writeCompileFile(t, home, rel, content)
	}
	if err := os.MkdirAll(filepath.Join(root, "outside", "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	mustRunCLI(t, "install", "--global")
	demo := filepath.Join(home, ".claude", "skills", "demo")
	if err := os.Remove(demo); err != nil {
		t.Fatal(err)
	}
	writeCompileFile(t, demo, "SKILL.md", handoverASkill)
	link86(t, filepath.Join(root, "outside", "sub"), filepath.Join(demo, "sub"))
	before := tree86(t, root)

	stdout, code := runCLI86(t, "uninstall", "--global", filepath.Join(home, "a"))

	want := " i Uninstalling from user scope (~/.apm/)\n > Uninstalling 1 package(s)...\n + _local/a - found in apm.yml\n" + uninstallTargetRefused86(".claude/skills/demo")
	if code != 1 || stdout != want {
		t.Errorf("exit %d, stdout:\n%s\nwant exit 1, stdout:\n%s", code, stdout, want)
	}
	assertTree86(t, root, before)
}

// Feedback test 4: skill-emptydir. A copy deploy makes a directory of the
// skill that holds no file it writes, as the Oracle makes one whose only file
// is a .apm-pin (skex/p2: onlypin), so a directory linked out of .claude
// fails the deploy before anything is made through it.
func TestInstall_SkillEmptyDirLinksOutside_NotMade(t *testing.T) {
	root := upDepProject(t, "  apm:\n    - ../dep\n")
	writeCompileFile(t, root, "dep/.apm/skills/sk/empty/inner/.apm-pin", "pin\n")
	if err := os.MkdirAll(filepath.Join(root, "outside"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustRunCLI(t, "install")
	empty := filepath.Join(".claude", "skills", "sk", "empty")
	if err := os.RemoveAll(empty); err != nil {
		t.Fatal(err)
	}
	link86(t, filepath.Join(root, "outside"), empty)
	before := tree86(t, root)

	stdout, code := runCLI86(t, "install")

	real := realDir86(t, root)
	want := " > Installing dependencies from apm.yml...\n" +
		" i Targets: claude  (source: apm.yml)\n" +
		" ! deploy sk to claude failed: " + containment86(filepath.Join(cwd86(t), empty), filepath.Join(real, "proj", ".claude", "skills", "sk"), filepath.Join(real, "outside")) + "\n" +
		"   + ../dep (local)\n" +
		"  |-- (files unchanged)\n" +
		" + Installed 1 APM dependency in <t>s.\n"
	if code != 0 || stdout != want {
		t.Errorf("exit %d, stdout:\n%s\nwant exit 0, stdout:\n%s", code, stdout, want)
	}
	assertTree86(t, root, before)
}

// Feedback test 5: restoreFromTrial checks each segment before it makes it,
// so a parent that leads out of its target root is not written through
// even when the check before the uninstall's first write did not run.
func TestRestoreFromTrial_ParentLinksOutside_NotWritten(t *testing.T) {
	tests := []struct{ name, link, p string }{
		{name: "directory inside a skill", link: ".claude/skills/demo/sub", p: ".claude/skills/demo/sub/b.md"},
		{name: "rules directory", link: ".claude/rules", p: ".claude/rules/r.md"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := realDir86(t, t.TempDir())
			trial, root, outside := filepath.Join(base, "trial"), filepath.Join(base, "root"), filepath.Join(base, "outside")
			writeCompileFile(t, trial, tt.p, "from b\n")
			if err := os.MkdirAll(outside, 0o755); err != nil {
				t.Fatal(err)
			}
			link86(t, outside, filepath.Join(root, filepath.FromSlash(tt.link)))
			before := tree86(t, base)

			err := restoreFromTrial(trial, root, tt.p)

			want := &deploy.DeployPathError{Path: tt.p, Link: tt.link, Resolved: outside, Root: filepath.Join(root, ".claude")}
			var got *deploy.DeployPathError
			if !errors.As(err, &got) || !reflect.DeepEqual(got, want) {
				t.Errorf("restoreFromTrial = %v, want %v", err, want)
			}
			assertTree86(t, base, before)
		})
	}
}
