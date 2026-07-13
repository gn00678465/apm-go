package localbundle

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIntegrateLocalBundle_ZeroTargets_NoOpNoError(t *testing.T) {
	bundleDir := buildTestBundle(t)
	projectDir := t.TempDir()

	result, err := IntegrateLocalBundle(bundleDir, nil, projectDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Files) != 0 {
		t.Errorf("Files = %v, want none when targets is empty", result.Files)
	}
}

func TestIntegrateLocalBundle_DeploysToClaudeTarget(t *testing.T) {
	bundleDir := buildTestBundle(t)
	projectDir := t.TempDir()

	result, err := IntegrateLocalBundle(bundleDir, []string{"claude"}, projectDir)
	if err != nil {
		t.Fatal(err)
	}

	wantFiles := []string{
		".claude/agents/foo.md",
		".claude/commands/greet.md",
		".claude/rules/baz.md",
		".agents/skills/bar/SKILL.md",
		".claude/skills/bar/SKILL.md",
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

	// claude does not support TypeHooks (deploy/claude.go's doc comment) --
	// bundle's hooks.json must not be deployed for this target.
	if _, statErr := os.Stat(filepath.Join(projectDir, ".claude", "hooks.json")); statErr == nil {
		t.Error("claude has no hooks primitive support; .claude/hooks.json must not have been written")
	}

	// The agent's content must be copied verbatim (no transformation).
	data, err := os.ReadFile(filepath.Join(projectDir, ".claude", "agents", "foo.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "# agent foo" {
		t.Errorf(".claude/agents/foo.md content = %q, want verbatim source copy", data)
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
	projectDir := t.TempDir()

	result, err := IntegrateLocalBundle(bundleDir, []string{"codex"}, projectDir)
	if err != nil {
		t.Fatal(err)
	}
	if !containsString(result.Files, ".codex/hooks.json") {
		t.Errorf("Files = %v, want .codex/hooks.json (codex supports TypeHooks)", result.Files)
	}
	data, err := os.ReadFile(filepath.Join(projectDir, ".codex", "hooks.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "PreToolUse") {
		t.Errorf(".codex/hooks.json = %s, want the bundle's merged hook content", data)
	}
}

func TestIntegrateLocalBundle_UnknownTarget_Skipped(t *testing.T) {
	bundleDir := buildTestBundle(t)
	projectDir := t.TempDir()

	result, err := IntegrateLocalBundle(bundleDir, []string{"not-a-real-target"}, projectDir)
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
