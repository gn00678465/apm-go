package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Expected values in this file are Oracle 0.33.0's, measured for issue #129
// items 47, 48, 51 and 52 (runs 47, 48, 48b, 48c, 48both, 51 and 52). The
// Oracle's lines that apm-go leaves out by an existing deviation (the
// redeploy lines of a package that received no file, the Skipped line of a
// missing target directory) are not expected.

// A path apm.yml declares twice is two declarations, so its uninstall is
// ambiguous and changes nothing (models/dependency/selection.py:39-105,
// commands/uninstall/engine.py:686-698).
func TestUninstall_PathDeclaredTwice_Ambiguous(t *testing.T) {
	root := t.TempDir()
	setTestHome(t, filepath.Join(root, "home"))
	proj := filepath.Join(root, "proj")
	writeCompileFile(t, proj, "dep/apm.yml", "name: dep\nversion: 1.0.0\n")
	writeCompileFile(t, proj, "dep/.apm/skills/s1/SKILL.md", "---\nname: s1\ndescription: d\n---\nbody\n")
	writeCompileFile(t, proj, "apm.yml", "name: proj\nversion: 1.0.0\ntargets: [claude]\ndependencies:\n  apm:\n    - ./dep\n    - path: ./dep\n")
	t.Chdir(proj)
	mustRunCLI(t, "install")
	before := treeSnapshot(t, proj)

	const want = " > Uninstalling 1 package(s)...\n" +
		" x Local dependency '_local/dep' is ambiguous: it matches 2 declarations. Use one exact path from apm.yml to uninstall a single dependency.\n" +
		" x Uninstall aborted: 1 requested package(s) could not be selected. Resolve the errors above and retry; no changes were made.\n"
	for _, args := range [][]string{{"uninstall", "./dep", "--dry-run"}, {"uninstall", "./dep"}} {
		stdout, stderr, exitCode := runCompileCLI(t, args...)
		if exitCode != 1 || stderr != "" || stdout != want {
			t.Errorf("%v: exit = %d, stderr = %q, stdout:\n%s\nwant:\n%s", args, exitCode, stderr, stdout, want)
		}
	}
	if after := treeSnapshot(t, proj); !reflect.DeepEqual(before, after) {
		t.Errorf("files changed:\nbefore %v\nafter  %v", before, after)
	}
}

const (
	sharedSkillA = "---\nname: sa\ndescription: d\n---\nfrom a\n"
	sharedSkillB = "---\nname: sb\ndescription: d\n---\nfrom b\n"
	sharedHashA  = "sha256:948924641b6ef52d88f911e24cf7c03abcf86903b84ff022afef1995bbbfff67"
	sharedHashB  = "sha256:a5ffeb45fd924ad03f2ce64c9e6bd1fcb62f7b2bfde19067c6fac0f67619fe97"
)

// sharedLocalLock is the lock Oracle 0.33.0's install writes for local
// packages that declare one directory name, the given paths in order, the
// last one's skill deployed.
func sharedLocalLock(paths ...string) string {
	var b strings.Builder
	b.WriteString("lockfile_version: '1'\napm_version: 0.33.0\ndependencies:\n")
	for i, p := range paths {
		b.WriteString("- repo_url: _local/dep\n  name: dep\n  version: 1.0.0\n  package_type: apm_package\n")
		if i == len(paths)-1 && p == "./b/dep" {
			b.WriteString("  deployed_files:\n  - .claude/skills/sb\n  - .claude/skills/sb/SKILL.md\n  deployed_file_hashes:\n    .claude/skills/sb/SKILL.md: " + sharedHashB + "\n")
		}
		b.WriteString("  source: local\n  local_path: " + p + "\n")
	}
	if paths[len(paths)-1] == "./b/dep" {
		b.WriteString(sharedDeployments("sb", "./b/dep", sharedHashB))
	} else {
		b.WriteString("deployments: []\n")
	}
	return b.String()
}

func sharedDeployments(skill, owner, hash string) string {
	row := func(value, contentHash string) string {
		return "- kind: project-relative\n  target: claude\n  value: " + value + "\n  runtime: null\n  scope: project\n  owners:\n  - " + owner + "\n  active_owner: " + owner + "\n  content_hash: " + contentHash + "\n"
	}
	return "deployments:\n" + row(".claude/skills/"+skill, "null") + row(".claude/skills/"+skill+"/SKILL.md", hash)
}

// sharedLocalProject writes the state Oracle 0.33.0's install leaves for
// ./a/dep, ./b/dep and, with three, ./c/dep, which share apm_modules/_local/dep:
// the last one copied holds the directory.
func sharedLocalProject(t *testing.T, three bool) string {
	t.Helper()
	root := t.TempDir()
	setTestHome(t, filepath.Join(root, "home"))
	proj := filepath.Join(root, "proj")
	writeCompileFile(t, proj, "a/dep/apm.yml", "name: dep\nversion: 1.0.0\n")
	writeCompileFile(t, proj, "a/dep/.apm/skills/sa/SKILL.md", sharedSkillA)
	writeCompileFile(t, proj, "b/dep/apm.yml", "name: dep\nversion: 2.0.0\n")
	writeCompileFile(t, proj, "b/dep/.apm/skills/sb/SKILL.md", sharedSkillB)
	writeCompileFile(t, proj, ".gitignore", "\n# APM dependencies\napm_modules/\n")
	paths := []string{"./a/dep", "./b/dep"}
	if three {
		writeCompileFile(t, proj, "c/dep/apm.yml", "name: dep\nversion: 1.0.0\n")
		writeCompileFile(t, proj, "apm_modules/_local/dep/apm.yml", "name: dep\nversion: 1.0.0\n")
		paths = append(paths, "./c/dep")
	} else {
		writeCompileFile(t, proj, "apm_modules/_local/dep/apm.yml", "name: dep\nversion: 2.0.0\n")
		writeCompileFile(t, proj, "apm_modules/_local/dep/.apm/skills/sb/SKILL.md", sharedSkillB)
		writeCompileFile(t, proj, ".claude/skills/sb/SKILL.md", sharedSkillB)
	}
	manifest := "name: proj\nversion: 1.0.0\ntargets: [claude]\ndependencies:\n  apm:\n"
	for _, p := range paths {
		manifest += "    - " + p + "\n"
	}
	writeCompileFile(t, proj, "apm.yml", manifest)
	writeCompileFile(t, proj, "apm.lock.yaml", sharedLocalLock(paths...))
	t.Chdir(proj)
	return proj
}

// Removing one of two local declarations that share apm_modules/_local/dep
// gives the directory the copy of the one that stays and keeps its lock
// entry (commands/uninstall/engine.py:177-261,813-880).
func TestUninstall_SharedLocalSlot_Refresh(t *testing.T) {
	tests := []struct {
		name, arg, stays string
		cleaned          string
		handedOver       string
		lock             string
		modules          map[string]string
		claude           map[string]string
	}{
		{
			// The lock records the new copy's version, as the next install
			// records it. Deviation: Oracle 0.33.0 keeps the version of the
			// copy it replaced (1.0.0), which its next install rewrites.
			name: "the other declaration", arg: "./a/dep", stays: "./b/dep",
			handedOver: "  |-- 1 skill(s) integrated -> .claude/skills/\n",
			lock: "lockfile_version: '1'\napm_version: 0.33.0\ndependencies:\n- repo_url: _local/dep\n  name: dep\n  version: 2.0.0\n  package_type: apm_package\n" +
				"  deployed_files:\n  - .claude/skills/sb\n  - .claude/skills/sb/SKILL.md\n  deployed_file_hashes:\n    .claude/skills/sb/SKILL.md: " + sharedHashB + "\n" +
				"  source: local\n  local_path: ./b/dep\n" + sharedDeployments("sb", "./b/dep", sharedHashB),
			modules: map[string]string{"apm.yml": "name: dep\nversion: 2.0.0\n", ".apm/skills/sb/SKILL.md": sharedSkillB},
			claude:  map[string]string{"skills/sb/SKILL.md": sharedSkillB},
		},
		{
			name: "the declaration whose copy is there", arg: "./b/dep", stays: "./a/dep",
			cleaned:    " i Cleaned 2 stale files from ./b/dep\n",
			handedOver: "  |-- 1 skill(s) integrated -> .claude/skills/\n",
			lock: "lockfile_version: '1'\napm_version: 0.33.0\ndependencies:\n- repo_url: _local/dep\n  name: dep\n  version: 1.0.0\n  package_type: apm_package\n" +
				"  deployed_files:\n  - .claude/skills/sa\n  - .claude/skills/sa/SKILL.md\n  deployed_file_hashes:\n    .claude/skills/sa/SKILL.md: " + sharedHashA + "\n" +
				"  source: local\n  local_path: ./a/dep\n" + sharedDeployments("sa", "./a/dep", sharedHashA),
			modules: map[string]string{"apm.yml": "name: dep\nversion: 1.0.0\n", ".apm/skills/sa/SKILL.md": sharedSkillA},
			claude:  map[string]string{"skills/sa/SKILL.md": sharedSkillA},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proj := sharedLocalProject(t, false)
			before := treeSnapshot(t, proj)

			wantDry := " > Uninstalling 1 package(s)...\n + _local/dep - found in apm.yml\n" +
				" i Dry run: Would remove 1 package(s):\n i   - _local/dep from apm.yml\n" +
				" i   - refresh _local/dep in apm_modules/ for 1 surviving declaration(s)\n" +
				" + Dry run complete - no changes made\n"
			if got := mustRunCLI(t, "uninstall", "--dry-run", tt.arg); got != wantDry {
				t.Errorf("dry run:\n%s\nwant:\n%s", got, wantDry)
			}
			if after := treeSnapshot(t, proj); !reflect.DeepEqual(before, after) {
				t.Fatalf("dry run changed files")
			}
			c := &settled{deployRoot: proj, manifestDir: proj}
			c.start(t)

			want := " > Uninstalling 1 package(s)...\n + _local/dep - found in apm.yml\n" +
				" i Refreshed _local/dep in apm_modules/ for 1 surviving declaration(s)\n" + tt.cleaned +
				" i Removed _local/dep from dependencies.apm in apm.yml\n" +
				" + Updated " + filepath.Join(proj, "apm.yml") + " (removed 1 package(s))\n" + tt.handedOver +
				" + Uninstall complete: Removed 1 package(s) from apm.yml\n"
			if got := mustRunCLI(t, "uninstall", tt.arg); got != want {
				t.Errorf("uninstall:\n%s\nwant:\n%s", got, want)
			}
			wantManifest := "name: proj\nversion: 1.0.0\ntargets: [claude]\ndependencies:\n  apm:\n    - " + tt.stays + "\n"
			if got := readFileString(t, "apm.yml"); got != wantManifest {
				t.Errorf("apm.yml:\n%s\nwant:\n%s", got, wantManifest)
			}
			if got := readFileString(t, "apm.lock.yaml"); got != tt.lock {
				t.Errorf("apm.lock.yaml:\n%s\nwant:\n%s", got, tt.lock)
			}
			if got := treeSnapshot(t, filepath.Join(proj, "apm_modules")); !reflect.DeepEqual(got, prefixed("_local/dep/", tt.modules)) {
				t.Errorf("apm_modules = %v, want %v", got, tt.modules)
			}
			if got := treeSnapshot(t, filepath.Join(proj, ".claude")); !reflect.DeepEqual(got, prefixed("", tt.claude)) {
				t.Errorf(".claude = %v, want %v", got, tt.claude)
			}
			c.check(t)
		})
	}
}

// With two declarations still sharing the directory, the dry run previews
// the refresh and the uninstall refuses before any write
// (commands/uninstall/engine.py:205-216).
func TestUninstall_SharedLocalSlot_TwoSurvivors(t *testing.T) {
	proj := sharedLocalProject(t, true)
	before := treeSnapshot(t, proj)

	wantDry := " > Uninstalling 1 package(s)...\n + _local/dep - found in apm.yml\n" +
		" i Dry run: Would remove 1 package(s):\n i   - _local/dep from apm.yml\n" +
		" i   - refresh _local/dep in apm_modules/ for 2 surviving declaration(s)\n" +
		" + Dry run complete - no changes made\n"
	if got := mustRunCLI(t, "uninstall", "--dry-run", "./a/dep"); got != wantDry {
		t.Errorf("dry run:\n%s\nwant:\n%s", got, wantDry)
	}
	const want = " > Uninstalling 1 package(s)...\n + _local/dep - found in apm.yml\n" +
		" x Cannot preserve _local/dep because 2 declarations would still share one install slot. Use exact paths from apm.yml in one uninstall command so at most one declaration remains.\n" +
		" x Error uninstalling packages: Multiple local declarations would remain in one shared install slot. No manifest changes were made.\n"
	stdout, stderr, exitCode := runCompileCLI(t, "uninstall", "./a/dep")
	if exitCode != 1 || stderr != "" || stdout != want {
		t.Errorf("exit = %d, stderr = %q, stdout:\n%s\nwant:\n%s", exitCode, stderr, stdout, want)
	}
	if after := treeSnapshot(t, proj); !reflect.DeepEqual(before, after) {
		t.Errorf("files changed:\nbefore %v\nafter  %v", before, after)
	}
}

// Removing both declarations removes the directory once; the second finds it
// gone (commands/uninstall/engine.py:813-880).
func TestUninstall_SharedLocalSlot_BothRemoved(t *testing.T) {
	proj := sharedLocalProject(t, false)

	want := " > Uninstalling 2 package(s)...\n + _local/dep - found in apm.yml\n + _local/dep - found in apm.yml\n" +
		" i Removed _local/dep from apm_modules/\n ! Package _local/dep not found in apm_modules/\n" +
		" i Cleaned 2 stale files from ./b/dep\n" +
		" i Removed _local/dep from dependencies.apm in apm.yml\n i Removed _local/dep from dependencies.apm in apm.yml\n" +
		" + Updated " + filepath.Join(proj, "apm.yml") + " (removed 2 package(s))\n" +
		" + Uninstall complete: Removed 2 package(s) from apm.yml, Removed 1 package(s) from apm_modules/\n"
	if got := mustRunCLI(t, "uninstall", "./a/dep", "./b/dep"); got != want {
		t.Errorf("uninstall:\n%s\nwant:\n%s", got, want)
	}
	for _, p := range []string{"apm.lock.yaml", ".claude", "apm_modules/_local"} {
		if _, err := os.Lstat(p); !os.IsNotExist(err) {
			t.Errorf("%s is still there (%v)", p, err)
		}
	}
}

// A marketplace argument names the package it resolved to in its found line
// (commands/uninstall/engine.py:675-679).
func TestUninstall_MarketplaceFoundLineNamesCanonical(t *testing.T) {
	_, market := marketplaceLocal70(t)
	writeCompileFile(t, market, "marketplace.json", `{"name": "sample", "plugins": [{"name": "foo", "source": {"type": "github", "repo": "acme/foo"}}]}`)
	mustRunCLI(t, "marketplace", "update", "sample")
	writeCompileFile(t, ".", "apm_modules/acme/foo/apm.yml", "name: foo\nversion: 1.0.0\n")
	writeCompileFile(t, ".", "apm.yml", "name: repro\nversion: 1.0.0\ndependencies:\n  apm:\n  - acme/foo\n")
	writeCompileFile(t, ".", "apm.lock.yaml", "lockfile_version: '1'\ndependencies:\n- repo_url: acme/foo\n  resolved_commit: 0123456789abcdef0123456789abcdef01234567\n  version: 1.0.0\n  discovered_via: sample\n  marketplace_plugin_name: foo\n")

	got := mustRunCLI(t, "uninstall", "--dry-run", "foo@sample")
	const want = " > Uninstalling 1 package(s)...\n + foo@sample - found in apm.yml (as acme/foo)\n" +
		" i Dry run: Would remove 1 package(s):\n i   - acme/foo from apm.yml\n i   - acme/foo from apm_modules/\n" +
		" + Dry run complete - no changes made\n"
	if got != want {
		t.Errorf("dry run:\n%s\nwant:\n%s", got, want)
	}
}

// A plugin installed from a local-directory marketplace is removed by its
// marketplace name: the lock records it by its path
// (models/dependency/identity.py:212-215).
func TestUninstall_LocalMarketplacePlugin(t *testing.T) {
	proj, market := marketplaceLocal70(t)
	mustRunCLI(t, "install", "unlocked@sample", "--target", "claude")
	plugin := filepath.Join(market, "plugin")

	got := mustRunCLI(t, "uninstall", "unlocked@sample")
	want := " > Uninstalling 1 package(s)...\n + unlocked@sample - found in apm.yml (as " + plugin + ")\n" +
		" i Removed _local/plugin from apm_modules/\n" +
		" i Cleaned 3 stale files from " + plugin + "\n" +
		" i Removed _local/plugin from dependencies.apm in apm.yml\n" +
		" + Updated " + filepath.Join(proj, "apm.yml") + " (removed 1 package(s))\n" +
		" + Uninstall complete: Removed 1 package(s) from apm.yml, Removed 1 package(s) from apm_modules/\n"
	if got != want {
		t.Errorf("uninstall:\n%s\nwant:\n%s", got, want)
	}
	if got, want := readFileString(t, "apm.yml"), "name: repro\nversion: 1.0.0\ndependencies:\n  apm:\n    - ./dep\n"; got != want {
		t.Errorf("apm.yml:\n%s\nwant:\n%s", got, want)
	}
}

// prefixed gives the treeSnapshot of files whose contents are m, each path
// below prefix.
func prefixed(prefix string, m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		sum := sha256.Sum256([]byte(v))
		out[prefix+k] = hex.EncodeToString(sum[:])
	}
	return out
}
