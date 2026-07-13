package localbundle

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/pack/bundle"
)

func TestVerifyBundleIntegrity_CleanBundle_NoErrors(t *testing.T) {
	bundleDir := buildTestBundle(t)
	info, err := DetectLocalBundle(bundleDir)
	if err != nil || info == nil {
		t.Fatalf("detect failed: info=%v err=%v", info, err)
	}

	errs := VerifyBundleIntegrity(info.SourceDir, info.PackMeta)
	if len(errs) != 0 {
		t.Errorf("errs = %v, want none for a clean, untampered bundle", errs)
	}
}

func TestVerifyBundleIntegrity_TamperedFile_HashMismatch(t *testing.T) {
	bundleDir := buildTestBundle(t)
	info, err := DetectLocalBundle(bundleDir)
	if err != nil || info == nil {
		t.Fatalf("detect failed: info=%v err=%v", info, err)
	}

	if err := os.WriteFile(filepath.Join(bundleDir, "agents", "foo.md"), []byte("TAMPERED"), 0o644); err != nil {
		t.Fatal(err)
	}

	errs := VerifyBundleIntegrity(info.SourceDir, info.PackMeta)
	if !containsSubstring(errs, "Hash mismatch for agents/foo.md") {
		t.Errorf("errs = %v, want a hash-mismatch error naming agents/foo.md", errs)
	}
}

func TestVerifyBundleIntegrity_Symlink_Rejected(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires elevated privileges on windows")
	}
	bundleDir := buildTestBundle(t)
	info, err := DetectLocalBundle(bundleDir)
	if err != nil || info == nil {
		t.Fatalf("detect failed: info=%v err=%v", info, err)
	}

	linkPath := filepath.Join(bundleDir, "evil-link.md")
	if err := os.Symlink(filepath.Join(bundleDir, "plugin.json"), linkPath); err != nil {
		t.Fatal(err)
	}

	errs := VerifyBundleIntegrity(info.SourceDir, info.PackMeta)
	if !containsSubstring(errs, "Symlink rejected") || !containsSubstring(errs, "evil-link.md") {
		t.Errorf("errs = %v, want a symlink-rejected error naming evil-link.md", errs)
	}
}

// TestVerifyBundleIntegrity_ListedSymlink_StillRejected covers Python's
// "even when not listed in the manifest" AND the reverse: a symlink whose
// path IS mentioned in pack.bundle_files must still be rejected by the
// symlink sweep, never merely treated as a hash check.
func TestVerifyBundleIntegrity_ListedSymlink_StillRejected(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires elevated privileges on windows")
	}
	bundleDir := t.TempDir()
	mustWriteFile(t, filepath.Join(bundleDir, "plugin.json"), "{}")
	linkPath := filepath.Join(bundleDir, "agents", "foo.md")
	if err := os.MkdirAll(filepath.Dir(linkPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(bundleDir, "plugin.json"), linkPath); err != nil {
		t.Fatal(err)
	}

	meta := bundle.PackMetadata{BundleFiles: map[string]string{"agents/foo.md": "deadbeef"}}
	errs := VerifyBundleIntegrity(bundleDir, meta)
	if !containsSubstring(errs, "Symlink rejected") {
		t.Errorf("errs = %v, want a symlink-rejected error even though the path is listed in bundle_files", errs)
	}
}

// TestVerifyBundleIntegrity_Junction_Rejected covers Gate 6b's B2 finding
// (codex-verify-gate6b-fix.md): an NTFS junction ANYWHERE under the bundle
// root -- even one that requires no elevated privilege to create, unlike a
// real symlink -- must be rejected by the same sweep that rejects symlinks,
// not silently pass because os.Lstat only sets os.ModeSymlink for a true
// symlink reparse tag (isSymlinkOrReparsePoint covers the gap).
func TestVerifyBundleIntegrity_Junction_Rejected(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("NTFS junctions are windows-specific")
	}
	bundleDir := t.TempDir()
	mustWriteFile(t, filepath.Join(bundleDir, "plugin.json"), "{}")

	linkPath := filepath.Join(bundleDir, "skills", "linked")
	if err := os.MkdirAll(filepath.Dir(linkPath), 0o755); err != nil {
		t.Fatal(err)
	}
	outsideDir := t.TempDir()
	createJunction(t, linkPath, outsideDir)

	errs := VerifyBundleIntegrity(bundleDir, bundle.PackMetadata{})
	if !containsSubstring(errs, "Symlink rejected") || !containsSubstring(errs, "skills/linked") {
		t.Errorf("errs = %v, want a symlink/reparse-point-rejected error naming skills/linked", errs)
	}
}

// TestVerifyBundleIntegrity_JunctionWithListedTargetFile_StillRejected
// reproduces codex's exact B2 PoC shape: a tampered bundle whose manifest
// lists a path THROUGH the junction (skills/linked/SKILL.md) with a hash
// matching the file OUTSIDE the bundle the junction points at -- a leaf-only
// os.Lstat on the joined path would transparently resolve through the
// junction and see a perfectly normal, hash-matching regular file. The
// symlink sweep must still reject the bundle because it visits the junction
// entry itself (skills/linked), independent of what any manifest key claims.
func TestVerifyBundleIntegrity_JunctionWithListedTargetFile_StillRejected(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("NTFS junctions are windows-specific")
	}
	bundleDir := t.TempDir()
	mustWriteFile(t, filepath.Join(bundleDir, "plugin.json"), "{}")

	outsideDir := t.TempDir()
	mustWriteFile(t, filepath.Join(outsideDir, "SKILL.md"), "outside secret")
	data, err := os.ReadFile(filepath.Join(outsideDir, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)

	linkPath := filepath.Join(bundleDir, "skills", "linked")
	if err := os.MkdirAll(filepath.Dir(linkPath), 0o755); err != nil {
		t.Fatal(err)
	}
	createJunction(t, linkPath, outsideDir)

	meta := bundle.PackMetadata{BundleFiles: map[string]string{
		"skills/linked/SKILL.md": hex.EncodeToString(sum[:]),
	}}
	errs := VerifyBundleIntegrity(bundleDir, meta)
	if !containsSubstring(errs, "Symlink rejected") || !containsSubstring(errs, "skills/linked") {
		t.Errorf("errs = %v, want a symlink/reparse-point-rejected error naming skills/linked even though a manifest key's hash matches the outside file", errs)
	}
}

func TestVerifyBundleIntegrity_UnlistedExtraFile_Rejected(t *testing.T) {
	bundleDir := buildTestBundle(t)
	info, err := DetectLocalBundle(bundleDir)
	if err != nil || info == nil {
		t.Fatalf("detect failed: info=%v err=%v", info, err)
	}

	if err := os.WriteFile(filepath.Join(bundleDir, "agents", "sneaky.md"), []byte("not in manifest"), 0o644); err != nil {
		t.Fatal(err)
	}

	errs := VerifyBundleIntegrity(info.SourceDir, info.PackMeta)
	if !containsSubstring(errs, "Unlisted bundle file") || !containsSubstring(errs, "agents/sneaky.md") {
		t.Errorf("errs = %v, want an unlisted-bundle-file error naming agents/sneaky.md", errs)
	}
}

func TestVerifyBundleIntegrity_MultipleUnlistedFiles_AllReported(t *testing.T) {
	bundleDir := buildTestBundle(t)
	info, err := DetectLocalBundle(bundleDir)
	if err != nil || info == nil {
		t.Fatalf("detect failed: info=%v err=%v", info, err)
	}
	mustWriteFile(t, filepath.Join(bundleDir, "agents", "sneaky1.md"), "x")
	mustWriteFile(t, filepath.Join(bundleDir, "agents", "sneaky2.md"), "y")

	errs := VerifyBundleIntegrity(info.SourceDir, info.PackMeta)
	if !containsSubstring(errs, "agents/sneaky1.md") || !containsSubstring(errs, "agents/sneaky2.md") {
		t.Errorf("errs = %v, want both unlisted files reported", errs)
	}
}

func TestVerifyBundleIntegrity_PathTraversalKey_Rejected(t *testing.T) {
	bundleDir := t.TempDir()
	mustWriteFile(t, filepath.Join(bundleDir, "plugin.json"), "{}")
	meta := bundle.PackMetadata{BundleFiles: map[string]string{"../../etc/passwd": "deadbeef"}}

	errs := VerifyBundleIntegrity(bundleDir, meta)
	if !containsSubstring(errs, "Unsafe bundle_files entry") {
		t.Errorf("errs = %v, want an unsafe bundle_files entry error", errs)
	}
}

func TestVerifyBundleIntegrity_MissingListedFile_Rejected(t *testing.T) {
	bundleDir := t.TempDir()
	mustWriteFile(t, filepath.Join(bundleDir, "plugin.json"), "{}")
	meta := bundle.PackMetadata{BundleFiles: map[string]string{"agents/gone.md": "deadbeef"}}

	errs := VerifyBundleIntegrity(bundleDir, meta)
	if !containsSubstring(errs, "Missing bundle file: agents/gone.md") {
		t.Errorf("errs = %v, want a missing-bundle-file error", errs)
	}
}

func TestVerifyBundleIntegrity_NoBundleFiles_EveryFileUnlisted(t *testing.T) {
	// Mirrors Python: a lockfile present but with no pack.bundle_files (an
	// anomalous/legacy shape) flags EVERY bundle file as unlisted, rather
	// than silently skipping verification.
	bundleDir := t.TempDir()
	mustWriteFile(t, filepath.Join(bundleDir, "plugin.json"), "{}")
	mustWriteFile(t, filepath.Join(bundleDir, "agents", "foo.md"), "x")

	errs := VerifyBundleIntegrity(bundleDir, bundle.PackMetadata{})
	if !containsSubstring(errs, "Unlisted bundle file (not in pack.bundle_files): agents/foo.md") {
		t.Errorf("errs = %v, want agents/foo.md reported unlisted when bundle_files is empty", errs)
	}
}

func TestCheckTargetMismatch_NoBundleTargets_NoWarning(t *testing.T) {
	if got := CheckTargetMismatch(nil, []string{"claude"}); got != "" {
		t.Errorf("got = %q, want empty", got)
	}
}

func TestCheckTargetMismatch_AllIsTargetAgnostic_NoWarning(t *testing.T) {
	if got := CheckTargetMismatch([]string{"all"}, []string{"codex"}); got != "" {
		t.Errorf("got = %q, want empty", got)
	}
}

func TestCheckTargetMismatch_InstallIsSuperset_NoWarning(t *testing.T) {
	if got := CheckTargetMismatch([]string{"claude"}, []string{"claude", "codex"}); got != "" {
		t.Errorf("got = %q, want empty", got)
	}
}

func TestCheckTargetMismatch_MissingTarget_WarnsAndNamesIt(t *testing.T) {
	got := CheckTargetMismatch([]string{"claude", "copilot"}, []string{"claude"})
	if got == "" {
		t.Fatal("got = empty, want a mismatch warning")
	}
	if !strings.Contains(got, "copilot") {
		t.Errorf("got = %q, want it to name the missing target copilot", got)
	}
}

func containsSubstring(errs []string, substr string) bool {
	for _, e := range errs {
		if strings.Contains(e, substr) {
			return true
		}
	}
	return false
}
