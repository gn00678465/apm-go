// Tests for output.go: mkt-054's output-path resolution (default path per
// profile, apm.yml-declared overrides in both YAML forms, CLI overrides,
// and the path-traversal guard) plus the atomic JSON writer.
package build

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/marketplace/authoring"
)

// ── DefaultOutputPath ─────────────────────────────────────────────────────

func TestDefaultOutputPath(t *testing.T) {
	tests := []struct {
		format string
		want   string
		wantOK bool
	}{
		{"claude", filepath.Join(".claude-plugin", "marketplace.json"), true},
		{"codex", filepath.Join(".agents", "plugins", "marketplace.json"), true},
		{"bogus", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.format, func(t *testing.T) {
			got, ok := DefaultOutputPath(tt.format)
			if ok != tt.wantOK || got != tt.want {
				t.Errorf("DefaultOutputPath(%q) = (%q, %v), want (%q, %v)", tt.format, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

// ── ResolveOutputPath: CLI > apm.yml override > profile default ─────────

func TestResolveOutputPath_DefaultsWhenNoOverrides(t *testing.T) {
	got, err := ResolveOutputPath("claude", nil, nil)
	if err != nil {
		t.Fatalf("ResolveOutputPath() error = %v", err)
	}
	want := filepath.Join(".claude-plugin", "marketplace.json")
	if got != want {
		t.Errorf("ResolveOutputPath() = %q, want %q", got, want)
	}
}

func TestResolveOutputPath_ConfigOverrideWinsOverDefault(t *testing.T) {
	configPaths := map[string]string{"claude": "dist/marketplace.json"}
	got, err := ResolveOutputPath("claude", configPaths, nil)
	if err != nil {
		t.Fatalf("ResolveOutputPath() error = %v", err)
	}
	if got != "dist/marketplace.json" {
		t.Errorf("ResolveOutputPath() = %q, want dist/marketplace.json", got)
	}
}

func TestResolveOutputPath_CLIOverrideWinsOverConfigOverride(t *testing.T) {
	configPaths := map[string]string{"claude": "dist/marketplace.json"}
	cliOverrides := map[string]string{"claude": "cli-dist/marketplace.json"}
	got, err := ResolveOutputPath("claude", configPaths, cliOverrides)
	if err != nil {
		t.Fatalf("ResolveOutputPath() error = %v", err)
	}
	if got != "cli-dist/marketplace.json" {
		t.Errorf("ResolveOutputPath() = %q, want cli-dist/marketplace.json", got)
	}
}

func TestResolveOutputPath_UnknownFormat_ReturnsError(t *testing.T) {
	_, err := ResolveOutputPath("bogus", nil, nil)
	if err == nil {
		t.Fatal("expected an error for an unknown output format")
	}
}

// ── LoadOutputPathOverrides: map form + legacy sibling form ──────────────

func writeApmYML(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "apm.yml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadOutputPathOverrides_MapForm(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeApmYML(t, dir, `name: demo
marketplace:
  owner:
    name: Acme
  outputs:
    claude:
      path: dist/claude-marketplace.json
    codex:
      path: dist/codex-marketplace.json
  packages: []
`)

	// Act
	got, err := LoadOutputPathOverrides(dir, authoring.ConfigSourceApmYML)

	// Assert
	if err != nil {
		t.Fatalf("LoadOutputPathOverrides() error = %v", err)
	}
	want := map[string]string{
		"claude": "dist/claude-marketplace.json",
		"codex":  "dist/codex-marketplace.json",
	}
	if len(got) != len(want) {
		t.Fatalf("got = %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("got[%q] = %q, want %q", k, got[k], v)
		}
	}
}

func TestLoadOutputPathOverrides_CompatSiblingForm(t *testing.T) {
	// Arrange: the legacy per-format sub-block form, `marketplace.<fmt>.output`.
	dir := t.TempDir()
	writeApmYML(t, dir, `name: demo
marketplace:
  owner:
    name: Acme
  claude:
    output: legacy/claude-marketplace.json
  packages: []
`)

	// Act
	got, err := LoadOutputPathOverrides(dir, authoring.ConfigSourceApmYML)

	// Assert
	if err != nil {
		t.Fatalf("LoadOutputPathOverrides() error = %v", err)
	}
	if got["claude"] != "legacy/claude-marketplace.json" {
		t.Errorf("got[claude] = %q, want legacy/claude-marketplace.json", got["claude"])
	}
}

func TestLoadOutputPathOverrides_MapFormWinsOverCompatForm(t *testing.T) {
	// Arrange: both forms declare a path for "claude" -- design.md's stated
	// priority is "map 形式優先".
	dir := t.TempDir()
	writeApmYML(t, dir, `name: demo
marketplace:
  owner:
    name: Acme
  claude:
    output: legacy/claude-marketplace.json
  outputs:
    claude:
      path: dist/claude-marketplace.json
  packages: []
`)

	// Act
	got, err := LoadOutputPathOverrides(dir, authoring.ConfigSourceApmYML)

	// Assert
	if err != nil {
		t.Fatalf("LoadOutputPathOverrides() error = %v", err)
	}
	if got["claude"] != "dist/claude-marketplace.json" {
		t.Errorf("got[claude] = %q, want the map form's path to win", got["claude"])
	}
}

func TestLoadOutputPathOverrides_NoOverridesDeclared_ReturnsNil(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeApmYML(t, dir, `name: demo
marketplace:
  owner:
    name: Acme
  outputs:
    claude: {}
  packages: []
`)

	// Act
	got, err := LoadOutputPathOverrides(dir, authoring.ConfigSourceApmYML)

	// Assert
	if err != nil {
		t.Fatalf("LoadOutputPathOverrides() error = %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got = %v, want no overrides", got)
	}
}

func TestLoadOutputPathOverrides_LegacyConfigSource_ReadsMarketplaceYml(t *testing.T) {
	// Arrange: a legacy standalone marketplace.yml holds the block at its
	// own document root (no "marketplace:" key wrapper).
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "marketplace.yml"), []byte(`owner:
  name: Acme
outputs:
  claude:
    path: legacy-root/marketplace.json
packages: []
`), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	got, err := LoadOutputPathOverrides(dir, authoring.ConfigSourceLegacy)

	// Assert
	if err != nil {
		t.Fatalf("LoadOutputPathOverrides() error = %v", err)
	}
	if got["claude"] != "legacy-root/marketplace.json" {
		t.Errorf("got[claude] = %q, want legacy-root/marketplace.json", got["claude"])
	}
}

// ── EnsureWithinRoot: mkt-054's path-traversal guard ──────────────────────

func TestEnsureWithinRoot_RelativePathInsideRoot_Passes(t *testing.T) {
	root := t.TempDir()
	got, err := EnsureWithinRoot(root, filepath.Join("dist", "marketplace.json"))
	if err != nil {
		t.Fatalf("EnsureWithinRoot() error = %v", err)
	}
	wantPrefix, _ := filepath.Abs(root)
	if !strings.HasPrefix(got, wantPrefix) {
		t.Errorf("EnsureWithinRoot() = %q, want it rooted under %q", got, wantPrefix)
	}
}

func TestEnsureWithinRoot_TraversalEscapingRoot_ReturnsError(t *testing.T) {
	root := t.TempDir()
	_, err := EnsureWithinRoot(root, filepath.Join("..", "..", "etc", "passwd"))
	if err == nil {
		t.Fatal("expected an error: path escapes the project root")
	}
}

func TestEnsureWithinRoot_AbsolutePathOutsideRoot_ReturnsError(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	_, err := EnsureWithinRoot(root, filepath.Join(outside, "marketplace.json"))
	if err == nil {
		t.Fatal("expected an error: absolute path outside the project root")
	}
}

// mustSymlink creates a symlink, skipping the test when the host cannot make
// one (unprivileged Windows without Developer Mode).
func mustSymlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symlink creation unsupported/unprivileged on this Windows host: %v", err)
		}
		t.Fatalf("os.Symlink(%q, %q): %v", target, link, err)
	}
}

// TestEnsureWithinRoot_SymlinkedDirEscapingRoot_ReturnsError is the
// 2026-08-12 fix for the gap an external audit found: the guard used to be
// purely lexical (Abs/Clean/Rel), so a DIRECTORY symlink inside the root
// pointing outside it passed containment and every writer downstream followed
// the link out of the project. Upstream has never had this hole -- its
// ensure_path_within calls Path.resolve() and documents the intent verbatim:
// "symlinks are resolved so that a link pointing outside the base is caught
// as well" (v0.28.0:src/apm_cli/utils/path_security.py:98-119).
//
// The victim path here is the real one: .codex-plugin/plugin.json, which
// pluginmanifest.Write resolves through this function before writing.
func TestEnsureWithinRoot_SymlinkedDirEscapingRoot_ReturnsError(t *testing.T) {
	// Arrange: <root>/.codex-plugin -> <outside>
	root := t.TempDir()
	outside := t.TempDir()
	mustSymlink(t, outside, filepath.Join(root, ".codex-plugin"))

	// Act
	got, err := EnsureWithinRoot(root, filepath.Join(".codex-plugin", "plugin.json"))

	// Assert
	if err == nil {
		t.Fatalf("EnsureWithinRoot() = %q, nil -- want an error: the path resolves outside the project root via a directory symlink", got)
	}
}

// TestEnsureWithinRoot_JunctionEscapingRoot_ReturnsError is the Windows half
// of the same hole, and the one that actually matters there: creating a
// SYMLINK on Windows needs privilege (Developer Mode or admin), but creating
// a JUNCTION needs none, so a junction is the escape an unprivileged local
// attacker would use.
//
// It is a separate test because filepath.EvalSymlinks does not resolve
// junctions at all (measured on go1.26.3: it returns the junction path
// unchanged and Lstat reports ModeIrregular, not ModeSymlink) -- an
// EvalSymlinks-based fix passes the symlink test above while leaving this one
// red, which is exactly what happened during this fix's first attempt.
func TestEnsureWithinRoot_JunctionEscapingRoot_ReturnsError(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("junctions are a Windows-only reparse point")
	}
	// Arrange: <root>/.codex-plugin is a junction to <outside>
	root := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(root, ".codex-plugin")
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, outside).CombinedOutput(); err != nil {
		t.Skipf("cannot create a junction on this host: %v (%s)", err, out)
	}

	// Act
	got, err := EnsureWithinRoot(root, filepath.Join(".codex-plugin", "plugin.json"))

	// Assert
	if err == nil {
		t.Fatalf("EnsureWithinRoot() = %q, nil -- want an error: the path resolves outside the project root via a junction", got)
	}
}

// TestEnsureWithinRoot_MultiSegmentRelativeLinkEscaping_ReturnsError is the
// escape an external audit (2026-08-12) found in this guard's FIRST fix: when
// a link's target has more than one segment, substituting the whole target at
// once and Lstat-ing the result skips the intermediate components -- and an
// intermediate component can itself be a link out of the root.
//
//	<root>/a    -> b/leaf   (relative, two segments)
//	<root>/b    -> <outside>
//	<outside>/leaf          (a plain directory)
//
// Lstat(<root>/b/leaf) reports a plain directory, because the OS silently
// followed <root>/b. The lexical string "<root>/b/leaf" then looks contained
// while the real location is <outside>/leaf. Measured before the fix:
// symlink=false irregular=false at that path.
func TestEnsureWithinRoot_MultiSegmentRelativeLinkEscaping_ReturnsError(t *testing.T) {
	// Arrange
	base := t.TempDir()
	root := filepath.Join(base, "R")
	outside := filepath.Join(base, "O")
	if err := os.MkdirAll(filepath.Join(outside, "leaf"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	mustSymlink(t, filepath.Join("b", "leaf"), filepath.Join(root, "a"))
	mustSymlink(t, outside, filepath.Join(root, "b"))

	// Act
	got, err := EnsureWithinRoot(root, filepath.Join("a", "out.json"))

	// Assert
	if err == nil {
		t.Fatalf("EnsureWithinRoot() = %q, nil -- want an error: a/out.json resolves into %s via the intermediate link b", got, outside)
	}
}

// TestEnsureWithinRoot_VolumeRootedLinkTargetEscaping_ReturnsError covers the
// Windows link target that looks relative to Go but is not: a target starting
// with a separator and carrying no volume ("\Users\...") is rooted at the
// LINK's volume, yet filepath.IsAbs reports false for it (measured:
// IsAbs(`\outside`) == false, VolumeName == ""). Treating it as relative to
// the link's parent directory both mis-resolves the path and hides an escape.
func TestEnsureWithinRoot_VolumeRootedLinkTargetEscaping_ReturnsError(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("volume-rooted-without-volume targets are a Windows concept")
	}
	// Arrange: <root>/.codex-plugin -> \Users\...\outside  (no "C:" prefix)
	base := t.TempDir()
	root := filepath.Join(base, "R")
	outside := filepath.Join(base, "O")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	rooted := strings.TrimPrefix(outside, filepath.VolumeName(outside))
	if filepath.IsAbs(rooted) {
		t.Fatalf("fixture invalid: %q is still absolute, the case under test needs a volume-less rooted target", rooted)
	}
	mustSymlink(t, rooted, filepath.Join(root, ".codex-plugin"))

	// Act
	got, err := EnsureWithinRoot(root, filepath.Join(".codex-plugin", "plugin.json"))

	// Assert
	if err == nil {
		t.Fatalf("EnsureWithinRoot() = %q, nil -- want an error: the volume-rooted target resolves to %s", got, outside)
	}
}

// TestEnsureWithinRoot_LinkCycle_ReturnsError proves the hop limit is reachable
// and fails closed rather than hanging. The error CAUSE is asserted, not just
// its presence: a cycle that happened to fail for some unrelated reason would
// otherwise satisfy this test (external audit, 2026-08-12).
func TestEnsureWithinRoot_LinkCycle_ReturnsError(t *testing.T) {
	// Arrange: <root>/x -> <root>/y -> <root>/x
	root := t.TempDir()
	mustSymlink(t, filepath.Join(root, "y"), filepath.Join(root, "x"))
	mustSymlink(t, filepath.Join(root, "x"), filepath.Join(root, "y"))

	// Act
	_, err := EnsureWithinRoot(root, filepath.Join("x", "out.json"))

	// Assert
	if err == nil {
		t.Fatal("EnsureWithinRoot() = nil error, want a failure for a symlink cycle")
	}
	if !strings.Contains(err.Error(), "too many symbolic links") {
		t.Errorf("error = %v, want the hop-limit failure (a cycle must be stopped by the hop counter, not by something incidental)", err)
	}
}

// TestLinkTargetStart is a filesystem-free unit test for the link-target
// classification, so the platform rules stay covered even on a host where
// every symlink/junction fixture above skips for lack of privilege -- the
// "all escape tests skipped, suite still green" hole an external audit
// flagged (2026-08-12).
func TestLinkTargetStart(t *testing.T) {
	linkDir := filepath.Join(string(filepath.Separator)+"vol", "proj", "sub")
	if runtime.GOOS == "windows" {
		linkDir = `C:\proj\sub`
	}

	t.Run("relative target starts at the link's directory", func(t *testing.T) {
		base, parts, err := linkTargetStart(filepath.Join("a", "b"), linkDir)
		if err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		if base != linkDir {
			t.Errorf("base = %q, want %q", base, linkDir)
		}
		if strings.Join(parts, "/") != "a/b" {
			t.Errorf("parts = %v, want [a b]", parts)
		}
	})

	t.Run("parent-climbing target keeps .. for the caller", func(t *testing.T) {
		_, parts, err := linkTargetStart(filepath.Join("..", "O"), linkDir)
		if err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		if strings.Join(parts, "/") != "../O" {
			t.Errorf("parts = %v, want [.. O] (dropping .. here would hide an escape)", parts)
		}
	})

	if runtime.GOOS != "windows" {
		return
	}

	t.Run("volume-rooted target starts at the link's volume", func(t *testing.T) {
		base, parts, err := linkTargetStart(`\outside`, linkDir)
		if err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		if base != `C:\` {
			t.Errorf("base = %q, want %q (a leading separator with no volume is rooted on the link's volume, not relative to it)", base, `C:\`)
		}
		if strings.Join(parts, "/") != "outside" {
			t.Errorf("parts = %v, want [outside]", parts)
		}
	})

	t.Run("absolute target starts at its own volume", func(t *testing.T) {
		base, parts, err := linkTargetStart(`D:\x\y`, linkDir)
		if err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		if base != `D:\` {
			t.Errorf("base = %q, want %q", base, `D:\`)
		}
		if strings.Join(parts, "/") != "x/y" {
			t.Errorf("parts = %v, want [x y]", parts)
		}
	})

	t.Run("drive-relative target is refused", func(t *testing.T) {
		if _, _, err := linkTargetStart(`C:foo`, linkDir); err == nil {
			t.Error("linkTargetStart(`C:foo`) = nil error, want a refusal: it resolves against that drive's per-process current directory, which this guard cannot observe")
		}
	})
}

// TestEnsureWithinRoot_DriveRelativeLinkTarget_ReturnsError is the end-to-end
// half of the drive-relative case: a link whose stored target is "C:foo"
// must fail the guard rather than being resolved to something arbitrary.
func TestEnsureWithinRoot_DriveRelativeLinkTarget_ReturnsError(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("drive-relative paths are a Windows concept")
	}
	// Arrange
	root := t.TempDir()
	mustSymlink(t, `C:foo`, filepath.Join(root, ".codex-plugin"))

	// Act
	got, err := EnsureWithinRoot(root, filepath.Join(".codex-plugin", "plugin.json"))

	// Assert
	if err == nil {
		t.Fatalf("EnsureWithinRoot() = %q, nil -- want a refusal for a drive-relative link target", got)
	}
}

// TestEnsureWithinRoot_RelativeParentLinkEscaping_ReturnsError covers a link
// target that climbs out with "..", which the lexical Clean at the top of
// EnsureWithinRoot never sees because it lives inside the link, not the path.
func TestEnsureWithinRoot_RelativeParentLinkEscaping_ReturnsError(t *testing.T) {
	// Arrange: <base>/R/.codex-plugin -> ../O
	base := t.TempDir()
	root := filepath.Join(base, "R")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(base, "O"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustSymlink(t, filepath.Join("..", "O"), filepath.Join(root, ".codex-plugin"))

	// Act
	got, err := EnsureWithinRoot(root, filepath.Join(".codex-plugin", "plugin.json"))

	// Assert
	if err == nil {
		t.Fatalf("EnsureWithinRoot() = %q, nil -- want an error: the link target climbs out of the root with ..", got)
	}
}

// TestEnsureWithinRoot_SymlinkedDirInsideRoot_Passes is the other half: a
// symlink that stays inside the root must still be accepted, so the fix
// rejects escapes rather than rejecting symlinks.
func TestEnsureWithinRoot_SymlinkedDirInsideRoot_Passes(t *testing.T) {
	// Arrange: <root>/link -> <root>/real
	root := t.TempDir()
	real := filepath.Join(root, "real")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	mustSymlink(t, real, filepath.Join(root, "link"))

	// Act
	_, err := EnsureWithinRoot(root, filepath.Join("link", "marketplace.json"))

	// Assert
	if err != nil {
		t.Errorf("EnsureWithinRoot() error = %v, want nil: a symlink that stays inside the root is legitimate", err)
	}
}

// TestEnsureWithinRoot_ReturnsResolvedPath locks the check-A-write-B fix: the
// value handed back is the location that was actually validated, so a caller
// writing to it cannot be redirected by swapping a link afterwards.
func TestEnsureWithinRoot_ReturnsResolvedPath(t *testing.T) {
	// Arrange: <root>/link -> <root>/real
	root := t.TempDir()
	real := filepath.Join(root, "real")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	mustSymlink(t, real, filepath.Join(root, "link"))

	// Act
	got, err := EnsureWithinRoot(root, filepath.Join("link", "out.json"))
	if err != nil {
		t.Fatalf("EnsureWithinRoot() error = %v", err)
	}

	// Assert
	want := filepath.Join(real, "out.json")
	if got != want {
		t.Errorf("EnsureWithinRoot() = %q, want the resolved path %q (returning the lexical form re-traverses the link on every use)", got, want)
	}
}

// TestEnsureWithinRoot_SymlinkedRootItself_Passes covers the case that makes
// naive "resolve only the path" implementations reject valid projects: the
// PROJECT ROOT is itself reached through a symlink (macOS /tmp -> /private/tmp
// is the everyday example, and t.TempDir() returns exactly such a path there).
// Resolving the path but not the root would make every write look like an
// escape.
func TestEnsureWithinRoot_SymlinkedRootItself_Passes(t *testing.T) {
	// Arrange: <parent>/rootlink -> <parent>/realroot, used AS the root
	parent := t.TempDir()
	realRoot := filepath.Join(parent, "realroot")
	if err := os.MkdirAll(realRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	rootLink := filepath.Join(parent, "rootlink")
	mustSymlink(t, realRoot, rootLink)

	// Act
	_, err := EnsureWithinRoot(rootLink, filepath.Join("dist", "marketplace.json"))

	// Assert
	if err != nil {
		t.Errorf("EnsureWithinRoot() error = %v, want nil: a symlinked project root is legitimate", err)
	}
}

// ── WriteOutput: 2-space indent + trailing newline, atomic write ────────

func TestWriteOutput_WritesTwoSpaceIndentedJSONWithTrailingNewline(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "marketplace.json")
	doc := map[string]any{"name": "demo", "plugins": []any{}}

	// Act
	if err := WriteOutput(path, doc); err != nil {
		t.Fatalf("WriteOutput() error = %v", err)
	}

	// Assert
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !strings.HasSuffix(string(data), "\n") {
		t.Errorf("output does not end with a trailing newline: %q", data)
	}
	if !strings.Contains(string(data), "\n  \"name\": \"demo\"") {
		t.Errorf("output is not 2-space indented: %s", data)
	}
	var roundTrip map[string]any
	if err := json.Unmarshal(data, &roundTrip); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
}

func TestWriteOutput_CreatesMissingParentDirectories(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a", "b", "c", "marketplace.json")
	if err := WriteOutput(path, map[string]any{"name": "demo"}); err != nil {
		t.Fatalf("WriteOutput() error = %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected file to exist: %v", err)
	}
}

func TestWriteOutput_OverwritesExistingFileAtomically(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "marketplace.json")
	if err := os.WriteFile(path, []byte("stale content"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := WriteOutput(path, map[string]any{"name": "fresh"}); err != nil {
		t.Fatalf("WriteOutput() error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "stale") {
		t.Errorf("expected stale content to be replaced, got: %s", data)
	}
	if !strings.Contains(string(data), "fresh") {
		t.Errorf("expected fresh content to be written, got: %s", data)
	}
}

// TestKnownOutputFormats_IsDerivedFromTheDefaultPathTable covers what the
// derivation cannot: defaultOutputPaths is now the single source for both
// KnownOutputFormats and DefaultOutputPath, so the two can no longer drift
// apart -- an external audit (2026-08-13) showed the previous version of
// this test could not actually detect the reverse direction, because a
// `case` added to a switch is not enumerable and its hand-written mirror
// list would simply go stale with it.
//
// What is left to pin is membership. The profile set is user-facing:
// --marketplace validates against it and pack writes one file per configured
// profile, so adding or removing one is a CLI contract change and should
// surface as a failing test rather than only in a diff.
func TestKnownOutputFormats_IsDerivedFromTheDefaultPathTable(t *testing.T) {
	for format := range KnownOutputFormats {
		if _, ok := DefaultOutputPath(format); !ok {
			t.Errorf("KnownOutputFormats accepts %q, but DefaultOutputPath has no path for it", format)
		}
	}
	want := map[string]bool{"claude": true, "codex": true}
	if !reflect.DeepEqual(KnownOutputFormats, want) {
		t.Errorf("KnownOutputFormats = %v, want %v; adding or removing an output profile changes what --marketplace accepts and what pack writes", KnownOutputFormats, want)
	}
}
