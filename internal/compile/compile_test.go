package compile

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/manifest"
)

func mkFile(t *testing.T, base, rel, content string) {
	t.Helper()
	p := filepath.Join(base, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// TestCollectInstructions_LocalDependencySourcePaths covers Step 0 probe /
// design.md §3: local instructions get relpath ".apm/instructions/x", a
// direct git-style dependency ("acme/dep") gets relpath
// "apm_modules/acme/dep/.apm/instructions/x" -- oracle-confirmed 2026-07-11
// scratch probe (apm_cli 0.21.0, `compile --single-agents --no-links
// --no-constitution -t antigravity`), matching design.md's predicted shape
// exactly. Both bodies must be present in the collected result.
func TestCollectInstructions_LocalDependencySourcePaths(t *testing.T) {
	dir := t.TempDir()
	mkFile(t, dir, "apm.yml", "name: probe\nversion: \"1.0.0\"\n")
	mkFile(t, dir, ".apm/instructions/local.instructions.md", "Local body.\n")
	mkFile(t, dir, "apm_modules/acme/dep/.apm/instructions/dep.instructions.md", "Dependency body.\n")

	m := &manifest.Manifest{
		ParsedDeps: []*manifest.DependencyReference{
			{RepoURL: "acme/dep", Owner: "acme", Repo: "dep", Source: "git"},
		},
	}

	got, err := CollectInstructions(dir, m)
	if err != nil {
		t.Fatalf("CollectInstructions: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 instructions, got %d: %+v", len(got), got)
	}

	byPath := map[string]SourcedInstruction{}
	for _, inst := range got {
		byPath[inst.RelPath] = inst
	}

	local, ok := byPath[".apm/instructions/local.instructions.md"]
	if !ok {
		t.Fatalf("missing local relpath, got paths: %v", relPaths(got))
	}
	if local.Body != "Local body." {
		t.Errorf("local body = %q", local.Body)
	}

	dep, ok := byPath["apm_modules/acme/dep/.apm/instructions/dep.instructions.md"]
	if !ok {
		t.Fatalf("missing dependency relpath, got paths: %v", relPaths(got))
	}
	if dep.Body != "Dependency body." {
		t.Errorf("dependency body = %q", dep.Body)
	}
}

func relPaths(insts []SourcedInstruction) []string {
	out := make([]string, len(insts))
	for i, inst := range insts {
		out[i] = inst.RelPath
	}
	return out
}

// TestCollectInstructions_PriorityAndTransitiveOrder covers CMP-02: local
// wins over any dependency with the same instruction name, a direct
// dependency wins over a transitive one with the same name, and two direct
// dependencies sharing a name resolve to whichever was declared first in
// apm.yml -- mirroring deploy.ResolvePrimitives (req-pr-002/003) fed by the
// same local -> direct(declaration order) -> transitive(lockfile sort)
// priority ordering deploy.Run uses (deploy.go:72-118).
func TestCollectInstructions_PriorityAndTransitiveOrder(t *testing.T) {
	dir := t.TempDir()
	mkFile(t, dir, "apm.yml", "name: probe\nversion: \"1.0.0\"\n")

	// local vs. dependency same-name conflict: local wins.
	mkFile(t, dir, ".apm/instructions/shared.instructions.md", "Local shared wins.\n")
	mkFile(t, dir, "apm_modules/acme/dep-a/.apm/instructions/shared.instructions.md", "Dep shared loses.\n")

	// two direct deps, same-name instruction: first-declared (dep-a) wins.
	mkFile(t, dir, "apm_modules/acme/dep-a/.apm/instructions/pick.instructions.md", "dep-a wins (first-declared).\n")
	mkFile(t, dir, "apm_modules/acme/dep-b/.apm/instructions/pick.instructions.md", "dep-b loses.\n")

	// direct dep vs transitive dep same-name conflict: direct wins.
	mkFile(t, dir, "apm_modules/acme/dep-a/.apm/instructions/direct-wins.instructions.md", "direct wins.\n")
	mkFile(t, dir, "apm_modules/zzz/transitive/.apm/instructions/direct-wins.instructions.md", "transitive loses.\n")

	// transitive-only instruction still included.
	mkFile(t, dir, "apm_modules/zzz/transitive/.apm/instructions/onlytransitive.instructions.md", "only transitive.\n")

	mkFile(t, dir, "apm.lock.yaml", ""+
		"lockfile_version: \"1\"\n"+
		"dependencies:\n"+
		"  - repo_url: zzz/transitive\n"+
		"    source: git\n"+
		"    depth: 2\n")

	m := &manifest.Manifest{
		ParsedDeps: []*manifest.DependencyReference{
			{RepoURL: "acme/dep-a", Owner: "acme", Repo: "dep-a", Source: "git"},
			{RepoURL: "acme/dep-b", Owner: "acme", Repo: "dep-b", Source: "git"},
		},
	}

	got, err := CollectInstructions(dir, m)
	if err != nil {
		t.Fatalf("CollectInstructions: %v", err)
	}

	byPath := map[string]string{}
	for _, inst := range got {
		byPath[inst.RelPath] = inst.Body
	}

	if body := byPath[".apm/instructions/shared.instructions.md"]; body != "Local shared wins." {
		t.Errorf("local-vs-dependency conflict: got body %q", body)
	}
	if _, present := byPath["apm_modules/acme/dep-a/.apm/instructions/shared.instructions.md"]; present {
		t.Errorf("losing dependency copy of shared.instructions.md must not appear in output")
	}

	if body := byPath["apm_modules/acme/dep-a/.apm/instructions/pick.instructions.md"]; body != "dep-a wins (first-declared)." {
		t.Errorf("first-declared-wins: got body %q", body)
	}
	if _, present := byPath["apm_modules/acme/dep-b/.apm/instructions/pick.instructions.md"]; present {
		t.Errorf("second-declared dependency copy of pick.instructions.md must not appear in output")
	}

	if body := byPath["apm_modules/acme/dep-a/.apm/instructions/direct-wins.instructions.md"]; body != "direct wins." {
		t.Errorf("direct-vs-transitive conflict: got body %q", body)
	}
	if _, present := byPath["apm_modules/zzz/transitive/.apm/instructions/direct-wins.instructions.md"]; present {
		t.Errorf("losing transitive copy of direct-wins.instructions.md must not appear in output")
	}

	if body := byPath["apm_modules/zzz/transitive/.apm/instructions/onlytransitive.instructions.md"]; body != "only transitive." {
		t.Errorf("transitive-only instruction missing or wrong body: %q", body)
	}
}

// TestCollectInstructions_IgnoresWrongSuffixAndSymlink covers CMP-03: a
// plain .md file (wrong suffix) is never collected, a directory named
// "*.instructions.md" is never collected, and a symlink named
// "*.instructions.md" pointing OUTSIDE the source tree must not leak the
// external file's content into the compiled output (compile is an
// additional safety filter on top of deploy.CollectLocalPrimitives, which
// does not itself skip symlinks).
func TestCollectInstructions_IgnoresWrongSuffixAndSymlink(t *testing.T) {
	dir := t.TempDir()
	mkFile(t, dir, "apm.yml", "name: probe\nversion: \"1.0.0\"\n")
	mkFile(t, dir, ".apm/instructions/plain.md", "Plain markdown, wrong suffix.\n")
	if err := os.MkdirAll(filepath.Join(dir, ".apm/instructions/adir.instructions.md"), 0755); err != nil {
		t.Fatal(err)
	}
	mkFile(t, dir, ".apm/instructions/valid.instructions.md", "Valid body.\n")

	// Secret file OUTSIDE the source tree, referenced only via symlink.
	outsideDir := t.TempDir()
	secretPath := filepath.Join(outsideDir, "secret.md")
	const secretToken = "TOP-SECRET-TOKEN-not-for-compile-output"
	if err := os.WriteFile(secretPath, []byte(secretToken), 0644); err != nil {
		t.Fatal(err)
	}

	linkPath := filepath.Join(dir, ".apm/instructions/evil.instructions.md")
	if err := os.Symlink(secretPath, linkPath); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symlink creation unsupported/unprivileged on this Windows host: %v", err)
		}
		t.Fatalf("os.Symlink: %v", err)
	}

	m := &manifest.Manifest{}
	got, err := CollectInstructions(dir, m)
	if err != nil {
		t.Fatalf("CollectInstructions: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("expected exactly 1 instruction (valid.instructions.md only), got %d: %v", len(got), relPaths(got))
	}
	if got[0].RelPath != ".apm/instructions/valid.instructions.md" {
		t.Errorf("unexpected surviving instruction: %s", got[0].RelPath)
	}

	rendered := RenderAgentsMD(got, "test")
	if strings.Contains(rendered, secretToken) {
		t.Fatalf("symlinked external content leaked into rendered output:\n%s", rendered)
	}

	// External secret file must be byte-unchanged.
	secretAfter, err := os.ReadFile(secretPath)
	if err != nil {
		t.Fatalf("re-reading secret file: %v", err)
	}
	if string(secretAfter) != secretToken {
		t.Errorf("external secret file bytes changed: %q", secretAfter)
	}
}

func TestHasCompilableContent(t *testing.T) {
	t.Run("empty project has no content", func(t *testing.T) {
		dir := t.TempDir()
		if HasCompilableContent(dir) {
			t.Error("expected false for empty project")
		}
	})

	t.Run("local instructions file is content", func(t *testing.T) {
		dir := t.TempDir()
		mkFile(t, dir, ".apm/instructions/a.instructions.md", "A\n")
		if !HasCompilableContent(dir) {
			t.Error("expected true when a local instructions file exists")
		}
	})

	t.Run("apm_modules directory alone is content", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(dir, "apm_modules"), 0755); err != nil {
			t.Fatal(err)
		}
		if !HasCompilableContent(dir) {
			t.Error("expected true when apm_modules/ exists")
		}
	})
}

func TestFilterAgentsFamily(t *testing.T) {
	got := FilterAgentsFamily([]string{"claude", "codex", "copilot", "opencode", "antigravity"})
	want := []string{"codex", "opencode", "antigravity"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}

	if got := FilterAgentsFamily([]string{"claude", "copilot"}); len(got) != 0 {
		t.Errorf("expected no agents-family matches, got %v", got)
	}
	if got := FilterAgentsFamily(nil); len(got) != 0 {
		t.Errorf("expected no matches for nil input, got %v", got)
	}
}

// TestRun_WritesAndIsIdempotent exercises the end-to-end Run() orchestration
// (collect -> render -> stabilize -> write), including the idempotent
// no-op-on-unchanged-content path (design.md §6).
func TestRun_WritesAndIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	mkFile(t, dir, "apm.yml", "name: probe\nversion: \"1.0.0\"\n")
	mkFile(t, dir, ".apm/instructions/a.instructions.md", "Body A.\n")
	m := &manifest.Manifest{}

	result, err := Run(dir, m)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !result.Wrote {
		t.Fatalf("expected first Run to write AGENTS.md")
	}
	if result.InstructionCount != 1 {
		t.Errorf("InstructionCount = %d, want 1", result.InstructionCount)
	}
	// R12e: Result.Sources surfaces the display-relative source path of
	// every compiled instruction, in render order.
	if len(result.Sources) != 1 || result.Sources[0] != ".apm/instructions/a.instructions.md" {
		t.Errorf("Sources = %v, want [.apm/instructions/a.instructions.md]", result.Sources)
	}

	agentsPath := filepath.Join(dir, "AGENTS.md")
	first, err := os.ReadFile(agentsPath)
	if err != nil {
		t.Fatalf("reading AGENTS.md: %v", err)
	}
	firstInfo, err := os.Stat(agentsPath)
	if err != nil {
		t.Fatal(err)
	}

	result2, err := Run(dir, m)
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if result2.Wrote {
		t.Errorf("second Run with unchanged input must be a no-op (Wrote=false)")
	}
	second, err := os.ReadFile(agentsPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Errorf("content changed on idempotent re-run")
	}
	secondInfo, err := os.Stat(agentsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !firstInfo.ModTime().Equal(secondInfo.ModTime()) {
		t.Errorf("mtime changed on idempotent re-run (file was rewritten)")
	}

	// Changing the input must change both content and Build ID.
	mkFile(t, dir, ".apm/instructions/a.instructions.md", "Body A changed.\n")
	result3, err := Run(dir, m)
	if err != nil {
		t.Fatalf("third Run: %v", err)
	}
	if !result3.Wrote {
		t.Errorf("expected third Run (changed input) to write")
	}
	third, err := os.ReadFile(agentsPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(third) == string(first) {
		t.Errorf("content did not change after input changed")
	}
	if !strings.Contains(string(third), "Body A changed.") {
		t.Errorf("new body not present in rewritten AGENTS.md")
	}
}

func TestLoadLockfileDeps_MissingOrInvalid(t *testing.T) {
	t.Run("missing lockfile yields nil", func(t *testing.T) {
		dir := t.TempDir()
		if got := loadLockfileDeps(dir); got != nil {
			t.Errorf("expected nil, got %v", got)
		}
	})

	t.Run("invalid YAML yields nil, not an error", func(t *testing.T) {
		dir := t.TempDir()
		mkFile(t, dir, "apm.lock.yaml", "not: [valid\n")
		if got := loadLockfileDeps(dir); got != nil {
			t.Errorf("expected nil, got %v", got)
		}
	})

	t.Run("missing lockfile_version yields nil", func(t *testing.T) {
		dir := t.TempDir()
		mkFile(t, dir, "apm.lock.yaml", "dependencies: []\n")
		if got := loadLockfileDeps(dir); got != nil {
			t.Errorf("expected nil, got %v", got)
		}
	})
}

func TestSortedTransitiveDeps_TieBreaksOnVirtualPath(t *testing.T) {
	deps := []lockfile.LockedDep{
		{RepoURL: "acme/repo", VirtualPath: "b"},
		{RepoURL: "acme/repo", VirtualPath: "a"},
		{RepoURL: "acme/repo", VirtualPath: ""},
	}
	got := sortedTransitiveDeps(deps, map[string]bool{})
	want := []string{"acme/repo", "acme/repo/a", "acme/repo/b"}
	for i, w := range want {
		if got[i].UniqueKey() != w {
			t.Errorf("index %d: got %q, want %q", i, got[i].UniqueKey(), w)
		}
	}
}

func TestSortedTransitiveDeps_ExcludesDirectKeys(t *testing.T) {
	deps := []lockfile.LockedDep{
		{RepoURL: "acme/direct"},
		{RepoURL: "acme/transitive"},
	}
	got := sortedTransitiveDeps(deps, map[string]bool{"acme/direct": true})
	if len(got) != 1 || got[0].UniqueKey() != "acme/transitive" {
		t.Errorf("expected only acme/transitive, got %v", got)
	}
}

func TestRun_ProjectDirWithoutContent(t *testing.T) {
	dir := t.TempDir()
	m := &manifest.Manifest{}
	result, err := Run(dir, m)
	if err != nil {
		t.Fatalf("Run on empty project: %v", err)
	}
	if result.InstructionCount != 0 {
		t.Errorf("expected 0 instructions, got %d", result.InstructionCount)
	}
	if !result.Wrote {
		t.Errorf("expected a first write even with zero instructions")
	}
}

// TestWriteFile_AtomicFailurePreservesExisting covers IO-03: when the
// underlying atomic write primitive fails partway, WriteAGENTSMD must
// return an error and leave any pre-existing AGENTS.md byte-unchanged --
// atomicWrite is swapped for a failing stub so this is deterministic and
// cross-platform (no reliance on OS-specific permission tricks).
func TestWriteFile_AtomicFailurePreservesExisting(t *testing.T) {
	dir := t.TempDir()
	agentsPath := filepath.Join(dir, "AGENTS.md")
	const existingContent = "EXISTING HAND-WRITTEN CONTENT\n"
	if err := os.WriteFile(agentsPath, []byte(existingContent), 0644); err != nil {
		t.Fatal(err)
	}

	original := atomicWrite
	atomicWrite = func(path, content string) error {
		return errSimulatedAtomicWriteFailure
	}
	defer func() { atomicWrite = original }()

	wrote, err := WriteAGENTSMD(dir, "DIFFERENT NEW CONTENT\n")
	if err == nil {
		t.Fatal("expected an error from a simulated atomic-write failure")
	}
	if wrote {
		t.Error("wrote must be false on failure")
	}

	got, err := os.ReadFile(agentsPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != existingContent {
		t.Errorf("existing AGENTS.md was modified despite atomic-write failure: %q", got)
	}
}
