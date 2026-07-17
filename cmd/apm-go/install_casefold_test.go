package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/manifest"
	"github.com/apm-go/apm/internal/yamlcore"
)

// fixtureLoader materializes a prebuilt skill-repo fixture into apm_modules in
// place of a real git clone. The BUG-1 case-fold DEPLOY tests need real skill
// files on disk under the dependency's apm_modules key so deploy can copy them
// out. A real clone would work only via a `file`-transport clone (git
// insteadOf redirecting a fake remote host to a local repo), which the git
// ext:: transport hardening (GIT_ALLOW_PROTOCOL without "file" for a
// remote-shaped URL) correctly refuses. Copying the fixture reproduces exactly
// what a clone leaves behind -- installPath is ModulesDir/<RepoURL> -- while
// still exercising the same resolve -> dedup -> deploy pipeline. resolvedRef is
// unused: these fixtures are single-commit and never re-checked-out.
type fixtureLoader struct {
	modulesDir string
	fixtureDir string
}

func (l *fixtureLoader) LoadPackage(ref *manifest.DependencyReference, resolvedRef string) (*manifest.Manifest, error) {
	key := ref.RepoURL
	if ref.VirtualPath != "" {
		key += "/" + ref.VirtualPath
	}
	dest := filepath.Join(l.modulesDir, filepath.FromSlash(key))
	if _, err := os.Stat(dest); os.IsNotExist(err) {
		if err := copyTreeSkipGit(l.fixtureDir, dest); err != nil {
			return nil, err
		}
	}
	data, err := os.ReadFile(filepath.Join(dest, "apm.yml"))
	if err != nil {
		return nil, err
	}
	node, err := yamlcore.SafeLoad(data)
	if err != nil {
		return nil, err
	}
	m, _, err := manifest.ParseManifest(node)
	return m, err
}

// copyTreeSkipGit recursively copies src into dst, skipping any .git directory
// (a clone's working tree is all deploy needs; the .git metadata is not).
func copyTreeSkipGit(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(dst, rel), 0755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), b, 0644)
	})
}

// captureInstallCombined redirects BOTH os.Stdout and os.Stderr for the
// duration of fn into a single interleaved buffer, for tests that only need
// to prove the ABSENCE of certain noise (e.g. "shadowed", "deployed 0
// files") anywhere in the visible output, regardless of which stream it
// would have landed on.
func captureInstallCombined(t *testing.T, fn func()) string {
	t.Helper()
	origOut, origErr := os.Stdout, os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	os.Stderr = w
	defer func() { os.Stdout, os.Stderr = origOut, origErr }()

	fn()

	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// TestInstall_CaseFoldDedup is BUG-1's core reproduction (prd.md "BUG-1 ｜
// 大小寫重複依賴", design.md §2, AC-B1-1): two positional packages naming the
// SAME GitHub repository under different case ("Owner/Repo" vs
// "owner/repo") must resolve, persist, lock, and materialize as ONE
// dependency -- never two -- mirroring GitHub's own case-insensitive
// owner/repo semantics (manifest.CanonicalRepoIdentity). Before the fix,
// resolver.depKey and cmd/apm-go's positional-package dedup both compared
// RepoURL as a raw, case-sensitive string, so the second case variant
// silently became a SECOND m.ParsedDeps entry, a second BFS node
// ("Resolved 2"), a second apm_modules materialization attempt, and (since
// both ultimately point at the same physical repo) primitive-shadowing
// noise plus a "deployed 0 files" ghost for whichever one lost.
func TestInstall_CaseFoldDedup(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origDir)

	remotesDir := filepath.Join(dir, "remotes")
	if err := os.MkdirAll(remotesDir, 0755); err != nil {
		t.Fatal(err)
	}
	// H4: each skill spans multiple files across multiple deploy target
	// roots, so "1 dependency" is never confused with "1 deployed file".
	repoDir := gitSkillRepo(t, remotesDir, "case-repo", map[string][]string{
		"skillOne": {"SKILL.md", "notes.md"},
		"skillTwo": {"SKILL.md", "notes.md"},
	})

	if err := os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	deps := &installDeps{
		tags:   &mockInstallTagLister{},
		loader: &fixtureLoader{modulesDir: "apm_modules", fixtureDir: repoDir},
	}

	var installErr error
	out := captureInstallCombined(t, func() {
		installErr = runInstall(deps, false, true, "claude", nil, []string{"Owner/Repo", "owner/repo"})
	})
	if installErr != nil {
		t.Fatalf("runInstall: %v\noutput:\n%s", installErr, out)
	}

	if !strings.Contains(out, "Resolved 1 dependency") {
		t.Errorf("output: expected \"Resolved 1 dependency\", got:\n%s", out)
	}
	if strings.Contains(out, "shadowed") {
		t.Errorf("output: unexpected shadow-conflict noise from the case-duplicate:\n%s", out)
	}
	if strings.Contains(out, "deployed 0 files") {
		t.Errorf("output: unexpected ghost \"deployed 0 files\" entry:\n%s", out)
	}

	m := readManifestParsed(t)
	if len(m.ParsedDeps) != 1 {
		t.Fatalf("apm.yml: expected exactly ONE dependency entry, got %d: %+v", len(m.ParsedDeps), m.ParsedDeps)
	}

	lock := readLockfile(t)
	if len(lock.Dependencies) != 1 {
		t.Fatalf("apm.lock.yaml: expected exactly ONE dependency, got %d: %+v", len(lock.Dependencies), lock.Dependencies)
	}

	entries, err := os.ReadDir(filepath.Join(dir, "apm_modules"))
	if err != nil {
		t.Fatalf("read apm_modules: %v", err)
	}
	if len(entries) != 1 {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Errorf("apm_modules: expected exactly ONE materialized directory, got %d: %v", len(entries), names)
	}

	for _, skill := range []string{"skillOne", "skillTwo"} {
		for _, p := range expectedSkillDeployPaths(skill, []string{"SKILL.md", "notes.md"}) {
			if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(p))); err != nil {
				t.Errorf("expected %s to exist: %v", p, err)
			}
		}
	}

	// V1-2: a subsequent bare install and update must keep the dedup single
	// -- not re-split into two entries the next time the full manifest is
	// re-resolved.
	if err := runInstall(deps, false, true, "claude", nil, nil); err != nil {
		t.Fatalf("bare re-install: %v", err)
	}
	m2 := readManifestParsed(t)
	if len(m2.ParsedDeps) != 1 {
		t.Errorf("after bare install: expected ONE apm.yml entry, got %d: %+v", len(m2.ParsedDeps), m2.ParsedDeps)
	}
	lock2 := readLockfile(t)
	if len(lock2.Dependencies) != 1 {
		t.Errorf("after bare install: expected ONE lockfile dependency, got %d", len(lock2.Dependencies))
	}
	entries2, err := os.ReadDir(filepath.Join(dir, "apm_modules"))
	if err != nil {
		t.Fatalf("read apm_modules after bare install: %v", err)
	}
	if len(entries2) != 1 {
		t.Errorf("after bare install: expected ONE materialized directory, got %d", len(entries2))
	}

	if err := runUpdate(deps, false, false, "", false); err != nil {
		t.Fatalf("update: %v", err)
	}
	lock3 := readLockfile(t)
	if len(lock3.Dependencies) != 1 {
		t.Errorf("after update: expected ONE lockfile dependency, got %d", len(lock3.Dependencies))
	}
}

// TestInstall_CaseFoldDedup_DifferentReposNotMerged is the AC-B1-2 guard
// (design.md §2): two genuinely DIFFERENT repositories (different owner AND
// repo, no case overlap at all) must never be folded together just because
// canonical-identity comparison was introduced -- over-aggressive folding
// would be just as wrong as the original bug.
func TestInstall_CaseFoldDedup_DifferentReposNotMerged(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origDir)

	if err := os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &mockInstallLoader{}}
	if err := runInstall(deps, false, true, "claude", nil, []string{"acme/one", "acme/two"}); err != nil {
		t.Fatalf("runInstall: %v", err)
	}

	m := readManifestParsed(t)
	if len(m.ParsedDeps) != 2 {
		t.Fatalf("apm.yml: expected TWO distinct dependency entries (acme/one, acme/two), got %d: %+v", len(m.ParsedDeps), m.ParsedDeps)
	}

	lock := readLockfile(t)
	if len(lock.Dependencies) != 2 {
		t.Fatalf("apm.lock.yaml: expected TWO dependencies, got %d: %+v", len(lock.Dependencies), lock.Dependencies)
	}
}

// TestInstall_CaseFoldDedup_SelectorConflictNotSilentlyMerged is the second
// half of design.md §0/§2's "same identity, different selector" rule: two
// refs to the SAME repository identity but pinned to different, conflicting
// literal refs (branches) must not be quietly/indistinguishably merged --
// the existing first-declared-wins policy applies (exactly as it already
// does for two exact-same-case installs of the same dep with different
// refs across separate calls), but the caller must be WARNED that the
// second ref was dropped, rather than the difference vanishing with zero
// trace.
func TestInstall_CaseFoldDedup_SelectorConflictNotSilentlyMerged(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origDir)

	if err := os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &mockInstallLoader{}}
	var installErr error
	out := captureInstallCombined(t, func() {
		installErr = runInstall(deps, false, true, "claude", nil, []string{"Owner/Repo#branch-a", "owner/repo#branch-b"})
	})
	if installErr != nil {
		t.Fatalf("runInstall: %v\noutput:\n%s", installErr, out)
	}
	if !strings.Contains(out, "conflicts with already-declared") {
		t.Errorf("expected a conflicting-selector warning, got:\n%s", out)
	}

	m := readManifestParsed(t)
	if len(m.ParsedDeps) != 1 {
		t.Fatalf("apm.yml: expected exactly ONE dependency entry, got %d: %+v", len(m.ParsedDeps), m.ParsedDeps)
	}
	if got := m.ParsedDeps[0].Reference; got != "branch-a" {
		t.Errorf("apm.yml: expected first-declared ref %q to win, got %q", "branch-a", got)
	}
}

// TestInstall_CaseFoldWildcardReset is the BUG-1 x BUG-2 interaction
// regression (prd.md AC-B1-3, AC-B2-2, design.md §2): re-declaring the SAME
// repository across three case variants, each with a different --skill
// flag, must still resolve to ONE dependency whose --skill subset unions
// across calls and RESETS to full on '*' -- exactly as if every call had
// used the exact same case string.
func TestInstall_CaseFoldWildcardReset(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origDir)

	remotesDir := filepath.Join(dir, "remotes")
	if err := os.MkdirAll(remotesDir, 0755); err != nil {
		t.Fatal(err)
	}
	repoDir := gitSkillRepo(t, remotesDir, "case-repo", map[string][]string{
		"skillA": {"SKILL.md", "notes.md"},
		"skillB": {"SKILL.md", "notes.md"},
	})

	if err := os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	deps := &installDeps{
		tags:   &mockInstallTagLister{},
		loader: &fixtureLoader{modulesDir: "apm_modules", fixtureDir: repoDir},
	}

	if err := runInstall(deps, false, true, "claude", []string{"skillA"}, []string{"RepoA/x"}); err != nil {
		t.Fatalf("step 1 (RepoA/x --skill skillA): %v", err)
	}
	if err := runInstall(deps, false, true, "claude", []string{"skillB"}, []string{"repoa/x"}); err != nil {
		t.Fatalf("step 2 (repoa/x --skill skillB): %v", err)
	}

	m2 := readManifestParsed(t)
	if len(m2.ParsedDeps) != 1 {
		t.Fatalf("step2: expected exactly ONE apm.yml entry, got %d: %+v", len(m2.ParsedDeps), m2.ParsedDeps)
	}
	if got := m2.ParsedDeps[0].SkillSubset; len(got) != 2 || got[0] != "skillA" || got[1] != "skillB" {
		t.Errorf("step2: apm.yml skills: = %v, want union [skillA skillB]", got)
	}

	if err := runInstall(deps, false, true, "claude", []string{"*"}, []string{"REPOA/x"}); err != nil {
		t.Fatalf("step 3 (REPOA/x --skill '*'): %v", err)
	}

	m3 := readManifestParsed(t)
	if len(m3.ParsedDeps) != 1 {
		t.Fatalf("step3: expected exactly ONE apm.yml entry after RESET, got %d: %+v", len(m3.ParsedDeps), m3.ParsedDeps)
	}
	if got := m3.ParsedDeps[0].SkillSubset; len(got) != 0 {
		t.Errorf("step3: apm.yml skills: expected RESET to full (no subset), got %v", got)
	}

	lock3 := readLockfile(t)
	if len(lock3.Dependencies) != 1 {
		t.Fatalf("step3: expected exactly ONE lockfile dependency after RESET, got %d: %+v", len(lock3.Dependencies), lock3.Dependencies)
	}

	entries, err := os.ReadDir(filepath.Join(dir, "apm_modules"))
	if err != nil {
		t.Fatalf("read apm_modules: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("expected exactly ONE materialized apm_modules directory, got %d", len(entries))
	}

	for _, skill := range []string{"skillA", "skillB"} {
		for _, p := range expectedSkillDeployPaths(skill, []string{"SKILL.md", "notes.md"}) {
			if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(p))); err != nil {
				t.Errorf("after RESET: expected %s to exist (full deploy): %v", p, err)
			}
		}
	}
}

// TestInstall_CaseFoldDedup_LockfileUpgradeCompat is the AC-B1-4 upgrade
// regression: a project whose apm.lock.yaml already carries a mixed-case
// duplicate pair for the SAME repository -- an artifact a PRE-FIX apm-go
// could have written -- must, after upgrading to the fix, converge back
// down to exactly one lockfile dependency (matching apm.yml's one,
// first-declared entry) the next time install runs, instead of the stale
// duplicate persisting or (worse) growing to three.
func TestInstall_CaseFoldDedup_LockfileUpgradeCompat(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origDir)

	remotesDir := filepath.Join(dir, "remotes")
	if err := os.MkdirAll(remotesDir, 0755); err != nil {
		t.Fatal(err)
	}
	repoDir := gitSkillRepo(t, remotesDir, "case-repo", map[string][]string{
		"skillOne": {"SKILL.md", "notes.md"},
	})

	if err := os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\ndependencies:\n  apm:\n    - Owner/Repo\n"), 0644); err != nil {
		t.Fatal(err)
	}

	deps := &installDeps{
		tags:   &mockInstallTagLister{},
		loader: &fixtureLoader{modulesDir: "apm_modules", fixtureDir: repoDir},
	}

	// Baseline install to get a legitimate, real lockfile for "Owner/Repo".
	if err := runInstall(deps, false, true, "claude", nil, nil); err != nil {
		t.Fatalf("baseline install: %v", err)
	}
	baseline := readLockfile(t)
	if len(baseline.Dependencies) != 1 {
		t.Fatalf("baseline: expected ONE lockfile dependency, got %d", len(baseline.Dependencies))
	}

	// Simulate a PRE-FIX polluted lockfile: duplicate the dependency entry
	// under the OTHER case, as a pre-fix apm-go's resolver/deploy would
	// have written from two case-variant CLI installs.
	polluted := &lockfile.Lockfile{Version: baseline.Version, Dependencies: append([]lockfile.LockedDep{}, baseline.Dependencies...)}
	dup := baseline.Dependencies[0]
	dup.RepoURL = "owner/repo"
	polluted.Dependencies = append(polluted.Dependencies, dup)
	if len(polluted.Dependencies) != 2 {
		t.Fatalf("test setup: expected 2 polluted dependencies, got %d", len(polluted.Dependencies))
	}
	out, err := lockfile.WriteLockfile(polluted, nil)
	if err != nil {
		t.Fatalf("write polluted lockfile: %v", err)
	}
	if err := os.WriteFile("apm.lock.yaml", out, 0644); err != nil {
		t.Fatal(err)
	}
	// Sanity: the polluted lockfile really does parse with 2 entries before
	// the upgrade-compat install runs.
	prenode, err := yamlcore.SafeLoad(out)
	if err != nil {
		t.Fatal(err)
	}
	preLock, err := lockfile.ParseLockfile(prenode)
	if err != nil {
		t.Fatalf("parse polluted lockfile: %v", err)
	}
	if len(preLock.Dependencies) != 2 {
		t.Fatalf("test setup: polluted lockfile must parse with 2 dependencies, got %d", len(preLock.Dependencies))
	}

	entriesBefore, err := os.ReadDir(filepath.Join(dir, "apm_modules"))
	if err != nil {
		t.Fatalf("read apm_modules before upgrade-compat install: %v", err)
	}

	// The upgrade-compat run: apm.yml (the source of truth for what's
	// ACTUALLY declared) still has only ONE entry, so re-resolving from it
	// must rebuild the lockfile back down to one dependency -- not persist
	// or grow the polluted duplicate.
	if err := runInstall(deps, false, true, "claude", nil, nil); err != nil {
		t.Fatalf("upgrade-compat install: %v", err)
	}

	after := readLockfile(t)
	if len(after.Dependencies) != 1 {
		t.Fatalf("after upgrade-compat install: expected lockfile to converge to ONE dependency, got %d: %+v", len(after.Dependencies), after.Dependencies)
	}
	if got := after.Dependencies[0].RepoURL; got != "Owner/Repo" {
		t.Errorf("after upgrade-compat install: expected first-declared spelling %q preserved, got %q", "Owner/Repo", got)
	}

	entriesAfter, err := os.ReadDir(filepath.Join(dir, "apm_modules"))
	if err != nil {
		t.Fatalf("read apm_modules after upgrade-compat install: %v", err)
	}
	if len(entriesAfter) != len(entriesBefore) {
		t.Errorf("apm_modules: expected directory count to stay at %d (no re-download for the phantom duplicate), got %d", len(entriesBefore), len(entriesAfter))
	}

	m := readManifestParsed(t)
	if len(m.ParsedDeps) != 1 {
		t.Errorf("apm.yml: expected to remain untouched at ONE entry, got %d: %+v", len(m.ParsedDeps), m.ParsedDeps)
	}
}
