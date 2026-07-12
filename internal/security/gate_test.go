package security

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestScanPolicy_EffectiveBlock(t *testing.T) {
	if WarnPolicy.EffectiveBlock(false) {
		t.Error("WarnPolicy should never block")
	}
	if WarnPolicy.EffectiveBlock(true) {
		t.Error("WarnPolicy should never block, even with force")
	}
	if !BlockPolicy.EffectiveBlock(false) {
		t.Error("BlockPolicy without force should block")
	}
	if BlockPolicy.EffectiveBlock(true) {
		t.Error("BlockPolicy with force (ForceOverrides=true) should not block")
	}
	if ReportPolicy.EffectiveBlock(false) {
		t.Error("ReportPolicy should never block")
	}
	if ReportPolicy.EffectiveBlock(true) {
		t.Error("ReportPolicy should never block, even with force")
	}
}

func TestSecurityGate_ScanText_WarnPolicyNeverBlocks(t *testing.T) {
	content := "x" + string(rune(0x202E)) + "y" // critical bidi-override
	verdict := SecurityGate{}.ScanText(content, "f.md", WarnPolicy)
	if !verdict.HasCritical {
		t.Fatal("expected HasCritical=true")
	}
	if verdict.ShouldBlock {
		t.Error("WarnPolicy should never ShouldBlock, even with critical findings")
	}
	if verdict.CriticalCount != 1 {
		t.Errorf("CriticalCount = %d, want 1", verdict.CriticalCount)
	}
}

func TestSecurityGate_ScanText_BlockPolicyBlocksWithoutForce(t *testing.T) {
	content := "x" + string(rune(0x202E)) + "y"
	verdict := SecurityGate{}.ScanText(content, "f.md", BlockPolicy)
	if !verdict.ShouldBlock {
		t.Error("BlockPolicy with critical findings and force=false should ShouldBlock")
	}
}

func TestSecurityGate_ScanFiles_ForceOverridesCriticalBlockOnly(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "dirty.md")
	if err := os.WriteFile(p, []byte("x"+string(rune(0x202E))+"y"), 0o644); err != nil {
		t.Fatal(err)
	}

	blocked := SecurityGate{}.ScanFiles(dir, BlockPolicy, false)
	if !blocked.ShouldBlock {
		t.Error("BlockPolicy without force should ShouldBlock")
	}

	forced := SecurityGate{}.ScanFiles(dir, BlockPolicy, true)
	if forced.ShouldBlock {
		t.Error("BlockPolicy with force=true (ForceOverrides=true) should not ShouldBlock")
	}
	if !forced.HasCritical {
		t.Error("--force must not change HasCritical, only ShouldBlock")
	}
}

func TestSecurityGate_ScanText_CleanContentEmptyVerdict(t *testing.T) {
	verdict := SecurityGate{}.ScanText("clean ascii text", "f.md", BlockPolicy)
	if verdict.HasCritical || verdict.ShouldBlock || len(verdict.FindingsByFile) != 0 {
		t.Errorf("clean content should produce an empty verdict, got %+v", verdict)
	}
	if verdict.FilesScanned != 1 {
		t.Errorf("FilesScanned = %d, want 1", verdict.FilesScanned)
	}
}

func TestSecurityGate_ScanFiles_SkipsSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires elevated privileges on Windows")
	}
	dir := t.TempDir()
	clean := filepath.Join(dir, "clean.md")
	if err := os.WriteFile(clean, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	dirty := filepath.Join(dir, "dirty.md")
	if err := os.WriteFile(dirty, []byte("x"+string(rune(0x202E))+"y"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.md")
	if err := os.Symlink(dirty, link); err != nil {
		t.Skipf("symlink not supported in this environment: %v", err)
	}

	verdict := SecurityGate{}.ScanFiles(dir, BlockPolicy, false)

	if _, ok := verdict.FindingsByFile["link.md"]; ok {
		t.Error("symlinked file must be skipped (Lstat-based), not scanned")
	}
	if _, ok := verdict.FindingsByFile["dirty.md"]; !ok {
		t.Error("regular dirty.md should still be scanned and flagged")
	}
	if verdict.FilesScanned != 2 {
		t.Errorf("FilesScanned = %d, want 2 (clean.md + dirty.md; symlink excluded)", verdict.FilesScanned)
	}
}

func TestSecurityGate_ScanFiles_NestedDirsUseForwardSlashRelPaths(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(sub, "nested.md")
	if err := os.WriteFile(p, []byte("x"+string(rune(0x200B))+"y"), 0o644); err != nil {
		t.Fatal(err)
	}

	verdict := SecurityGate{}.ScanFiles(dir, WarnPolicy, false)
	if _, ok := verdict.FindingsByFile["sub/nested.md"]; !ok {
		keys := make([]string, 0, len(verdict.FindingsByFile))
		for k := range verdict.FindingsByFile {
			keys = append(keys, k)
		}
		t.Errorf("expected findings keyed by forward-slash relative path 'sub/nested.md', got keys %v", keys)
	}
}

func TestScanVerdict_AllFindings(t *testing.T) {
	v := ScanVerdict{
		FindingsByFile: map[string][]ScanFinding{
			"a.md": {{Severity: SeverityCritical}},
			"b.md": {{Severity: SeverityWarning}, {Severity: SeverityInfo}},
		},
	}
	if all := v.AllFindings(); len(all) != 3 {
		t.Errorf("AllFindings length = %d, want 3", len(all))
	}
}
