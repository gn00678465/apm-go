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
	_, outcome, err := Write(&buf, dir, "bogus", &bundle.PluginManifest{Name: "demo"}, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if outcome != Skipped {
		t.Errorf("outcome = %v, want Skipped for an unknown ecosystem", outcome)
	}
	if !strings.Contains(buf.String(), "unknown plugin ecosystem") {
		t.Errorf("output = %q, want an unknown-ecosystem warning", buf.String())
	}
}

func TestWrite_DryRun_WritesNothing(t *testing.T) {
	dir := t.TempDir()
	var buf bytes.Buffer
	_, outcome, err := Write(&buf, dir, "claude", &bundle.PluginManifest{Name: "demo"}, false, true)
	if err != nil {
		t.Fatal(err)
	}
	if outcome != DryRun {
		t.Errorf("outcome = %v, want DryRun for --dry-run", outcome)
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
	_, outcome, err := Write(&buf, dir, "claude", &bundle.PluginManifest{Name: "demo", Version: "1.0.0"}, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if outcome != Written {
		t.Fatalf("outcome = %v, want Written for a new file", outcome)
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
	_, outcome, err := Write(&buf, dir, "claude", &bundle.PluginManifest{Name: "demo"}, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if outcome != Skipped {
		t.Errorf("outcome = %v, want Skipped when an existing file is preserved", outcome)
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
	_, outcome, err := Write(&buf, dir, "claude", &bundle.PluginManifest{Name: "fresh"}, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if outcome != Written {
		t.Fatalf("outcome = %v, want Written with --force", outcome)
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
	if _, _, err := Write(&buf, dir, "copilot", &bundle.PluginManifest{Name: "demo"}, false, false); err != nil {
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
	if _, _, err := Write(&buf, dir, "claude", &bundle.PluginManifest{Name: "demo"}, false, false); err != nil {
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
	_, outcome, err := Write(&buf, root, "claude", &bundle.PluginManifest{Name: "demo"}, true, false)
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if outcome != Written {
		t.Fatalf("Write() outcome = %v, want Written (output: %s)", outcome, buf.String())
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

// Mode handling moved to rootfs.RootWriter when the writer stopped using path
// strings (2026-08-13): the permission-preservation and hard-link tests that
// used to live here are now TestRootWriter_WriteFileAtomicKeepsAnExistingFilesMode,
// TestRootWriter_WriteFileAtomicCreatesAWritableFile and
// TestRootWriter_WriteFileAtomicDoesNotWriteThroughAHardLink in
// internal/marketplace/build, alongside the containment tests they share a
// code path with.

// seedPluginJSON puts original at ecosystem's plugin.json under a fresh
// project and returns the project root and the absolute path Write reports.
func seedPluginJSON(t *testing.T, ecosystem string, original []byte) (root, path string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path = filepath.Join(root, filepath.FromSlash(PluginEcosystemPaths[ecosystem]))
	if original == nil {
		return root, path
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}
	return root, path
}

func assertFileBytes(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s changed: got %q, want %q", path, got, want)
	}
}

// Oracle core/plugin_manifest.py:438-445: an existing file without --force is
// skipped before the dry-run branch, so --dry-run previews what pack does.
func TestWrite_DryRun_ExistingFile_NoForce_Skips(t *testing.T) {
	original := []byte("hand-authored content\n")
	root, path := seedPluginJSON(t, "claude", original)

	var buf bytes.Buffer
	gotPath, outcome, err := Write(&buf, root, "claude", &bundle.PluginManifest{Name: "demo"}, false, true)
	if err != nil {
		t.Fatal(err)
	}
	if outcome != Skipped {
		t.Errorf("outcome = %v, want Skipped", outcome)
	}
	if gotPath != path {
		t.Errorf("path = %q, want %q", gotPath, path)
	}
	want := path + " already exists; skipping plugin.json generation. Re-run with --force to overwrite it."
	if !strings.Contains(buf.String(), want) {
		t.Errorf("output = %q, want %q", buf.String(), want)
	}
	if strings.Contains(buf.String(), "Would write") {
		t.Errorf("output = %q, must not promise a write", buf.String())
	}
	assertFileBytes(t, path, original)
}

func TestWrite_DryRun_ExistingFile_Force_PreviewsOverwrite(t *testing.T) {
	original := []byte("stale\n")
	root, path := seedPluginJSON(t, "claude", original)

	var buf bytes.Buffer
	gotPath, outcome, err := Write(&buf, root, "claude", &bundle.PluginManifest{Name: "demo"}, true, true)
	if err != nil {
		t.Fatal(err)
	}
	if outcome != DryRun {
		t.Errorf("outcome = %v, want DryRun", outcome)
	}
	if gotPath != path {
		t.Errorf("path = %q, want %q", gotPath, path)
	}
	want := "Would overwrite plugin manifest at " + path + " with generated manifest from apm.yml (--force)."
	if !strings.Contains(buf.String(), want) {
		t.Errorf("output = %q, want %q", buf.String(), want)
	}
	assertFileBytes(t, path, original)
}

func TestWrite_DryRun_NewFile_PreviewsWriteAndCreatesNothing(t *testing.T) {
	root, path := seedPluginJSON(t, "claude", nil)

	var buf bytes.Buffer
	gotPath, outcome, err := Write(&buf, root, "claude", &bundle.PluginManifest{Name: "demo"}, false, true)
	if err != nil {
		t.Fatal(err)
	}
	if outcome != DryRun {
		t.Errorf("outcome = %v, want DryRun", outcome)
	}
	if gotPath != path {
		t.Errorf("path = %q, want %q", gotPath, path)
	}
	if want := "Would write plugin manifest to " + path; !strings.Contains(buf.String(), want) {
		t.Errorf("output = %q, want %q", buf.String(), want)
	}
	if _, err := os.Stat(filepath.Join(root, ".claude-plugin")); !os.IsNotExist(err) {
		t.Errorf("--dry-run created .claude-plugin/ (stat err = %v)", err)
	}
}

func TestWrite_DryRun_CopilotExistingFile_SkipsWithoutGithubInfoLine(t *testing.T) {
	original := []byte("hand-authored content\n")
	root, path := seedPluginJSON(t, "copilot", original)

	var buf bytes.Buffer
	_, outcome, err := Write(&buf, root, "copilot", &bundle.PluginManifest{Name: "demo"}, false, true)
	if err != nil {
		t.Fatal(err)
	}
	if outcome != Skipped {
		t.Errorf("outcome = %v, want Skipped", outcome)
	}
	if want := path + " already exists; skipping plugin.json generation."; !strings.Contains(buf.String(), want) {
		t.Errorf("output = %q, want %q", buf.String(), want)
	}
	if strings.Contains(buf.String(), "Writing generated plugin manifest under .github/") {
		t.Errorf("output = %q, must not announce a .github/ write", buf.String())
	}
	assertFileBytes(t, path, original)
}

// assertReportsPath checks that the returned path and every output line that
// names a path under root use want, never the symlink's target.
func assertReportsPath(t *testing.T, out, gotPath, root, want, target string) {
	t.Helper()
	if gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if strings.Contains(out, target) {
		t.Errorf("output names the symlink target %q:\n%s", target, out)
	}
	named := false
	for _, line := range strings.Split(out, "\n") {
		if !strings.Contains(line, root) {
			continue
		}
		named = true
		if !strings.Contains(line, want) {
			t.Errorf("line %q does not name %q", line, want)
		}
	}
	if !named {
		t.Errorf("no output line names the manifest:\n%s", out)
	}
}

// Oracle reports project_root / rel_path (core/plugin_manifest.py:432,
// 439-483): the project root resolved, the path below it as written, so a
// symlinked plugin.json is reported under its own name.
func TestWrite_SymlinkedManifest_ReportsUnresolvedPath(t *testing.T) {
	for _, tc := range []struct {
		name          string
		force, dryRun bool
	}{
		{"dry-run", false, true},
		{"dry-run force", true, true},
		{"run", false, false},
		{"run force", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, want := seedPluginJSON(t, "claude", nil)
			target := filepath.Join(root, "custom.json")
			if err := os.WriteFile(target, []byte("{}\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Dir(want), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join("..", "custom.json"), want); err != nil {
				t.Skipf("cannot create a symlink on this host: %v", err)
			}

			var buf bytes.Buffer
			gotPath, _, err := Write(&buf, root, "claude", &bundle.PluginManifest{Name: "demo"}, tc.force, tc.dryRun)
			if err != nil {
				t.Fatal(err)
			}
			assertReportsPath(t, buf.String(), gotPath, root, want, target)
		})
	}
}

func TestWrite_SymlinkedManifestDir_ReportsUnresolvedPath(t *testing.T) {
	root, want := seedPluginJSON(t, "claude", nil)
	real := filepath.Join(root, "realdir")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(real, "plugin.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("realdir", filepath.Join(root, ".claude-plugin")); err != nil {
		t.Skipf("cannot create a symlink on this host: %v", err)
	}

	var buf bytes.Buffer
	gotPath, _, err := Write(&buf, root, "claude", &bundle.PluginManifest{Name: "demo"}, false, true)
	if err != nil {
		t.Fatal(err)
	}
	assertReportsPath(t, buf.String(), gotPath, root, want, real)
}
