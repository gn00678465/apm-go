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

// TestWrite_HardLinkedTargetIsNotWrittenThrough is the regression for the
// escape an external audit found on 2026-08-12: a hard link planted inside
// the project at the manifest's path, whose other name lives OUTSIDE the
// project, is invisible to every path-based containment check -- Lstat
// reports an ordinary file, because that is exactly what it is. os.WriteFile
// truncates in place and so wrote through to the outside name; measured with
// `pack --force`, the outside file's contents were replaced by the manifest.
//
// A temp-file-plus-rename replaces the directory entry instead, leaving the
// other name pointing at the original inode.
func TestWrite_HardLinkedTargetIsNotWrittenThrough(t *testing.T) {
	// Arrange: <outside>/victim.json, hard-linked to <root>/.claude-plugin/plugin.json
	base := t.TempDir()
	root := filepath.Join(base, "R")
	outside := filepath.Join(base, "O")
	if err := os.MkdirAll(filepath.Join(root, ".claude-plugin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(outside, "victim.json")
	const victimContent = `{"SECRET":"must-not-be-destroyed"}`
	if err := os.WriteFile(victim, []byte(victimContent), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(victim, filepath.Join(root, ".claude-plugin", "plugin.json")); err != nil {
		t.Skipf("cannot create a hard link on this host: %v", err)
	}

	// Act: --force, so the existing-file guard does not short-circuit the write
	var buf bytes.Buffer
	wrote, err := Write(&buf, root, "claude", &bundle.PluginManifest{Name: "demo"}, true, false)
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if !wrote {
		t.Fatalf("Write() wrote = false, want true (output: %s)", buf.String())
	}

	// Assert
	got, err := os.ReadFile(victim)
	if err != nil {
		t.Fatalf("read victim: %v", err)
	}
	if string(got) != victimContent {
		t.Errorf("the file outside the project was written through the hard link:\ngot  %s\nwant %s", got, victimContent)
	}
}

// Mode handling moved to build.RootWriter when the writer stopped using path
// strings (2026-08-13): the permission-preservation and hard-link tests that
// used to live here are now TestRootWriter_WriteFileAtomicKeepsAnExistingFilesMode,
// TestRootWriter_WriteFileAtomicCreatesAWritableFile and
// TestRootWriter_WriteFileAtomicDoesNotWriteThroughAHardLink in
// internal/marketplace/build, alongside the containment tests they share a
// code path with.
