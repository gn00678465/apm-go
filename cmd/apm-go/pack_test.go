// Tests for pack.go: `apm pack`'s CLI wiring (mkt-054/055) -- flag surface,
// output-location correctness (never the repo root), --marketplace-path/
// --marketplace filtering, --dry-run, and exit-code behavior (0 success, 1
// for every marketplace config/build error; 2/3/4 are out of this
// sub-task's scope and must not be reachable).
package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/marketplace/build"
)

// runPackCmd executes `pack <args...>` against a fresh packCmd() tree,
// capturing combined stdout+stderr the same way runMarketplaceCmd does.
func runPackCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	c := packCmd()
	var buf bytes.Buffer
	c.SetOut(&buf)
	c.SetErr(&buf)
	c.SetArgs(args)
	err := c.Execute()
	return buf.String(), err
}

func writePackApmYML(t *testing.T, content string) {
	t.Helper()
	if err := os.WriteFile("apm.yml", []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func packRevParse(t *testing.T, dir, ref string) string {
	t.Helper()
	return gitCmd(t, dir, "rev-list", "-n", "1", ref)
}

// ── flags wired / deliberately absent ────────────────────────────────────

func TestPackCmd_FlagsWired(t *testing.T) {
	cmd := packCmd()
	for _, name := range []string{"offline", "include-prerelease", "dry-run", "marketplace", "marketplace-path", "output", "target", "archive", "archive-format", "legacy-skill-paths", "check-versions", "check-clean"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("pack is missing --%s", name)
		}
	}
	if cmd.Flags().ShorthandLookup("m") == nil {
		t.Error("pack is missing the -m shorthand for --marketplace")
	}
	if cmd.Flags().ShorthandLookup("v") == nil {
		t.Error("pack is missing the -v shorthand for --verbose")
	}
	if cmd.Flags().ShorthandLookup("o") == nil {
		t.Error("pack is missing the -o shorthand for --output")
	}
	if cmd.Flags().ShorthandLookup("t") == nil {
		t.Error("pack is missing the -t shorthand for --target")
	}
}

// TestPackCmd_DoesNotExposeDeferredFlags: check-versions/check-clean were
// removed from this list in ticket 17 phase 4 (now implemented, see
// TestPackCmd_FlagsWired) -- allow-head remains deferred.
func TestPackCmd_DoesNotExposeDeferredFlags(t *testing.T) {
	cmd := packCmd()
	for _, name := range []string{"allow-head"} {
		if cmd.Flags().Lookup(name) != nil {
			t.Errorf("pack must not expose --%s (deferred to a later sub-task, see design.md)", name)
		}
	}
}

// ── no producer applies -> exit 1, "nothing to pack" ─────────────────────
//
// Phase 2-5 (design.md Gate 1 disposition) replaced the P0 quick-win's
// exit-0 "nothing to do" info with Python's own BuildOrchestrator.run
// BuildError semantics: apm-go now implements all three producers
// (Bundle/Marketplace/PluginManifest), so a project with none of
// dependencies:/marketplace:/target:{claude,copilot} genuinely has nothing
// any producer can act on, and that is a user-facing failure (exit 1), not
// a silent no-op.

// wantNothingToPack duplicates pack.ErrNothingToPack's text as an
// independent string literal -- not a reference to the production
// identifier -- so a wording change in internal/pack breaks this test with
// a red diff instead of both sides silently drifting together (same
// verbatim-lock pattern as errNoDeployTarget's literal check in
// install_test.go).
const wantNothingToPack = "apm.yml has neither 'dependencies:' nor 'marketplace:' block, and 'target:' does not include 'claude' or 'copilot'. Nothing to pack. Add dependencies via 'apm-go install <pkg>', configure a 'marketplace:' block, or set 'target:' to include 'claude' or 'copilot'."

func TestPackCmd_NoMarketplaceBlock_ExitsOne(t *testing.T) {
	// Arrange
	chdirTemp(t)
	writePackApmYML(t, "name: demo\nversion: 1.0.0\n")

	// Act
	out, err := runPackCmd(t)

	// Assert
	if err == nil {
		t.Fatalf("expected an error for a manifest with no producer inputs (output: %s)", out)
	}
	if exitCodeOf(err) != 1 {
		t.Errorf("exitCodeOf(err) = %d, want 1", exitCodeOf(err))
	}
	if err.Error() != wantNothingToPack {
		t.Errorf("err = %q, want %q", err.Error(), wantNothingToPack)
	}
}

func TestPackCmd_ExplicitNullMarketplaceKey_ExitsOne(t *testing.T) {
	// Arrange: a bare "marketplace:" key with nothing after it is mkt-047's
	// "_has_marketplace_block" null case -- not really present.
	chdirTemp(t)
	writePackApmYML(t, "name: demo\nversion: 1.0.0\nmarketplace:\n")

	// Act
	_, err := runPackCmd(t)

	// Assert
	if err == nil {
		t.Fatal("expected an error for a null marketplace: key")
	}
	if exitCodeOf(err) != 1 {
		t.Errorf("exitCodeOf(err) = %d, want 1", exitCodeOf(err))
	}
}

func TestPackCmd_NoApmYMLAtAll_ExitsOne(t *testing.T) {
	// Arrange: not even an apm.yml exists yet.
	chdirTemp(t)

	// Act
	_, err := runPackCmd(t)

	// Assert
	if err == nil {
		t.Fatal("expected an error when no apm.yml exists at all")
	}
	if exitCodeOf(err) != 1 {
		t.Errorf("exitCodeOf(err) = %d, want 1", exitCodeOf(err))
	}
}

// ── pack --help documents all three producers ────────────────────────────

// TestPackCmd_HelpMatchesOraclePackHelpDocstring is ticket 17 phase 1's
// Long-text regression: packCmd's Long field is now the Oracle's own
// _PACK_HELP docstring (commands/pack.py:25-66) verbatim ("apm" ->
// "apm-go"), not apm-go's previous self-authored three-bullet summary.
// Locks the first description paragraph (the exact string help_semantic's
// own parseHelpDescriptionParagraph extracts and compares) plus every
// section header and the exit-code table, so a future edit can't silently
// drift back toward paraphrasing.
func TestPackCmd_HelpMatchesOraclePackHelpDocstring(t *testing.T) {
	out, err := runPackCmd(t, "--help")
	if err != nil {
		t.Fatalf("pack --help returned error: %v", err)
	}
	if !strings.Contains(out, "Pack distributable artifacts from your APM project.") {
		t.Errorf("pack --help output missing the Oracle's first description paragraph verbatim:\n%s", out)
	}
	for _, line := range []string{
		"dependencies: block  ->  bundle (directory or archive; see --archive and --archive-format)",
		"marketplace: block   ->  selected marketplace artifacts",
		"target: / targets:   ->  ecosystem-specific plugin.json (claude/copilot)",
		"both blocks present  ->  bundle plus selected marketplace artifacts",
		"The lockfile (apm.lock.yaml) pins bundle contents.",
		"Examples:",
		"apm-go pack --format apm -o ./dist       # Legacy APM bundle layout",
		"Exit codes:",
		"0  Success",
		"1  Build or runtime error",
		"2  Manifest schema validation error",
		"3  Version alignment check failed (--check-versions)",
		"4  Marketplace working-tree drift detected (--check-clean)",
	} {
		if !strings.Contains(out, line) {
			t.Errorf("pack --help output missing %q:\n%s", line, out)
		}
	}
}

// ── Gate 2: dependencies:/target: without marketplace: actually build ────
//
// Phase 2-5 upgraded BundleProducer/PluginManifestProducer from P0's
// warn-only stubs to full producers -- dependencies: now really builds a
// bundle under ./build/, and target: claude/copilot now really writes
// plugin.json, matching Python's oracle instead of deferring to a warning.

func TestRunPack_DependenciesOnly_BuildsRealBundle(t *testing.T) {
	// A remote dependencies.apm entry with no apm.lock.yaml present is
	// enough to trigger BundleProducer (hasDeps only looks at
	// ParsedDeps -- it does not require the dependency to actually be
	// resolved/materialized); with no lockfile, BundleProducer's
	// dependency-collection loop is simply empty and the bundle is built
	// purely from the project's own local .apm/ content.
	dir := chdirTemp(t)
	if err := os.MkdirAll(filepath.Join(dir, ".apm", "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".apm", "agents", "foo.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	writePackApmYML(t, "name: demo\nversion: 1.0.0\ndependencies:\n  apm:\n    - acme/tool\n")

	out, err := runPackCmd(t)
	if err != nil {
		t.Fatalf("pack returned error: %v (output: %s)", err, out)
	}
	if !strings.Contains(out, "Packed") {
		t.Errorf("output = %q, want a real bundle-built confirmation", out)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "build", "demo-1.0.0", "plugin.json")); statErr != nil {
		t.Errorf("expected a real bundle at build/demo-1.0.0/plugin.json: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "build", "demo-1.0.0", "agents", "foo.md")); statErr != nil {
		t.Errorf("expected local .apm/agents content bundled: %v", statErr)
	}
}

// ── --output/-o (ticket 17 phase 1) ─────────────────────────────────────

// TestPackCmd_OutputFlag_WritesToCustomDirectory proves --output/-o is
// actually wired through to the bundle producer, not just parsed and
// dropped.
func TestPackCmd_OutputFlag_WritesToCustomDirectory(t *testing.T) {
	dir := chdirTemp(t)
	if err := os.MkdirAll(filepath.Join(dir, ".apm", "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".apm", "agents", "foo.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	writePackApmYML(t, "name: demo\nversion: 1.0.0\ndependencies:\n  apm:\n    - acme/tool\n")

	out, err := runPackCmd(t, "--output", "./custom-out")
	if err != nil {
		t.Fatalf("pack --output returned error: %v (output: %s)", err, out)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "custom-out", "demo-1.0.0", "plugin.json")); statErr != nil {
		t.Errorf("expected the bundle under custom-out/, not build/: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "build")); !os.IsNotExist(statErr) {
		t.Errorf("build/ should not exist when --output overrides it (stat err: %v)", statErr)
	}
}

// TestPackCmd_OutputFlag_DefaultsToBuildWhenNotGiven locks the pre-ticket-17
// default: omitting --output must keep writing under ./build, unchanged.
func TestPackCmd_OutputFlag_DefaultsToBuildWhenNotGiven(t *testing.T) {
	dir := chdirTemp(t)
	if err := os.MkdirAll(filepath.Join(dir, ".apm", "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".apm", "agents", "foo.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	writePackApmYML(t, "name: demo\nversion: 1.0.0\ndependencies:\n  apm:\n    - acme/tool\n")

	out, err := runPackCmd(t)
	if err != nil {
		t.Fatalf("pack returned error: %v (output: %s)", err, out)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "build", "demo-1.0.0", "plugin.json")); statErr != nil {
		t.Errorf("expected the default build/ location unchanged: %v", statErr)
	}
}

// TestPackCmd_OutputFlag_RejectsPathEscapingProjectRoot is the AC-required
// security regression: --output is a DELIBERATE apm-go-only hardening
// beyond the Oracle (which places no restriction on -o at all,
// commands/pack.py:206-211's bare click.Path()) -- both a relative ".."
// climb and an absolute path outside the project root must be rejected,
// reusing the same build.EnsureWithinRoot containment helper this producer
// already applies one level down (OutputDir/bundleRel).
func TestPackCmd_OutputFlag_RejectsPathEscapingProjectRoot(t *testing.T) {
	dir := chdirTemp(t)
	writePackApmYML(t, "name: demo\nversion: 1.0.0\ndependencies:\n  apm:\n    - acme/tool\n")

	tests := []struct {
		name   string
		output string
	}{
		{"relative traversal", "../outside"},
		{"absolute path outside root", filepath.Join(filepath.Dir(dir), "absolute-escape")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := runPackCmd(t, "--output", tt.output)
			if err == nil {
				t.Fatalf("pack --output %q succeeded, want a rejection (output: %s)", tt.output, out)
			}
			if !strings.Contains(err.Error(), "escapes the project root") {
				t.Errorf("error = %v, want it to name the containment violation", err)
			}
		})
	}
}

// ── --target/-t (ticket 17 phase 1) ─────────────────────────────────────

// TestPackCmd_TargetFlag_RecordsInEmbeddedLockfile proves --target/-t is
// wired through to the embedded apm.lock.yaml's pack.target metadata field
// (internal/pack/bundle/lockfile_pack.go's NewPackMetadata) -- a real
// apm.lock.yaml must already exist on disk for embedPackLockfile to run at
// all (bundle/producer.go:220's `if opts.Lockfile != nil` guard).
func TestPackCmd_TargetFlag_RecordsInEmbeddedLockfile(t *testing.T) {
	dir := chdirTemp(t)
	if err := os.MkdirAll(filepath.Join(dir, ".apm", "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".apm", "agents", "foo.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	writePackApmYML(t, "name: demo\nversion: 1.0.0\ndependencies:\n  apm:\n    - acme/tool\n")
	if err := os.WriteFile(filepath.Join(dir, "apm.lock.yaml"), []byte("lockfile_version: \"1\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := runPackCmd(t, "--target", "claude")
	if err != nil {
		t.Fatalf("pack --target returned error: %v (output: %s)", err, out)
	}
	data, rerr := os.ReadFile(filepath.Join(dir, "build", "demo-1.0.0", "apm.lock.yaml"))
	if rerr != nil {
		t.Fatalf("expected an embedded apm.lock.yaml: %v", rerr)
	}
	if !strings.Contains(string(data), "target: claude") {
		t.Errorf("embedded apm.lock.yaml = %q, want pack.target: claude", string(data))
	}
}

// TestPackCmd_TargetFlag_DefaultsToAllWhenNotGiven locks the pre-ticket-17
// default: omitting --target must keep recording pack.target: all,
// unchanged -- apm-go does not replicate the Oracle's detect_target()
// auto-fill (commands/pack.py:361-368) when --target is absent.
func TestPackCmd_TargetFlag_DefaultsToAllWhenNotGiven(t *testing.T) {
	dir := chdirTemp(t)
	if err := os.MkdirAll(filepath.Join(dir, ".apm", "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".apm", "agents", "foo.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	writePackApmYML(t, "name: demo\nversion: 1.0.0\ndependencies:\n  apm:\n    - acme/tool\n")
	if err := os.WriteFile(filepath.Join(dir, "apm.lock.yaml"), []byte("lockfile_version: \"1\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := runPackCmd(t)
	if err != nil {
		t.Fatalf("pack returned error: %v (output: %s)", err, out)
	}
	data, rerr := os.ReadFile(filepath.Join(dir, "build", "demo-1.0.0", "apm.lock.yaml"))
	if rerr != nil {
		t.Fatalf("expected an embedded apm.lock.yaml: %v", rerr)
	}
	if !strings.Contains(string(data), "target: all") {
		t.Errorf("embedded apm.lock.yaml = %q, want the unchanged default pack.target: all", string(data))
	}
}

// ── --archive/--archive-format (ticket 17 phase 2) ──────────────────────

// TestPackCmd_HelpMatchesOracleArchiveFlagsText locks --archive/
// --archive-format's --help description text to the Oracle's own wording
// (commands/pack.py:184-205), including the Choice metavar and the baked-in
// "  [default: zip]" annotation (two leading spaces, Click's own
// show_default=True rendering).
func TestPackCmd_HelpMatchesOracleArchiveFlagsText(t *testing.T) {
	out, err := runPackCmd(t, "--help")
	if err != nil {
		t.Fatalf("pack --help returned error: %v", err)
	}
	for _, want := range []string{
		"--archive",
		"Produce a .zip archive instead of a directory (previous default: .tar.gz; use --archive-format tar.gz for legacy CI pipelines).",
		"--archive-format [zip|tar.gz]",
		"Archive format when --archive is set. 'zip' (default) is Claude Code and plugin-host compatible and matches apm publish output. 'tar.gz' is typically smaller for text-heavy bundles and preserves the previous default for CI pipelines that rely on it.  [default: zip]",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("pack --help output missing %q:\n%s", want, out)
		}
	}
}

// archiveFixtureApmYML/writeArchiveFixture set up a minimal project whose
// pack --archive run has something real to bundle plus an on-disk
// apm.lock.yaml (needed for embedPackLockfile to run at all).
func writeArchiveFixture(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, ".apm", "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".apm", "agents", "foo.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	writePackApmYML(t, "name: demo\nversion: 1.0.0\ndependencies:\n  apm:\n    - acme/tool\n")
	if err := os.WriteFile(filepath.Join(dir, "apm.lock.yaml"), []byte("lockfile_version: \"1\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestPackCmd_ArchiveFlag_ProducesRealZipAndRemovesDirectory proves --archive
// actually writes a real .zip (not just accepting the flag), with the
// Oracle's own naming/prefix convention (projected_archive_path: "<bundle
// name>.zip" under the output directory; write_zip_archive's
// f"{bundle_dir.name}/{...}" entry-name prefix) and removes the intermediate
// directory, mirroring export_plugin_bundle's real-run sequence.
func TestPackCmd_ArchiveFlag_ProducesRealZipAndRemovesDirectory(t *testing.T) {
	dir := chdirTemp(t)
	writeArchiveFixture(t, dir)

	out, err := runPackCmd(t, "--archive")
	if err != nil {
		t.Fatalf("pack --archive returned error: %v (output: %s)", err, out)
	}

	archivePath := filepath.Join(dir, "build", "demo-1.0.0.zip")
	if !strings.Contains(out, "demo-1.0.0.zip") {
		t.Errorf("output = %q, want it to report the archive path", out)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "build", "demo-1.0.0")); !os.IsNotExist(statErr) {
		t.Errorf("intermediate bundle directory should be removed after archiving (stat err: %v)", statErr)
	}

	zr, zerr := zip.OpenReader(archivePath)
	if zerr != nil {
		t.Fatalf("open produced zip: %v", zerr)
	}
	defer zr.Close()

	names := make(map[string]bool, len(zr.File))
	for _, f := range zr.File {
		names[f.Name] = true
	}
	for _, want := range []string{"demo-1.0.0/plugin.json", "demo-1.0.0/agents/foo.md", "demo-1.0.0/apm.lock.yaml"} {
		if !names[want] {
			t.Errorf("zip entries = %v, want %q present", names, want)
		}
	}
}

// TestPackCmd_ArchiveFormatFlag_TarGz proves --archive-format tar.gz
// switches the container format and suffix.
func TestPackCmd_ArchiveFormatFlag_TarGz(t *testing.T) {
	dir := chdirTemp(t)
	writeArchiveFixture(t, dir)

	out, err := runPackCmd(t, "--archive", "--archive-format", "tar.gz")
	if err != nil {
		t.Fatalf("pack --archive --archive-format tar.gz returned error: %v (output: %s)", err, out)
	}

	archivePath := filepath.Join(dir, "build", "demo-1.0.0.tar.gz")
	f, oerr := os.Open(archivePath)
	if oerr != nil {
		t.Fatalf("open produced tar.gz: %v", oerr)
	}
	defer f.Close()
	gz, gerr := gzip.NewReader(f)
	if gerr != nil {
		t.Fatalf("gzip.NewReader: %v", gerr)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	names := map[string]bool{}
	for {
		hdr, terr := tr.Next()
		if terr == io.EOF {
			break
		}
		if terr != nil {
			t.Fatalf("tar read: %v", terr)
		}
		names[hdr.Name] = true
	}
	if !names["demo-1.0.0/plugin.json"] {
		t.Errorf("tar entries = %v, want demo-1.0.0/plugin.json present", names)
	}
}

// TestPackCmd_ArchiveFlag_DryRun_WritesNothing proves dry-run reports the
// PROJECTED archive path without writing anything at all, mirroring
// export_plugin_bundle's dry-run branch.
func TestPackCmd_ArchiveFlag_DryRun_WritesNothing(t *testing.T) {
	dir := chdirTemp(t)
	writeArchiveFixture(t, dir)

	out, err := runPackCmd(t, "--archive", "--dry-run")
	if err != nil {
		t.Fatalf("pack --archive --dry-run returned error: %v (output: %s)", err, out)
	}
	if !strings.Contains(out, "demo-1.0.0.zip") {
		t.Errorf("dry-run output = %q, want the projected archive path", out)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "build")); !os.IsNotExist(statErr) {
		t.Errorf("--dry-run must not write anything, but build/ exists (stat err: %v)", statErr)
	}
}

// TestPackCmd_ArchiveFormat_WithoutArchive_Errors mirrors pack.py:329-337's
// UsageError, exit 2, with the Usage/Try-help preamble -- confirmed live
// against the pinned Oracle (ctx.get_parameter_source check: only fires when
// --archive-format was EXPLICITLY given on the command line).
func TestPackCmd_ArchiveFormat_WithoutArchive_Errors(t *testing.T) {
	dir := chdirTemp(t)
	writeArchiveFixture(t, dir)

	out, err := runPackCmd(t, "--archive-format", "tar.gz")
	if err == nil {
		t.Fatalf("expected an error (output: %s)", out)
	}
	if exitCodeOf(err) != 2 {
		t.Errorf("exitCodeOf(err) = %d, want 2", exitCodeOf(err))
	}
	want := "--archive-format has no effect without --archive; add --archive to produce a .tar.gz archive."
	if err.Error() != want {
		t.Errorf("err = %q, want %q", err.Error(), want)
	}
}

// TestPackCmd_ArchiveFormat_InvalidChoice_Errors mirrors Click's Choice
// validation error text, verified live against the pinned Oracle:
// "Invalid value for '--archive-format': 'bogus' is not one of 'zip', 'tar.gz'."
func TestPackCmd_ArchiveFormat_InvalidChoice_Errors(t *testing.T) {
	dir := chdirTemp(t)
	writeArchiveFixture(t, dir)

	out, err := runPackCmd(t, "--archive", "--archive-format", "bogus")
	if err == nil {
		t.Fatalf("expected an error (output: %s)", out)
	}
	if exitCodeOf(err) != 2 {
		t.Errorf("exitCodeOf(err) = %d, want 2", exitCodeOf(err))
	}
	want := "Invalid value for '--archive-format': 'bogus' is not one of 'zip', 'tar.gz'."
	if err.Error() != want {
		t.Errorf("err = %q, want %q", err.Error(), want)
	}
}

// TestPackCmd_ArchiveFormat_CaseSensitive proves --archive-format's Choice
// is case-SENSITIVE, unlike --format's case-insensitive Choice -- verified
// live: `--archive-format ZIP` (uppercase) is rejected on the pinned Oracle.
func TestPackCmd_ArchiveFormat_CaseSensitive(t *testing.T) {
	dir := chdirTemp(t)
	writeArchiveFixture(t, dir)

	out, err := runPackCmd(t, "--archive", "--archive-format", "ZIP")
	if err == nil {
		t.Fatalf("--archive-format ZIP should be rejected (case-sensitive Choice), output: %s", out)
	}
	if !strings.Contains(err.Error(), "'ZIP' is not one of") {
		t.Errorf("err = %v, want the Choice error naming the rejected uppercase value", err)
	}
}

// TestPackCmd_ArchiveFormat_MissingArgument_BareUsageError mirrors the
// bare (no Usage:/Try-help preamble) shape verified live to be identical to
// --format's own missing-argument case.
func TestPackCmd_ArchiveFormat_MissingArgument_BareUsageError(t *testing.T) {
	dir := chdirTemp(t)
	writeArchiveFixture(t, dir)

	out, err := runPackCmd(t, "--archive", "--archive-format")
	if err == nil {
		t.Fatalf("expected an error (output: %s)", out)
	}
	if exitCodeOf(err) != 2 {
		t.Errorf("exitCodeOf(err) = %d, want 2", exitCodeOf(err))
	}
	want := "Option '--archive-format' requires an argument."
	if err.Error() != want {
		t.Errorf("err = %q, want %q", err.Error(), want)
	}
}

// archiveSizeSuffixRe matches bundleSizeSuffix's three rendered shapes:
// " (N bytes)", " (X.Y KiB)", " (X.Y MiB)".
var archiveSizeSuffixRe = regexp.MustCompile(`\((\d+ bytes|\d+\.\d KiB|\d+\.\d MiB)\)`)

// TestPackCmd_ArchiveFlag_PrintsSizeSuffix mirrors _bundle_size_suffix
// (commands/pack.py:617-629): the success line for an ARCHIVE gets a size
// annotation the directory-bundle success line never had.
func TestPackCmd_ArchiveFlag_PrintsSizeSuffix(t *testing.T) {
	dir := chdirTemp(t)
	writeArchiveFixture(t, dir)

	out, err := runPackCmd(t, "--archive")
	if err != nil {
		t.Fatalf("pack --archive returned error: %v (output: %s)", err, out)
	}
	if !archiveSizeSuffixRe.MatchString(out) {
		t.Errorf("output = %q, want a size suffix matching %s", out, archiveSizeSuffixRe)
	}
}

// TestPackCmd_NoArchive_NoSizeSuffix locks the OTHER half of
// _bundle_size_suffix's own guard (`if not path.is_file(): return ""`): a
// plain directory bundle must never get a size annotation.
func TestPackCmd_NoArchive_NoSizeSuffix(t *testing.T) {
	dir := chdirTemp(t)
	writeArchiveFixture(t, dir)

	out, err := runPackCmd(t)
	if err != nil {
		t.Fatalf("pack returned error: %v (output: %s)", err, out)
	}
	if archiveSizeSuffixRe.MatchString(out) {
		t.Errorf("output = %q, a directory bundle must not get a size suffix", out)
	}
}

const zipMigrationNoticeText = "Note: --archive now produces .zip by default. Use --archive-format tar.gz to restore the previous format for legacy pipelines."

// TestPackCmd_ArchiveImplicitZip_PrintsMigrationNotice mirrors
// show_zip_migration_notice's positive case (commands/pack.py:566-570):
// --archive with NO --archive-format given resolves to the implicit "zip"
// default and shows the one-time migration notice.
func TestPackCmd_ArchiveImplicitZip_PrintsMigrationNotice(t *testing.T) {
	dir := chdirTemp(t)
	writeArchiveFixture(t, dir)

	out, err := runPackCmd(t, "--archive")
	if err != nil {
		t.Fatalf("pack --archive returned error: %v (output: %s)", err, out)
	}
	if !strings.Contains(out, zipMigrationNoticeText) {
		t.Errorf("output = %q, want the zip-migration notice", out)
	}
}

// TestPackCmd_ArchiveExplicitZip_SuppressesMigrationNotice mirrors
// show_zip_migration_notice's negative case: an EXPLICIT
// `--archive-format zip` (ctx.get_parameter_source is COMMANDLINE) must NOT
// show the notice, even though the resolved format is still "zip".
func TestPackCmd_ArchiveExplicitZip_SuppressesMigrationNotice(t *testing.T) {
	dir := chdirTemp(t)
	writeArchiveFixture(t, dir)

	out, err := runPackCmd(t, "--archive", "--archive-format", "zip")
	if err != nil {
		t.Fatalf("pack --archive --archive-format zip returned error: %v (output: %s)", err, out)
	}
	if strings.Contains(out, zipMigrationNoticeText) {
		t.Errorf("output = %q, an explicit --archive-format zip must not show the migration notice", out)
	}
}

// TestPackCmd_ArchiveTarGz_NoMigrationNotice: the notice is "zip"-specific
// (it advertises restoring the tar.gz default) -- --archive-format tar.gz
// must never show it.
func TestPackCmd_ArchiveTarGz_NoMigrationNotice(t *testing.T) {
	dir := chdirTemp(t)
	writeArchiveFixture(t, dir)

	out, err := runPackCmd(t, "--archive", "--archive-format", "tar.gz")
	if err != nil {
		t.Fatalf("pack --archive --archive-format tar.gz returned error: %v (output: %s)", err, out)
	}
	if strings.Contains(out, zipMigrationNoticeText) {
		t.Errorf("output = %q, --archive-format tar.gz must not show the zip-migration notice", out)
	}
}

// ── --legacy-skill-paths (ticket 17 phase 3) ────────────────────────────

// TestPackCmd_HelpMatchesOracleLegacySkillPathsText locks --legacy-skill-
// paths' --help description to the Oracle's own wording
// (commands/pack.py:286-295).
func TestPackCmd_HelpMatchesOracleLegacySkillPathsText(t *testing.T) {
	out, err := runPackCmd(t, "--help")
	if err != nil {
		t.Fatalf("pack --help returned error: %v", err)
	}
	want := "Deploy skill files to per-client paths (e.g. .cursor/skills/) instead of the shared .agents/skills/ directory. Compatibility flag for projects that need per-client skill layouts."
	if !strings.Contains(out, want) {
		t.Errorf("pack --help output missing %q:\n%s", want, out)
	}
}

// TestPackCmd_LegacySkillPaths_IsANoOp locks the Oracle-verified behavior:
// --legacy-skill-paths is accepted by `pack_cmd` (commands/pack.py:314) but
// never read anywhere in its body -- its real effect (apply_legacy_skill_
// paths, integration/targets.py:994) is wired only into `install`/`deps
// update`, commands that deploy to per-client target directories, a concept
// `pack`'s own target-agnostic bundle producers never have. Verified live
// against the pinned Oracle: `pack --legacy-skill-paths` and a plain `pack`
// produce byte-identical output and an identical bundle tree. This test
// locks the SAME no-op on apm-go's side -- a future change that wires this
// flag into real per-client behavior on `pack` without updating this test
// (and the Oracle-parity finding it documents) would be a regression, not
// a fix.
func TestPackCmd_LegacySkillPaths_IsANoOp(t *testing.T) {
	dir := chdirTemp(t)
	writeArchiveFixture(t, dir)

	withoutFlag, err := runPackCmd(t)
	if err != nil {
		t.Fatalf("pack returned error: %v (output: %s)", err, withoutFlag)
	}
	withoutTree := readTreeUnderBuild(t, dir)
	if err := os.RemoveAll(filepath.Join(dir, "build")); err != nil {
		t.Fatal(err)
	}

	withFlag, err := runPackCmd(t, "--legacy-skill-paths")
	if err != nil {
		t.Fatalf("pack --legacy-skill-paths returned error: %v (output: %s)", err, withFlag)
	}
	withTree := readTreeUnderBuild(t, dir)

	if withoutFlag != withFlag {
		t.Errorf("output differs with --legacy-skill-paths:\nwithout: %q\nwith:    %q", withoutFlag, withFlag)
	}
	if len(withoutTree) != len(withTree) {
		t.Fatalf("bundle file count differs: without=%d with=%d", len(withoutTree), len(withTree))
	}
	for relPath, wantContent := range withoutTree {
		gotContent, ok := withTree[relPath]
		if !ok {
			t.Errorf("bundle file %q present without the flag, missing with it", relPath)
			continue
		}
		if gotContent != wantContent {
			t.Errorf("bundle file %q content differs with --legacy-skill-paths", relPath)
		}
	}
}

// TestPackCmd_LegacySkillPaths_IsANoOp_PluginManifestPath is the
// PluginManifestProducer counterpart of TestPackCmd_LegacySkillPaths_IsANoOp
// -- verified live against the pinned Oracle too (a target: claude apm.yml
// produces a byte-identical .claude-plugin/plugin.json with and without the
// flag).
func TestPackCmd_LegacySkillPaths_IsANoOp_PluginManifestPath(t *testing.T) {
	dir := chdirTemp(t)
	if err := os.MkdirAll(filepath.Join(dir, ".apm", "skills", "hello"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".apm", "skills", "hello", "SKILL.md"),
		[]byte("---\nname: hello\ndescription: test skill\n---\nHello.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writePackApmYML(t, "name: demo\nversion: 1.0.0\ntarget:\n  - claude\n")

	if _, err := runPackCmd(t); err != nil {
		t.Fatalf("pack returned error: %v", err)
	}
	without, rerr := os.ReadFile(filepath.Join(dir, ".claude-plugin", "plugin.json"))
	if rerr != nil {
		t.Fatalf("expected a real plugin.json: %v", rerr)
	}
	if err := os.RemoveAll(filepath.Join(dir, ".claude-plugin")); err != nil {
		t.Fatal(err)
	}

	if _, err := runPackCmd(t, "--legacy-skill-paths"); err != nil {
		t.Fatalf("pack --legacy-skill-paths returned error: %v", err)
	}
	with, rerr := os.ReadFile(filepath.Join(dir, ".claude-plugin", "plugin.json"))
	if rerr != nil {
		t.Fatalf("expected a real plugin.json: %v", rerr)
	}

	if string(without) != string(with) {
		t.Errorf("plugin.json differs with --legacy-skill-paths:\nwithout: %s\nwith:    %s", without, with)
	}
}

// readTreeUnderBuild returns every regular file's relative path -> content
// under dir/build, for a byte-level "did --legacy-skill-paths change
// anything" comparison.
func readTreeUnderBuild(t *testing.T, dir string) map[string]string {
	t.Helper()
	tree := map[string]string{}
	root := filepath.Join(dir, "build")
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		data, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			return rerr
		}
		tree[rel] = string(data)
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return tree
}

// ── --check-versions/--check-clean (ticket 17 phase 4) ──────────────────

// writeLockstepFixture sets up a marketplace: block using the "lockstep"
// versioning strategy (marketplace.version must equal every local
// package's own apm.yml version) plus one local package pkgs/a whose own
// version is pkgVersion.
func writeLockstepFixture(t *testing.T, dir, marketplaceVersion, pkgVersion string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "pkgs", "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pkgs", "a", "apm.yml"),
		[]byte("name: pkg-a\nversion: "+pkgVersion+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writePackApmYML(t, "name: demo\nversion: "+marketplaceVersion+"\nlicense: MIT\n"+
		"marketplace:\n  owner:\n    name: Acme\n  outputs:\n    - claude\n"+
		"  versioning:\n    strategy: lockstep\n  packages:\n    - name: pkg-a\n      source: ./pkgs/a\n")
}

func TestPackCmd_HelpMatchesOracleCheckFlagsText(t *testing.T) {
	out, err := runPackCmd(t, "--help")
	if err != nil {
		t.Fatalf("pack --help returned error: %v", err)
	}
	for _, want := range []string{
		"Release gate: verify per-package versions agree with the configured marketplace.versioning.strategy (lockstep | tag_pattern | per_package). Exits 3 on misalignment. Composes with --check-clean and --dry-run.",
		"Release gate: regenerate every configured marketplace output to a temp representation and diff against the effective on-disk path, including --marketplace-path overrides. Exits 4 for drift. Use with --dry-run to check without normal pack output generation.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("pack --help output missing %q:\n%s", want, out)
		}
	}
}

func TestPackCmd_CheckVersions_Lockstep_Success(t *testing.T) {
	dir := chdirTemp(t)
	writeLockstepFixture(t, dir, "1.0.0", "1.0.0")

	out, err := runPackCmd(t, "--check-versions", "-m", "none")
	if err != nil {
		t.Fatalf("pack --check-versions returned error: %v (output: %s)", err, out)
	}
	if !strings.Contains(out, "Version alignment OK [strategy=lockstep, expected=1.0.0]") {
		t.Errorf("output = %q, want the success header", out)
	}
	if !strings.Contains(out, "pkgs/a  1.0.0  [matches]") {
		t.Errorf("output = %q, want the matching package row", out)
	}
}

// TestPackCmd_CheckVersions_Lockstep_Failure_Exit3 proves a real mismatch
// exits 3 with the Oracle's exact wording. runReleaseGates' own error
// (wrapped via withSilentExitCode, not withExitCode -- Oracle's own
// ctx.exit(3) is a bare process exit with no additional message) is only
// silenced by main()'s own root Execute() path (root.go's SilenceErrors:
// true); packCmd() in isolation has no such setting, so cobra's own
// default "Error: <err>" echo still appears in THIS test harness's
// captured output -- that's a property of calling packCmd().Execute()
// directly, not of withSilentExitCode itself, which was verified silent
// against the REAL binary end-to-end.
func TestPackCmd_CheckVersions_Lockstep_Failure_Exit3(t *testing.T) {
	dir := chdirTemp(t)
	writeLockstepFixture(t, dir, "1.0.0", "2.0.0")

	out, err := runPackCmd(t, "--check-versions", "--dry-run", "-m", "none")
	if err == nil {
		t.Fatalf("expected an error (output: %s)", out)
	}
	if exitCodeOf(err) != 3 {
		t.Errorf("exitCodeOf(err) = %d, want 3", exitCodeOf(err))
	}
	if !isSilentExit(err) {
		t.Error("err is not a silent exit -- main()'s root Execute() would print a redundant \"[x] ...\" line the Oracle's bare ctx.exit(3) never does")
	}
	for _, want := range []string{
		"Version alignment failed [strategy=lockstep, expected=1.0.0]",
		"pkgs/a  2.0.0  [drift:expected=1.0.0]",
		"pkgs/a: expected 1.0.0, found 2.0.0",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output = %q, want %q", out, want)
		}
	}
}

func TestPackCmd_CheckVersions_NoMarketplaceBlock_SkipsWithInfo(t *testing.T) {
	dir := chdirTemp(t)
	if err := os.MkdirAll(filepath.Join(dir, ".apm", "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".apm", "agents", "foo.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	writePackApmYML(t, "name: demo\nversion: 1.0.0\ndependencies:\n  apm:\n    - acme/tool\n")

	out, err := runPackCmd(t, "--check-versions")
	if err != nil {
		t.Fatalf("pack --check-versions returned error: %v (output: %s)", err, out)
	}
	want := "Version alignment check skipped: no marketplace block; nothing to check."
	if !strings.Contains(out, want) {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestPackCmd_CheckClean_NoMarketplaceBlock_SkipsWithInfo(t *testing.T) {
	dir := chdirTemp(t)
	if err := os.MkdirAll(filepath.Join(dir, ".apm", "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".apm", "agents", "foo.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	writePackApmYML(t, "name: demo\nversion: 1.0.0\ndependencies:\n  apm:\n    - acme/tool\n")

	out, err := runPackCmd(t, "--check-clean")
	if err != nil {
		t.Fatalf("pack --check-clean returned error: %v (output: %s)", err, out)
	}
	want := "Marketplace drift check skipped: no marketplace block; nothing to check."
	if !strings.Contains(out, want) {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestPackCmd_CheckClean_Success(t *testing.T) {
	dir := chdirTemp(t)
	writeLockstepFixture(t, dir, "1.0.0", "1.0.0")

	// Seed a matching marketplace.json by actually packing once.
	if _, err := runPackCmd(t); err != nil {
		t.Fatalf("seed pack returned error: %v", err)
	}

	out, err := runPackCmd(t, "--check-clean", "--dry-run", "-m", "none")
	if err != nil {
		t.Fatalf("pack --check-clean returned error: %v (output: %s)", err, out)
	}
	if !strings.Contains(out, "Marketplace working tree clean [outputs=claude]") {
		t.Errorf("output = %q, want the clean-tree success header", out)
	}
}

// TestPackCmd_CheckClean_Failure_Exit4 proves a real drift (the on-disk
// marketplace.json no longer matches apm.yml) exits 4 with the Oracle's
// exact recovery-recipe text, silently (withSilentExitCode).
func TestPackCmd_CheckClean_Failure_Exit4(t *testing.T) {
	dir := chdirTemp(t)
	writeLockstepFixture(t, dir, "1.0.0", "1.0.0")
	if _, err := runPackCmd(t); err != nil {
		t.Fatalf("seed pack returned error: %v", err)
	}
	// Introduce drift: change the marketplace owner after the seed pack.
	data, rerr := os.ReadFile(filepath.Join(dir, "apm.yml"))
	if rerr != nil {
		t.Fatal(rerr)
	}
	if err := os.WriteFile(filepath.Join(dir, "apm.yml"), []byte(strings.Replace(string(data), "Acme", "Acme Renamed", 1)), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := runPackCmd(t, "--check-clean", "--dry-run", "-m", "none")
	if err == nil {
		t.Fatalf("expected an error (output: %s)", out)
	}
	if exitCodeOf(err) != 4 {
		t.Errorf("exitCodeOf(err) = %d, want 4", exitCodeOf(err))
	}
	if !isSilentExit(err) {
		t.Error("err is not a silent exit -- main()'s root Execute() would print a redundant \"[x] ...\" line the Oracle's bare ctx.exit(4) never does")
	}
	for _, want := range []string{
		"Marketplace working tree dirty [outputs=claude]",
		"drift: 1 differences",
		`owner.name  "Acme" -> "Acme Renamed"`,
		"To recover cleanly (fold into the current commit):",
		"apm-go pack                       # regenerate locally",
		"git commit --amend --no-edit   # fold into the current commit",
		"Why this exists: marketplace.json is checked in (lockfile pattern)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

// TestPackCmd_CheckVersionsAndCheckClean_BothFail_Exit3Wins mirrors
// pack.py's own "Gate exit codes... 3 wins over 4" comment, confirmed live
// against the pinned Oracle: when both gates fail simultaneously, exit 3
// (version) takes precedence over exit 4 (drift).
func TestPackCmd_CheckVersionsAndCheckClean_BothFail_Exit3Wins(t *testing.T) {
	dir := chdirTemp(t)
	writeLockstepFixture(t, dir, "1.0.0", "1.0.0")
	if _, err := runPackCmd(t); err != nil {
		t.Fatalf("seed pack returned error: %v", err)
	}
	data, rerr := os.ReadFile(filepath.Join(dir, "apm.yml"))
	if rerr != nil {
		t.Fatal(rerr)
	}
	// Break BOTH gates: bump the marketplace version (lockstep drift) AND
	// rename the owner (marketplace.json drift) in one edit.
	newContent := strings.Replace(string(data), "version: 1.0.0", "version: 9.9.9", 1)
	newContent = strings.Replace(newContent, "Acme", "Acme Renamed", 1)
	if err := os.WriteFile(filepath.Join(dir, "apm.yml"), []byte(newContent), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := runPackCmd(t, "--check-versions", "--check-clean", "--dry-run", "-m", "none")
	if err == nil {
		t.Fatalf("expected an error (output: %s)", out)
	}
	if exitCodeOf(err) != 3 {
		t.Errorf("exitCodeOf(err) = %d, want 3 (version wins over drift)", exitCodeOf(err))
	}
	if !strings.Contains(out, "Version alignment failed") {
		t.Errorf("output = %q, want the version-alignment failure too (both gates still render)", out)
	}
	if !strings.Contains(out, "Marketplace working tree dirty") {
		t.Errorf("output = %q, want the drift failure too (both gates still render even though exit 3 wins)", out)
	}
}

// TestRunPack_DependenciesOnly_ListsPackedFiles is the R12a regression
// (prd.md/design.md §3): --dry-run already lists every file result.Produce
// would pack via ux.BulletList -- the real (non-dry-run) run must print the
// SAME list, not just the aggregate count, matching its own dry-run preview.
// TestRunPack_DependenciesOnly_DefaultVerbosityOmitsFileList is ticket 13
// finding 1: pack.py's _render_bundle_result lists the real (non-dry-run)
// run's packed files via logger.verbose_detail, gated on -v -- verified
// directly against the pinned Oracle (a plain `apm pack` prints only the
// 3-line summary). apm-go previously printed the file list unconditionally.
func TestRunPack_DependenciesOnly_DefaultVerbosityOmitsFileList(t *testing.T) {
	dir := chdirTemp(t)
	if err := os.MkdirAll(filepath.Join(dir, ".apm", "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".apm", "agents", "foo.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	writePackApmYML(t, "name: demo\nversion: 1.0.0\ndependencies:\n  apm:\n    - acme/tool\n")

	out, err := runPackCmd(t)
	if err != nil {
		t.Fatalf("pack returned error: %v (output: %s)", err, out)
	}
	if !strings.Contains(out, "Packed 2 file(s)") {
		t.Errorf("output = %q, want the summary count line", out)
	}
	// "plugin.json" also appears inside the fixed "bundle ready" wording
	// line itself, so check for the bulleted per-file listing form
	// specifically (ux.BulletList's own "* <item>" rendering), not a bare
	// substring match.
	if strings.Contains(out, "* plugin.json") {
		t.Errorf("output = %q, must NOT list plugin.json at default verbosity", out)
	}
	if strings.Contains(out, filepath.ToSlash(filepath.Join("agents", "foo.md"))) {
		t.Errorf("output = %q, must NOT list agents/foo.md at default verbosity", out)
	}
}

// TestRunPack_DependenciesOnly_Verbose_ListsPackedFiles is the -v
// counterpart of the test above: the same run, with --verbose, must list
// every packed file (pack.py's logger.verbose_detail firing per file).
func TestRunPack_DependenciesOnly_Verbose_ListsPackedFiles(t *testing.T) {
	dir := chdirTemp(t)
	if err := os.MkdirAll(filepath.Join(dir, ".apm", "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".apm", "agents", "foo.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	writePackApmYML(t, "name: demo\nversion: 1.0.0\ndependencies:\n  apm:\n    - acme/tool\n")

	out, err := runPackCmd(t, "--verbose")
	if err != nil {
		t.Fatalf("pack returned error: %v (output: %s)", err, out)
	}
	if !strings.Contains(out, "plugin.json") {
		t.Errorf("output = %q, want the packed file list to include plugin.json", out)
	}
	if !strings.Contains(out, filepath.ToSlash(filepath.Join("agents", "foo.md"))) {
		t.Errorf("output = %q, want the packed file list to include agents/foo.md", out)
	}
}

func TestRunPack_TargetClaudeOnly_WritesRealPluginJSON(t *testing.T) {
	dir := chdirTemp(t)
	writePackApmYML(t, "name: demo\nversion: 1.0.0\ntarget:\n  - claude\n")

	out, err := runPackCmd(t)
	if err != nil {
		t.Fatalf("pack returned error: %v (output: %s)", err, out)
	}
	if !strings.Contains(out, "Generated plugin manifest") {
		t.Errorf("output = %q, want a real plugin.json confirmation", out)
	}
	data, rerr := os.ReadFile(filepath.Join(dir, ".claude-plugin", "plugin.json"))
	if rerr != nil {
		t.Fatalf("expected a real plugin.json: %v", rerr)
	}
	if !strings.Contains(string(data), `"name": "demo"`) {
		t.Errorf("plugin.json = %s, want the synthesized name field", data)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "build")); !os.IsNotExist(statErr) {
		t.Errorf("target-only input must not also build a bundle (stat err = %v)", statErr)
	}
}

// TestPack_LegacySingularTargetKey_StillResolves is AC29 (R1.4/C4)'s pack-side
// companion to install_test.go's TestInstall_LegacySingularTargetKey_
// StillDeploys (2026-07-30 codex Tier 2 B4): pack.go's loadPackManifest has
// its own SafeLoad -> ParseManifest entry point, entirely separate from
// install's -- an apm.yml written with only the pre-existing singular
// target: key must still resolve through THIS entry point too and produce a
// real plugin.json, not just through install's.
func TestPack_LegacySingularTargetKey_StillResolves(t *testing.T) {
	dir := chdirTemp(t)
	writePackApmYML(t, "name: demo\nversion: 1.0.0\ntarget:\n  - claude\n  - copilot\n")

	out, err := runPackCmd(t)
	if err != nil {
		t.Fatalf("pack returned error: %v (output: %s)", err, out)
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".claude-plugin", "plugin.json")); statErr != nil {
		t.Errorf("expected a real plugin.json for claude: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".github", "plugin", "plugin.json")); statErr != nil {
		t.Errorf("expected a real plugin.json for copilot: %v", statErr)
	}
}

func TestRunPack_TargetCodexOnly_ExitsOne_NotPluginManifestEcosystem(t *testing.T) {
	// codex is a valid target but NOT a plugin-manifest ecosystem
	// (claude/copilot only) -- with no dependencies:/marketplace: either,
	// this is still "nothing to pack".
	dir := chdirTemp(t)
	writePackApmYML(t, "name: demo\nversion: 1.0.0\ntarget:\n  - codex\n")

	_, err := runPackCmd(t)
	if err == nil {
		t.Fatal("expected an error: codex alone triggers no producer")
	}
	if exitCodeOf(err) != 1 {
		t.Errorf("exitCodeOf(err) = %d, want 1", exitCodeOf(err))
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".claude-plugin")); !os.IsNotExist(statErr) {
		t.Errorf("apm-go must not produce a plugin.json for a non-plugin-manifest target (stat err = %v)", statErr)
	}
}

func TestRunPack_GenuinelyEmptyApmYML_ExitsOne(t *testing.T) {
	dir := chdirTemp(t)
	writePackApmYML(t, "name: demo\nversion: 1.0.0\n")

	_, err := runPackCmd(t)
	if err == nil {
		t.Fatal("expected an error for a genuinely empty apm.yml")
	}
	if exitCodeOf(err) != 1 {
		t.Errorf("exitCodeOf(err) = %d, want 1", exitCodeOf(err))
	}
	entries, rerr := os.ReadDir(dir)
	if rerr != nil {
		t.Fatal(rerr)
	}
	if len(entries) != 1 || entries[0].Name() != "apm.yml" {
		t.Errorf("pack must not write any file/dir for a genuinely empty apm.yml, got %v", entries)
	}
}

// ── output location: never the repo root, both outputs written ──────────

func TestPackCmd_TwoOutputs_WrittenAtCorrectPaths_NotRepoRoot(t *testing.T) {
	// Arrange
	dir := chdirTemp(t)
	if err := os.MkdirAll(filepath.Join(dir, "pkgs", "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	writePackApmYML(t, `name: demo
marketplace:
  owner:
    name: Acme
  outputs:
    claude: {}
    codex: {}
  packages:
    - name: tool-a
      source: ./pkgs/a
      category: utility
`)

	// Act
	out, err := runPackCmd(t)

	// Assert
	if err != nil {
		t.Fatalf("pack returned error: %v (output: %s)", err, out)
	}
	claudePath := filepath.Join(dir, ".claude-plugin", "marketplace.json")
	codexPath := filepath.Join(dir, ".agents", "plugins", "marketplace.json")
	if _, statErr := os.Stat(claudePath); statErr != nil {
		t.Errorf("expected claude output at %s: %v", claudePath, statErr)
	}
	if _, statErr := os.Stat(codexPath); statErr != nil {
		t.Errorf("expected codex output at %s: %v", codexPath, statErr)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "marketplace.json")); !os.IsNotExist(statErr) {
		t.Errorf("marketplace.json must never be written to the repo root (stat err = %v)", statErr)
	}

	claudeData, err := os.ReadFile(claudePath)
	if err != nil {
		t.Fatal(err)
	}
	// category is now passed through to claude output too (mapper.go's
	// ClaudePlugin.Category), matching upstream apm 0.26.0's own claude
	// marketplace.json (eval-real-run-20260728.md:243-263) -- this used to
	// assert the OPPOSITE (no "category" field), which was itself the mkt-052
	// gap this fix closes; see mapper.go's ClaudePlugin.Category doc comment.
	if !strings.Contains(string(claudeData), `"tool-a"`) || !strings.Contains(string(claudeData), `"category": "utility"`) {
		t.Errorf("claude output = %s, want tool-a and category=utility both present", claudeData)
	}
	codexData, err := os.ReadFile(codexPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(codexData), `"category": "utility"`) {
		t.Errorf("codex output = %s, want category=utility present", codexData)
	}
}

// ── F1 fix: local package metadata enrichment end-to-end ─────────────────

func TestPackCmd_LocalPackage_EnrichesFromItsOwnApmYML(t *testing.T) {
	// Arrange: the marketplace.packages[] entry declares neither
	// description nor version -- pack must read them from the local
	// package's own apm.yml on disk (F1 fix; previously local packages were
	// never enriched at all).
	dir := chdirTemp(t)
	pkgDir := filepath.Join(dir, "pkgs", "tool")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "apm.yml"), []byte("name: tool\ndescription: A local tool\nversion: 3.1.4\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writePackApmYML(t, `name: demo
marketplace:
  owner:
    name: Acme
  packages:
    - name: tool-a
      source: ./pkgs/tool
`)

	// Act
	out, err := runPackCmd(t)

	// Assert
	if err != nil {
		t.Fatalf("pack returned error: %v (output: %s)", err, out)
	}
	data, rerr := os.ReadFile(filepath.Join(dir, ".claude-plugin", "marketplace.json"))
	if rerr != nil {
		t.Fatal(rerr)
	}
	if !strings.Contains(string(data), `"description": "A local tool"`) {
		t.Errorf("output = %s, want description enriched from local apm.yml", data)
	}
	if !strings.Contains(string(data), `"version": "3.1.4"`) {
		t.Errorf("output = %s, want version enriched from local apm.yml", data)
	}
}

func TestPackCmd_LocalPackage_CuratorDescriptionWinsOverLocalApmYML(t *testing.T) {
	// Arrange: curator-supplied description must win over the local
	// package's own apm.yml value.
	dir := chdirTemp(t)
	pkgDir := filepath.Join(dir, "pkgs", "tool")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "apm.yml"), []byte("name: tool\ndescription: from local apm.yml\nversion: 9.9.9\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writePackApmYML(t, `name: demo
marketplace:
  owner:
    name: Acme
  packages:
    - name: tool-a
      source: ./pkgs/tool
      description: curator description
      version: "1.0.0"
`)

	// Act
	out, err := runPackCmd(t)

	// Assert
	if err != nil {
		t.Fatalf("pack returned error: %v (output: %s)", err, out)
	}
	data, rerr := os.ReadFile(filepath.Join(dir, ".claude-plugin", "marketplace.json"))
	if rerr != nil {
		t.Fatal(rerr)
	}
	if !strings.Contains(string(data), `"description": "curator description"`) {
		t.Errorf("output = %s, want curator's description to win", data)
	}
	if strings.Contains(string(data), "from local apm.yml") {
		t.Errorf("output = %s, must not contain the local apm.yml's own description", data)
	}
}

// TestPack_LocalSourceBecomesJunctionAfterAdd_Rejected is B-BLOCKING-1's
// (external audit round 6, 2026-07-31) end-to-end TOCTOU regression:
// mkt-046 lets `package add` reference a local source before its directory
// exists on disk, so `add`'s own containment check (verifyPackageSource,
// authoring/editor.go) only ever walks the longest EXISTING ancestor -- here,
// the project root itself -- and has nothing to reject. This reproduces the
// audit's own repro steps exactly: add a not-yet-existing local source, then
// replace it with a directory junction pointing outside the project root,
// then pack -- and asserts pack itself refuses to read (and therefore never
// embeds) the escaping target's apm.yml, rather than trusting the add-time
// check that already happened once against a path that didn't exist yet.
func TestPack_LocalSourceBecomesJunctionAfterAdd_Rejected(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("directory junctions are a Windows-only concept")
	}

	// Arrange: `package add ./later` while "later" does not exist yet.
	dir := chdirTemp(t)
	apmYML := "name: demo\nversion: 1.0.0\nmarketplace:\n  owner:\n    name: acme\n  packages: []\n"
	if err := os.WriteFile("apm.yml", []byte(apmYML), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := runMarketplaceCmd(t, "package", "add", "./later"); err != nil {
		t.Fatalf("package add ./later (not yet on disk, legitimate per mkt-046) returned error: %v (output: %s)", err, out)
	}

	// The path becomes a directory junction pointing outside the project
	// root strictly AFTER add -- this is the TOCTOU window B-BLOCKING-1
	// names; nothing in `add` itself could have seen this coming.
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "apm.yml"), []byte("name: leak\ndescription: SECRET-LEAK\nversion: 9.9.9\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	later := filepath.Join(dir, "later")
	mklink := exec.Command("cmd", "/c", "mklink", "/J", later, outside)
	if out, err := mklink.CombinedOutput(); err != nil {
		t.Skipf("SKIPPED: cannot create a directory junction in this environment (%v: %s); the junction-escape guard is untested by this run", err, out)
	}

	// Act
	out, err := runPackCmd(t)

	// Assert
	if err == nil {
		t.Fatalf("pack succeeded, want rejection of a local source that became a junction escaping the project root after add (output: %s)", out)
	}
	if data, rerr := os.ReadFile(filepath.Join(dir, ".claude-plugin", "marketplace.json")); rerr == nil && strings.Contains(string(data), "SECRET-LEAK") {
		t.Errorf("marketplace.json = %s, must never contain the escaping target's apm.yml contents", data)
	}
}

// TestPack_HandEditedEscapingLocalSource_Rejected is B-BLOCKING-1's second
// named regression: a curator can hand-edit apm.yml directly (bypassing
// `package add` and its own containment check entirely) to reference a
// local source that, by the time `pack` runs, is already a directory
// junction pointing outside the project root. This proves `pack`'s own
// containment check (localApmYMLPath/authoring.ResolveLocalSourceAgainstRoot,
// internal/marketplace/build/metadata.go) is the one actually enforcing the
// boundary at read time -- not merely relying on whatever `add` may or may
// not have checked earlier.
func TestPack_HandEditedEscapingLocalSource_Rejected(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("directory junctions are a Windows-only concept")
	}

	// Arrange
	dir := chdirTemp(t)
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "apm.yml"), []byte("name: leak\ndescription: SECRET-LEAK\nversion: 9.9.9\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	escaping := filepath.Join(dir, "escaping")
	mklink := exec.Command("cmd", "/c", "mklink", "/J", escaping, outside)
	if out, err := mklink.CombinedOutput(); err != nil {
		t.Skipf("SKIPPED: cannot create a directory junction in this environment (%v: %s); the junction-escape guard is untested by this run", err, out)
	}
	writePackApmYML(t, `name: demo
marketplace:
  owner:
    name: Acme
  packages:
    - name: leak
      source: ./escaping
`)

	// Act
	out, err := runPackCmd(t)

	// Assert
	if err == nil {
		t.Fatalf("pack succeeded, want rejection of a hand-edited local source that is already a junction escaping the project root (output: %s)", out)
	}
	if data, rerr := os.ReadFile(filepath.Join(dir, ".claude-plugin", "marketplace.json")); rerr == nil && strings.Contains(string(data), "SECRET-LEAK") {
		t.Errorf("marketplace.json = %s, must never contain the escaping target's apm.yml contents", data)
	}
}

// TestPack_NestedJunctionEscape_Rejected is B-BLOCKING-1's end-to-end
// "SECRET-NESTED" regression (external audit round 7, 2026-07-31
// follow-up): a package source that is a junction physically inside the
// project root, whose OWN target names a path through a SECOND, separate
// junction that escapes root, must be rejected by `pack` -- not accepted
// just because the literal (unresolved) target string happens to look
// contained. This is the exact end-to-end shape the audit's own repro used
// (a nested/chained junction, not a single-hop one already covered by
// TestPack_HandEditedEscapingLocalSource_Rejected above).
func TestPack_NestedJunctionEscape_Rejected(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("directory junctions are a Windows-only concept")
	}

	dir := chdirTemp(t)
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(outside, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "pkg", "apm.yml"), []byte("name: leak\ndescription: SECRET-NESTED\nversion: 9.9.9\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	inner := filepath.Join(dir, "inner")
	mklinkInner := exec.Command("cmd", "/c", "mklink", "/J", inner, outside)
	if out, err := mklinkInner.CombinedOutput(); err != nil {
		t.Skipf("SKIPPED: cannot create a directory junction in this environment (%v: %s); the nested-junction-escape guard is untested by this run", err, out)
	}
	outer := filepath.Join(dir, "outer")
	mklinkOuter := exec.Command("cmd", "/c", "mklink", "/J", outer, filepath.Join(dir, "inner", "pkg"))
	if out, err := mklinkOuter.CombinedOutput(); err != nil {
		t.Skipf("SKIPPED: cannot create the second (nested-target) directory junction in this environment (%v: %s); the nested-junction-escape guard is untested by this run", err, out)
	}

	writePackApmYML(t, `name: demo
marketplace:
  owner:
    name: Acme
  packages:
    - name: leak
      source: ./outer
`)

	// Act
	out, err := runPackCmd(t)

	// Assert
	if err == nil {
		t.Fatalf("pack succeeded, want rejection of a nested-junction local source escaping the project root (output: %s)", out)
	}
	if data, rerr := os.ReadFile(filepath.Join(dir, ".claude-plugin", "marketplace.json")); rerr == nil && strings.Contains(string(data), "SECRET-NESTED") {
		t.Errorf("marketplace.json = %s, must never contain the escaping target's apm.yml contents", data)
	}
}

// TestPack_LocalSourceApmYmlLeafSymlinkEscape_Rejected is B-BLOCKING-2's
// named regression (external audit round 7, 2026-07-31 follow-up): a local
// package's OWN DIRECTORY can be an entirely ordinary, in-root directory
// (nothing to reject there), while its "apm.yml" leaf file is itself a FILE
// symlink pointing outside the project root. localApmYMLPath's pre-fix
// containment check only ever validated the directory, never the leaf file
// it goes on to os.Stat/read -- so this used to slip straight through and
// have the escaping target's apm.yml contents embedded into
// marketplace.json. Skipped (visibly, not silently) when this process
// cannot create a file symlink -- e.g. Windows without Developer Mode or
// SeCreateSymbolicLinkPrivilege.
func TestPack_LocalSourceApmYmlLeafSymlinkEscape_Rejected(t *testing.T) {
	dir := chdirTemp(t)
	outside := t.TempDir()
	secretApmYml := filepath.Join(outside, "apm.yml")
	if err := os.WriteFile(secretApmYml, []byte("name: leak\ndescription: SECRET-LEAF\nversion: 9.9.9\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	pkgDir := filepath.Join(dir, "pkgs", "tool")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	leafLink := filepath.Join(pkgDir, "apm.yml")
	if err := os.Symlink(secretApmYml, leafLink); err != nil {
		t.Skipf("SKIPPED: cannot create a file symlink in this environment (%v); the apm.yml-leaf-symlink-escape guard is untested by this run", err)
	}

	writePackApmYML(t, `name: demo
marketplace:
  owner:
    name: Acme
  packages:
    - name: tool-a
      source: ./pkgs/tool
`)

	// Act
	out, err := runPackCmd(t)

	// Assert
	if err == nil {
		t.Fatalf("pack succeeded, want rejection of a local package whose apm.yml leaf is a symlink escaping the project root (output: %s)", out)
	}
	if data, rerr := os.ReadFile(filepath.Join(dir, ".claude-plugin", "marketplace.json")); rerr == nil && strings.Contains(string(data), "SECRET-LEAF") {
		t.Errorf("marketplace.json = %s, must never contain the escaping target's apm.yml contents", data)
	}
}

// TestPack_EmptySourcePackage_Rejected is the CLI end-to-end regression for
// schema.go's parsePackages fix: an empty `source: ""` package entry used to
// skip manifest.ValidateMarketplaceSource entirely (it only ran when
// "source != \"\""), letting `apm-go pack` succeed and silently emit a
// malformed claude plugins[] entry (`{"source":{"source":"github","ref":...,
// "sha":...}}`, missing "repo") -- reproduced end-to-end with a compiled
// binary before the fix, per agent-schema.md's now-removed matching source-
// table callout. ValidateMarketplaceSource's own dedicated message
// ("marketplace source is empty") must now surface all the way out to pack's
// exit code and stderr.
func TestPack_EmptySourcePackage_Rejected(t *testing.T) {
	chdirTemp(t)
	writePackApmYML(t, `name: demo
marketplace:
  owner:
    name: Acme
  packages:
    - name: ghost-pkg
      source: ""
      ref: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`)

	// Act
	out, err := runPackCmd(t)

	// Assert
	if err == nil {
		t.Fatalf("pack succeeded, want rejection of an empty-source package entry (output: %s)", out)
	}
	if !strings.Contains(out, "marketplace source is empty") {
		t.Errorf("pack output = %q, want it to contain manifest.ValidateMarketplaceSource's own %q message", out, "marketplace source is empty")
	}
}

// ── config-level path override (map form) ────────────────────────────────

func TestPackCmd_ConfigOverridePath_MapForm_IsUsed(t *testing.T) {
	// Arrange
	dir := chdirTemp(t)
	if err := os.MkdirAll(filepath.Join(dir, "pkgs", "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	writePackApmYML(t, `name: demo
marketplace:
  owner:
    name: Acme
  outputs:
    claude:
      path: dist/claude-marketplace.json
  packages:
    - name: tool-a
      source: ./pkgs/a
`)

	// Act
	out, err := runPackCmd(t)

	// Assert
	if err != nil {
		t.Fatalf("pack returned error: %v (output: %s)", err, out)
	}
	overridden := filepath.Join(dir, "dist", "claude-marketplace.json")
	if _, statErr := os.Stat(overridden); statErr != nil {
		t.Errorf("expected output at overridden path %s: %v", overridden, statErr)
	}
	defaultPath := filepath.Join(dir, ".claude-plugin", "marketplace.json")
	if _, statErr := os.Stat(defaultPath); !os.IsNotExist(statErr) {
		t.Errorf("default path should not have been written when overridden (stat err = %v)", statErr)
	}
}

// ── CLI --marketplace-path wins over the config-level override ──────────

func TestPackCmd_CLIPathOverride_WinsOverConfigOverride(t *testing.T) {
	// Arrange
	dir := chdirTemp(t)
	if err := os.MkdirAll(filepath.Join(dir, "pkgs", "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	writePackApmYML(t, `name: demo
marketplace:
  owner:
    name: Acme
  outputs:
    claude:
      path: dist/claude-marketplace.json
  packages:
    - name: tool-a
      source: ./pkgs/a
`)

	// Act
	out, err := runPackCmd(t, "--marketplace-path", "claude=cli-dist/marketplace.json")

	// Assert
	if err != nil {
		t.Fatalf("pack returned error: %v (output: %s)", err, out)
	}
	cliPath := filepath.Join(dir, "cli-dist", "marketplace.json")
	if _, statErr := os.Stat(cliPath); statErr != nil {
		t.Errorf("expected output at CLI-overridden path %s: %v", cliPath, statErr)
	}
	configPath := filepath.Join(dir, "dist", "claude-marketplace.json")
	if _, statErr := os.Stat(configPath); !os.IsNotExist(statErr) {
		t.Errorf("config-overridden path should not have been written when the CLI overrides it too (stat err = %v)", statErr)
	}
}

func TestPackCmd_MarketplacePathOverride_UnknownFormat_Errors(t *testing.T) {
	chdirTemp(t)
	writePackApmYML(t, `name: demo
marketplace:
  owner:
    name: Acme
  packages: []
`)

	_, err := runPackCmd(t, "--marketplace-path", "bogus=dist/x.json")
	if err == nil {
		t.Fatal("expected an error for an unknown --marketplace-path format")
	}
}

func TestPackCmd_MarketplacePathOverride_Malformed_Errors(t *testing.T) {
	chdirTemp(t)
	writePackApmYML(t, `name: demo
marketplace:
  owner:
    name: Acme
  packages: []
`)

	_, err := runPackCmd(t, "--marketplace-path", "claude-no-equals-sign")
	if err == nil {
		t.Fatal("expected an error for a malformed --marketplace-path value")
	}
}

func TestPackCmd_MarketplacePathOverride_Traversal_Errors(t *testing.T) {
	// Arrange
	dir := chdirTemp(t)
	if err := os.MkdirAll(filepath.Join(dir, "pkgs", "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	writePackApmYML(t, `name: demo
marketplace:
  owner:
    name: Acme
  packages:
    - name: tool-a
      source: ./pkgs/a
`)

	// Act
	_, err := runPackCmd(t, "--marketplace-path", "claude="+filepath.Join("..", "..", "escaped-marketplace.json"))

	// Assert
	if err == nil {
		t.Fatal("expected an error: --marketplace-path must not be allowed to escape the project root")
	}
	if _, statErr := os.Stat(filepath.Join(dir, "..", "..", "escaped-marketplace.json")); !os.IsNotExist(statErr) {
		t.Errorf("traversal path must never actually be written (stat err = %v)", statErr)
	}
}

// ── --dry-run: zero writes ───────────────────────────────────────────────

func TestPackCmd_DryRun_WritesNothing(t *testing.T) {
	// Arrange
	dir := chdirTemp(t)
	if err := os.MkdirAll(filepath.Join(dir, "pkgs", "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	writePackApmYML(t, `name: demo
marketplace:
  owner:
    name: Acme
  packages:
    - name: tool-a
      source: ./pkgs/a
`)

	// Act
	out, err := runPackCmd(t, "--dry-run")

	// Assert
	if err != nil {
		t.Fatalf("pack --dry-run returned error: %v (output: %s)", err, out)
	}
	if !strings.Contains(out, "Would") {
		t.Errorf("output = %q, want a dry-run notice", out)
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".claude-plugin", "marketplace.json")); !os.IsNotExist(statErr) {
		t.Errorf("--dry-run must not write any file (stat err = %v)", statErr)
	}
}

// ── -m/--marketplace filter ───────────────────────────────────────────────

func TestPackCmd_MarketplaceFilterNone_WritesNothing(t *testing.T) {
	// Arrange
	dir := chdirTemp(t)
	if err := os.MkdirAll(filepath.Join(dir, "pkgs", "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	writePackApmYML(t, `name: demo
marketplace:
  owner:
    name: Acme
  outputs:
    claude: {}
    codex: {}
  packages:
    - name: tool-a
      source: ./pkgs/a
      category: utility
`)

	// Act
	out, err := runPackCmd(t, "-m", "none")

	// Assert
	if err != nil {
		t.Fatalf("pack -m none returned error: %v (output: %s)", err, out)
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".claude-plugin", "marketplace.json")); !os.IsNotExist(statErr) {
		t.Errorf("-m none must not write claude output (stat err = %v)", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".agents", "plugins", "marketplace.json")); !os.IsNotExist(statErr) {
		t.Errorf("-m none must not write codex output (stat err = %v)", statErr)
	}
}

func TestPackCmd_MarketplaceFilterNarrowsToOneFormat(t *testing.T) {
	// Arrange
	dir := chdirTemp(t)
	if err := os.MkdirAll(filepath.Join(dir, "pkgs", "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	writePackApmYML(t, `name: demo
marketplace:
  owner:
    name: Acme
  outputs:
    claude: {}
    codex: {}
  packages:
    - name: tool-a
      source: ./pkgs/a
      category: utility
`)

	// Act
	out, err := runPackCmd(t, "-m", "claude")

	// Assert
	if err != nil {
		t.Fatalf("pack -m claude returned error: %v (output: %s)", err, out)
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".claude-plugin", "marketplace.json")); statErr != nil {
		t.Errorf("expected claude output to be written: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".agents", "plugins", "marketplace.json")); !os.IsNotExist(statErr) {
		t.Errorf("-m claude must not write codex output (stat err = %v)", statErr)
	}
}

func TestPackCmd_MarketplaceFilter_UnknownFormat_Errors(t *testing.T) {
	chdirTemp(t)
	writePackApmYML(t, `name: demo
marketplace:
  owner:
    name: Acme
  packages: []
`)

	_, err := runPackCmd(t, "-m", "bogus")
	if err == nil {
		t.Fatal("expected an error for an unknown -m/--marketplace format")
	}
}

// ── remote package resolution against a real local git fixture ──────────

// fixtureRemoteBuildLister is a build.RefLister test double, mirroring
// cmd/apm-go/marketplace_authoring_test.go's fixtureRemoteLister: it ignores
// the marketplace source string it is given and instead runs a real `git
// ls-remote` against a fixed local directory, so this test can drive
// ResolvePackages' genuine remote-resolution code path against a real
// repository fixture without touching the network -- now that
// manifest.ValidateMarketplaceSource rejects an absolute filesystem path as
// a marketplace source outright (BLOCKING 1, external audit round 4,
// 2026-07-30), so the source string in apm.yml must itself be
// req-mf-017-compliant (e.g. "owner/repo").
type fixtureRemoteBuildLister struct{ dir string }

func (f fixtureRemoteBuildLister) ListRemoteRefs(string) ([]build.RemoteRef, error) {
	// build.RemoteRef keeps each ref's full "refs/tags/..."/"refs/heads/..."
	// name intact (see reflister.go's own package doc comment: unlike
	// authoring's RefLister, ResolvePackages needs the untouched prefix to
	// tell a tag from a same-named branch) -- so, unlike
	// fixtureRemoteLister above, this does NOT strip the prefix.
	cmd := exec.Command("git", "ls-remote", "--tags", "--heads", "--", f.dir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git ls-remote %s: %s", f.dir, strings.TrimSpace(string(out)))
	}
	var refs []build.RemoteRef
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		refs = append(refs, build.RemoteRef{Name: parts[1], Commit: parts[0]})
	}
	return refs, nil
}

func TestPackCmd_RemotePackage_ResolvesAgainstRealGitTags(t *testing.T) {
	// Arrange: a "remote" package whose Source points at a real local git
	// repo fixture (mirroring internal/marketplace/build/builder_test.go's
	// own convention) -- proves AC3's local+remote mixed resolution wires
	// all the way through the CLI.
	dir := chdirTemp(t)
	remoteDir := t.TempDir()
	initGitRepoWithTags(t, remoteDir, "v1.0.0", "v1.1.0")
	wantSHA := packRevParse(t, remoteDir, "v1.1.0")

	origLister := build.DefaultRefLister
	build.DefaultRefLister = fixtureRemoteBuildLister{dir: remoteDir}
	t.Cleanup(func() { build.DefaultRefLister = origLister })

	writePackApmYML(t, `name: demo
marketplace:
  owner:
    name: Acme
  packages:
    - name: remote-tool
      source: owner/repo
      version: "^1.0.0"
`)

	// Act
	out, err := runPackCmd(t)

	// Assert
	if err != nil {
		t.Fatalf("pack returned error: %v (output: %s)", err, out)
	}
	data, rerr := os.ReadFile(filepath.Join(dir, ".claude-plugin", "marketplace.json"))
	if rerr != nil {
		t.Fatal(rerr)
	}
	if !strings.Contains(string(data), wantSHA) {
		t.Errorf("output = %s, want resolved sha %q present", data, wantSHA)
	}
}

// ── exit codes: build/config errors are 1, never 2/3/4 ──────────────────

func TestPackCmd_MutuallyExclusiveConfigs_ExitCode1(t *testing.T) {
	// Arrange
	chdirTemp(t)
	writePackApmYML(t, "name: demo\nmarketplace:\n  owner:\n    name: Acme\n  packages: []\n")
	if err := os.WriteFile("marketplace.yml", []byte("owner:\n  name: Acme\npackages: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	_, err := runPackCmd(t)

	// Assert
	if err == nil {
		t.Fatal("expected an error for mutually exclusive marketplace configs")
	}
	if exitCodeOf(err) != 1 {
		t.Errorf("exitCodeOf(err) = %d, want 1 (config errors are exit 1, not 2)", exitCodeOf(err))
	}
}

func TestPackCmd_CodexMissingCategory_ExitCode1(t *testing.T) {
	// Arrange
	chdirTemp(t)
	writePackApmYML(t, `name: demo
marketplace:
  owner:
    name: Acme
  outputs:
    codex: {}
  packages:
    - name: tool-a
      source: ./pkgs/a
`)

	// Act
	_, err := runPackCmd(t)

	// Assert
	if err == nil {
		t.Fatal("expected an error: codex output requires 'category' on every package")
	}
	if exitCodeOf(err) != 1 {
		t.Errorf("exitCodeOf(err) = %d, want 1", exitCodeOf(err))
	}
}

// TestPackCmd_MFilterExcludesCodex_MissingCategorySucceeds is F3's
// regression test: the codex category-required gate must only trigger once
// codex is actually still in the active outputs after -m filtering. Here
// `outputs:` configures codex, but `-m claude` filters it back out before
// ClaudeMapper/CodexMapper ever composes anything, so pack must succeed and
// build claude's output despite tool-a having no category at all.
func TestPackCmd_MFilterExcludesCodex_MissingCategorySucceeds(t *testing.T) {
	// Arrange
	dir := chdirTemp(t)
	if err := os.MkdirAll(filepath.Join(dir, "pkgs", "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	writePackApmYML(t, `name: demo
marketplace:
  owner:
    name: Acme
  outputs:
    claude: {}
    codex: {}
  packages:
    - name: tool-a
      source: ./pkgs/a
`)

	// Act
	out, err := runPackCmd(t, "-m", "claude")

	// Assert
	if err != nil {
		t.Fatalf("pack -m claude returned error: %v (output: %s) (F3: codex category gate must not fire when codex is filtered out)", err, out)
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".claude-plugin", "marketplace.json")); statErr != nil {
		t.Errorf("expected claude output to be written: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".agents", "plugins", "marketplace.json")); !os.IsNotExist(statErr) {
		t.Errorf("-m claude must not write codex output (stat err = %v)", statErr)
	}
}

func TestPackCmd_HeadNotAllowed_ExitCode1(t *testing.T) {
	// Arrange
	chdirTemp(t)
	remoteDir := t.TempDir()
	initGitRepoWithTags(t, remoteDir, "v1.0.0")
	gitCmd(t, remoteDir, "branch", "feature-x")

	writePackApmYML(t, `name: demo
marketplace:
  owner:
    name: Acme
  packages:
    - name: remote-tool
      source: `+filepath.ToSlash(remoteDir)+`
      ref: feature-x
`)

	// Act
	_, err := runPackCmd(t)

	// Assert
	if err == nil {
		t.Fatal("expected a HeadNotAllowedError for a branch ref")
	}
	if exitCodeOf(err) != 1 {
		t.Errorf("exitCodeOf(err) = %d, want 1", exitCodeOf(err))
	}
	if strings.Contains(err.Error(), "--allow-head") {
		t.Errorf("error must not mention the nonexistent --allow-head flag: %v", err)
	}
}

func TestPackCmd_Success_ExitCode0(t *testing.T) {
	// Arrange
	chdirTemp(t)
	if err := os.MkdirAll(filepath.Join("pkgs", "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	writePackApmYML(t, `name: demo
marketplace:
  owner:
    name: Acme
  packages:
    - name: tool-a
      source: ./pkgs/a
`)

	// Act
	_, err := runPackCmd(t)

	// Assert
	if exitCodeOf(err) != 0 {
		t.Errorf("exitCodeOf(err) = %d, want 0", exitCodeOf(err))
	}
}

// ── --offline: no cache layer, fails loud instead of silently degrading ─

func TestPackCmd_Offline_RemotePackageWithVersion_Errors(t *testing.T) {
	chdirTemp(t)
	writePackApmYML(t, `name: demo
marketplace:
  owner:
    name: Acme
  packages:
    - name: remote-tool
      source: owner/repo
      version: "^1.0.0"
`)

	_, err := runPackCmd(t, "--offline")
	if err == nil {
		t.Fatal("expected an error: --offline has no cached refs to resolve against")
	}
}

// ── legacy marketplace.yml source: deprecation warning ───────────────────

func TestPackCmd_LegacyConfigSource_PrintsDeprecationWarning(t *testing.T) {
	// Arrange
	dir := chdirTemp(t)
	if err := os.MkdirAll(filepath.Join(dir, "pkgs", "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("marketplace.yml", []byte(`owner:
  name: Acme
packages:
  - name: tool-a
    source: ./pkgs/a
`), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	out, err := runPackCmd(t)

	// Assert
	if err != nil {
		t.Fatalf("pack returned error: %v (output: %s)", err, out)
	}
	if !strings.Contains(out, "marketplace migrate") {
		t.Errorf("output = %q, want a deprecation warning pointing at 'apm marketplace migrate'", out)
	}
}

// ── authoring-path license nudge (export/authoring.py's _WARN_MESSAGE) ──

// wantLicenseUndeclaredWarning duplicates pack.go's licenseUndeclaredWarning
// as an independent string literal, matching this file's verbatim-lock
// convention.
const wantLicenseUndeclaredWarning = "No 'license:' field in apm.yml; the SBOM will record NOASSERTION for this package. Add a 'license:' field to apm.yml (an SPDX expression such as MIT or Apache-2.0, or UNLICENSED) to declare it."

func TestRunPack_NoLicenseField_WarnsEvenOnNothingToPack(t *testing.T) {
	// The nudge fires before producer routing, independent of whether pack
	// ultimately succeeds -- mirrors Python firing it before
	// BuildOrchestrator().run() is ever called.
	chdirTemp(t)
	writePackApmYML(t, "name: demo\nversion: 1.0.0\n")

	out, err := runPackCmd(t)
	if err == nil {
		t.Fatal("expected the usual nothing-to-pack error")
	}
	if !strings.Contains(out, wantLicenseUndeclaredWarning) {
		t.Errorf("output = %q, want the license-undeclared warning", out)
	}
}

func TestRunPack_LicenseDeclared_NoWarning(t *testing.T) {
	chdirTemp(t)
	writePackApmYML(t, "name: demo\nversion: 1.0.0\nlicense: MIT\ntarget:\n  - claude\n")

	out, err := runPackCmd(t)
	if err != nil {
		t.Fatalf("pack returned error: %v (output: %s)", err, out)
	}
	if strings.Contains(out, "NOASSERTION") {
		t.Errorf("output = %q, must not warn when license: is declared", out)
	}
}

func TestRunPack_EmptyLicenseField_StillWarns(t *testing.T) {
	chdirTemp(t)
	writePackApmYML(t, "name: demo\nversion: 1.0.0\nlicense: \"\"\ntarget:\n  - claude\n")

	out, err := runPackCmd(t)
	if err != nil {
		t.Fatalf("pack returned error: %v (output: %s)", err, out)
	}
	if !strings.Contains(out, wantLicenseUndeclaredWarning) {
		t.Errorf("output = %q, want the license-undeclared warning for an empty license: value", out)
	}
}

// TestPackCmd_EmptyDependencyListsStillTriggerBundle locks the exact apm.yml
// that `plugin init` scaffolds. Upstream's detect_outputs
// (build_orchestrator.py:363) is `if data and data.get("dependencies")` -- a
// TRUTHINESS check on the raw YAML value, and `{apm: [], mcp: []}` is a
// non-empty dict, so Python produces a bundle. apm-go used to compute
// hasDeps as len(ParsedDeps) > 0, which is "are there any dependency
// entries", and wrongly reported "nothing to pack".
//
// Verified end-to-end against both binaries on identical inputs (apm.yml +
// plugin.json only): upstream printed "Packed 1 file(s) -> build\d-0.1.0";
// apm-go exited 1. `plugin init`'s own Next Steps prints `apm-go pack`, so
// this is on the documented happy path, not an edge case.
func TestPackCmd_EmptyDependencyListsStillTriggerBundle(t *testing.T) {
	// Arrange: byte-for-byte the dependencies block `plugin init` writes.
	chdirTemp(t)
	writePackApmYML(t, "name: demo\nversion: 1.0.0\ndependencies:\n  apm: []\n  mcp: []\nincludes: auto\nscripts: {}\n")

	// Act
	_, err := runPackCmd(t)

	// Assert
	if err != nil && err.Error() == wantNothingToPack {
		t.Fatalf("pack reported %q, but an empty-but-present dependencies mapping is truthy upstream and must trigger the bundle producer", err.Error())
	}
}

// TestPackCmd_DependenciesTruthinessMatrix pins each shape of the
// `dependencies:` key to v0.28.0's `is not None` rule
// (build_orchestrator.py:361, PR #2458): any present, non-null value --
// including an empty `{}` -- runs the bundle producer; only a missing key
// or an explicit null does not. (v0.27 used Python truthiness instead,
// under which `dependencies: {}` was falsy and did NOT pack.)
func TestPackCmd_DependenciesTruthinessMatrix(t *testing.T) {
	tests := []struct {
		name string
		deps string
		// wantErr is the exact error pack must fail with, or "" when the
		// bundle producer must run instead.
		wantErr string
	}{
		{"absent key", "", wantNothingToPack},
		{"empty mapping", "dependencies: {}\n", ""},
		{"mapping of empty lists", "dependencies:\n  apm: []\n  mcp: []\n", ""},
		{"mapping with entries", "dependencies:\n  apm:\n    - owner/repo\n", ""},

		// apm-go's manifest schema rejects these two shapes before pack's
		// producer detection is reached, with a more specific message.
		// Explicit null is None upstream too (no bundle), so the outcome
		// (exit 1) agrees. An empty sequence is non-null upstream, so
		// v0.28's producer would start and then fail parsing the list as a
		// dependencies mapping -- both exit 1, apm-go just says why earlier.
		{"explicit null", "dependencies:\n", "apm.yml: dependencies must be a mapping"},
		{"empty sequence", "dependencies: []\n", "apm.yml: dependencies must be a mapping"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chdirTemp(t)
			writePackApmYML(t, "name: demo\nversion: 1.0.0\n"+tt.deps)

			_, err := runPackCmd(t)

			if tt.wantErr == "" {
				if err != nil && err.Error() == wantNothingToPack {
					t.Errorf("pack reported nothing-to-pack, want the bundle producer to run")
				}
				return
			}
			if err == nil {
				t.Fatalf("pack succeeded, want error %q", tt.wantErr)
			}
			if err.Error() != tt.wantErr {
				t.Errorf("err = %q, want %q", err.Error(), tt.wantErr)
			}
		})
	}
}

// TestDisplayPath is ticket 13 finding 2: pack's success/share-with lines
// print the bundle path relative to cwd, matching the Oracle's own
// bundle_path (built directly from --output's relative default "./build",
// never resolved to absolute) instead of apm-go's previous absolute
// filesystem path.
func TestDisplayPath(t *testing.T) {
	dir := chdirTemp(t)
	abs := filepath.Join(dir, "build", "demo-1.0.0")
	if got, want := displayPath(abs), filepath.Join("build", "demo-1.0.0"); got != want {
		t.Errorf("displayPath(%q) = %q, want %q", abs, got, want)
	}
}
