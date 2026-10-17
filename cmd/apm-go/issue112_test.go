package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Issue #112: a deployed path that a symlink takes out of the primitive's own
// directory, also to another place in its target root, is not written
// through, as Oracle 0.33.0 bounds it (utils/path_security.py:190-219). The
// expected lines are Oracle 0.33.0's (card #112 investigation, run1, run3,
// run5, run8), in apm-go's status symbols, without Rich's line wrap and the
// two experimental-resolver lines (R10).

const userShared112 = "USER SHARED CONTENT\n"

// project112 writes the project of the investigation: ./dep has the rule r,
// the agent a, the prompt p and the skill s.
func project112(t *testing.T, targets ...string) string {
	t.Helper()
	root := t.TempDir()
	manifest := "name: proj\nversion: 1.0.0\ntargets:\n"
	for _, target := range targets {
		manifest += "  - " + target + "\n"
	}
	for rel, content := range map[string]string{
		"proj/apm.yml":     manifest + "dependencies:\n  apm:\n    - ./dep\n",
		"proj/dep/apm.yml": "name: dep\nversion: 1.0.0\n",
		"proj/dep/.apm/instructions/r.instructions.md": "---\napplyTo: \"**/*.go\"\n---\nPACKAGE RULE\n",
		"proj/dep/.apm/agents/a.agent.md":              "---\nname: a\ndescription: pkg agent\n---\nPACKAGE AGENT\n",
		"proj/dep/.apm/prompts/p.prompt.md":            "---\ndescription: pkg prompt\n---\nPACKAGE PROMPT\n",
		"proj/dep/.apm/skills/s/SKILL.md":              "---\nname: s\ndescription: pkg skill\n---\nPACKAGE SKILL\n",
	} {
		writeCompileFile(t, root, rel, content)
	}
	t.Chdir(filepath.Join(root, "proj"))
	return root
}

// linkShared112 makes link (relative to the project) a symlink to target
// (relative to link's directory), a file holding userShared112.
func linkShared112(t *testing.T, link, target string) {
	t.Helper()
	shared := filepath.Join(filepath.Dir(link), target)
	writeCompileFile(t, ".", shared, userShared112)
	if err := os.RemoveAll(link); err != nil {
		t.Fatal(err)
	}
	link86(t, target, link)
}

// reason112 is the Oracle's reason for the project path link resolving to
// the project path resolved.
func reason112(t *testing.T, link, resolved string) string {
	t.Helper()
	real := realDir86(t, cwd86(t))
	return containment86(filepath.Join(cwd86(t), link), filepath.Join(real, filepath.Dir(link)), filepath.Join(real, resolved))
}

func assertShared112(t *testing.T, link, target string) {
	t.Helper()
	if got, _ := os.Readlink(link); got != target {
		t.Errorf("%s -> %q, want -> %q", link, got, target)
	}
	if got := readText(t, filepath.Join(filepath.Dir(link), target)); got != userShared112 {
		t.Errorf("user file %s = %q, want %q", target, got, userShared112)
	}
}

// A rule or a copilot instruction refused fails the install before anything
// is written: no deployed file, apm_modules/, lock or .gitignore (run1 rule,
// ruleproj, ginstr).
func TestInstall112_InstructionRefused(t *testing.T) {
	tests := []struct{ name, target, link, linkTarget, resolved string }{
		{"rule", "claude", ".claude/rules/r.md", "../shared/r.md", ".claude/shared/r.md"},
		{"rule linked out of .claude", "claude", ".claude/rules/r.md", "../../notes/r.md", "notes/r.md"},
		{"copilot instruction", "copilot", ".github/instructions/r.instructions.md", "../shared/r.instructions.md", ".github/shared/r.instructions.md"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := project112(t, tt.target)
			linkShared112(t, tt.link, tt.linkTarget)
			before := tree86(t, root)

			stdout, code := runCLI86(t, "install")

			want := " > Installing dependencies from apm.yml...\n" +
				" > Resolving ./dep...\n" +
				" i Targets: " + tt.target + "  (source: apm.yml)\n" +
				"   + ./dep (local)\n" +
				"   x 1 package failed:\n" +
				"    +- ./dep -- Failed to integrate primitives from local package: " + reason112(t, tt.link, tt.resolved) + "\n" +
				" x Installation failed with 1 error(s) in <t>s. No install transaction changes were committed.\n"
			if code != 1 || stdout != want {
				t.Errorf("exit %d, stdout:\n%s\nwant exit 1, stdout:\n%s", code, stdout, want)
			}
			assertTree86(t, root, before)
		})
	}
}

// An agent, a command or a prompt refused is skipped with the Oracle's
// warning; the rest of the package deploys, the exit code is 0, and the lock
// does not record the path (run1).
func TestInstall112_FileRefused_Skipped(t *testing.T) {
	tests := []struct{ name, target, link, linkTarget, resolved, kind string }{
		{"claude agent", "claude", ".claude/agents/a.md", "../shared/a.md", ".claude/shared/a.md", "agent"},
		{"claude command", "claude", ".claude/commands/p.md", "../shared/p.md", ".claude/shared/p.md", "command"},
		{"copilot prompt", "copilot", ".github/prompts/p.prompt.md", "../shared/p.prompt.md", ".github/shared/p.prompt.md", "prompt"},
		{"copilot agent", "copilot", ".github/agents/a.agent.md", "../shared/a.agent.md", ".github/shared/a.agent.md", "agent"},
		{"opencode command", "opencode", ".opencode/commands/p.md", "../shared/p.md", ".opencode/shared/p.md", "command"},
		{"opencode agent", "opencode", ".opencode/agents/a.md", "../shared/a.md", ".opencode/shared/a.md", "agent"},
		{"codex agent", "codex", ".codex/agents/a.toml", "../shared/a.toml", ".codex/shared/a.toml", "agent"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			project112(t, tt.target)
			linkShared112(t, tt.link, tt.linkTarget)

			stdout, code := runCLI86(t, "install")

			warning := "   ! [dep] Rejected " + tt.kind + " target path: " + reason112(t, tt.link, tt.resolved) + "\n"
			if code != 0 || !strings.Contains(stdout, warning) || !strings.HasSuffix(stdout, warning+" + Installed 1 APM dependency in <t>s.\n") {
				t.Errorf("exit %d, stdout:\n%s\nwant exit 0, ending with:\n%s + Installed 1 APM dependency in <t>s.", code, stdout, warning)
			}
			assertShared112(t, tt.link, tt.linkTarget)
			if lock := readText(t, "apm.lock.yaml"); strings.Contains(lock, tt.link) {
				t.Errorf("apm.lock.yaml records %s:\n%s", tt.link, lock)
			}
		})
	}
}

// The whole transcript of run1 cagent: the agents line is not printed.
func TestInstall112_ClaudeAgentRefused_Transcript(t *testing.T) {
	project112(t, "claude")
	linkShared112(t, ".claude/agents/a.md", "../shared/a.md")

	stdout, code := runCLI86(t, "install")

	want := " > Installing dependencies from apm.yml...\n" +
		" > Resolving ./dep...\n" +
		" i Targets: claude  (source: apm.yml)\n" +
		"   + ./dep (local)\n" +
		"  |-- 1 commands integrated -> .claude/commands/\n" +
		"  |-- 1 rule(s) integrated -> .claude/rules/\n" +
		"  |-- 1 skill(s) integrated -> .claude/skills/\n" +
		" i Added apm_modules/ to .gitignore\n" +
		"   ! [dep] Rejected agent target path: " + reason112(t, ".claude/agents/a.md", ".claude/shared/a.md") + "\n" +
		" + Installed 1 APM dependency in <t>s.\n"
	if code != 0 || stdout != want {
		t.Errorf("exit %d, stdout:\n%s\nwant exit 0, stdout:\n%s", code, stdout, want)
	}
}

// With claude and copilot, every refusal is named as the Oracle names it, in
// the order of its integration phases: prompts, agents, then commands
// (measured).
func TestInstall112_NotesInPhaseOrder(t *testing.T) {
	project112(t, "claude", "copilot")
	links := []struct{ link, target, resolved string }{
		{".claude/agents/a.md", "../shared/a.md", ".claude/shared/a.md"},
		{".claude/commands/p.md", "../shared/p.md", ".claude/shared/p.md"},
		{".github/prompts/p.prompt.md", "../shared/p.prompt.md", ".github/shared/p.prompt.md"},
		{".github/agents/a.agent.md", "../shared/a.agent.md", ".github/shared/a.agent.md"},
	}
	for _, l := range links {
		linkShared112(t, l.link, l.target)
	}

	stdout, code := runCLI86(t, "install")

	want := "   ! [dep] Rejected prompt target path: " + reason112(t, links[2].link, links[2].resolved) + "\n" +
		"   ! [dep] Rejected agent target path: " + reason112(t, links[0].link, links[0].resolved) + "\n" +
		"   ! [dep] Rejected agent target path: " + reason112(t, links[3].link, links[3].resolved) + "\n" +
		"   ! [dep] Rejected command target path: " + reason112(t, links[1].link, links[1].resolved) + "\n" +
		" + Installed 1 APM dependency in <t>s.\n"
	if code != 0 || !strings.HasSuffix(stdout, want) {
		t.Errorf("exit %d, stdout:\n%s\nwant exit 0, ending with:\n%s", code, stdout, want)
	}
	for _, l := range links {
		assertShared112(t, l.link, l.target)
	}
}

// A deployed file the user replaced by a symlink after the install (run5):
// a skill's file is replaced by the package's again, a rule fails the
// install, and an agent is skipped and keeps its lock record, as in the
// Oracle's lock.
func TestInstall112_LinkedAfterInstall(t *testing.T) {
	tests := []struct {
		name, link, linkTarget string
		code                   int
	}{
		{"skill file", ".claude/skills/s/SKILL.md", "../../shared/SKILL.md", 0},
		{"rule", ".claude/rules/r.md", "../shared/r.md", 1},
		{"agent", ".claude/agents/a.md", "../shared/a.md", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := project112(t, "claude")
			mustRunCLI(t, "install")
			lockBefore := readText(t, "apm.lock.yaml")
			deployed := readText(t, tt.link)
			linkShared112(t, tt.link, tt.linkTarget)
			before := tree86(t, root)

			stdout, code := runCLI86(t, "install")

			if code != tt.code {
				t.Errorf("exit %d, want %d, stdout:\n%s", code, tt.code, stdout)
			}
			if got := readText(t, "apm.lock.yaml"); got != lockBefore {
				t.Errorf("apm.lock.yaml:\n%s\nwant:\n%s", got, lockBefore)
			}
			switch tt.name {
			case "skill file":
				if info, err := os.Lstat(tt.link); err != nil || !info.Mode().IsRegular() || readText(t, tt.link) != deployed {
					t.Errorf("%s is not the package's file again: %v", tt.link, err)
				}
				if got := readText(t, filepath.Join(filepath.Dir(tt.link), tt.linkTarget)); got != userShared112 {
					t.Errorf("user file = %q, want %q", got, userShared112)
				}
			case "rule":
				assertTree86(t, root, before)
			case "agent":
				assertShared112(t, tt.link, tt.linkTarget)
				if want := "   ! [dep] Rejected agent target path: " + reason112(t, tt.link, ".claude/shared/a.md") + "\n"; !strings.Contains(stdout, want) {
					t.Errorf("stdout:\n%s\nwant it to hold:\n%s", stdout, want)
				}
			}
		})
	}
}

// --global (run3): a rule and a command are written as files of their own,
// so they are refused as in a project; an agent is a symlink deploy, and the
// user's symlink there stays.
func TestInstallGlobal112(t *testing.T) {
	tests := []struct {
		name, link, linkTarget string
		code                   int
	}{
		{"rule", ".claude/rules/r.md", "../shared/r.md", 1},
		{"command", ".claude/commands/p.md", "../shared/p.md", 0},
		{"agent", ".claude/agents/a.md", "../shared/a.md", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := project112(t, "claude")
			home := filepath.Join(root, "home")
			t.Setenv("HOME", home)
			writeCompileFile(t, root, "gitconfig", "")
			t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(root, "gitconfig"))
			t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
			link := filepath.Join(home, tt.link)
			writeCompileFile(t, home, filepath.Join(filepath.Dir(tt.link), tt.linkTarget), userShared112)
			link86(t, tt.linkTarget, link)
			dep := filepath.Join(root, "proj", "dep")

			stdout, code := runCLI86(t, "install", "-g", "--target", "claude", dep)

			realHome := realDir86(t, home)
			resolved := filepath.Join(realHome, filepath.Dir(tt.link), tt.linkTarget)
			reason := containment86(link, filepath.Join(realHome, filepath.Dir(tt.link)), filepath.Clean(resolved))
			if code != tt.code {
				t.Errorf("exit %d, want %d, stdout:\n%s", code, tt.code, stdout)
			}
			if tt.code == 1 {
				want := "   + " + dep + " (local)\n" +
					" i Removed apm.yml created by the failed install.\n" +
					"   x 1 package failed:\n" +
					"    +- " + dep + " -- Failed to integrate primitives from local package: " + reason + "\n" +
					" x Installation failed with 1 error(s) in <t>s. No install transaction changes were committed.\n"
				if !strings.HasSuffix(stdout, want) {
					t.Errorf("stdout:\n%s\nwant it to end with:\n%s", stdout, want)
				}
				assertNoFile(t, filepath.Join(home, ".apm", "apm.yml"))
			} else if want := "   ! [dep] Rejected " + tt.name + " target path: " + reason + "\n"; !strings.Contains(stdout, want) {
				t.Errorf("stdout:\n%s\nwant it to hold:\n%s", stdout, want)
			}
			if got, _ := os.Readlink(link); got != tt.linkTarget {
				t.Errorf("%s -> %q, want -> %q", tt.link, got, tt.linkTarget)
			}
			if got := readText(t, resolved); got != userShared112 {
				t.Errorf("user file = %q, want %q", got, userShared112)
			}
		})
	}
}

// The project's own rule (run8): the Oracle's lines for the project's
// content, and nothing written, its agent included.
func TestInstall112_ProjectRuleRefused(t *testing.T) {
	root := t.TempDir()
	writeCompileFile(t, root, "proj/apm.yml", "name: proj\nversion: 1.0.0\ntargets:\n  - claude\n")
	writeCompileFile(t, root, "proj/.apm/instructions/r.instructions.md", "---\napplyTo: \"**/*.go\"\n---\nPROJECT RULE\n")
	writeCompileFile(t, root, "proj/.apm/agents/a.agent.md", "---\nname: a\ndescription: proj agent\n---\nPROJECT AGENT\n")
	t.Chdir(filepath.Join(root, "proj"))
	linkShared112(t, ".claude/rules/r.md", "../shared/r.md")
	before := tree86(t, root)

	stdout, code := runCLI86(t, "install")

	reason := reason112(t, ".claude/rules/r.md", ".claude/shared/r.md")
	want := " > Installing dependencies from apm.yml...\n" +
		" i Targets: claude  (source: apm.yml)\n" +
		"   + <project root> (local)\n" +
		" x Root project primitives could not be integrated: " + reason + "\n" +
		"   x 1 package failed:\n" +
		"    +- <root> -- Failed to integrate root project primitives: " + reason + "\n" +
		" x Installation failed with 1 error(s) in <t>s. No install transaction changes were committed.\n"
	if code != 1 || stdout != want {
		t.Errorf("exit %d, stdout:\n%s\nwant exit 1, stdout:\n%s", code, stdout, want)
	}
	assertTree86(t, root, before)
}

// update and a frozen install refuse the rule before they write anything,
// with one error line.
func TestUpdateAndFrozen112_RuleRefused(t *testing.T) {
	for _, tt := range []struct {
		args   []string
		prefix string
	}{
		{[]string{"update"}, " x deploy: "},
		{[]string{"install", "--frozen"}, " x frozen install: deploy: "},
	} {
		args := tt.args
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			root := project112(t, "claude")
			mustRunCLI(t, "install")
			// A frozen install deploys only when a locked file is missing,
			// and refuses a locked file with other bytes before that: the
			// user's file has the rule's bytes.
			rule := readText(t, ".claude/rules/r.md")
			linkShared112(t, ".claude/rules/r.md", "../shared/r.md")
			writeCompileFile(t, ".", ".claude/shared/r.md", rule)
			if err := os.Remove(filepath.Join(".claude", "agents", "a.md")); err != nil {
				t.Fatal(err)
			}
			before := tree86(t, root)

			stdout, code := runCLI86(t, args...)

			line := tt.prefix + reason112(t, ".claude/rules/r.md", ".claude/shared/r.md") + "\n"
			if code != 1 || !strings.HasSuffix(stdout, line) {
				t.Errorf("exit %d, stdout:\n%s\nwant exit 1, ending with:\n%s", code, stdout, line)
			}
			assertTree86(t, filepath.Join(root, "proj", ".claude"), subtree112(before, filepath.Join("proj", ".claude")))
			if _, err := os.Stat("apm.lock.yaml"); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// subtree112 is the part of a tree86 listing below dir, relative to it.
func subtree112(tree map[string]string, dir string) map[string]string {
	out := map[string]string{}
	for p, v := range tree {
		if p == dir {
			out["."] = v
		} else if rel, ok := strings.CutPrefix(p, dir+string(filepath.Separator)); ok {
			out[rel] = v
		}
	}
	return out
}

// A rule of a transitive package refused: the install removes the
// apm_modules directory of each package it fetched, the transitive one
// included, and keeps one that was there before, as Oracle 0.33.0 does
// (measured with a registry package that declares another).
func TestInstall112_TransitiveRuleRefused_RemovesFetchedModules(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(map[bool]string{false: "fresh", true: "transitive already there"}[existing], func(t *testing.T) {
			t.Setenv("APM_CONFIG_DIR", t.TempDir())
			t.Setenv("HOME", t.TempDir())
			checkoutB := issue69Checkout(t, map[string]string{
				"apm.yml":                             "name: b\nversion: 1.0.0\n",
				".apm/instructions/r.instructions.md": "---\napplyTo: \"**/*.go\"\n---\nPACKAGE RULE\n",
			})
			loader := &issue69GitLoader{t: t, checkouts: map[string]string{
				"acme/a": issue69Checkout(t, map[string]string{
					"apm.yml":                 "name: a\nversion: 1.0.0\ndependencies:\n  apm:\n    - git: acme/b\n      ref: main\n",
					".apm/agents/pa.agent.md": "---\nname: pa\ndescription: d\n---\nA\n",
				}),
				"acme/b": checkoutB,
			}}
			dir := chdirTemp(t)
			writeCompileFile(t, dir, "apm.yml", "name: proj\nversion: 1.0.0\ntargets: [claude]\ndependencies:\n  apm:\n    - git: acme/a\n      ref: main\n")
			linkShared112(t, ".claude/rules/r.md", "../shared/r.md")
			if existing {
				if err := os.CopyFS(filepath.Join("apm_modules", "acme", "b"), os.DirFS(checkoutB)); err != nil {
					t.Fatal(err)
				}
			}
			before := tree86(t, dir)

			var err error
			stdout := captureUninstallStdout(t, func() {
				err = runInstall(&installDeps{tags: &mockInstallTagLister{}, loader: loader}, false, false, "", "", nil, nil)
			})

			if exitCodeOf(err) != 1 || !strings.Contains(stdout, "    +- acme/b -- Failed to integrate primitives from cached package: "+reason112(t, ".claude/rules/r.md", ".claude/shared/r.md")+"\n") {
				t.Errorf("err %v, stdout:\n%s", err, stdout)
			}
			assertTree86(t, dir, before)
		})
	}
}

// replaceWithShared112 replaces the deployed file link (relative to root) by
// a symlink to ../shared/<its name>: a copy of the deployed bytes for "same",
// userShared112 for "diff", and nothing for "dangling".
func replaceWithShared112(t *testing.T, root, link, content string) string {
	t.Helper()
	target := "../shared/" + filepath.Base(link)
	abs := filepath.Join(root, link)
	shared := filepath.Join(filepath.Dir(abs), target)
	switch content {
	case "same":
		writeCompileFile(t, filepath.Dir(shared), filepath.Base(shared), readText(t, abs))
	case "diff":
		writeCompileFile(t, filepath.Dir(shared), filepath.Base(shared), userShared112)
	}
	if err := os.Remove(abs); err != nil {
		t.Fatal(err)
	}
	link86(t, target, abs)
	return target
}

// A deployed agent, command or prompt that the user then replaced by a
// symlink out of its directory is the user's: install and update leave it,
// print only the Rejected line, and keep its lock record while the file it
// names exists, as Oracle 0.33.0 does (card #112 step 16, s16/after).
func TestInstall112_RejectedAfterInstall_Kept(t *testing.T) {
	kinds := []struct{ name, target, link, kind string }{
		{"claude agent", "claude", ".claude/agents/a.md", "agent"},
		{"claude command", "claude", ".claude/commands/p.md", "command"},
		{"opencode command", "opencode", ".opencode/commands/p.md", "command"},
		{"copilot prompt", "copilot", ".github/prompts/p.prompt.md", "prompt"},
		{"codex agent", "codex", ".codex/agents/a.toml", "agent"},
	}
	for _, k := range kinds {
		for _, content := range []string{"same", "diff", "dangling"} {
			for _, cmd := range []string{"install", "update"} {
				t.Run(k.name+"/"+content+"/"+cmd, func(t *testing.T) {
					project112(t, k.target)
					mustRunCLI(t, "install")
					lockBefore := readText(t, "apm.lock.yaml")
					shared := filepath.Join(filepath.Dir(k.link), "..", "shared", filepath.Base(k.link))
					var sharedBefore string
					target := replaceWithShared112(t, ".", k.link, content)
					if content != "dangling" {
						sharedBefore = readText(t, shared)
					}

					stdout, code := runCLI86(t, cmd)

					if code != 0 {
						t.Errorf("exit %d, stdout:\n%s", code, stdout)
					}
					if got, _ := os.Readlink(k.link); got != target {
						t.Errorf("%s -> %q, want -> %q", k.link, got, target)
					}
					if content != "dangling" && readText(t, shared) != sharedBefore {
						t.Errorf("user file %s changed", shared)
					}
					want := "! [dep] Rejected " + k.kind + " target path: " + reason112(t, k.link, filepath.Clean(shared)) + "\n"
					if !strings.Contains(stdout, want) || strings.Contains(stdout, " i Cleaned") || strings.Contains(stdout, "hash mismatch") {
						t.Errorf("stdout:\n%s\nwant the Rejected line %q and no Cleaned or hash mismatch line", stdout, want)
					}
					lock := readText(t, "apm.lock.yaml")
					if content == "dangling" {
						if strings.Contains(lock, k.link) {
							t.Errorf("apm.lock.yaml still records %s:\n%s", k.link, lock)
						}
					} else if lock != lockBefore {
						t.Errorf("apm.lock.yaml:\n%s\nwant:\n%s", lock, lockBefore)
					}
				})
			}
		}
	}
}

// A rejected path does not stop the rest of its bucket's cleanup: the agent
// the package no longer has is removed, the user's symlink stays (s16/after
// cagent-same-stale, as Oracle 0.33.0).
func TestInstall112_RejectedAfterInstall_OtherStaleCleaned(t *testing.T) {
	project112(t, "claude")
	writeCompileFile(t, ".", "dep/.apm/agents/b.agent.md", "---\nname: b\ndescription: other agent\n---\nOTHER AGENT\n")
	mustRunCLI(t, "install")
	target := replaceWithShared112(t, ".", ".claude/agents/a.md", "same")
	if err := os.Remove("dep/.apm/agents/b.agent.md"); err != nil {
		t.Fatal(err)
	}

	stdout, code := runCLI86(t, "install")

	if code != 0 || !strings.Contains(stdout, " i Cleaned 1 stale file from ./dep\n  - .claude/agents/b.md\n") {
		t.Errorf("exit %d, stdout:\n%s", code, stdout)
	}
	assertNoFile(t, ".claude/agents/b.md")
	if got, _ := os.Readlink(".claude/agents/a.md"); got != target {
		t.Errorf(".claude/agents/a.md -> %q, want -> %q", got, target)
	}
	lock := readText(t, "apm.lock.yaml")
	if !strings.Contains(lock, ".claude/agents/a.md") || strings.Contains(lock, ".claude/agents/b.md") {
		t.Errorf("apm.lock.yaml:\n%s", lock)
	}
}

// --global: the deploy's own symlink to the agent replaced by the user's
// symlink to a copy of the same bytes stays, with its lock record
// (s16/after gagent-same-global, as Oracle 0.33.0).
func TestInstallGlobal112_RejectedAfterInstall_Kept(t *testing.T) {
	root := project112(t, "claude")
	home := filepath.Join(root, "home")
	t.Setenv("HOME", home)
	writeCompileFile(t, root, "gitconfig", "")
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(root, "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	mustRunCLI(t, "install", "-g", "--target", "claude", filepath.Join(root, "proj", "dep"))
	lockPath := filepath.Join(home, ".apm", "apm.lock.yaml")
	lockBefore := readText(t, lockPath)
	link := filepath.Join(home, ".claude", "agents", "a.md")
	target := replaceWithShared112(t, home, ".claude/agents/a.md", "same")

	stdout, code := runCLI86(t, "install", "-g")

	if code != 0 || !strings.Contains(stdout, "Rejected agent target path") || strings.Contains(stdout, " i Cleaned") {
		t.Errorf("exit %d, stdout:\n%s", code, stdout)
	}
	if got, _ := os.Readlink(link); got != target {
		t.Errorf("%s -> %q, want -> %q", link, got, target)
	}
	if got := readText(t, lockPath); got != lockBefore {
		t.Errorf("lock:\n%s\nwant:\n%s", got, lockBefore)
	}
}

// owners112 writes a project whose ./dep-a and ./dep-b both have the agent a,
// dep-a also the agent x, installs it so dep-a owns .claude/agents/a.md, and
// replaces that file by the user's symlink to a copy of its bytes. It returns
// the symlink's target and dep-a's recorded hash of the path.
func owners112(t *testing.T) (target, hash string) {
	t.Helper()
	root := t.TempDir()
	for rel, content := range map[string]string{
		"apm.yml":                      "name: proj\nversion: 1.0.0\ntargets:\n  - claude\ndependencies:\n  apm:\n    - ./dep-a\n    - ./dep-b\n",
		"dep-a/apm.yml":                "name: dep-a\nversion: 1.0.0\n",
		"dep-a/.apm/agents/a.agent.md": "---\nname: a\ndescription: agent of dep-a\n---\nAGENT A\n",
		"dep-a/.apm/agents/x.agent.md": "---\nname: x\ndescription: other agent of dep-a\n---\nAGENT X\n",
		"dep-b/apm.yml":                "name: dep-b\nversion: 1.0.0\n",
		"dep-b/.apm/agents/a.agent.md": "---\nname: a\ndescription: agent of dep-b\n---\nAGENT B\n",
	} {
		writeCompileFile(t, root, rel, content)
	}
	t.Chdir(root)
	mustRunCLI(t, "install")
	hash = readLockfile(t).FindByKey("_local/dep-a").DeployedHashes[".claude/agents/a.md"]
	if hash == "" {
		t.Fatalf("dep-a does not record .claude/agents/a.md:\n%s", readText(t, "apm.lock.yaml"))
	}
	return replaceWithShared112(t, ".", ".claude/agents/a.md", "same"), hash
}

// The user's symlink stays whichever package now has the agent: no cleanup
// of the run removes it, dep-a's record of it stays with its hash, and the
// package refused at it records nothing and gets no deployed 0 files line,
// as Oracle 0.33.0 (card #112 step 22, ownership.py).
func TestInstall112_RejectedAfterInstall_OwnerChanged(t *testing.T) {
	cases := []struct {
		name, apmYML, local, refusedKey string
	}{
		{"reordered", "    - ./dep-b\n    - ./dep-a\n", "", "_local/dep-b"},
		{"orphan", "    - ./dep-b\n", "", "_local/dep-b"},
		{"local owner", "    - ./dep-a\n    - ./dep-b\n", "---\nname: a\ndescription: project agent\n---\nPROJECT AGENT\n", ""},
	}
	for _, c := range cases {
		for _, cmd := range []string{"install", "update"} {
			t.Run(c.name+"/"+cmd, func(t *testing.T) {
				target, hash := owners112(t)
				sharedBefore := readText(t, ".claude/shared/a.md")
				writeCompileFile(t, ".", "apm.yml", "name: proj\nversion: 1.0.0\ntargets:\n  - claude\ndependencies:\n  apm:\n"+c.apmYML)
				if c.local != "" {
					writeCompileFile(t, ".", ".apm/agents/a.agent.md", c.local)
				}

				stdout, code := runCLI86(t, cmd)

				if code != 0 {
					t.Errorf("exit %d, stdout:\n%s", code, stdout)
				}
				if got, _ := os.Readlink(".claude/agents/a.md"); got != target {
					t.Errorf(".claude/agents/a.md -> %q, want -> %q", got, target)
				}
				if readText(t, ".claude/shared/a.md") != sharedBefore {
					t.Errorf("user file .claude/shared/a.md changed")
				}
				if !strings.Contains(stdout, "Rejected agent target path") || strings.Contains(stdout, "  - .claude/agents/a.md\n") {
					t.Errorf("stdout:\n%s\nwant a Rejected line and no cleanup of .claude/agents/a.md", stdout)
				}
				if c.refusedKey != "" && strings.Contains(stdout, c.refusedKey+" deployed 0 files") {
					t.Errorf("stdout:\n%s\nwant no deployed 0 files line for %s", stdout, c.refusedKey)
				}
				if c.local != "" && cmd == "install" && !strings.Contains(stdout, "   + <project root> (local)\n  |-- (files unchanged)\n") {
					t.Errorf("stdout:\n%s\nwant the project's header with (files unchanged)", stdout)
				}
				lock := readLockfile(t)
				depA := lock.FindByKey("_local/dep-a")
				if depA == nil || depA.DeployedHashes[".claude/agents/a.md"] != hash {
					t.Fatalf("_local/dep-a does not keep .claude/agents/a.md with %s:\n%s", hash, readText(t, "apm.lock.yaml"))
				}
				if b := lock.FindByKey("_local/dep-b"); b != nil && slices.Contains(b.DeployedFiles, ".claude/agents/a.md") {
					t.Errorf("_local/dep-b records .claude/agents/a.md")
				}
				if slices.Contains(lock.LocalDeployedFiles, ".claude/agents/a.md") {
					t.Errorf("the project records .claude/agents/a.md")
				}
				if c.name == "orphan" {
					assertNoFile(t, ".claude/agents/x.md")
					if !strings.Contains(stdout, " i Cleaned 1 file from packages no longer in apm.yml\n  - .claude/agents/x.md\n") {
						t.Errorf("stdout:\n%s\nwant dep-a's x.md cleaned", stdout)
					}
					if !slices.Equal(depA.DeployedFiles, []string{".claude/agents/a.md"}) {
						t.Errorf("_local/dep-a records %v, want only .claude/agents/a.md", depA.DeployedFiles)
					}
				}
			})
		}
	}
}

// A package whose one file is refused prints its Rejected line and no
// deployed 0 files line, as Oracle 0.33.0 prints its header and
// "(files unchanged)".
func TestInstall112_OnlyFileRejected_NoZeroFilesWarning(t *testing.T) {
	root := t.TempDir()
	writeCompileFile(t, root, "apm.yml", "name: proj\nversion: 1.0.0\ntargets:\n  - claude\ndependencies:\n  apm:\n    - ./dep\n")
	writeCompileFile(t, root, "dep/apm.yml", "name: dep\nversion: 1.0.0\n")
	writeCompileFile(t, root, "dep/.apm/agents/a.agent.md", "---\nname: a\ndescription: pkg agent\n---\nPACKAGE AGENT\n")
	t.Chdir(root)
	mustRunCLI(t, "install")
	replaceWithShared112(t, ".", ".claude/agents/a.md", "diff")

	stdout, code := runCLI86(t, "install")

	if code != 0 || !strings.Contains(stdout, "Rejected agent target path") || strings.Contains(stdout, "deployed 0 files") {
		t.Errorf("exit %d, stdout:\n%s", code, stdout)
	}
}

// globalOwners112 makes HOME a fresh directory below a new root holding
// ./dep-a and ./dep-b, each with the agent a and the copilot instruction r,
// and returns the root.
func globalOwners112(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, name := range []string{"dep-a", "dep-b"} {
		writeCompileFile(t, root, name+"/apm.yml", "name: "+name+"\nversion: 1.0.0\n")
		writeCompileFile(t, root, name+"/.apm/agents/a.agent.md", "---\nname: a\ndescription: agent\n---\nAGENT "+name+"\n")
		writeCompileFile(t, root, name+"/.apm/instructions/r.instructions.md", "---\napplyTo: \"**/*.go\"\n---\nRULE "+name+"\n")
	}
	t.Setenv("HOME", filepath.Join(root, "home"))
	writeCompileFile(t, root, "gitconfig", "")
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(root, "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Chdir(root)
	return root
}

// writeGlobalManifest112 writes ~/.apm/apm.yml with target and the packages
// of root named in deps, in that order.
func writeGlobalManifest112(t *testing.T, root, target string, deps ...string) {
	t.Helper()
	manifest := "name: g\nversion: 1.0.0\ntargets:\n  - " + target + "\ndependencies:\n  apm:\n"
	for _, d := range deps {
		manifest += "    - " + filepath.Join(root, d) + "\n"
	}
	writeCompileFile(t, filepath.Join(root, "home", ".apm"), "apm.yml", manifest)
}

// recordsPath112 reports which buckets of the --global lock record rel: the
// dependency keys, and "" for the project's own content.
func recordsPath112(t *testing.T, home, rel string) []string {
	t.Helper()
	t.Chdir(filepath.Join(home, ".apm"))
	lock := readLockfile(t)
	var owners []string
	if slices.Contains(lock.LocalDeployedFiles, rel) {
		owners = append(owners, "")
	}
	for _, dep := range lock.Dependencies {
		if slices.Contains(dep.DeployedFiles, rel) {
			owners = append(owners, dep.UniqueKey())
		}
	}
	return owners
}

// A --global deploy's own symlink into apm_modules/ or .apm/ is replaced by
// the new winner's when the owner of the path changes, as origin/main and
// Oracle 0.33.0 deploy the new winner: no Rejected line, and the lock records
// the path under the new winner alone (card #112 step 31, global-ownership.py).
func TestInstallGlobal112_OwnerChanged_DeploySymlinkReplaced(t *testing.T) {
	const agentLink = ".claude/agents/a.md"
	cases := []struct {
		name, target, link      string
		before, after           []string
		localBefore, localAfter bool
		wantSource, wantOwner   string
	}{
		{"agent reordered", "claude", agentLink, []string{"dep-a", "dep-b"}, []string{"dep-b", "dep-a"}, false, false, "apm_modules/_local/dep-b/.apm/agents/a.agent.md", "_local/dep-b"},
		{"agent owner removed", "claude", agentLink, []string{"dep-a", "dep-b"}, []string{"dep-b"}, false, false, "apm_modules/_local/dep-b/.apm/agents/a.agent.md", "_local/dep-b"},
		{"agent taken by the project", "claude", agentLink, []string{"dep-a", "dep-b"}, []string{"dep-a", "dep-b"}, false, true, ".apm/agents/a.agent.md", ""},
		{"agent given back by the project", "claude", agentLink, []string{"dep-a", "dep-b"}, []string{"dep-a", "dep-b"}, true, false, "apm_modules/_local/dep-a/.apm/agents/a.agent.md", "_local/dep-a"},
		{"copilot instruction reordered", "copilot", ".github/instructions/r.instructions.md", []string{"dep-a", "dep-b"}, []string{"dep-b", "dep-a"}, false, false, "apm_modules/_local/dep-b/.apm/instructions/r.instructions.md", "_local/dep-b"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := globalOwners112(t)
			home := filepath.Join(root, "home")
			projectAgent := filepath.Join(home, ".apm", ".apm", "agents", "a.agent.md")
			if c.localBefore {
				writeCompileFile(t, filepath.Dir(projectAgent), "a.agent.md", "---\nname: a\ndescription: project agent\n---\nPROJECT AGENT\n")
			}
			writeGlobalManifest112(t, root, c.target, c.before...)
			mustRunCLI(t, "install", "-g")
			writeGlobalManifest112(t, root, c.target, c.after...)
			if c.localAfter {
				writeCompileFile(t, filepath.Dir(projectAgent), "a.agent.md", "---\nname: a\ndescription: project agent\n---\nPROJECT AGENT\n")
			} else if err := os.RemoveAll(projectAgent); err != nil {
				t.Fatal(err)
			}

			stdout, code := runCLI86(t, "install", "-g")

			if code != 0 || strings.Contains(stdout, "Rejected") || strings.Contains(stdout, "resolves to") {
				t.Errorf("exit %d, stdout:\n%s\nwant exit 0 and no refusal", code, stdout)
			}
			want := filepath.Join(home, ".apm", filepath.FromSlash(c.wantSource))
			if got, _ := os.Readlink(filepath.Join(home, filepath.FromSlash(c.link))); !strings.HasSuffix(got, filepath.FromSlash(c.wantSource)) || !sameFile112(t, got, want) {
				t.Errorf("%s -> %q, want -> %s", c.link, got, want)
			}
			if got := recordsPath112(t, home, c.link); !slices.Equal(got, []string{c.wantOwner}) {
				t.Errorf("lock records %s under %q, want under %q alone", c.link, got, c.wantOwner)
			}
		})
	}
}

func sameFile112(t *testing.T, a, b string) bool {
	t.Helper()
	ia, err1 := os.Stat(a)
	ib, err2 := os.Stat(b)
	return err1 == nil && err2 == nil && os.SameFile(ia, ib)
}

// The user's own symlink, out of ~/.apm and of the agent's directory, is
// still refused and kept when the owner of the path changes (card #112 step
// 24, gownership.py).
func TestInstallGlobal112_OwnerChanged_UserSymlinkKept(t *testing.T) {
	for _, after := range [][]string{{"dep-b", "dep-a"}, {"dep-b"}} {
		t.Run(strings.Join(after, ","), func(t *testing.T) {
			root := globalOwners112(t)
			home := filepath.Join(root, "home")
			writeGlobalManifest112(t, root, "claude", "dep-a", "dep-b")
			mustRunCLI(t, "install", "-g")
			target := replaceWithShared112(t, home, ".claude/agents/a.md", "same")
			shared := readText(t, filepath.Join(home, ".claude", "shared", "a.md"))
			writeGlobalManifest112(t, root, "claude", after...)

			stdout, code := runCLI86(t, "install", "-g")

			if code != 0 || !strings.Contains(stdout, "! [dep-b] Rejected agent target path") {
				t.Errorf("exit %d, stdout:\n%s", code, stdout)
			}
			if got, _ := os.Readlink(filepath.Join(home, ".claude", "agents", "a.md")); got != target {
				t.Errorf(".claude/agents/a.md -> %q, want -> %q", got, target)
			}
			if readText(t, filepath.Join(home, ".claude", "shared", "a.md")) != shared {
				t.Errorf("user file .claude/shared/a.md changed")
			}
			if got := recordsPath112(t, home, ".claude/agents/a.md"); !slices.Equal(got, []string{"_local/dep-a"}) {
				t.Errorf("lock records .claude/agents/a.md under %q, want under _local/dep-a alone", got)
			}
		})
	}
}

// A path another package deploys after the owner changes is recorded by that
// package alone, not also under its old owner (Oracle 0.33.0 for the packages
// reordered; card #112 step 32, projowner.py).
func TestInstall112_OwnerChanged_OneRecord(t *testing.T) {
	cases := []struct {
		name, apmYML, local, wantOwner string
	}{
		{"reordered", "    - ./dep-b\n    - ./dep-a\n", "", "_local/dep-b"},
		{"project agent", "    - ./dep-a\n    - ./dep-b\n", "---\nname: a\ndescription: project agent\n---\nPROJECT AGENT\n", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			owners112(t)
			if err := os.Remove(".claude/agents/a.md"); err != nil {
				t.Fatal(err)
			}
			mustRunCLI(t, "install")
			writeCompileFile(t, ".", "apm.yml", "name: proj\nversion: 1.0.0\ntargets:\n  - claude\ndependencies:\n  apm:\n"+c.apmYML)
			if c.local != "" {
				writeCompileFile(t, ".", ".apm/agents/a.agent.md", c.local)
			}

			mustRunCLI(t, "install")

			lock := readLockfile(t)
			var owners []string
			if slices.Contains(lock.LocalDeployedFiles, ".claude/agents/a.md") {
				owners = append(owners, "")
			}
			for _, dep := range lock.Dependencies {
				if slices.Contains(dep.DeployedFiles, ".claude/agents/a.md") {
					owners = append(owners, dep.UniqueKey())
				}
			}
			if !slices.Equal(owners, []string{c.wantOwner}) {
				t.Errorf("lock records .claude/agents/a.md under %q, want under %q alone", owners, c.wantOwner)
			}
		})
	}
}
