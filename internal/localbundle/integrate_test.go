package localbundle

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/pack/bundle"
)

func TestIntegrateLocalBundle_ZeroTargets_NoOpNoError(t *testing.T) {
	bundleDir := buildTestBundle(t)
	projectDir := t.TempDir()

	result, err := IntegrateLocalBundle(bundleDir, nil, nil, projectDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Files) != 0 {
		t.Errorf("Files = %v, want none when targets is empty", result.Files)
	}
}

// TestIntegrateLocalBundle_DeploysToClaudeTarget locks in the corrected
// routing architecture (Gate 6b): every bundle file deploys VERBATIM (same
// relative path, same bytes) under the target's resolved deploy root --
// NOT re-derived into a deploy.Primitive and re-run through
// deploy.Adapters["claude"].DeployPrimitive's own naming/extension
// transform (services.py:702-1057 is a raw per-file copy, never a
// primitive-transform re-application). Claude's skills primitive has no
// deploy_root override in Python's KNOWN_TARGETS (targets.py:513), so
// skills land ONLY under .claude/skills/ for a claude-only install -- not
// also under the cross-tool .agents/skills/ apm-go's REGULAR (non-bundle)
// deploy pipeline additionally writes to (claude.go's deploySkillClaude,
// an apm-go extension local-bundle install does not replicate).
func TestIntegrateLocalBundle_DeploysToClaudeTarget(t *testing.T) {
	bundleDir := buildTestBundle(t)
	meta := bundleTestPackMeta(t, bundleDir)
	projectDir := t.TempDir()

	result, err := IntegrateLocalBundle(bundleDir, meta, []string{"claude"}, projectDir)
	if err != nil {
		t.Fatal(err)
	}

	wantFiles := []string{
		".claude/agents/foo.md",
		".claude/commands/greet.md",
		".claude/instructions/baz.instructions.md",
		".claude/skills/bar/SKILL.md",
		".claude/hooks.json",
		".mcp.json",
	}
	for _, f := range wantFiles {
		if !containsString(result.Files, f) {
			t.Errorf("Files = %v, want it to contain %s", result.Files, f)
		}
		if _, ok := result.Hashes[f]; !ok {
			t.Errorf("Hashes missing entry for %s", f)
		}
		if _, statErr := os.Stat(filepath.Join(projectDir, filepath.FromSlash(f))); statErr != nil {
			t.Errorf("expected %s to exist on disk: %v", f, statErr)
		}
	}

	// A claude-only install must NOT also write the cross-tool .agents/
	// path -- that's apm-go's regular-deploy-pipeline extension (req-tg-003),
	// which the oracle (Python) never does for a claude-only target.
	if _, statErr := os.Stat(filepath.Join(projectDir, ".agents", "skills", "bar", "SKILL.md")); statErr == nil {
		t.Error(".agents/skills/bar/SKILL.md must not exist for a claude-only install (claude has no skills deploy_root override)")
	}

	// The agent's content must be copied verbatim (no transformation).
	data, err := os.ReadFile(filepath.Join(projectDir, ".claude", "agents", "foo.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "# agent foo" {
		t.Errorf(".claude/agents/foo.md content = %q, want verbatim source copy", data)
	}

	// The instruction file must be copied verbatim (bundle's own
	// "instructions/" directory, target's default root, no rename/reformat
	// into a claude_rules-style ".claude/rules/<name>.md" -- that transform
	// only applies to apm-go's REGULAR .apm/-source deploy pipeline, not a
	// local-bundle's already-final files).
	instrData, err := os.ReadFile(filepath.Join(projectDir, ".claude", "instructions", "baz.instructions.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(instrData), "instruction body") {
		t.Errorf(".claude/instructions/baz.instructions.md content = %q, want verbatim source copy", instrData)
	}

	// hooks.json deploys verbatim to <root>/hooks.json for every target --
	// Python's routing algorithm has no per-target primitive-support gate
	// for it (only "instructions" and "extensions" get special-cased).
	hooksData, err := os.ReadFile(filepath.Join(projectDir, ".claude", "hooks.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(hooksData), "PreToolUse") {
		t.Errorf(".claude/hooks.json = %s, want the bundle's merged hook content", hooksData)
	}

	// The bundle's .mcp.json server must have been wired into the target's
	// native .mcp.json (deploy.MCPTarget), not copied byte-for-byte from the
	// bundle (which lacks the top-level "type" normalization claude writes).
	mcpData, err := os.ReadFile(filepath.Join(projectDir, ".mcp.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(mcpData), "demo-server") {
		t.Errorf(".mcp.json = %s, want the bundle's demo-server entry wired in", mcpData)
	}
}

func TestIntegrateLocalBundle_HooksDeployedForSupportingTarget(t *testing.T) {
	bundleDir := buildTestBundle(t)
	meta := bundleTestPackMeta(t, bundleDir)
	projectDir := t.TempDir()

	result, err := IntegrateLocalBundle(bundleDir, meta, []string{"codex"}, projectDir)
	if err != nil {
		t.Fatal(err)
	}
	if !containsString(result.Files, ".codex/hooks.json") {
		t.Errorf("Files = %v, want .codex/hooks.json", result.Files)
	}
	data, err := os.ReadFile(filepath.Join(projectDir, ".codex", "hooks.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "PreToolUse") {
		t.Errorf(".codex/hooks.json = %s, want the bundle's merged hook content", data)
	}

	// codex's skills primitive DOES override deploy_root to ".agents"
	// (targets.py:700), unlike claude -- so a codex-only install lands
	// skills under the cross-tool root, not .codex/skills/.
	if !containsString(result.Files, ".agents/skills/bar/SKILL.md") {
		t.Errorf("Files = %v, want .agents/skills/bar/SKILL.md (codex's skills deploy_root override)", result.Files)
	}
	if _, statErr := os.Stat(filepath.Join(projectDir, ".codex", "skills")); statErr == nil {
		t.Error(".codex/skills must not exist -- codex's skills deploy_root override routes to .agents instead")
	}

	// codex has no native "instructions" primitive (targets.py:691-710):
	// the bundle's instructions/baz.instructions.md must be skipped (with a
	// diagnostic), not silently dropped without explanation and not
	// deployed to a nonexistent .codex/instructions/ path.
	if containsString(result.Files, ".codex/instructions/baz.instructions.md") {
		t.Errorf("Files = %v, codex has no instructions primitive -- must not deploy instructions verbatim", result.Files)
	}
	foundDiag := false
	for _, d := range result.Diags {
		if strings.Contains(d, "instructions") && strings.Contains(d, "codex") {
			foundDiag = true
		}
	}
	if !foundDiag {
		t.Errorf("Diags = %v, want a diagnostic explaining the skipped instructions file", result.Diags)
	}
}

func TestIntegrateLocalBundle_UnknownTarget_Skipped(t *testing.T) {
	bundleDir := buildTestBundle(t)
	meta := bundleTestPackMeta(t, bundleDir)
	projectDir := t.TempDir()

	result, err := IntegrateLocalBundle(bundleDir, meta, []string{"not-a-real-target"}, projectDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Files) != 0 {
		t.Errorf("Files = %v, want none for an unregistered target", result.Files)
	}
}

func containsString(items []string, want string) bool {
	for _, it := range items {
		if it == want {
			return true
		}
	}
	return false
}

// TestIntegrateLocalBundle_NilMeta_FallsBackToDirectoryWalk covers
// bundleDeployFileRels's fallback branch (services.py's "prevents
// zero-deploy when an older bundle lands"): a bundle with no apm.lock.yaml
// pack: section at all (meta=nil, e.g. produced by an older or non-apm-go
// tool) still deploys every file under bundleDir, skipping only
// apm.lock.yaml/plugin.json/.mcp.json.
func TestIntegrateLocalBundle_NilMeta_FallsBackToDirectoryWalk(t *testing.T) {
	bundleDir := t.TempDir()
	mustWriteFile(t, filepath.Join(bundleDir, "agents", "foo.md"), "# agent foo")
	mustWriteFile(t, filepath.Join(bundleDir, "plugin.json"), `{"name":"demo"}`)
	projectDir := t.TempDir()

	result, err := IntegrateLocalBundle(bundleDir, nil, []string{"claude"}, projectDir)
	if err != nil {
		t.Fatal(err)
	}
	if !containsString(result.Files, ".claude/agents/foo.md") {
		t.Errorf("Files = %v, want .claude/agents/foo.md deployed via the fallback walk", result.Files)
	}
	if containsString(result.Files, ".claude/plugin.json") {
		t.Errorf("Files = %v, plugin.json must never deploy (bundle metadata)", result.Files)
	}
}

// TestIntegrateLocalBundle_NilMeta_CaseInsensitiveMetadataExcluded covers
// Gate 6b's A6 finding (codex-verify-gate6b-fix.md): a legacy/non-apm-go
// bundle (meta=nil, fallback walk) whose metadata files happen to be
// uppercase-named (APM.LOCK.YAML, PLUGIN.JSON, .MCP.JSON -- NTFS is
// case-preserving, not case-sensitive, so any of these could exist on disk
// verbatim) must still be excluded from deploy, not just the exact-lowercase
// names the old case-sensitive apm.lock.yaml check and the plugin.json/
// .mcp.json-only case-fold covered.
func TestIntegrateLocalBundle_NilMeta_CaseInsensitiveMetadataExcluded(t *testing.T) {
	bundleDir := t.TempDir()
	mustWriteFile(t, filepath.Join(bundleDir, "agents", "foo.md"), "# agent foo")
	mustWriteFile(t, filepath.Join(bundleDir, "PLUGIN.JSON"), `{"name":"demo"}`)
	mustWriteFile(t, filepath.Join(bundleDir, ".MCP.JSON"), `{"mcpServers":{}}`)
	mustWriteFile(t, filepath.Join(bundleDir, "APM.LOCK.YAML"), "version: \"1\"\n")
	projectDir := t.TempDir()

	result, err := IntegrateLocalBundle(bundleDir, nil, []string{"claude"}, projectDir)
	if err != nil {
		t.Fatal(err)
	}
	if !containsString(result.Files, ".claude/agents/foo.md") {
		t.Errorf("Files = %v, want .claude/agents/foo.md deployed via the fallback walk", result.Files)
	}
	for _, meta := range []string{"PLUGIN.JSON", ".MCP.JSON", "APM.LOCK.YAML"} {
		want := ".claude/" + meta
		if containsString(result.Files, want) {
			t.Errorf("Files = %v, uppercase-named metadata file %s must never deploy (bundle metadata)", result.Files, meta)
		}
		if _, statErr := os.Stat(filepath.Join(projectDir, ".claude", meta)); statErr == nil {
			t.Errorf("%s must not exist on disk -- uppercase-named metadata must still be excluded", filepath.Join(".claude", meta))
		}
	}
}

// TestIntegrateLocalBundle_JunctionSource_NotDeployed is IntegrateLocalBundle's
// own defense-in-depth counterpart to verify_test.go's junction-rejection
// coverage (Gate 6b's B2 finding): even called directly with a manifest that
// lists a path through a junction, the per-file Lstat in deployBundleFile
// must reject the reparse-point entry itself rather than transparently
// resolving through it to whatever regular file sits on the other side.
func TestIntegrateLocalBundle_JunctionSource_NotDeployed(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("NTFS junctions are windows-specific")
	}
	bundleDir := t.TempDir()
	outsideDir := t.TempDir()
	mustWriteFile(t, filepath.Join(outsideDir, "SKILL.md"), "outside secret")

	linkPath := filepath.Join(bundleDir, "skills", "linked")
	if err := os.MkdirAll(filepath.Dir(linkPath), 0o755); err != nil {
		t.Fatal(err)
	}
	createJunction(t, linkPath, outsideDir)

	projectDir := t.TempDir()
	meta := &bundle.PackMetadata{BundleFiles: map[string]string{"skills/linked/SKILL.md": "deadbeef"}}
	result, err := IntegrateLocalBundle(bundleDir, meta, []string{"claude"}, projectDir)
	if err != nil {
		t.Fatal(err)
	}
	if containsString(result.Files, ".claude/skills/linked/SKILL.md") {
		t.Errorf("Files = %v, must not deploy through a junction source", result.Files)
	}
	if _, statErr := os.Stat(filepath.Join(projectDir, ".claude", "skills", "linked", "SKILL.md")); statErr == nil {
		t.Error(".claude/skills/linked/SKILL.md must not exist on disk -- junction source must be rejected")
	}
}

// TestNormalizedBundleText covers the CRLF-normalization gate itself
// (services.py v0.23.1's _normalized_bundle_text + normalize_crlf_to_lf):
// a text-suffix file gets its CRLF sequences collapsed to LF, a non-text
// suffix is left alone (verbatim signal), and a text-suffix file that is
// not valid UTF-8 also falls back to verbatim (Python's UnicodeDecodeError
// fallback).
func TestNormalizedBundleText(t *testing.T) {
	tests := []struct {
		name     string
		rel      string
		data     []byte
		wantText bool
		wantData string
	}{
		{"markdown CRLF normalized", "instructions/a.md", []byte("line1\r\nline2\r\n"), true, "line1\nline2\n"},
		{"json CRLF normalized", "hooks.json", []byte("{\r\n}\r\n"), true, "{\n}\n"},
		{"non-text suffix left verbatim", "skills/x/scripts/run.sh", []byte("echo\r\nhi\r\n"), false, ""},
		{"invalid utf-8 text-suffix falls back to verbatim", "agents/bad.md", []byte{0x2d, 0x2d, 0xff, 0xfe, 0x0d, 0x0a}, false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := normalizedBundleText(tt.rel, tt.data)
			if ok != tt.wantText {
				t.Fatalf("ok = %v, want %v", ok, tt.wantText)
			}
			if ok && string(got) != tt.wantData {
				t.Errorf("normalized = %q, want %q", got, tt.wantData)
			}
		})
	}
}
