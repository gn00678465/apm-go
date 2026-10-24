package main

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

// Expected values in this file are Oracle 0.33.0's, measured for the review
// of PR #171 (runs below /home/madao/.cache/apm-go-oracle-ref/B/: modified,
// useredit, modagent, useragent, agentsame, nolock, empty, aliasslot and
// multi). The Oracle's lines that apm-go leaves out by an existing deviation
// are not expected.

const (
	sharedAgentB     = "---\nname: rb\ndescription: d\n---\nagent b\n"
	sharedAgentHashB = "sha256:4f8f6347c801191f6709955c4360fd7d2291b8f285a73a792960fe404cfb5b4f"
	sharedSkillNewB  = "---\nname: sb\ndescription: d\n---\nNEW B\n"
	sharedHashNewB   = "sha256:28657ad9f6f117a7fd49822b5ddf2487d19292197beb08c58931e05b50e4b48a"
	sharedAgentNewB  = "---\nname: rb\ndescription: d\n---\nNEW AGENT\n"
	sharedHashNewAg  = "sha256:d24da976316543d9623032a40c138ac108561bae3f880c693b5869e1da2f54b5"
)

// withSharedAgent gives ./b/dep, its copy and the deployed state an agent, as
// Oracle 0.33.0's install records it.
func withSharedAgent(t *testing.T, proj string) {
	t.Helper()
	writeCompileFile(t, proj, "b/dep/.apm/agents/rb.agent.md", sharedAgentB)
	writeCompileFile(t, proj, "apm_modules/_local/dep/.apm/agents/rb.agent.md", sharedAgentB)
	writeCompileFile(t, proj, ".claude/agents/rb.md", sharedAgentB)
	lock := sharedLocalLock("./a/dep", "./b/dep")
	lock = strings.Replace(lock, "  deployed_files:\n", "  deployed_files:\n  - .claude/agents/rb.md\n", 1)
	lock = strings.Replace(lock, "  deployed_file_hashes:\n", "  deployed_file_hashes:\n    .claude/agents/rb.md: "+sharedAgentHashB+"\n", 1)
	writeCompileFile(t, proj, "apm.lock.yaml", lock)
}

// The package that stays has its deployed files written again from its new
// copy whatever is on disk, and the lock records what is written, so a frozen
// install of the result passes (commands/uninstall/cli.py:516-524,544-585).
func TestUninstall_SharedLocalSlot_RedeploysSurvivor(t *testing.T) {
	const skillOnly = "  |-- 1 skill(s) integrated -> .claude/skills/\n"
	const agentAndSkill = "  |-- 1 agents integrated -> .claude/agents/\n" + skillOnly
	tests := []struct {
		name   string
		setup  func(t *testing.T, proj string)
		blocks string
		claude map[string]string
		hashes map[string]string
	}{
		{
			name: "source skill changed",
			setup: func(t *testing.T, proj string) {
				writeCompileFile(t, proj, "b/dep/.apm/skills/sb/SKILL.md", sharedSkillNewB)
			},
			blocks: skillOnly,
			claude: map[string]string{"skills/sb/SKILL.md": sharedSkillNewB},
			hashes: map[string]string{".claude/skills/sb/SKILL.md": sharedHashNewB},
		},
		{
			name: "deployed skill edited",
			setup: func(t *testing.T, proj string) {
				writeCompileFile(t, proj, ".claude/skills/sb/SKILL.md", "---\nname: sb\ndescription: d\n---\nUSER\n")
			},
			blocks: skillOnly,
			claude: map[string]string{"skills/sb/SKILL.md": sharedSkillB},
			hashes: map[string]string{".claude/skills/sb/SKILL.md": sharedHashB},
		},
		{
			name: "source agent and skill changed",
			setup: func(t *testing.T, proj string) {
				withSharedAgent(t, proj)
				writeCompileFile(t, proj, "b/dep/.apm/skills/sb/SKILL.md", sharedSkillNewB)
				writeCompileFile(t, proj, "b/dep/.apm/agents/rb.agent.md", sharedAgentNewB)
			},
			blocks: agentAndSkill,
			claude: map[string]string{"skills/sb/SKILL.md": sharedSkillNewB, "agents/rb.md": sharedAgentNewB},
			hashes: map[string]string{".claude/skills/sb/SKILL.md": sharedHashNewB, ".claude/agents/rb.md": sharedHashNewAg},
		},
		{
			name: "deployed agent edited",
			setup: func(t *testing.T, proj string) {
				withSharedAgent(t, proj)
				writeCompileFile(t, proj, ".claude/agents/rb.md", "---\nname: rb\ndescription: d\n---\nUSER AGENT\n")
			},
			blocks: agentAndSkill,
			claude: map[string]string{"skills/sb/SKILL.md": sharedSkillB, "agents/rb.md": sharedAgentB},
			hashes: map[string]string{".claude/skills/sb/SKILL.md": sharedHashB, ".claude/agents/rb.md": sharedAgentHashB},
		},
		{
			// The Oracle cleans the files before it deploys them again, so an
			// agent with the same bytes is integrated, not adopted.
			name:   "nothing changed",
			setup:  withSharedAgent,
			blocks: agentAndSkill,
			claude: map[string]string{"skills/sb/SKILL.md": sharedSkillB, "agents/rb.md": sharedAgentB},
			hashes: map[string]string{".claude/skills/sb/SKILL.md": sharedHashB, ".claude/agents/rb.md": sharedAgentHashB},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proj := sharedLocalProject(t, false)
			tt.setup(t, proj)

			want := " > Uninstalling 1 package(s)...\n + _local/dep - found in apm.yml\n" +
				" i Refreshed _local/dep in apm_modules/ for 1 surviving declaration(s)\n" +
				" i Removed _local/dep from dependencies.apm in apm.yml\n" +
				" + Updated " + filepath.Join(proj, "apm.yml") + " (removed 1 package(s))\n" + tt.blocks +
				" + Uninstall complete: Removed 1 package(s) from apm.yml\n"
			if got := mustRunCLI(t, "uninstall", "./a/dep"); got != want {
				t.Errorf("uninstall:\n%s\nwant:\n%s", got, want)
			}
			if got := treeSnapshot(t, filepath.Join(proj, ".claude")); !reflect.DeepEqual(got, prefixed("", tt.claude)) {
				t.Errorf(".claude = %v, want %v", got, prefixed("", tt.claude))
			}
			lock := readFileString(t, "apm.lock.yaml")
			for p, hash := range tt.hashes {
				if !strings.Contains(lock, "    "+p+": "+hash+"\n") {
					t.Errorf("apm.lock.yaml does not record %s: %s:\n%s", p, hash, lock)
				}
			}
			mustRunCLI(t, "install", "--frozen")
		})
	}
}

// With no lock the refresh completes and no file is handed over
// (commands/uninstall/cli.py:436-505). Deviation: the Oracle also prints the
// lines of the packages it deploys again, which apm-go prints only for a
// package that received a recorded path.
func TestUninstall_SharedLocalSlot_NoLock(t *testing.T) {
	proj := sharedLocalProject(t, false)
	if err := os.Remove("apm.lock.yaml"); err != nil {
		t.Fatal(err)
	}

	want := " > Uninstalling 1 package(s)...\n + _local/dep - found in apm.yml\n" +
		" i Refreshed _local/dep in apm_modules/ for 1 surviving declaration(s)\n" +
		" i Removed _local/dep from dependencies.apm in apm.yml\n" +
		" + Updated " + filepath.Join(proj, "apm.yml") + " (removed 1 package(s))\n" +
		" + Uninstall complete: Removed 1 package(s) from apm.yml\n"
	if got := mustRunCLI(t, "uninstall", "./a/dep"); got != want {
		t.Errorf("uninstall:\n%s\nwant:\n%s", got, want)
	}
	if got, want := readFileString(t, "apm.yml"), "name: proj\nversion: 1.0.0\ntargets: [claude]\ndependencies:\n  apm:\n    - ./b/dep\n"; got != want {
		t.Errorf("apm.yml:\n%s\nwant:\n%s", got, want)
	}
	wantModules := prefixed("_local/dep/", map[string]string{"apm.yml": "name: dep\nversion: 2.0.0\n", ".apm/skills/sb/SKILL.md": sharedSkillB})
	if got := treeSnapshot(t, filepath.Join(proj, "apm_modules")); !reflect.DeepEqual(got, wantModules) {
		t.Errorf("apm_modules = %v, want %v", got, wantModules)
	}
	if _, err := os.Lstat("apm.lock.yaml"); !os.IsNotExist(err) {
		t.Errorf("apm.lock.yaml was written (%v)", err)
	}
}

// A source that is no longer a package (no apm.yml, SKILL.md or plugin.json)
// is not staged, and nothing changes (install/phases/local_content.py:253-266,
// commands/uninstall/engine.py:225-243).
func TestUninstall_SharedLocalSlot_SourceNotAPackage(t *testing.T) {
	proj := sharedLocalProject(t, false)
	for _, p := range []string{"b/dep/apm.yml", "b/dep/.apm/skills/sb/SKILL.md"} {
		if err := os.Remove(p); err != nil {
			t.Fatal(err)
		}
	}
	before := treeSnapshot(t, proj)

	const want = " > Uninstalling 1 package(s)...\n + _local/dep - found in apm.yml\n" +
		" x Failed to refresh _local/dep from its declared local path. Check the exact path in apm.yml, run 'apm-go install', and retry.\n" +
		" x Error uninstalling packages: A surviving local dependency could not be staged. No manifest changes were made.\n"
	stdout, stderr, exitCode := runCompileCLI(t, "uninstall", "./a/dep")
	if exitCode != 1 || stderr != "" || stdout != want {
		t.Errorf("exit = %d, stderr = %q, stdout:\n%s\nwant:\n%s", exitCode, stderr, stdout, want)
	}
	if after := treeSnapshot(t, proj); !reflect.DeepEqual(before, after) {
		t.Errorf("files changed:\nbefore %v\nafter  %v", before, after)
	}
}

// Two declarations that share apm_modules/_local through `alias: _local`.
// Deviation: Oracle 0.33.0 stages the copy below apm_modules/_local, the
// directory it replaces, and ends with exit 1 and the shared-slot refresh
// failure (commands/uninstall/engine.py:192-195,262-289; measured); apm-go
// stages it beside the package directories and refreshes the directory.
func TestUninstall_SharedLocalSlot_AliasLocal(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", filepath.Join(root, "home"))
	proj := filepath.Join(root, "proj")
	writeCompileFile(t, proj, "a/dep/apm.yml", "name: dep\nversion: 1.0.0\n")
	writeCompileFile(t, proj, "a/dep/.apm/skills/sa/SKILL.md", sharedSkillA)
	writeCompileFile(t, proj, "b/dep/apm.yml", "name: dep\nversion: 2.0.0\n")
	writeCompileFile(t, proj, "b/dep/.apm/skills/sb/SKILL.md", sharedSkillB)
	writeCompileFile(t, proj, "apm_modules/_local/apm.yml", "name: dep\nversion: 2.0.0\n")
	writeCompileFile(t, proj, "apm_modules/_local/.apm/skills/sb/SKILL.md", sharedSkillB)
	writeCompileFile(t, proj, ".claude/skills/sb/SKILL.md", sharedSkillB)
	writeCompileFile(t, proj, ".gitignore", "\n# APM dependencies\napm_modules/\n")
	writeCompileFile(t, proj, "apm.yml", "name: proj\nversion: 1.0.0\ntargets: [claude]\ndependencies:\n  apm:\n"+
		"    - path: ./a/dep\n      alias: _local\n    - path: ./b/dep\n      alias: _local\n")
	writeCompileFile(t, proj, "apm.lock.yaml", strings.ReplaceAll(sharedLocalLock("./a/dep", "./b/dep"), "- repo_url: _local/dep\n", "- repo_url: _local/dep\n  alias: _local\n"))
	t.Chdir(proj)

	want := " > Uninstalling 1 package(s)...\n + _local/dep - found in apm.yml\n" +
		" i Refreshed _local/dep in apm_modules/ for 1 surviving declaration(s)\n" +
		" i Removed _local/dep from dependencies.apm in apm.yml\n" +
		" + Updated " + filepath.Join(proj, "apm.yml") + " (removed 1 package(s))\n" +
		"  |-- 1 skill(s) integrated -> .claude/skills/\n" +
		" + Uninstall complete: Removed 1 package(s) from apm.yml\n"
	if got := mustRunCLI(t, "uninstall", "./a/dep"); got != want {
		t.Errorf("uninstall:\n%s\nwant:\n%s", got, want)
	}
	if got, want := readFileString(t, "apm.yml"), "name: proj\nversion: 1.0.0\ntargets: [claude]\ndependencies:\n  apm:\n    - path: ./b/dep\n      alias: _local\n"; got != want {
		t.Errorf("apm.yml:\n%s\nwant:\n%s", got, want)
	}
	wantModules := prefixed("_local/", map[string]string{"apm.yml": "name: dep\nversion: 2.0.0\n", ".apm/skills/sb/SKILL.md": sharedSkillB})
	if got := treeSnapshot(t, filepath.Join(proj, "apm_modules")); !reflect.DeepEqual(got, wantModules) {
		t.Errorf("apm_modules = %v, want %v", got, wantModules)
	}
	mustRunCLI(t, "install", "--frozen")
}

// When one of several shared directories cannot be replaced, every directory
// already replaced gets its original back and apm.yml, the lock and
// apm_modules stay as they were, with the Oracle's failure line
// (commands/uninstall/engine.py:262-289, cli.py:451-453; measured with
// `alias: _local`, where the Oracle's refresh fails the same way).
func TestUninstall_SharedLocalSlot_ActivationFailureRollsBack(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a directory the process cannot write")
	}
	root := t.TempDir()
	t.Setenv("HOME", filepath.Join(root, "home"))
	proj := filepath.Join(root, "proj")
	manifest := "name: proj\nversion: 1.0.0\ntargets: [claude]\ndependencies:\n  apm:\n"
	lock := "lockfile_version: '1'\ndependencies:\n"
	for _, slot := range []struct{ a, b, dir, alias string }{{"a1", "b1", "AAA", "AAA"}, {"a", "b", "_local/dep", ""}} {
		for _, key := range []string{slot.a, slot.b} {
			writeCompileFile(t, proj, key+"/dep/apm.yml", "name: dep\nversion: 1.0.0\n")
			writeCompileFile(t, proj, key+"/dep/.apm/skills/s"+key+"/SKILL.md", "---\nname: s"+key+"\ndescription: d\n---\nbody\n")
			if slot.alias != "" {
				manifest += "    - path: ./" + key + "/dep\n      alias: " + slot.alias + "\n"
				lock += "- repo_url: _local/dep\n  alias: " + slot.alias + "\n  source: local\n  local_path: ./" + key + "/dep\n"
			} else {
				manifest += "    - ./" + key + "/dep\n"
				lock += "- repo_url: _local/dep\n  source: local\n  local_path: ./" + key + "/dep\n"
			}
		}
		writeCompileFile(t, proj, "apm_modules/"+slot.dir+"/apm.yml", "name: dep\nversion: 1.0.0\n")
		writeCompileFile(t, proj, "apm_modules/"+slot.dir+"/.apm/skills/s"+slot.b+"/SKILL.md", "---\nname: s"+slot.b+"\ndescription: d\n---\nbody\n")
	}
	writeCompileFile(t, proj, "apm.yml", manifest)
	writeCompileFile(t, proj, "apm.lock.yaml", lock)
	t.Chdir(proj)
	locked := filepath.Join(proj, "apm_modules", "_local")
	if err := os.Chmod(locked, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
	before := treeSnapshot(t, proj)

	const want = " > Uninstalling 2 package(s)...\n + _local/dep - found in apm.yml\n + _local/dep - found in apm.yml\n" +
		" x Uninstall incomplete: shared-slot refresh failed. apm.yml and lockfile ownership were retained; some package files may already have been removed. Resolve the filesystem error and retry the same uninstall command, or run 'apm-go install' to restore the declared packages. Run with --verbose for detailed diagnostics.\n"
	stdout, stderr, exitCode := runCompileCLI(t, "uninstall", "./a/dep", "./a1/dep")
	if exitCode != 1 || stderr != "" || stdout != want {
		t.Errorf("exit = %d, stderr = %q, stdout:\n%s\nwant:\n%s", exitCode, stderr, stdout, want)
	}
	if after := treeSnapshot(t, proj); !reflect.DeepEqual(before, after) {
		t.Errorf("files changed:\nbefore %v\nafter  %v", before, after)
	}
}

// At user scope the package that stays is deployed again from its new copy
// too: the skill an earlier copy deploy left takes the new content, here
// through apm-go's skill symlink where the Oracle copies it, and the lock
// records that content (measured, global-modified).
func TestUninstallGlobal_SharedLocalSlot_RedeploysSurvivor(t *testing.T) {
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	chdirTemp(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	oldDir, oldDeploy := globalDirOverride, globalDeployDirOverride
	globalDirOverride, globalDeployDirOverride = filepath.Join(home, ".apm"), home
	t.Cleanup(func() { globalDirOverride, globalDeployDirOverride = oldDir, oldDeploy })
	apm := filepath.Join(home, ".apm")
	writeCompileFile(t, apm, "a/dep/apm.yml", "name: dep\nversion: 1.0.0\n")
	writeCompileFile(t, apm, "a/dep/.apm/skills/sa/SKILL.md", sharedSkillA)
	writeCompileFile(t, apm, "b/dep/apm.yml", "name: dep\nversion: 1.0.0\n")
	writeCompileFile(t, apm, "b/dep/.apm/skills/sb/SKILL.md", sharedSkillNewB)
	writeCompileFile(t, apm, "apm_modules/_local/dep/apm.yml", "name: dep\nversion: 1.0.0\n")
	writeCompileFile(t, apm, "apm_modules/_local/dep/.apm/skills/sb/SKILL.md", sharedSkillB)
	writeCompileFile(t, home, ".claude/skills/sb/SKILL.md", sharedSkillB)
	writeCompileFile(t, apm, "apm.yml", "name: g\nversion: 1.0.0\ntargets: [claude]\ndependencies:\n  apm:\n    - ./a/dep\n    - ./b/dep\n")
	writeCompileFile(t, apm, "apm.lock.yaml", "lockfile_version: '1'\ndependencies:\n"+
		"- repo_url: _local/dep\n  source: local\n  local_path: ./a/dep\n"+
		"- repo_url: _local/dep\n  deployed_files:\n  - .claude/skills/sb\n  - .claude/skills/sb/SKILL.md\n"+
		"  deployed_file_hashes:\n    .claude/skills/sb/SKILL.md: "+sharedHashB+"\n  source: local\n  local_path: ./b/dep\n")

	want := " > Uninstalling 1 package(s)...\n + _local/dep - found in apm.yml\n" +
		" i Refreshed _local/dep in apm_modules/ for 1 surviving declaration(s)\n" +
		" i Removed _local/dep from dependencies.apm in apm.yml\n" +
		" + Updated " + filepath.Join(apm, "apm.yml") + " (removed 1 package(s))\n" +
		"  |-- 1 skill(s) integrated -> .claude/skills/\n" +
		" + Uninstall complete: Removed 1 package(s) from apm.yml\n"
	if got := mustRunCLI(t, "uninstall", "-g", "./a/dep"); got != want {
		t.Errorf("uninstall:\n%s\nwant:\n%s", got, want)
	}
	if got := readFileString(t, filepath.Join(home, ".claude", "skills", "sb", "SKILL.md")); got != sharedSkillNewB {
		t.Errorf("~/.claude/skills/sb/SKILL.md = %q, want %q", got, sharedSkillNewB)
	}
	if lock := readFileString(t, filepath.Join(apm, "apm.lock.yaml")); !strings.Contains(lock, "    .claude/skills/sb/SKILL.md: "+sharedHashNewB+"\n") {
		t.Errorf("apm.lock.yaml does not record the new skill:\n%s", lock)
	}
	mustRunCLI(t, "install", "-g", "--frozen")
}
