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

	got, _, err := CollectInstructions(dir, m)
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

	got, _, err := CollectInstructions(dir, m)
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
// external file's content into the compiled output.
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
	got, _, err := CollectInstructions(dir, m)
	if err != nil {
		t.Fatalf("CollectInstructions: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("expected exactly 1 instruction (valid.instructions.md only), got %d: %v", len(got), relPaths(got))
	}
	if got[0].RelPath != ".apm/instructions/valid.instructions.md" {
		t.Errorf("unexpected surviving instruction: %s", got[0].RelPath)
	}

	rendered := RenderAgentsMD(got)
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

// An instruction whose frontmatter cannot be read is skipped before the
// conflicts are resolved, so a dependency's instruction of the same name is
// compiled in its place (measured with Oracle 0.33.0: the dependency's x is
// compiled and the local x is reported as not parsed).
func TestCollectInstructions_SkipsUnreadableFrontmatter(t *testing.T) {
	dir := t.TempDir()
	mkFile(t, dir, ".apm/instructions/x.instructions.md", "---\napplyTo: **/*.go\n---\nbad local\n")
	mkFile(t, dir, ".apm/instructions/ok.instructions.md", "---\napplyTo: \"ok/**\"\n---\nok local\n")
	mkFile(t, dir, "apm_modules/acme/dep/.apm/instructions/x.instructions.md", "---\napplyTo: \"dep/**\"\n---\ngood dep\n")
	m := &manifest.Manifest{
		ParsedDeps: []*manifest.DependencyReference{
			{RepoURL: "acme/dep", Owner: "acme", Repo: "dep", Source: "git"},
		},
	}

	got, skipped, err := CollectInstructions(dir, m)
	if err != nil {
		t.Fatalf("CollectInstructions: %v", err)
	}
	want := []string{".apm/instructions/ok.instructions.md", "apm_modules/acme/dep/.apm/instructions/x.instructions.md"}
	if paths := relPaths(got); strings.Join(paths, "|") != strings.Join(want, "|") {
		t.Errorf("compiled %v, want %v", paths, want)
	}
	if len(skipped) != 1 || skipped[0].RelPath != ".apm/instructions/x.instructions.md" || !strings.Contains(skipped[0].Err.Error(), "YAML parse error") {
		t.Errorf("skipped = %+v, want the local x with its YAML error", skipped)
	}
}

// An instruction compile cannot use is no candidate, so the next one of its
// name takes its place, and only some are reported, as Oracle 0.33.0's
// discovery skips them (primitives/discovery.py:225-263, 385-430, 678-692;
// measured with ~/.cache/apm-go-verify/issue76/scripts/unread.sh and
// unread_f.sh).
func TestCollectInstructions_SkipsWhatTheOracleSkips(t *testing.T) {
	const dep = "apm_modules/acme/dep/.apm/instructions/"
	pad := strings.Repeat("a", 8192)
	tests := []struct {
		name       string
		files      map[string]string
		unreadable string
		want       []string // compiled RelPath: Body
		wantWarned []string
	}{
		{
			name:       "a: unreadable local file is reported and a dependency takes its name",
			files:      map[string]string{".apm/instructions/y.instructions.md": "local y\n", dep + "y.instructions.md": "dep y\n"},
			unreadable: ".apm/instructions/y.instructions.md",
			want:       []string{dep + "y.instructions.md: dep y"},
			wantWarned: []string{".apm/instructions/y.instructions.md"},
		},
		{
			name:       "b: local file that is not UTF-8 is reported",
			files:      map[string]string{".apm/instructions/w.instructions.md": "---\napplyTo: \"w/**\"\n---\nlocal w \xff\n", ".apm/instructions/u.instructions.md": "local u \xff no fm\n"},
			wantWarned: []string{".apm/instructions/u.instructions.md", ".apm/instructions/w.instructions.md"},
		},
		{
			name:       "d: unreadable dependency file is skipped without a word",
			files:      map[string]string{".apm/instructions/x.instructions.md": "local x\n", dep + "x.instructions.md": "dep x\n"},
			unreadable: dep + "x.instructions.md",
			want:       []string{".apm/instructions/x.instructions.md: local x"},
		},
		{
			name: "e: dependency file not UTF-8 in its first 8192 bytes is skipped without a word",
			files: map[string]string{
				dep + "v.instructions.md":  "dep v \xff\n",
				dep + "e.instructions.md":  pad[:8190] + "\xe0\x80\n",
				dep + "c1.instructions.md": "\xc3",
				dep + "e2.instructions.md": "\xe0\xa0",
			},
		},
		{
			name: "f: dependency file not UTF-8 only after byte 8192 is reported",
			files: map[string]string{
				dep + "f.instructions.md":  pad + "\xff\n",
				dep + "ac.instructions.md": "A\xc3",
				dep + "c.instructions.md":  pad[:8191] + "\xc3A\n",
				dep + "ok.instructions.md": pad[:8191] + "\xc3\xa9\n",
			},
			want:       []string{dep + "ok.instructions.md: " + pad[:8191] + "\xc3\xa9"},
			wantWarned: []string{dep + "ac.instructions.md", dep + "c.instructions.md", dep + "f.instructions.md"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.unreadable != "" && (runtime.GOOS == "windows" || os.Geteuid() == 0) {
				t.Skip("needs a file that refuses a read: Windows ignores the read bits of mode 000 and root is not bound by them")
			}
			dir := t.TempDir()
			for rel, content := range tt.files {
				mkFile(t, dir, rel, content)
			}
			if tt.unreadable != "" {
				p := filepath.Join(dir, filepath.FromSlash(tt.unreadable))
				if err := os.Chmod(p, 0); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { os.Chmod(p, 0o644) })
			}
			m := &manifest.Manifest{ParsedDeps: []*manifest.DependencyReference{{RepoURL: "acme/dep", Owner: "acme", Repo: "dep", Source: "git"}}}

			got, skipped, err := CollectInstructions(dir, m)
			if err != nil {
				t.Fatalf("CollectInstructions: %v", err)
			}
			var compiled, warned []string
			for _, inst := range got {
				compiled = append(compiled, inst.RelPath+": "+inst.Body)
			}
			for _, s := range skipped {
				warned = append(warned, s.RelPath)
			}
			if strings.Join(compiled, "|") != strings.Join(tt.want, "|") {
				t.Errorf("compiled %q, want %q", compiled, tt.want)
			}
			if strings.Join(warned, "|") != strings.Join(tt.wantWarned, "|") {
				t.Errorf("reported %q, want %q", warned, tt.wantWarned)
			}
		})
	}
}

func symlinkOrSkip(t *testing.T, target, link string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(link), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symlink creation unsupported/unprivileged on this Windows host: %v", err)
		}
		t.Fatalf("os.Symlink: %v", err)
	}
}

// bodiesInOrder fails unless each of want appears in rendered, in that
// order, and none of absent does.
func bodiesInOrder(t *testing.T, rendered string, want, absent []string) {
	t.Helper()
	at := 0
	for _, w := range want {
		i := strings.Index(rendered[at:], w)
		if i < 0 {
			t.Fatalf("%q missing or out of order in:\n%s", w, rendered)
		}
		at += i + len(w)
	}
	for _, a := range absent {
		if strings.Contains(rendered, a) {
			t.Errorf("%q should not be compiled:\n%s", a, rendered)
		}
	}
}

func renderLocal(t *testing.T, dir string) string {
	t.Helper()
	got, _, err := CollectInstructions(dir, &manifest.Manifest{})
	if err != nil {
		t.Fatalf("CollectInstructions: %v", err)
	}
	return RenderAgentsMD(got)
}

func scoped(glob, body string) string {
	return "---\napplyTo: \"" + glob + "\"\n---\n" + body + "\n"
}

// The project tree of issue #131's run1: Oracle 0.33.0 compiles every
// *.instructions.md of the project, keeps .apm/instructions/top over
// .apm/instructions/sub/top, and reads nothing below node_modules, build,
// vendor or a nested repository (~/.cache/apm-go-verify/issue131/run1-oracle;
// the Oracle places the two applyTo groups in src/AGENTS.md, apm-go in the
// root one, ruling R11).
func TestCollectInstructions_WalksTheProjectAsTheOracle(t *testing.T) {
	dir := t.TempDir()
	mkFile(t, dir, "apm.yml", "name: fx\nversion: 1.0.0\n")
	mkFile(t, dir, ".apm/instructions/top.instructions.md", "Top rule.\n")
	mkFile(t, dir, ".apm/instructions/sub/nested.instructions.md", scoped("src/**", "Nested rule."))
	mkFile(t, dir, ".apm/instructions/sub/deeper/deep.instructions.md", "Deep rule.\n")
	mkFile(t, dir, ".apm/instructions/sub/top.instructions.md", "Shadow top rule.\n")
	mkFile(t, dir, ".github/instructions/gh.instructions.md", "Github rule.\n")
	mkFile(t, dir, "root.instructions.md", "Root rule.\n")
	mkFile(t, dir, "src/local.instructions.md", scoped("src/**/*.go", "Src rule."))
	mkFile(t, dir, "docs/doc.instructions.md", "Doc rule.\n")
	mkFile(t, dir, "node_modules/m/nm.instructions.md", "NM rule.\n")
	mkFile(t, dir, "build/b.instructions.md", "Build rule.\n")
	mkFile(t, dir, "vendor/v.instructions.md", "Vendor rule.\n")
	mkFile(t, dir, "nested/n.instructions.md", "Nested repo rule.\n")
	if err := os.MkdirAll(filepath.Join(dir, "nested/.git"), 0755); err != nil {
		t.Fatal(err)
	}

	bodiesInOrder(t, renderLocal(t, dir),
		[]string{"## Global Instructions", "Deep rule.", "Top rule.", "Github rule.", "Doc rule.", "Root rule.",
			"## Files matching `src/**`", "Nested rule.", "## Files matching `src/**/*.go`", "Src rule."},
		[]string{"Shadow top rule.", "NM rule.", "Build rule.", "Vendor rule.", "Nested repo rule."})
}

// A file at the project root is collected before any below .apm, so it
// keeps a name both have.
func TestCollectInstructions_RootFileWinsItsName(t *testing.T) {
	dir := t.TempDir()
	mkFile(t, dir, "top.instructions.md", "Root top.\n")
	mkFile(t, dir, ".apm/instructions/top.instructions.md", "Apm top.\n")
	bodiesInOrder(t, renderLocal(t, dir), []string{"Root top."}, []string{"Apm top."})
}

// DEFAULT_SKIP_DIRS is compared as written: Build/ is read, build/ is not
// (measured with Oracle 0.33.0 in the Windows sandbox, issue #131 s131.ps1
// scenario N).
func TestCollectInstructions_SkipListIsCaseSensitive(t *testing.T) {
	dir := t.TempDir()
	mkFile(t, dir, "Build/c.instructions.md", "Build cap.\n")
	mkFile(t, dir, "x/build/b.instructions.md", "Build.\n")
	bodiesInOrder(t, renderLocal(t, dir), []string{"Build cap."}, []string{"Build.\n"})
}

// A symlinked directory below .apm/instructions, or anywhere else, is not
// entered (issue #131 run l2-oracle: only Top.).
func TestCollectInstructions_DoesNotEnterLinkedDirectories(t *testing.T) {
	ext := t.TempDir()
	mkFile(t, ext, "instr/ei.instructions.md", "Ext instr.\n")
	mkFile(t, ext, "docs/ed.instructions.md", "Ext doc.\n")
	dir := t.TempDir()
	mkFile(t, dir, ".apm/instructions/top.instructions.md", "Top.\n")
	symlinkOrSkip(t, filepath.Join(ext, "instr"), filepath.Join(dir, ".apm/instructions/linked"))
	symlinkOrSkip(t, filepath.Join(ext, "docs"), filepath.Join(dir, "docs"))
	bodiesInOrder(t, renderLocal(t, dir), []string{"Top."}, []string{"Ext instr.", "Ext doc."})
}

// A linked .apm/instructions, or a linked .apm, gives the first level of
// .apm/instructions, as before, where the Oracle reads none of it (issue
// #131 comments 6286949791 and 6286973860; runs l1 and l3).
func TestCollectInstructions_LinkedAPMReadsItsFirstLevel(t *testing.T) {
	t.Run(".apm/instructions is a link", func(t *testing.T) {
		ext := t.TempDir()
		mkFile(t, ext, "ei.instructions.md", "Ext instr.\n")
		mkFile(t, ext, "sub/deep.instructions.md", "Ext deep.\n")
		dir := t.TempDir()
		symlinkOrSkip(t, ext, filepath.Join(dir, ".apm/instructions"))
		if !HasCompilableContent(dir) {
			t.Error("HasCompilableContent = false, want true")
		}
		bodiesInOrder(t, renderLocal(t, dir), []string{"Ext instr."}, []string{"Ext deep."})
	})
	t.Run(".apm is a link", func(t *testing.T) {
		ext := t.TempDir()
		mkFile(t, ext, "instructions/a.instructions.md", "Ext apm.\n")
		mkFile(t, ext, "instructions/sub/b.instructions.md", "Ext sub.\n")
		dir := t.TempDir()
		symlinkOrSkip(t, ext, filepath.Join(dir, ".apm"))
		if !HasCompilableContent(dir) {
			t.Error("HasCompilableContent = false, want true")
		}
		bodiesInOrder(t, renderLocal(t, dir), []string{"Ext apm."}, []string{"Ext sub."})
	})
}

// Oracle 0.33.0 compiles a project whose one instruction is in a
// subdirectory of .apm/instructions (run e1-oracle), and refuses one whose
// one instruction is outside .apm (run e2-oracle).
func TestHasCompilableContent_AsTheOracle(t *testing.T) {
	sub := t.TempDir()
	mkFile(t, sub, ".apm/instructions/sub/a.instructions.md", "Sub only.\n")
	if !HasCompilableContent(sub) {
		t.Error("instruction below .apm/instructions/sub: got false, want true")
	}
	root := t.TempDir()
	mkFile(t, root, "root.instructions.md", "Root.\n")
	if HasCompilableContent(root) {
		t.Error("instruction at the root and no .apm: got true, want false")
	}
}
