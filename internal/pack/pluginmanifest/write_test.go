package pluginmanifest

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/pack/bundle"
)

func TestWrite_UnknownEcosystem_WarnsAndWritesNothing(t *testing.T) {
	dir := t.TempDir()
	var buf bytes.Buffer
	wrote, err := Write(&buf, dir, "bogus", &bundle.PluginManifest{Name: "demo"}, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if wrote {
		t.Error("wrote = true, want false for an unknown ecosystem")
	}
	if !strings.Contains(buf.String(), "unknown plugin ecosystem") {
		t.Errorf("output = %q, want an unknown-ecosystem warning", buf.String())
	}
}

func TestWrite_DryRun_WritesNothing(t *testing.T) {
	dir := t.TempDir()
	var buf bytes.Buffer
	wrote, err := Write(&buf, dir, "claude", &bundle.PluginManifest{Name: "demo"}, false, true)
	if err != nil {
		t.Fatal(err)
	}
	if wrote {
		t.Error("wrote = true, want false for --dry-run")
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".claude-plugin", "plugin.json")); !os.IsNotExist(statErr) {
		t.Errorf("dry-run must not create any file (stat err = %v)", statErr)
	}
	if !strings.Contains(buf.String(), "Would write plugin manifest") {
		t.Errorf("output = %q, want a dry-run notice", buf.String())
	}
}

func TestWrite_NewFile_Succeeds(t *testing.T) {
	dir := t.TempDir()
	var buf bytes.Buffer
	wrote, err := Write(&buf, dir, "claude", &bundle.PluginManifest{Name: "demo", Version: "1.0.0"}, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if !wrote {
		t.Fatal("wrote = false, want true for a new file")
	}
	data, rerr := os.ReadFile(filepath.Join(dir, ".claude-plugin", "plugin.json"))
	if rerr != nil {
		t.Fatal(rerr)
	}
	if !strings.Contains(string(data), `"name": "demo"`) {
		t.Errorf("output = %s, want name field", data)
	}
	if !strings.HasSuffix(string(data), "\n") {
		t.Error("output must end with a trailing newline")
	}
}

func TestWrite_ExistingFile_NoForce_SkipsWithWarning(t *testing.T) {
	dir := t.TempDir()
	claudeDir := filepath.Join(dir, ".claude-plugin")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	original := []byte("hand-authored content\n")
	if err := os.WriteFile(filepath.Join(claudeDir, "plugin.json"), original, 0o644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	wrote, err := Write(&buf, dir, "claude", &bundle.PluginManifest{Name: "demo"}, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if wrote {
		t.Error("wrote = true, want false when an existing file is preserved")
	}
	data, rerr := os.ReadFile(filepath.Join(claudeDir, "plugin.json"))
	if rerr != nil {
		t.Fatal(rerr)
	}
	if !bytes.Equal(data, original) {
		t.Errorf("existing file was modified without --force: got %q, want %q", data, original)
	}
	if !strings.Contains(buf.String(), "already exists; skipping") {
		t.Errorf("output = %q, want a skip warning", buf.String())
	}
	if !strings.Contains(buf.String(), "--force") {
		t.Errorf("output = %q, want a mention of --force", buf.String())
	}
}

func TestWrite_ExistingFile_Force_Overwrites(t *testing.T) {
	dir := t.TempDir()
	claudeDir := filepath.Join(dir, ".claude-plugin")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(claudeDir, "plugin.json"), []byte("stale\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	wrote, err := Write(&buf, dir, "claude", &bundle.PluginManifest{Name: "fresh"}, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if !wrote {
		t.Fatal("wrote = false, want true with --force")
	}
	data, rerr := os.ReadFile(filepath.Join(claudeDir, "plugin.json"))
	if rerr != nil {
		t.Fatal(rerr)
	}
	if !strings.Contains(string(data), `"name": "fresh"`) {
		t.Errorf("output = %s, want overwritten content", data)
	}
	if !strings.Contains(buf.String(), "Overwriting") {
		t.Errorf("output = %q, want an overwrite warning", buf.String())
	}
}

func TestWrite_CopilotPath_ExtraGithubInfoLine(t *testing.T) {
	dir := t.TempDir()
	var buf bytes.Buffer
	if _, err := Write(&buf, dir, "copilot", &bundle.PluginManifest{Name: "demo"}, false, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), ".github/") {
		t.Errorf("output = %q, want a .github/ elevated-trust info line", buf.String())
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".github", "plugin", "plugin.json")); statErr != nil {
		t.Errorf("expected copilot output path: %v", statErr)
	}
}

func TestWrite_ClaudePath_NoGithubInfoLine(t *testing.T) {
	dir := t.TempDir()
	var buf bytes.Buffer
	if _, err := Write(&buf, dir, "claude", &bundle.PluginManifest{Name: "demo"}, false, false); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), ".github/") {
		t.Errorf("output = %q, claude output must not print the .github/ info line", buf.String())
	}
}
