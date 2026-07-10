package deploy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/lockfile"
)

// writeDeployedFile writes content under dir/relPath and returns the sha256
// envelope hash it deployed at, matching what install.go/deploy.Run records
// in LockedDep.DeployedHashes.
func writeDeployedFile(t *testing.T, dir, relPath, content string) string {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	hash, err := lockfile.HashFileBytes(full)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	return hash
}

func TestRemoveDeployedFiles_NormalRemoval(t *testing.T) {
	dir := t.TempDir()
	rel := ".agents/skills/foo/SKILL.md"
	hash := writeDeployedFile(t, dir, rel, "skill content")

	removed, kept, diags := RemoveDeployedFiles(dir, []string{rel}, map[string]string{rel: hash})

	if len(kept) != 0 {
		t.Fatalf("expected no kept files, got %v (diags=%v)", kept, diags)
	}
	if len(removed) != 1 || removed[0] != rel {
		t.Fatalf("expected removed=[%s], got %v", rel, removed)
	}
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel))); !os.IsNotExist(err) {
		t.Fatalf("expected file to be deleted, stat err=%v", err)
	}
}

func TestRemoveDeployedFiles_HashMismatchIsKeptWithWarning(t *testing.T) {
	dir := t.TempDir()
	rel := ".agents/commands/bar.md"
	origHash := writeDeployedFile(t, dir, rel, "original content")

	// User hand-edited the file after deploy.
	full := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.WriteFile(full, []byte("user edited content"), 0o644); err != nil {
		t.Fatalf("edit file: %v", err)
	}

	removed, kept, diags := RemoveDeployedFiles(dir, []string{rel}, map[string]string{rel: origHash})

	if len(removed) != 0 {
		t.Fatalf("expected no removed files, got %v", removed)
	}
	if len(kept) != 1 || kept[0] != rel {
		t.Fatalf("expected kept=[%s], got %v", rel, kept)
	}
	if len(diags) == 0 {
		t.Fatalf("expected a diagnostic warning for hash mismatch, got none")
	}
	if _, err := os.Stat(full); err != nil {
		t.Fatalf("expected user-edited file to remain on disk, stat err=%v", err)
	}
	if got, _ := os.ReadFile(full); string(got) != "user edited content" {
		t.Fatalf("file content changed unexpectedly: %q", got)
	}
}

func TestRemoveDeployedFiles_MissingHashKeyIsKept(t *testing.T) {
	dir := t.TempDir()
	rel := ".agents/agents/baz.md"
	writeDeployedFile(t, dir, rel, "content")

	// hashes map has no entry for rel at all (older lockfile / provenance gap).
	removed, kept, diags := RemoveDeployedFiles(dir, []string{rel}, map[string]string{})

	if len(removed) != 0 {
		t.Fatalf("expected no removed files, got %v", removed)
	}
	if len(kept) != 1 || kept[0] != rel {
		t.Fatalf("expected kept=[%s], got %v", rel, kept)
	}
	if len(diags) == 0 {
		t.Fatalf("expected a diagnostic for missing hash, got none")
	}
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel))); err != nil {
		t.Fatalf("expected file to remain on disk, stat err=%v", err)
	}
}

func TestRemoveDeployedFiles_PathEscapeIsRejected(t *testing.T) {
	dir := t.TempDir()
	// Sibling directory outside dir, to prove nothing outside dir is touched.
	parent := filepath.Dir(dir)
	outside := filepath.Join(parent, "escaped-uninstall-victim.txt")
	if err := os.WriteFile(outside, []byte("should never be deleted"), 0o644); err != nil {
		t.Fatalf("seed outside file: %v", err)
	}
	defer os.Remove(outside)

	rel := "../" + filepath.Base(outside)
	removed, kept, diags := RemoveDeployedFiles(dir, []string{rel}, map[string]string{rel: "sha256:deadbeef"})

	if len(removed) != 0 {
		t.Fatalf("expected no removed files, got %v", removed)
	}
	if len(kept) != 1 {
		t.Fatalf("expected kept=[%s], got %v", rel, kept)
	}
	if len(diags) == 0 {
		t.Fatalf("expected a diagnostic for path escape, got none")
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("expected outside file to survive, stat err=%v", err)
	}
}

func TestRemoveDeployedFiles_UserHandwrittenFileNotInListUntouched(t *testing.T) {
	dir := t.TempDir()
	deployedRel := ".agents/instructions/pkg.md"
	hash := writeDeployedFile(t, dir, deployedRel, "deployed content")

	// A user-authored file living alongside the deployed one, never passed in files.
	userRel := ".agents/instructions/my-notes.md"
	writeDeployedFile(t, dir, userRel, "user notes")

	removed, kept, diags := RemoveDeployedFiles(dir, []string{deployedRel}, map[string]string{deployedRel: hash})

	if len(kept) != 0 || len(diags) != 0 {
		t.Fatalf("expected clean removal of only the deployed file, kept=%v diags=%v", kept, diags)
	}
	if len(removed) != 1 || removed[0] != deployedRel {
		t.Fatalf("expected removed=[%s], got %v", deployedRel, removed)
	}
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(userRel))); err != nil {
		t.Fatalf("expected user file to survive untouched, stat err=%v", err)
	}
}

func TestRemoveDeployedFiles_NonexistentFileSkippedWithoutError(t *testing.T) {
	dir := t.TempDir()
	rel := ".agents/skills/gone/SKILL.md"

	removed, kept, diags := RemoveDeployedFiles(dir, []string{rel}, map[string]string{rel: "sha256:whatever"})

	if len(removed) != 0 {
		t.Fatalf("expected no removed entries for a file that never existed, got %v", removed)
	}
	if len(kept) != 0 {
		t.Fatalf("expected no kept entries for a nonexistent file, got %v", kept)
	}
	if len(diags) != 0 {
		t.Fatalf("expected no diagnostics for a nonexistent file, got %v", diags)
	}
}

func TestRemoveDeployedFiles_CleansUpEmptyParentDirectories(t *testing.T) {
	dir := t.TempDir()
	rel := "apm_modules/owner/repo/skills/foo/SKILL.md"
	hash := writeDeployedFile(t, dir, rel, "content")

	_, kept, diags := RemoveDeployedFiles(dir, []string{rel}, map[string]string{rel: hash})
	if len(kept) != 0 || len(diags) != 0 {
		t.Fatalf("expected clean removal, kept=%v diags=%v", kept, diags)
	}

	// The whole now-empty chain up to (but not including) projectDir must be gone.
	for _, sub := range []string{
		"apm_modules/owner/repo/skills/foo",
		"apm_modules/owner/repo/skills",
		"apm_modules/owner/repo",
		"apm_modules/owner",
		"apm_modules",
	} {
		full := filepath.Join(dir, filepath.FromSlash(sub))
		if _, err := os.Stat(full); !os.IsNotExist(err) {
			t.Fatalf("expected empty parent %s to be cleaned up, stat err=%v", sub, err)
		}
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("projectDir itself must survive, stat err=%v", err)
	}
}

func TestRemoveDeployedFiles_StopsCleanupWhenSiblingRemains(t *testing.T) {
	dir := t.TempDir()
	removedRel := "apm_modules/owner/repo/skills/foo/SKILL.md"
	hash := writeDeployedFile(t, dir, removedRel, "content")

	// A sibling file in the same parent dir that must NOT be deleted, and
	// whose presence must halt upward empty-dir cleanup at that level.
	siblingRel := "apm_modules/owner/repo/skills/bar/SKILL.md"
	writeDeployedFile(t, dir, siblingRel, "other skill")

	_, kept, diags := RemoveDeployedFiles(dir, []string{removedRel}, map[string]string{removedRel: hash})
	if len(kept) != 0 || len(diags) != 0 {
		t.Fatalf("expected clean removal, kept=%v diags=%v", kept, diags)
	}

	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(removedRel))); !os.IsNotExist(err) {
		t.Fatalf("expected removed file gone, stat err=%v", err)
	}
	// Parent "skills" dir must remain since "bar" is still inside it.
	if _, err := os.Stat(filepath.Join(dir, "apm_modules/owner/repo/skills")); err != nil {
		t.Fatalf("expected non-empty parent to survive, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(siblingRel))); err != nil {
		t.Fatalf("expected sibling file to survive, stat err=%v", err)
	}
}

func TestRemoveDeployedFiles_AntigravityAgentDirPrunedSiblingSurvives(t *testing.T) {
	// Lifecycle regression for the antigravity agents primitive
	// (.agents/agents/<name>/agent.md): uninstall's reverse-clean is generic
	// over the lockfile's deployed_files/deployed_file_hashes, so the new
	// per-agent-directory path must behave exactly like existing primitives --
	// the file is removed, its now-empty per-agent directory is pruned by
	// cleanupEmptyParents, and a sibling agent deployed by another package
	// survives along with the shared .agents/agents/ root.
	dir := t.TempDir()
	removedRel := ".agents/agents/reviewer/agent.md"
	hash := writeDeployedFile(t, dir, removedRel, "reviewer agent body")
	siblingRel := ".agents/agents/helper/agent.md"
	writeDeployedFile(t, dir, siblingRel, "helper agent body")

	removed, kept, diags := RemoveDeployedFiles(dir, []string{removedRel}, map[string]string{removedRel: hash})

	if len(kept) != 0 || len(diags) != 0 {
		t.Fatalf("expected clean removal, kept=%v diags=%v", kept, diags)
	}
	if len(removed) != 1 || removed[0] != removedRel {
		t.Fatalf("expected removed=[%s], got %v", removedRel, removed)
	}
	if _, err := os.Stat(filepath.Join(dir, ".agents", "agents", "reviewer")); !os.IsNotExist(err) {
		t.Fatalf("expected empty per-agent dir to be pruned, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(siblingRel))); err != nil {
		t.Fatalf("expected sibling agent to survive, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".agents", "agents")); err != nil {
		t.Fatalf("expected shared .agents/agents/ root to survive, stat err=%v", err)
	}
}

func TestRemoveDeployedFiles_TargetIsDirectoryIsKept(t *testing.T) {
	// Defensive case: deployed_files entries should always be regular files,
	// but if a directory ever ends up at that path (stale/corrupted
	// provenance, or something else created it there), hashing it must fail
	// closed -- the entry is kept with a diagnostic, never force-removed.
	dir := t.TempDir()
	rel := ".agents/skills/weird-dir-entry"
	if err := os.MkdirAll(filepath.Join(dir, filepath.FromSlash(rel)), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	removed, kept, diags := RemoveDeployedFiles(dir, []string{rel}, map[string]string{rel: "sha256:deadbeef"})

	if len(removed) != 0 {
		t.Fatalf("expected no removed entries, got %v", removed)
	}
	if len(kept) != 1 || kept[0] != rel {
		t.Fatalf("expected kept=[%s], got %v", rel, kept)
	}
	if len(diags) == 0 {
		t.Fatalf("expected a diagnostic explaining why the directory was kept")
	}
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel))); err != nil {
		t.Fatalf("expected directory to survive, stat err=%v", err)
	}
}

func TestSafeRemoveModuleDir_NormalRemoval(t *testing.T) {
	dir := t.TempDir()
	pkgDir := filepath.Join(dir, "apm_modules", "acme", "foo")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "apm.yml"), []byte("name: foo\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	removed, err := SafeRemoveModuleDir(dir, "acme/foo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !removed {
		t.Fatal("expected removed=true")
	}
	if _, err := os.Stat(pkgDir); !os.IsNotExist(err) {
		t.Fatalf("expected package dir gone, stat err=%v", err)
	}
	// The now-empty "apm_modules/acme" and "apm_modules" chain must also be
	// cleaned up (same convention as RemoveDeployedFiles's cleanup).
	if _, err := os.Stat(filepath.Join(dir, "apm_modules")); !os.IsNotExist(err) {
		t.Fatalf("expected apm_modules to be cleaned up, stat err=%v", err)
	}
}

func TestSafeRemoveModuleDir_SiblingPackageSurvives(t *testing.T) {
	dir := t.TempDir()
	fooDir := filepath.Join(dir, "apm_modules", "acme", "foo")
	barDir := filepath.Join(dir, "apm_modules", "acme", "bar")
	if err := os.MkdirAll(fooDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(barDir, 0o755); err != nil {
		t.Fatal(err)
	}

	removed, err := SafeRemoveModuleDir(dir, "acme/foo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !removed {
		t.Fatal("expected removed=true")
	}
	if _, err := os.Stat(fooDir); !os.IsNotExist(err) {
		t.Fatalf("expected acme/foo gone, stat err=%v", err)
	}
	if _, err := os.Stat(barDir); err != nil {
		t.Fatalf("expected acme/bar (sibling) to survive, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "apm_modules", "acme")); err != nil {
		t.Fatalf("expected apm_modules/acme to survive (still has bar), stat err=%v", err)
	}
}

func TestSafeRemoveModuleDir_NonexistentIsNoOp(t *testing.T) {
	dir := t.TempDir()
	removed, err := SafeRemoveModuleDir(dir, "acme/never-installed")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if removed {
		t.Fatal("expected removed=false for a directory that never existed")
	}
}

func TestSafeRemoveModuleDir_PathEscapeIsRejected(t *testing.T) {
	dir := t.TempDir()
	// A sibling directory outside apm_modules that a crafted ".." identityKey
	// would otherwise be able to reach.
	victim := filepath.Join(dir, "victim")
	if err := os.MkdirAll(filepath.Join(victim, "keepme"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "apm_modules"), 0o755); err != nil {
		t.Fatal(err)
	}

	removed, err := SafeRemoveModuleDir(dir, "../victim")
	if err == nil {
		t.Fatal("expected an error for a path-escaping identityKey")
	}
	if removed {
		t.Fatal("expected removed=false when the path escapes apm_modules")
	}
	if _, statErr := os.Stat(filepath.Join(victim, "keepme")); statErr != nil {
		t.Fatalf("expected victim directory to survive untouched, stat err=%v", statErr)
	}
}

func TestSafeRemoveModuleDir_EmptyIdentityKeyIsNoOp(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "apm_modules", "something"), 0o755); err != nil {
		t.Fatal(err)
	}

	removed, err := SafeRemoveModuleDir(dir, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if removed {
		t.Fatal("expected removed=false for an empty identityKey")
	}
	if _, err := os.Stat(filepath.Join(dir, "apm_modules", "something")); err != nil {
		t.Fatalf("expected apm_modules to survive untouched, stat err=%v", err)
	}
}

func TestRemoveDeployedFiles_MultipleFilesMixedOutcomes(t *testing.T) {
	dir := t.TempDir()
	okRel := ".agents/commands/ok.md"
	okHash := writeDeployedFile(t, dir, okRel, "fine")

	editedRel := ".agents/commands/edited.md"
	editedHash := writeDeployedFile(t, dir, editedRel, "original")
	os.WriteFile(filepath.Join(dir, filepath.FromSlash(editedRel)), []byte("edited"), 0o644)

	removed, kept, diags := RemoveDeployedFiles(dir, []string{okRel, editedRel},
		map[string]string{okRel: okHash, editedRel: editedHash})

	if len(removed) != 1 || removed[0] != okRel {
		t.Fatalf("expected removed=[%s], got %v", okRel, removed)
	}
	if len(kept) != 1 || kept[0] != editedRel {
		t.Fatalf("expected kept=[%s], got %v", editedRel, kept)
	}
	if len(diags) != 1 || !strings.Contains(diags[0], editedRel) {
		t.Fatalf("expected one diag referencing %s, got %v", editedRel, diags)
	}
}
