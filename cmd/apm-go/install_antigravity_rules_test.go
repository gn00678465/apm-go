package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// agRulesProject has ./dep with one instruction and one of its own, as in
// skex/scripts/mkag.sh, and installs to antigravity alone.
func agRulesProject(t *testing.T) string {
	t.Helper()
	root := bootstrapProject(t, "proj")
	writeCompileFile(t, root, "proj/apm.yml", "name: proj\nversion: 1.0.0\ntargets: [antigravity]\ndependencies:\n  apm:\n    - ./dep\n")
	writeCompileFile(t, root, "proj/dep/apm.yml", "name: dep\nversion: 1.0.0\n")
	writeCompileFile(t, root, "proj/dep/.apm/instructions/single.instructions.md", "---\napplyTo: \"src/**/*.py\"\n---\n# single\nbody\n")
	writeCompileFile(t, root, "proj/.apm/instructions/own.instructions.md", "---\napplyTo: \"**/*.py\"\ntitle: x\n---\nown\n")
	return filepath.Join(root, "proj")
}

// The rules Oracle 0.33.0 writes for agRulesProject (skex/ag1/or).
const (
	agSingleRule = "---\ntrigger: glob\nglobs: \"src/**/*.py\"\n---\n\n# single\nbody\n"
	agOwnRule    = "---\ntrigger: glob\nglobs: \"**/*.py\"\n---\n\nown\n"
)

func readProjFile(t *testing.T, proj, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(proj, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// Antigravity's instructions are rule(s) in the install block, and a file
// the user put where the project's rule goes is written over and not
// counted as skipped (skex/ag1, ag4).
func TestInstall_AntigravityRulesSummary(t *testing.T) {
	proj := agRulesProject(t)
	writeCompileFile(t, proj, ".agents/rules/own.md", "user file\n")
	stdout, stderr, code := runCompileCLI(t, "install")
	if code != 0 {
		t.Fatalf("exit %d\n%s\n%s", code, stdout, stderr)
	}
	for _, line := range []string{
		"  |-- 1 rule(s) integrated -> .agents/plugins/dep/rules/\n",
		"  |-- 1 rule(s) integrated -> .agents/rules/\n",
	} {
		if !strings.Contains(stdout, line) {
			t.Errorf("stdout lacks %q:\n%s", line, stdout)
		}
	}
	if strings.Contains(stdout, "skipped") {
		t.Errorf("stdout counts a skipped file:\n%s", stdout)
	}
	if got := readProjFile(t, proj, ".agents/rules/own.md"); got != agOwnRule {
		t.Errorf("own.md = %q", got)
	}
	if got := readProjFile(t, proj, ".agents/plugins/dep/rules/single.md"); got != agSingleRule {
		t.Errorf("single.md = %q", got)
	}

	// With copilot, the last target by name, the kind is instruction(s)
	// (skex/ag3).
	stdout, _, _ = runCompileCLI(t, "install", "--target", "copilot,antigravity")
	if line := "  |-- 1 instruction(s) integrated (1 adopted) -> .agents/plugins/dep/rules/, .github/instructions/\n"; !strings.Contains(stdout, line) {
		t.Errorf("stdout lacks %q:\n%s", line, stdout)
	}
}

// mainAgLock is the lock apm-go 7984723 writes for agRulesProject, with the
// hashes of the instructions copied as they are.
const mainAgLock = `lockfile_version: '1'
apm_version: dev
dependencies:
- repo_url: _local/dep
  name: dep
  version: 1.0.0
  package_type: apm_package
  deployed_files:
  - .agents/plugins/dep/plugin.json
  - .agents/plugins/dep/rules/single.md
  deployed_file_hashes:
    .agents/plugins/dep/plugin.json: sha256:4b0bb2e19915abc25d7d2efa662a9bf6dca4862ccdb045e5624d469cb06eafb1
    .agents/plugins/dep/rules/single.md: sha256:7a780b624319dab9279daff870aec070c70ec550d3682bc494d345b3fc6c8cc4
  source: local
  local_path: ./dep
local_deployed_files:
- .agents/rules/own.md
local_deployed_file_hashes:
  .agents/rules/own.md: sha256:d095d30c9e7a98eaf1fc96ed3331946960fccb83c242386b6ef39cb31bc439e8
`

// After apm-go 7984723, the first install writes the rules and the lock
// again, and a frozen install and audit then pass.
func TestInstall_AntigravityRulesUpgradeMainLock(t *testing.T) {
	proj := agRulesProject(t)
	writeCompileFile(t, proj, "apm.lock.yaml", mainAgLock)
	writeCompileFile(t, proj, ".agents/plugins/dep/plugin.json", "{\"name\": \"dep\"}\n")
	writeCompileFile(t, proj, ".agents/plugins/dep/rules/single.md", readProjFile(t, proj, "dep/.apm/instructions/single.instructions.md"))
	writeCompileFile(t, proj, ".agents/rules/own.md", readProjFile(t, proj, ".apm/instructions/own.instructions.md"))

	stdout, stderr, code := runCompileCLI(t, "install")
	if code != 0 || !strings.Contains(stdout, "  |-- 1 rule(s) integrated -> .agents/plugins/dep/rules/\n") || !strings.Contains(stdout, "  |-- 1 rule(s) integrated -> .agents/rules/\n") {
		t.Fatalf("exit %d\n%s\n%s", code, stdout, stderr)
	}
	if got := readProjFile(t, proj, ".agents/rules/own.md"); got != agOwnRule {
		t.Errorf("own.md = %q", got)
	}
	lock := crlfLock(t, proj)
	if h := lock.LocalDeployedHashes[".agents/rules/own.md"]; h == "sha256:d095d30c9e7a98eaf1fc96ed3331946960fccb83c242386b6ef39cb31bc439e8" {
		t.Errorf("own.md hash %s, want the rule's", h)
	}
	for _, args := range [][]string{{"install", "--frozen"}, {"audit"}} {
		if stdout, stderr, code := runCompileCLI(t, args...); code != 0 {
			t.Errorf("%v: exit %d\n%s\n%s", args, code, stdout, stderr)
		}
	}
}
