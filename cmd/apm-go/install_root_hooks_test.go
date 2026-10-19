package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/lockfile"
)

// TestInstall_RootAgentZeroTarget: with no dependency and no target, a
// *.agent.md beside a project's .apm/ is content to integrate, so the install
// exits 2 as Oracle 0.33.0 does (agent/s14).
func TestInstall_RootAgentZeroTarget(t *testing.T) {
	root := bootstrapProject(t, "proj")
	writeCompileFile(t, root, "proj/apm.yml", "name: proj\nversion: 1.0.0\n")
	writeCompileFile(t, root, "proj/projag.agent.md", "---\ndescription: proj root agent\n---\nProj root body\n")
	if err := os.Mkdir(filepath.Join(root, "proj", ".apm"), 0o755); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := runCompileCLI(t, "install")
	if code != 2 || !strings.Contains(stdout, " x no deployment target detected") {
		t.Errorf("exit %d, stdout:\n%s\nstderr:\n%s\nwant exit 2 and the no-target error", code, stdout, stderr)
	}
	assertNoFile(t, filepath.Join(root, "proj", "apm.lock.yaml"))
}

// codexHooksProject is the project of orch/codex1 without root hooks/: its
// own hook and the hook of ./dep both go to .codex/hooks.json.
func codexHooksProject(t *testing.T) string {
	t.Helper()
	root := bootstrapProject(t, "proj")
	writeCompileFile(t, root, "proj/apm.yml", "name: myproj\nversion: 1.0.0\ntargets: [codex]\ndependencies:\n  apm:\n    - ./dep\n")
	writeCompileFile(t, root, "proj/dep/apm.yml", "name: depname\nversion: 1.0.0\n")
	writeCompileFile(t, root, "proj/dep/.apm/hooks/a.json", `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"./scripts/check.sh"}]}]}}`+"\n")
	writeCompileFile(t, root, "proj/.apm/hooks/l.json", `{"hooks":{"PostToolUse":[{"hooks":[{"type":"command","command":"echo local"}]}]}}`+"\n")
	return filepath.Join(root, "proj")
}

// mainCodexLock is the lock apm-go 7984723 writes for codexHooksProject: it
// records .codex/hooks.json under both buckets, the project's with the hash
// of content no longer on disk.
const mainCodexLock = `lockfile_version: '1'
apm_version: dev
dependencies:
- repo_url: _local/dep
  name: depname
  version: 1.0.0
  package_type: apm_package
  deployed_files:
  - .codex/hooks.json
  deployed_file_hashes:
    .codex/hooks.json: sha256:e45526fec8ca4fa1124214027cec2cc4fbbf3e131936805cd1a92465608047d8
  source: local
  local_path: ./dep
local_deployed_files:
- .codex/hooks.json
local_deployed_file_hashes:
  .codex/hooks.json: sha256:73d2eaac0be652205fb95162a3a22feb55ff69a6a41396ef2eb7ae063c150cec
`

// TestInstall_CodexHooksJSONRecordedOnce: the lock records .codex/hooks.json
// under the bucket whose content stays there, so audit and a frozen install
// pass (issue #126 comment 6267289737, item 2), and a lock that recorded it
// under both converges on the next install.
func TestInstall_CodexHooksJSONRecordedOnce(t *testing.T) {
	assertOnce := func(t *testing.T, proj string) {
		t.Helper()
		lock := crlfLock(t, proj)
		if len(lock.LocalDeployedFiles) != 0 || len(lock.LocalDeployedHashes) != 0 {
			t.Errorf("local records %v %v, want nothing", lock.LocalDeployedFiles, lock.LocalDeployedHashes)
		}
		want, err := lockfile.HashDeployedFile(filepath.Join(proj, ".codex", "hooks.json"))
		if err != nil {
			t.Fatal(err)
		}
		if len(lock.Dependencies) != 1 || lock.Dependencies[0].DeployedHashes[".codex/hooks.json"] != want {
			t.Errorf("dependencies = %+v, want ./dep recording .codex/hooks.json as %s", lock.Dependencies, want)
		}
		for _, args := range [][]string{{"audit"}, {"install", "--frozen"}} {
			if stdout, stderr, code := runCompileCLI(t, args...); code != 0 {
				t.Errorf("%v: exit %d\n%s%s", args, code, stdout, stderr)
			}
		}
	}
	t.Run("new", func(t *testing.T) {
		proj := codexHooksProject(t)
		stdout, stderr, code := runCompileCLI(t, "install")
		if code != 0 {
			t.Fatalf("install: exit %d\n%s%s", code, stdout, stderr)
		}
		if want := ` ! hooks "a" overwrites .codex/hooks.json already written by "l" (single-file target)`; !strings.Contains(stderr+stdout, want) {
			t.Errorf("install output lacks %q:\n%s%s", want, stdout, stderr)
		}
		assertOnce(t, proj)
	})
	t.Run("lock of both buckets", func(t *testing.T) {
		proj := codexHooksProject(t)
		if _, _, code := runCompileCLI(t, "install"); code != 0 {
			t.Fatal("first install failed")
		}
		writeCompileFile(t, proj, "apm.lock.yaml", mainCodexLock)
		stdout, stderr, code := runCompileCLI(t, "install")
		if code != 0 || strings.Contains(stdout+stderr, "keeping") || strings.Contains(stdout+stderr, "Cleaned") {
			t.Errorf("install: exit %d, want 0 with no cleanup line\n%s%s", code, stdout, stderr)
		}
		assertOnce(t, proj)
	})
}

// mainCopilotLock is the lock apm-go 7984723 writes for copilotHooksProject,
// with the copilot hooks under their stems.
const mainCopilotLock = `lockfile_version: '1'
apm_version: dev
dependencies:
- repo_url: _local/dep
  name: dep-yml
  version: 1.0.0
  package_type: apm_package
  deployed_files:
  - .github/hooks/h.json
  deployed_file_hashes:
    .github/hooks/h.json: sha256:5087312ea258669dce67fa2bfe058610cec1f0db41416c87241aaf476258226a
  source: local
  local_path: ./dep
local_deployed_files:
- .github/hooks/own.json
local_deployed_file_hashes:
  .github/hooks/own.json: sha256:9c9078f184e6b597e0da83849541313a05091196c88c83755419755f7d55a0f4
`

func copilotHooksProject(t *testing.T) string {
	t.Helper()
	root := bootstrapProject(t, "proj")
	proj := filepath.Join(root, "proj")
	writeCompileFile(t, proj, "apm.yml", "name: proj\nversion: 1.0.0\ntargets: [copilot]\ndependencies:\n  apm:\n    - ./dep\n")
	writeCompileFile(t, proj, "dep/apm.yml", "name: dep-yml\nversion: 1.0.0\n")
	writeCompileFile(t, proj, "dep/.apm/hooks/h.json", `{"hooks":{"preToolUse":[{"type":"command","bash":"echo h"}]}}`+"\n")
	writeCompileFile(t, proj, "dep/hooks/r.json", `{"hooks":{"sessionStart":[{"type":"command","bash":"echo r"}]}}`+"\n")
	writeCompileFile(t, proj, ".apm/hooks/own.json", `{"hooks":{"sessionStart":[{"type":"command","bash":"echo own"}]}}`+"\n")
	writeCompileFile(t, proj, ".github/hooks/h.json", `{"hooks":{"preToolUse":[{"type":"command","bash":"echo h"}]}}`+"\n")
	writeCompileFile(t, proj, ".github/hooks/own.json", `{"hooks":{"sessionStart":[{"type":"command","bash":"echo own"}]}}`+"\n")
	writeCompileFile(t, proj, "apm.lock.yaml", mainCopilotLock)
	return proj
}

// TestInstall_CopilotHookOldNamesAreStale: after an upgrade, the hooks an
// earlier install wrote under their stems are stale and removed, and one
// the user edited is kept with a warning (hook/u1, u1e).
func TestInstall_CopilotHookOldNamesAreStale(t *testing.T) {
	for _, edited := range []bool{false, true} {
		proj := copilotHooksProject(t)
		if edited {
			writeCompileFile(t, proj, ".github/hooks/h.json", "{\"edited\":true}\n")
		}
		stdout, stderr, code := runCompileCLI(t, "install")
		if code != 0 {
			t.Fatalf("edited %v: exit %d\n%s%s", edited, code, stdout, stderr)
		}
		entries, err := os.ReadDir(filepath.Join(proj, ".github", "hooks"))
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, e := range entries {
			got = append(got, e.Name())
		}
		want := "dep-h.json dep-r.json proj-own.json"
		if edited {
			want = "dep-h.json dep-r.json h.json proj-own.json"
			if w := ` ! keeping ".github/hooks/h.json": modified since deploy (hash mismatch)`; !strings.Contains(stdout+stderr, w) {
				t.Errorf("output lacks %q:\n%s%s", w, stdout, stderr)
			}
		}
		if strings.Join(got, " ") != want {
			t.Errorf("edited %v: .github/hooks = %v, want %s", edited, got, want)
		}
		if edited {
			// The lock keeps the old record, as Oracle 0.33.0's does (hook/u1e).
			const old = "sha256:5087312ea258669dce67fa2bfe058610cec1f0db41416c87241aaf476258226a"
			if lock := crlfLock(t, proj); lock.Dependencies[0].DeployedHashes[".github/hooks/h.json"] != old {
				t.Errorf("lock records %v, want h.json kept as %s", lock.Dependencies[0].DeployedHashes, old)
			}
		} else if _, _, code := runCompileCLI(t, "install", "--frozen"); code != 0 {
			t.Errorf("frozen install exit %d", code)
		}
	}
}
