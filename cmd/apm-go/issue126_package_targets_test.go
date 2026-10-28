package main

import (
	"os"
	"path/filepath"
	"testing"
)

// Issue #126 item 25: the targets a dependency's own apm.yml declares narrow
// the active targets further, after its entry's `targets:`. Every wanted
// tree, line and lock is what Oracle 0.33.0 deployed, printed and wrote for
// the same project (~/.cache/apm-go-verify/issue126/tgt, and impl/E-orfix1,
// E-orfix2 for the two lock fixtures), with apm-go's status symbols.

// issue126TargetsProject is a local package ./dep (an agent, an instruction
// and a skill) whose apm.yml ends with depTail, declared by a project for
// claude and copilot with entryTail ending its entry.
func issue126TargetsProject(t *testing.T, depTail, entryTail string) string {
	t.Helper()
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	home := t.TempDir()
	// Windows takes the home directory from USERPROFILE.
	setTestHome(t, home)
	dir := chdirTemp(t)
	writeCompileFile(t, dir, "dep/.apm/agents/dagent.agent.md", "---\nname: dagent\ndescription: d\n---\nD\n")
	writeCompileFile(t, dir, "dep/.apm/instructions/di.instructions.md", "---\napplyTo: \"**\"\n---\nDI\n")
	writeCompileFile(t, dir, "dep/.apm/skills/dskill/SKILL.md", "---\nname: dskill\ndescription: d\n---\nS\n")
	issue126SetPackageTargets(t, depTail)
	issue69SetEntry(t, entryTail)
	return dir
}

func issue126SetPackageTargets(t *testing.T, depTail string) {
	t.Helper()
	writeCompileFile(t, ".", "dep/apm.yml", "name: dep\nversion: 1.0.0\n"+depTail)
}

var (
	issue126Claude  = []string{".claude/agents/dagent.md", ".claude/rules/di.md", ".claude/skills/dskill/SKILL.md"}
	issue126Copilot = []string{".agents/skills/dskill/SKILL.md", ".github/agents/dagent.agent.md", ".github/instructions/di.instructions.md"}
	issue126Both    = []string{".agents/skills/dskill/SKILL.md", ".claude/agents/dagent.md", ".claude/rules/di.md", ".claude/skills/dskill/SKILL.md", ".github/agents/dagent.agent.md", ".github/instructions/di.instructions.md"}
)

func TestInstall_Issue126_PackageTargetsNarrowActiveTargets(t *testing.T) {
	for _, tc := range []struct {
		name, depTail string
		deployed      []string
	}{
		{"p2 plural scalar", "targets: claude\n", issue126Claude},
		{"p3 singular", "target: claude\n", issue126Claude},
		{"p4 singular csv", "target: \"claude,codex\"\n", issue126Claude},
		{"p16 unknown to apm-go", "targets: [claude, cursor]\n", issue126Claude},
		{"p17 singular list", "target: [claude]\n", issue126Claude},
		{"q1 singular alias", "target: vscode\n", issue126Copilot},
		{"q2 singular agents", "target: agents\n", issue126Copilot},
		{"q4 singular alias list", "target: [vscode, claude]\n", issue126Both},
		{"q5 plural alias not resolved", "targets: [vscode, claude]\n", issue126Claude},
		{"q9 repeated", "targets: [claude, claude]\n", issue126Claude},
		{"q10 stripped", "targets: [\" claude \"]\n", issue126Claude},
		{"p1 all", "targets: [all]\n", issue126Both},
		{"p10 all among others", "targets: [all, codex]\n", issue126Both},
		{"p14 singular all", "target: all\n", issue126Both},
		{"q12 singular null", "target:\n", issue126Both},
	} {
		t.Run(tc.name, func(t *testing.T) {
			issue126TargetsProject(t, tc.depTail, "")

			mustRunCLI(t, "install")

			assertIssue69Deployed(t, tc.deployed...)
			if got := issue69LockDep(t, "_local/dep").TargetSubset; got != nil {
				t.Errorf("target_subset = %q, want none: the lock does not record the package's own targets", got)
			}
		})
	}
}

func TestInstall_Issue126_PackageTargetsLock(t *testing.T) {
	want := issue69OracleLock(t, "local-package-targets")
	issue126TargetsProject(t, "targets: [claude]\n", "")

	got := mustRunCLI(t, "install")

	wantOut := " > Installing dependencies from apm.yml...\n" +
		" > Resolving ./dep...\n" +
		" i Targets: claude, copilot  (source: apm.yml)\n" +
		"   + ./dep (local)\n" +
		"  |-- 1 agents integrated -> .claude/agents/\n" +
		"  |-- 1 rule(s) integrated -> .claude/rules/\n" +
		"  |-- 1 skill(s) integrated -> .claude/skills/\n" +
		" i Added apm_modules/ to .gitignore\n" +
		" + Installed 1 APM dependency in <DUR>.\n"
	if got != wantOut {
		t.Errorf("install:\n%s\nwant:\n%s", got, wantOut)
	}
	assertIssue69Lock(t, want)
}

func TestInstall_Issue126_PackageTargetsNoOverlapWarns(t *testing.T) {
	for _, tc := range []struct{ name, depTail, line string }{
		{"c2 inactive", "targets: [codex]\n", "   ! [./dep] Package targets [codex] do not overlap authorized active targets; skipping\n"},
		{"p5 plural vscode", "targets: [vscode]\n", "   ! [./dep] Package targets [vscode] do not overlap authorized active targets; skipping\n"},
		{"q3 singular agy", "target: agy\n", "   ! [./dep] Package targets [antigravity] do not overlap authorized active targets; skipping\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			issue126TargetsProject(t, tc.depTail, "")

			got := mustRunCLI(t, "install")

			want := " > Installing dependencies from apm.yml...\n" +
				" > Resolving ./dep...\n" +
				" i Targets: claude, copilot  (source: apm.yml)\n" +
				"   + ./dep (local)\n" +
				" i Added apm_modules/ to .gitignore\n" +
				tc.line +
				" + Installed 1 APM dependency in <DUR>.\n"
			if got != want {
				t.Errorf("install:\n%s\nwant:\n%s", got, want)
			}
			assertIssue69Deployed(t)
		})
	}
	t.Run("lock", func(t *testing.T) {
		want := issue69OracleLock(t, "local-package-targets-skipped")
		issue126TargetsProject(t, "targets: [codex]\n", "")

		mustRunCLI(t, "install")

		assertIssue69Lock(t, want)
	})
}

// G3-or: the entry's targets: [claude] leaves claude, which the package's
// [copilot] does not name; an entry that leaves no target gets #69's line
// alone, whatever the package declares.
func TestInstall_Issue126_EntryThenPackageTargets(t *testing.T) {
	for _, tc := range []struct{ name, depTail, entryTail, line string }{
		{"package empties the entry's", "targets: [copilot]\n", "      targets: [claude]\n",
			"   ! [./dep] Package targets [copilot] do not overlap authorized active targets; skipping\n"},
		{"entry already empty", "targets: [claude]\n", "      targets: [codex]\n",
			"   ! [./dep] Per-dependency targets [codex] do not overlap active install targets; skipping\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			issue126TargetsProject(t, tc.depTail, tc.entryTail)

			got := mustRunCLI(t, "install")

			want := " > Installing dependencies from apm.yml...\n" +
				" > Resolving ./dep...\n" +
				" i Targets: claude, copilot  (source: apm.yml)\n" +
				"   + ./dep (local)\n" +
				" i Added apm_modules/ to .gitignore\n" +
				tc.line +
				" + Installed 1 APM dependency in <DUR>.\n"
			if got != want {
				t.Errorf("install:\n%s\nwant:\n%s", got, want)
			}
			assertIssue69Deployed(t)
		})
	}
	t.Run("both narrow", func(t *testing.T) {
		issue126TargetsProject(t, "targets: [claude, codex]\n", "      targets: [claude, copilot]\n")

		mustRunCLI(t, "install")

		assertIssue69Deployed(t, issue126Claude...)
		if got := issue69LockDep(t, "_local/dep").TargetSubset; len(got) != 2 {
			t.Errorf("target_subset = %q, want the entry's [claude copilot]", got)
		}
	})
}

// s1-or: a package that adds targets: later loses its files of the other
// target to the stale cleanup; update and a frozen install then keep that.
func TestInstall_Issue126_AddedPackageTargetsCleanTheOtherTarget(t *testing.T) {
	issue126TargetsProject(t, "", "")
	mustRunCLI(t, "install")
	assertIssue69Deployed(t, issue126Both...)
	issue126SetPackageTargets(t, "targets: [claude]\n")

	mustRunCLI(t, "install")

	assertIssue69Deployed(t, issue126Claude...)
	if got := mustRunCLI(t, "install", "--frozen"); got != " + Frozen install: all dependencies pinned and verified\n" {
		t.Errorf("frozen install:\n%s", got)
	}
	mustRunCLI(t, "update")
	assertIssue69Deployed(t, issue126Claude...)
}

// fz1-or: a clean clone of the Oracle's lock restores the files of the
// package's own target alone.
func TestInstall_Issue126_FrozenCleanCloneOfOracleLock(t *testing.T) {
	lock := issue69OracleLock(t, "local-package-targets")
	issue126TargetsProject(t, "targets: [claude]\n", "")
	if err := os.WriteFile("apm.lock.yaml", []byte(lock), 0o600); err != nil {
		t.Fatal(err)
	}

	if got := mustRunCLI(t, "install", "--frozen"); got != " + Frozen install: all dependencies pinned and verified\n" {
		t.Errorf("frozen install:\n%s", got)
	}

	assertIssue69Deployed(t, issue126Claude...)
	if data, err := os.ReadFile("apm.lock.yaml"); err != nil || string(data) != lock {
		t.Errorf("frozen install changed apm.lock.yaml (err %v)", err)
	}
}

// w2-or: after uninstall of x, y, whose apm.yml allows claude alone, has no
// copilot agent. The Oracle also deletes .claude/agents/shared.md, which
// apm-go hands over to y (the owner deviation of PRODUCT.md's uninstall
// item).
func TestUninstall_Issue126_HandOverOnlyToPackageTargets(t *testing.T) {
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	home := t.TempDir()
	// Windows takes the home directory from USERPROFILE.
	setTestHome(t, home)
	dir := chdirTemp(t)
	for _, p := range []string{"x", "y"} {
		writeCompileFile(t, dir, p+"/.apm/agents/shared.agent.md", "---\nname: shared\ndescription: d\n---\nbody from "+p+"\n")
	}
	writeCompileFile(t, dir, "x/apm.yml", "name: x\nversion: 1.0.0\n")
	writeCompileFile(t, dir, "y/apm.yml", "name: y\nversion: 1.0.0\ntargets: [claude]\n")
	writeCompileFile(t, dir, "apm.yml", "name: proj\nversion: 1.0.0\ntargets:\n  - claude\n  - copilot\ndependencies:\n  apm:\n    - ./x\n    - ./y\n")
	mustRunCLI(t, "install")

	mustRunCLI(t, "uninstall", "./x")

	assertIssue69Deployed(t, ".claude/agents/shared.md")
	data, err := os.ReadFile(filepath.Join(dir, ".claude", "agents", "shared.md"))
	if err != nil || string(data) != "---\nname: shared\ndescription: d\n---\nbody from y\n" {
		t.Errorf(".claude/agents/shared.md = %q (err %v), want y's", data, err)
	}
}
