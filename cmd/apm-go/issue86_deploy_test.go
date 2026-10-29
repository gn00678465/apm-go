package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Issue #86, second half: a deployed path whose directory leads out of its
// target root (.claude) through a symlink is neither removed nor written.

var elapsed86 = regexp.MustCompile(`in [0-9.]+s\.`)

func runCLI86(t *testing.T, args ...string) (string, int) {
	t.Helper()
	stdout, _, code := runCompileCLI(t, args...)
	return elapsed86.ReplaceAllString(stdout, "in <t>s."), code
}

// linkOutside86 moves dir (relative to the working directory) to
// outside/<name> and links it back.
func linkOutside86(t *testing.T, root, dir, name string) string {
	t.Helper()
	outside := filepath.Join(root, "outside", name)
	if err := os.Rename(dir, outside); err != nil {
		t.Fatal(err)
	}
	link86(t, outside, dir)
	return outside
}

func uninstallTargetRefused86(paths ...string) string {
	out := " x Uninstall could not remove tracked target files; manifest and lockfile ownership were preserved. Package directories may already have been removed.\n"
	for _, p := range paths {
		out += " x   - " + p + "\n"
	}
	return out + " x Resolve or remove the listed files, then retry uninstall.\n"
}

func keepOutside86(link, resolved, root string) string {
	return ` ! keeping "` + link + `": it is a symlink to ` + resolved + `, outside ` + root + `, so nothing below it is removed` + "\n"
}

// Feedback test 2: deploy-uninstall and deploy-nested-uninstall. Every path
// is checked before the first write, so nothing changes, apm_modules/
// included, where the Oracle has removed the package directory by then.
func TestUninstall_DeployedPathOutsideTargetRoot_Refused(t *testing.T) {
	tests := []struct {
		name, dir string
		listed    []string
	}{
		{name: "rules directory", dir: ".claude/rules", listed: []string{".claude/rules/r.md"}},
		{name: "directory inside a skill", dir: ".claude/skills/sk/data", listed: []string{".claude/skills/sk", ".claude/skills/sk/data/precious.txt"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := installedLocal86(t)
			writeCompileFile(t, root, "outside/keep.txt", "unrelated\n")
			linkOutside86(t, root, filepath.FromSlash(tt.dir), "moved")
			before := tree86(t, root)

			stdout, code := runCLI86(t, "uninstall", "../dep")

			want := " > Uninstalling 1 package(s)...\n + _local/dep - found in apm.yml\n" + uninstallTargetRefused86(tt.listed...)
			if code != 1 || stdout != want {
				t.Errorf("exit %d, stdout:\n%s\nwant exit 1, stdout:\n%s", code, stdout, want)
			}
			assertTree86(t, root, before)
		})
	}
}

// Feedback test 2: deploy-stale-install and its update twin. The rule's
// source is gone, so its old path is stale; it is the user's now: kept, one
// warning, dropped from the lock, and the next run says nothing of it.
func TestInstall_StaleDeployedPathOutsideTargetRoot_Kept(t *testing.T) {
	for _, cmd := range []string{"install", "update"} {
		t.Run(cmd, func(t *testing.T) {
			root := installedLocal86(t)
			outside := linkOutside86(t, root, filepath.Join(".claude", "rules"), "rules")
			if err := os.Remove(filepath.Join(root, "dep", ".apm", "instructions", "r.instructions.md")); err != nil {
				t.Fatal(err)
			}
			outsideBefore := tree86(t, filepath.Join(root, "outside"))

			stdout, code := runCLI86(t, cmd)

			real := realDir86(t, root)
			warning := keepOutside86(".claude/rules", filepath.Join(real, "outside", "rules"), filepath.Join(real, "proj", ".claude"))
			if code != 0 || strings.Count(stdout, warning) != 1 {
				t.Errorf("exit %d, stdout:\n%s\nwant exit 0 and once:\n%s", code, stdout, warning)
			}
			assertTree86(t, filepath.Join(root, "outside"), outsideBefore)
			if got := readText(t, filepath.Join(outside, "r.md")); got == "" {
				t.Errorf("outside/rules/r.md is empty")
			}
			if lock := readText(t, "apm.lock.yaml"); strings.Contains(lock, ".claude/rules/r.md") {
				t.Errorf("apm.lock.yaml still records .claude/rules/r.md:\n%s", lock)
			}

			again, code := runCLI86(t, cmd)
			if code != 0 || strings.Contains(again, "keeping") {
				t.Errorf("second %s: exit %d, stdout:\n%s\nwant exit 0 and no warning", cmd, code, again)
			}
			assertTree86(t, filepath.Join(root, "outside"), outsideBefore)
		})
	}
}

// Feedback test 2, in full: the warning comes after the install block, and
// nothing else changes in the transcript.
func TestInstall_StaleDeployedPathOutsideTargetRoot_Transcript(t *testing.T) {
	tests := map[string]func(warning string) string{
		"install": func(warning string) string {
			return " > Installing dependencies from apm.yml...\n" +
				" i Targets: claude  (source: apm.yml)\n" +
				"   + ../dep (local)\n" +
				"  |-- 1 skill(s) integrated -> .claude/skills/\n" +
				warning +
				" + Installed 1 APM dependency in <t>s.\n"
		},
		"update": func(warning string) string {
			return " i Targets: claude  (source: apm.yml)\n" +
				"_local/dep\n" +
				"└── 1 skill -> .claude/skills/\n" +
				warning +
				"\n + Installed 1 dependency\n" +
				"  - _local/dep (depth 1)\n"
		},
	}
	for cmd, want := range tests {
		t.Run(cmd, func(t *testing.T) {
			root := installedLocal86(t)
			linkOutside86(t, root, filepath.Join(".claude", "rules"), "rules")
			if err := os.Remove(filepath.Join(root, "dep", ".apm", "instructions", "r.instructions.md")); err != nil {
				t.Fatal(err)
			}

			stdout, code := runCLI86(t, cmd)

			real := realDir86(t, root)
			want := want(keepOutside86(".claude/rules", filepath.Join(real, "outside", "rules"), filepath.Join(real, "proj", ".claude")))
			if code != 0 || stdout != want {
				t.Errorf("exit %d, stdout:\n%s\nwant exit 0, stdout:\n%s", code, stdout, want)
			}
		})
	}
}

// global86 makes HOME=root/home with ~/.apm/apm.yml declaring root/home/dep
// by absolute path, which has the given files, and runs from root.
func global86(t *testing.T, files map[string]string) (root, home string) {
	t.Helper()
	root = t.TempDir()
	home = filepath.Join(root, "home")
	setTestHome(t, home)
	writeCompileFile(t, root, "home/.apm/apm.yml", "name: global\nversion: 0.0.0\ntarget: claude\ndependencies:\n  apm:\n    - "+filepath.Join(home, "dep")+"\n")
	writeCompileFile(t, root, "home/dep/apm.yml", "name: dep\nversion: 1.0.0\n")
	for rel, content := range files {
		writeCompileFile(t, root, "home/dep/"+rel, content)
	}
	writeCompileFile(t, root, "outside/keep.txt", "unrelated\n")
	t.Chdir(root)
	return root, home
}

const skill86 = "---\nname: demo\ndescription: demo\n---\nhello\n"

// Feedback test 3: global-install-agent and global-install-skill. The
// destination's parent leads outside, so nothing there is removed or
// replaced: the deploy of that primitive fails with a warning, as any other
// failed deploy does.
func TestInstallGlobal_DestinationParentOutside_NotDeployed(t *testing.T) {
	tests := []struct {
		name, file, content, linked, prim, dest string
		outside                                 map[string]string
	}{
		{name: "agent", file: ".apm/agents/r.md", content: "---\ndescription: reviewer\n---\nagent\n", linked: "agents", prim: "r", dest: ".claude/agents/r.md",
			outside: map[string]string{"r.md": "precious unrelated user content\n"}},
		{name: "skill", file: ".apm/skills/demo/SKILL.md", content: skill86, linked: "skills", prim: "demo", dest: ".claude/skills/demo",
			outside: map[string]string{"demo/SKILL.md": skill86}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, home := global86(t, map[string]string{tt.file: tt.content})
			for rel, content := range tt.outside {
				writeCompileFile(t, root, "outside/"+rel, content)
			}
			link86(t, filepath.Join(root, "outside"), filepath.Join(root, "home", ".claude", tt.linked))
			outsideBefore := tree86(t, filepath.Join(root, "outside"))

			stdout, code := runCLI86(t, "install", "--global")

			real := realDir86(t, root)
			// A user-scope install announces the scope, prints no Targets
			// line and no .gitignore line, and has a <project root> block
			// (r4-or/global2, install/manifest_helpers.py:63-74).
			want := " i Installing to user scope (~/.apm/)\n" +
				" > Installing dependencies from apm.yml...\n" +
				" > Resolving " + filepath.Join(home, "dep") + "...\n" +
				" ! deploy " + tt.prim + " to claude failed: " + tt.dest + ": symlink .claude/" + tt.linked + " resolves to " + filepath.Join(real, "outside") + ", outside " + filepath.Join(real, "home", ".claude") + "\n" +
				"   + " + filepath.Join(home, "dep") + " (local)\n" +
				"  |-- (files unchanged)\n" +
				"   + <project root> (local)\n" +
				"  |-- (files unchanged)\n" +
				" + Installed 1 APM dependency in <t>s.\n"
			if code != 0 || stdout != want {
				t.Errorf("exit %d, stdout:\n%s\nwant exit 0, stdout:\n%s", code, stdout, want)
			}
			assertTree86(t, filepath.Join(root, "outside"), outsideBefore)
		})
	}
}

// Feedback test 3: global-uninstall-skill and global-uninstall-plain.
func TestUninstallGlobal_DeployedPathOutsideTargetRoot_Refused(t *testing.T) {
	tests := []struct {
		name, linked string
		listed       []string
		copyRule     bool
	}{
		{name: "skill symlink below a linked skills directory", linked: "skills", listed: []string{".claude/skills/demo", ".claude/skills/demo/SKILL.md"}},
		{name: "file below a linked rules directory", linked: "rules", listed: []string{".claude/rules/r.md"}, copyRule: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, home := global86(t, map[string]string{".apm/skills/demo/SKILL.md": skill86, ".apm/instructions/r.instructions.md": issue86Rule})
			mustRunCLI(t, "install", "--global")
			if tt.copyRule {
				rule := filepath.Join(home, ".claude", "rules", "r.md")
				data := readText(t, rule)
				if err := os.Remove(rule); err != nil {
					t.Fatal(err)
				}
				writeCompileFile(t, home, ".claude/rules/r.md", data)
			}
			dir := filepath.Join(home, ".claude", tt.linked)
			moved := filepath.Join(root, "outside", tt.linked)
			if err := os.Rename(dir, moved); err != nil {
				t.Fatal(err)
			}
			link86(t, moved, dir)
			before := tree86(t, root)

			stdout, code := runCLI86(t, "uninstall", "--global", filepath.Join(home, "dep"))

			want := " i Uninstalling from user scope (~/.apm/)\n > Uninstalling 1 package(s)...\n + _local/dep - found in apm.yml\n" + uninstallTargetRefused86(tt.listed...)
			if code != 1 || stdout != want {
				t.Errorf("exit %d, stdout:\n%s\nwant exit 1, stdout:\n%s", code, stdout, want)
			}
			assertTree86(t, root, before)
		})
	}
}

// Feedback test 4: the project's .claude itself linked elsewhere is the
// user's choice and is compared resolved, so install and uninstall run as
// they do without the link, and the link stays.
func TestProjectTargetRootSymlink_InstallAndUninstall(t *testing.T) {
	root := upDepProject(t, "  apm:\n    - ../dep\n")
	writeCompileFile(t, root, "dep/.apm/instructions/r.instructions.md", issue86Rule)
	if err := os.MkdirAll(filepath.Join(root, "dotfiles", "claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	link86(t, filepath.Join(root, "dotfiles", "claude"), ".claude")

	mustRunCLI(t, "install")
	assertIssue50Exists(t, true, filepath.Join(root, "dotfiles", "claude", "rules", "r.md"), filepath.Join(root, "dotfiles", "claude", "skills", "sk", "SKILL.md"))

	stdout, code := runCLI86(t, "uninstall", "../dep")

	if code != 0 || strings.Contains(stdout, "keeping") {
		t.Errorf("exit %d, stdout:\n%s\nwant exit 0 and no warning", code, stdout)
	}
	if got := tree86(t, filepath.Join(root, "dotfiles")); len(got) != 2 || got["claude"] != "dir" {
		t.Errorf("dotfiles = %v, want only the empty dotfiles/claude", got)
	}
	if info, err := os.Lstat(".claude"); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Errorf(".claude is no longer the user's symlink (lstat %v, %v)", info, err)
	}
}

// Feedback item 5, for a copy deploy: a destination whose directory leads
// outside is not written, and the old lock record of the bucket stays, as
// for any failed deploy.
func TestInstall_DestinationParentOutside_NotWritten(t *testing.T) {
	tests := []struct {
		name, dir, prim, dest string
	}{
		{name: "rules directory", dir: ".claude/rules", prim: "r", dest: ".claude/rules/r.md"},
		{name: "directory inside a skill", dir: ".claude/skills/sk/data", prim: "sk", dest: ".claude/skills/sk/data/precious.txt"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := installedLocal86(t)
			lockBefore := readText(t, "apm.lock.yaml")
			outside := linkOutside86(t, root, filepath.FromSlash(tt.dir), "moved")
			for _, f := range []string{"r.md", "precious.txt"} {
				if _, err := os.Stat(filepath.Join(outside, f)); err == nil {
					writeCompileFile(t, outside, f, "the user's own\n")
				}
			}
			outsideBefore := tree86(t, filepath.Join(root, "outside"))

			stdout, code := runCLI86(t, "install")

			real := realDir86(t, root)
			warning := " ! deploy " + tt.prim + " to claude failed: " + tt.dest + ": symlink " + tt.dir + " resolves to " +
				filepath.Join(real, "outside", "moved") + ", outside " + filepath.Join(real, "proj", ".claude") + "\n"
			if code != 0 || !strings.Contains(stdout, warning) {
				t.Errorf("exit %d, stdout:\n%s\nwant exit 0 and:\n%s", code, stdout, warning)
			}
			assertTree86(t, filepath.Join(root, "outside"), outsideBefore)
			if got := readText(t, "apm.lock.yaml"); got != lockBefore {
				t.Errorf("apm.lock.yaml changed:\n%s\nwant:\n%s", got, lockBefore)
			}
		})
	}
}

// Step 22 feedback test 2: leaf-rule and leaf-skill, install and update. The
// deployed file is a symlink out of .claude, so nothing outside changes. A
// rule refused that way fails the command before anything is written, with
// the Oracle's text and exit 1 (measured for install, also for a rule linked
// into the project outside .claude); a file of a skill is replaced by the
// package's file, as the Oracle replaces the skill's directory.
func TestInstall_DestinationLinksOutside_NotWritten(t *testing.T) {
	tests := []struct {
		name, dest string
		code       int
		transcript map[string]func(reason string) string
	}{
		{name: "rule", dest: ".claude/rules/r.md", code: 1, transcript: map[string]func(string) string{
			"install": func(reason string) string {
				return " > Installing dependencies from apm.yml...\n" +
					" i Targets: claude  (source: apm.yml)\n" +
					"   + ../dep (local)\n" +
					"   x 1 package failed:\n" +
					"    +- ../dep -- Failed to integrate primitives from local package: " + reason + "\n" +
					" x Installation failed with 1 error(s) in <t>s. No install transaction changes were committed.\n"
			},
			"update": func(reason string) string {
				return " i Targets: claude  (source: apm.yml)\n" +
					" x deploy: " + reason + "\n"
			},
		}},
		{name: "file in a skill", dest: ".claude/skills/sk/data/precious.txt", transcript: map[string]func(string) string{
			"install": func(string) string {
				return " > Installing dependencies from apm.yml...\n" +
					" i Targets: claude  (source: apm.yml)\n" +
					"   + ../dep (local)\n" +
					"  |-- 1 rule(s) adopted -> .claude/rules/\n" +
					"  |-- 1 skill(s) integrated -> .claude/skills/\n" +
					" + Installed 1 APM dependency in <t>s.\n"
			},
			"update": func(string) string {
				return " i Targets: claude  (source: apm.yml)\n" +
					"_local/dep\n" +
					"├── 1 instruction -> .claude/rules/\n" +
					"└── 1 skill -> .claude/skills/\n" +
					" i Already up to date\n"
			},
		}},
	}
	for _, tt := range tests {
		for _, cmd := range []string{"install", "update"} {
			t.Run(tt.name+" "+cmd, func(t *testing.T) {
				root := installedLocal86(t)
				lockBefore := readText(t, "apm.lock.yaml")
				writeCompileFile(t, root, "outside/precious.txt", "precious unrelated user content\n")
				dest := filepath.FromSlash(tt.dest)
				if err := os.Remove(dest); err != nil {
					t.Fatal(err)
				}
				link86(t, filepath.Join(root, "outside", "precious.txt"), dest)
				before := tree86(t, root)

				stdout, code := runCLI86(t, cmd)

				real := realDir86(t, root)
				reason := containment86(filepath.Join(cwd86(t), dest), filepath.Join(real, "proj", filepath.Dir(dest)), filepath.Join(real, "outside", "precious.txt"))
				if want := tt.transcript[cmd](reason); code != tt.code || stdout != want {
					t.Errorf("exit %d, stdout:\n%s\nwant exit %d, stdout:\n%s", code, stdout, tt.code, want)
				}
				if tt.code == 0 {
					before[filepath.ToSlash(filepath.Join("proj", dest))] = "precious\n"
				}
				assertTree86(t, root, before)
				if got := readText(t, "apm.lock.yaml"); got != lockBefore {
					t.Errorf("apm.lock.yaml changed:\n%s\nwant:\n%s", got, lockBefore)
				}
			})
		}
	}
}
