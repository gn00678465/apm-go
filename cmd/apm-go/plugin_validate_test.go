package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// runPluginValidate builds a fresh pluginValidateCmd, runs it with args, and
// returns its stdout/stderr and the error cmd.Execute() returned (carrying
// the exit code via exitCodeOf). cmd.Execute() alone does not go through
// renderRootError, so a usage error's Usage/Try-help preamble is captured
// separately by the cases that need it (see TestPluginValidate_TooManyArgs_Exit2).
func execPluginValidate(t *testing.T, args ...string) (stdout string, err error) {
	t.Helper()
	cmd := pluginValidateCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs(args)
	err = cmd.Execute()
	return out.String(), err
}

func writeManifest(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// requireSymlinkSupport skips the calling test only if this host actually
// cannot create a symlink, instead of assuming Windows never can: Developer
// Mode (or elevation) lets os.Symlink succeed on Windows, and a blanket
// runtime.GOOS=="windows" skip would then hide these tests' evidence on a
// machine where the capability is present.
func requireSymlinkSupport(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink creation unavailable on this host (%v); needs Developer Mode or elevation on Windows", err)
	}
}

// snapshotTree is this file's name for the package's shared treeSnapshot
// helper (pack_format_test.go) -- NFR-001's before/after read-only proof.
func snapshotTree(t *testing.T, dir string) map[string]string {
	return treeSnapshot(t, dir)
}

// ── US1: pre-publish confirmation ───────────────────────────────────────

func TestPluginValidate_US1_AS1_CleanClaudeScaffold(t *testing.T) {
	dir := chdirTemp(t)
	if err := runPluginInit(t, "demo", "--yes", "--target", "claude"); err != nil {
		t.Fatalf("plugin init demo: %v", err)
	}
	out, err := execPluginValidate(t, filepath.Join(dir, "demo"))
	if err != nil {
		t.Fatalf("plugin validate demo: %v (output: %s)", err, out)
	}
	for _, want := range []string{
		"Structure: passed",
		"Name: passed",
		"Fields: passed",
		"Paths: passed",
		"Unrecognized: passed",
		"Summary: 5 passed, 0 warnings, 0 errors",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output = %q, want %q", out, want)
		}
	}
}

func TestPluginValidate_US1_AS2_CleanAgentPluginScaffold(t *testing.T) {
	dir := chdirTemp(t)
	if err := runPluginInit(t, "demo2", "--yes", "--format", "agent-plugin"); err != nil {
		t.Fatalf("plugin init demo2: %v", err)
	}
	out, err := execPluginValidate(t, filepath.Join(dir, "demo2"))
	if err != nil {
		t.Fatalf("plugin validate demo2: %v (output: %s)", err, out)
	}
	if !strings.Contains(out, "Summary: 5 passed, 0 warnings, 0 errors") {
		t.Errorf("output = %q, want the clean summary line", out)
	}
}

func TestPluginValidate_US1_AS3_MissingName(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, "plugin.json", `{}`)
	out, err := execPluginValidate(t, dir)
	if err == nil {
		t.Fatal("want error for missing name")
	}
	if exitCodeOf(err) != 1 {
		t.Errorf("exit code = %d, want 1", exitCodeOf(err))
	}
	if !strings.Contains(out, "Name: missing required field 'name'") {
		t.Errorf("output = %q, want the missing-name message", out)
	}
	if !strings.Contains(out, "Summary: ") || !strings.Contains(out, "1 errors") {
		t.Errorf("output = %q, want Summary with 1 errors", out)
	}
}

func TestPluginValidate_US1_AS4_WrongFieldType(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, "plugin.json", `{"name":"x","keywords":"a"}`)
	out, err := execPluginValidate(t, dir)
	if err == nil {
		t.Fatal("want error for wrong keywords type")
	}
	if !strings.Contains(out, "Fields: 'keywords' must be an array of strings") {
		t.Errorf("output = %q, want the keywords type message", out)
	}
	if exitCodeOf(err) != 1 {
		t.Errorf("exit code = %d, want 1", exitCodeOf(err))
	}
}

func TestPluginValidate_US1_AS5_PathMissingPrefix(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, "plugin.json", `{"name":"x","skills":"skills/"}`)
	out, err := execPluginValidate(t, dir)
	if err == nil {
		t.Fatal("want error for skills path missing './'")
	}
	if !strings.Contains(out, "Paths: 'skills' must start with './'") {
		t.Errorf("output = %q, want the skills path message", out)
	}
	if exitCodeOf(err) != 1 {
		t.Errorf("exit code = %d, want 1", exitCodeOf(err))
	}
}

func TestPluginValidate_US1_AS6_PathEscapes(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		// pathSyntaxError checks absolute, then './' prefix, then '..'
		// (pluginjson/validate.go) -- a bare "../..." fails the prefix
		// check first, so the '..' message needs a value that already
		// has the './' prefix to reach that third check.
		{"dot-dot segment", `{"name":"x","commands":"./../outside/x.md"}`, "Paths: 'commands' must not contain '..'"},
		{"absolute path", `{"name":"x","commands":"/outside/x.md"}`, "Paths: 'commands' must not be an absolute path"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeManifest(t, dir, "plugin.json", tc.content)
			out, err := execPluginValidate(t, dir)
			if err == nil {
				t.Fatal("want error for escaping path")
			}
			if !strings.Contains(out, tc.want) {
				t.Errorf("output = %q, want %q", out, tc.want)
			}
			if exitCodeOf(err) != 1 {
				t.Errorf("exit code = %d, want 1", exitCodeOf(err))
			}
		})
	}
}

func TestPluginValidate_US1_AS7_NoManifestFound(t *testing.T) {
	dir := t.TempDir()
	out, err := execPluginValidate(t, dir)
	if err == nil {
		t.Fatal("want error when no plugin.json found")
	}
	if exitCodeOf(err) != 1 {
		t.Errorf("exit code = %d, want 1", exitCodeOf(err))
	}
	want := fmt.Sprintf("no plugin.json found in %s (looked in plugin.json, .github/plugin/plugin.json, .claude-plugin/plugin.json, .cursor-plugin/plugin.json)", dir)
	if !strings.Contains(out, want) {
		t.Errorf("output = %q, want %q", out, want)
	}
	if strings.Contains(out, "Validation Results:") || strings.Contains(out, "Summary:") {
		t.Errorf("output = %q, must not print Results/Summary when no manifest found", out)
	}
}

func TestPluginValidate_US1_AS8_DirectFilePath(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, "manifest-named-anything.json", `{"name":"x"}`)
	filePath := filepath.Join(dir, "manifest-named-anything.json")
	out, err := execPluginValidate(t, filePath)
	if err != nil {
		t.Fatalf("plugin validate <file>: %v (output: %s)", err, out)
	}
	// All five checks still render their own "passed" line even when a
	// check's field is absent (e.g. no Paths-typed field present) -- there
	// is nothing wrong to report, so it passes trivially.
	if !strings.Contains(out, "Summary: 5 passed, 0 warnings, 0 errors") {
		t.Errorf("output = %q, want the clean summary for a minimal manifest", out)
	}
}

// ── US2: CI strict mode ─────────────────────────────────────────────────

func TestPluginValidate_US2_AS1_UnrecognizedFieldWarningNoStrict(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, "plugin.json", `{"name":"x","descripton":"d"}`)
	out, err := execPluginValidate(t, dir)
	if err != nil {
		t.Fatalf("without --strict, a warning must not fail: %v (output: %s)", err, out)
	}
	if !strings.Contains(out, "Unrecognized: unrecognized field 'descripton' (did you mean 'description'?)") {
		t.Errorf("output = %q, want the did-you-mean suggestion", out)
	}
	if !strings.Contains(out, "Summary: ") || !strings.Contains(out, "1 warnings") {
		t.Errorf("output = %q, want 1 warnings in Summary", out)
	}
}

func TestPluginValidate_US2_AS2_StrictUpgradesWarningToFailure(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, "plugin.json", `{"name":"x","descripton":"d"}`)
	out, err := execPluginValidate(t, dir, "--strict")
	if err == nil {
		t.Fatal("want --strict to fail on a warning")
	}
	if exitCodeOf(err) != 1 {
		t.Errorf("exit code = %d, want 1", exitCodeOf(err))
	}
	if !strings.Contains(out, "1 warnings") || !strings.Contains(out, "0 errors") {
		t.Errorf("output = %q, want the printed counts unchanged by --strict", out)
	}
}

func TestPluginValidate_US2_AS3_UnrecognizedNoSuggestion(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, "plugin.json", `{"name":"x","publisher":"p"}`)
	out, err := execPluginValidate(t, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v (output: %s)", err, out)
	}
	if !strings.Contains(out, "Unrecognized: unrecognized field 'publisher'") {
		t.Errorf("output = %q, want the bare unrecognized message", out)
	}
	if strings.Contains(out, "did you mean") {
		t.Errorf("output = %q, must not suggest anything for 'publisher'", out)
	}
}

func TestPluginValidate_US2_AS4_NonObjectMetadataAndExperimentalWarn(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, "plugin.json", `{"name":"x","metadata":"a","experimental":[]}`)
	out, err := execPluginValidate(t, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v (output: %s)", err, out)
	}
	for _, want := range []string{
		"Fields: 'metadata' should be an object; Claude Code ignores other values",
		"Fields: 'experimental' should be an object; Claude Code ignores other values",
		"Summary: ",
		"2 warnings, 0 errors",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output = %q, want %q", out, want)
		}
	}
}

func TestPluginValidate_US2_AS5_ExperimentalSubfields(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, "plugin.json", `{"name":"x","experimental":{"themes":1,"foo":1}}`)
	out, err := execPluginValidate(t, dir)
	if err == nil {
		t.Fatal("want error from experimental.themes type mismatch")
	}
	if !strings.Contains(out, "Fields: 'experimental.themes' must be a string or array") {
		t.Errorf("output = %q, want the experimental.themes error", out)
	}
	if !strings.Contains(out, "Unrecognized: unrecognized field 'experimental.foo'") {
		t.Errorf("output = %q, want the experimental.foo warning", out)
	}
}

// ── US3: adversarial / broken manifests ─────────────────────────────────

func TestPluginValidate_US3_AS1_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, "plugin.json", `{`)
	before := snapshotTree(t, dir)
	out, err := execPluginValidate(t, dir)
	if err == nil {
		t.Fatal("want Structure error for invalid JSON")
	}
	if exitCodeOf(err) != 1 {
		t.Errorf("exit code = %d, want 1", exitCodeOf(err))
	}
	if !strings.Contains(out, "Structure: invalid JSON:") {
		t.Errorf("output = %q, want the invalid-JSON message", out)
	}
	for _, notWant := range []string{"Name:", "Fields:", "Paths:", "Unrecognized:"} {
		if strings.Contains(out, notWant) {
			t.Errorf("output = %q, must not print %q once Structure fails", out, notWant)
		}
	}
	if !strings.Contains(out, "Summary: 0 passed, 0 warnings, 1 errors") {
		t.Errorf("output = %q, want the Structure-failed summary", out)
	}
	assertTreeUnchanged(t, dir, before)
}

func TestPluginValidate_US3_AS2_TopLevelNotObject(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, "plugin.json", `[]`)
	out, err := execPluginValidate(t, dir)
	if err == nil {
		t.Fatal("want Structure error")
	}
	if !strings.Contains(out, "Structure: top-level value must be an object") {
		t.Errorf("output = %q, want the top-level message", out)
	}
	if exitCodeOf(err) != 1 {
		t.Errorf("exit code = %d, want 1", exitCodeOf(err))
	}
}

// TestPluginValidate_US3_AS3_OversizedFile pins the size-cap failure to
// contracts/cli-plugin-validate.md's "無法讀取 manifest" row (added after
// WP03 review finding 6): oversize is a read-boundary failure like a
// disallowed file type or an escaping symlink, not a Structure finding --
// it prints after the progress line with no Results/Summary block, exit 1.
func TestPluginValidate_US3_AS3_OversizedFile(t *testing.T) {
	dir := t.TempDir()
	big := make([]byte, 5*1024*1024+1)
	for i := range big {
		big[i] = ' '
	}
	writeManifest(t, dir, "plugin.json", string(big))
	out, err := execPluginValidate(t, dir)
	if err == nil {
		t.Fatal("want a read failure for an oversized file")
	}
	if !strings.Contains(out, fmt.Sprintf("could not read '%s': file exceeds 5 MiB cap (%d bytes)", filepath.ToSlash(filepath.Join(dir, "plugin.json")), len(big))) {
		t.Errorf("output = %q, want the size-cap message", out)
	}
	if strings.Contains(out, "Validation Results:") || strings.Contains(out, "Summary:") {
		t.Errorf("output = %q, must not print Results/Summary for a read failure", out)
	}
	if exitCodeOf(err) != 1 {
		t.Errorf("exit code = %d, want 1", exitCodeOf(err))
	}
}

func TestPluginValidate_US3_AS4_DuplicateKeyWarningExit0(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, "plugin.json", `{"name":"a","name":"b"}`)
	out, err := execPluginValidate(t, dir)
	if err != nil {
		t.Fatalf("duplicate key must be a warning, not fail: %v (output: %s)", err, out)
	}
	if !strings.Contains(out, "Structure: duplicate key 'name' (last value wins)") {
		t.Errorf("output = %q, want the duplicate-key warning", out)
	}
}

// TestPluginValidate_US3_AS5_ReadOnly proves NFR-001 across the adversarial
// set plus a legitimate manifest: the target directory's file tree and
// every file's bytes are unchanged before and after invocation.
func TestPluginValidate_US3_AS5_ReadOnly(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{"invalid JSON", `{`},
		{"non-object", `[]`},
		{"non-UTF-8", string([]byte{0xff, 0xfe, 0x00})},
		{"legitimate manifest", `{"name":"x"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeManifest(t, dir, "plugin.json", tc.content)
			before := snapshotTree(t, dir)
			if _, err := execPluginValidate(t, dir, "-v"); err != nil {
				// errors are expected for some cases; only the tree matters here
				_ = err
			}
			assertTreeUnchanged(t, dir, before)
		})
	}

	t.Run("oversized file", func(t *testing.T) {
		dir := t.TempDir()
		big := make([]byte, 5*1024*1024+1)
		writeManifest(t, dir, "plugin.json", string(big))
		before := snapshotTree(t, dir)
		if _, err := execPluginValidate(t, dir); err == nil {
			t.Fatal("want error for oversized file")
		}
		assertTreeUnchanged(t, dir, before)
	})
}

// ── Usage errors ─────────────────────────────────────────────────────────

func TestPluginValidate_TooManyArgs_Exit2(t *testing.T) {
	dir := chdirTemp(t)
	root := buildRootCmd()
	root.SetArgs([]string{"plugin", "validate", "a", "b"})

	var stdout string
	var exitCode int
	stderr := captureStderr(t, func() {
		stdout = captureStdout(t, func() {
			cmd, err := root.ExecuteC()
			if err == nil {
				t.Fatal("want a usage error for two positional args")
			}
			exitCode = renderRootError(cmd, err)
		})
	})
	if exitCode != 2 {
		t.Errorf("exit code = %d, want 2", exitCode)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty (usage errors render on stderr)", stdout)
	}
	if !strings.Contains(stderr, "Usage:") || !strings.Contains(stderr, "Try '") {
		t.Errorf("stderr = %q, want the Usage/Try-help preamble", stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, "a")); !os.IsNotExist(err) {
		t.Error("usage error must not touch the filesystem")
	}
}

func TestPluginValidate_UnknownFlag_Exit2(t *testing.T) {
	chdirTemp(t)
	root := buildRootCmd()
	root.SetArgs([]string{"plugin", "validate", "--bogus"})

	var exitCode int
	stderr := captureStderr(t, func() {
		captureStdout(t, func() {
			cmd, err := root.ExecuteC()
			if err == nil {
				t.Fatal("want a usage error for an unknown flag")
			}
			exitCode = renderRootError(cmd, err)
		})
	})
	if exitCode != 2 {
		t.Errorf("exit code = %d, want 2", exitCode)
	}
	if !strings.Contains(stderr, "Usage:") {
		t.Errorf("stderr = %q, want the Usage preamble", stderr)
	}
}

// ── Help surface ─────────────────────────────────────────────────────────

func TestPluginValidate_HelpSurface(t *testing.T) {
	pluginRoot := pluginCmd()
	var out bytes.Buffer
	pluginRoot.SetOut(&out)
	pluginRoot.SetArgs([]string{"--help"})
	if err := pluginRoot.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"init", "validate"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("plugin --help = %q, want it to list %q", out.String(), want)
		}
	}

	cmd := pluginValidateCmd()
	var out2 bytes.Buffer
	cmd.SetOut(&out2)
	cmd.SetArgs([]string{"--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"--strict", "-v, --verbose"} {
		if !strings.Contains(out2.String(), want) {
			t.Errorf("plugin validate --help = %q, want it to document %q", out2.String(), want)
		}
	}
}

// ── Edge cases ────────────────────────────────────────────────────────────

// multiple-candidate-locations: the root plugin.json wins over
// .claude-plugin/plugin.json even though the latter is invalid.
func TestPluginValidate_Edge_MultipleCandidateLocations(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, "plugin.json", `{"name":"x"}`)
	writeManifest(t, dir, ".claude-plugin/plugin.json", `{`)
	out, err := execPluginValidate(t, dir)
	if err != nil {
		t.Fatalf("root plugin.json is valid, want exit 0: %v (output: %s)", err, out)
	}
	firstLine := strings.SplitN(out, "\n", 2)[0]
	if !strings.Contains(firstLine, "plugin.json") || strings.Contains(firstLine, ".claude-plugin") {
		t.Errorf("first line = %q, want it to name the root plugin.json", firstLine)
	}
}

// verbose-lists-known-fields: -v lists recognized fields, in file order,
// before Validation Results:; without -v those lines are absent.
func TestPluginValidate_Edge_VerboseListsKnownFields(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, "plugin.json", `{"name":"x","version":"1.0.0","descripton":"d"}`)

	out, err := execPluginValidate(t, dir, "-v")
	if err != nil {
		t.Fatalf("unexpected error: %v (output: %s)", err, out)
	}
	nameIdx := strings.Index(out, "name")
	versionIdx := strings.Index(out, "version")
	resultsIdx := strings.Index(out, "Validation Results:")
	if nameIdx < 0 || versionIdx < 0 || resultsIdx < 0 {
		t.Fatalf("output = %q, want name/version fields and Validation Results:", out)
	}
	if !(nameIdx < versionIdx && versionIdx < resultsIdx) {
		t.Errorf("output = %q, want field lines before Validation Results: in file order", out)
	}
	// The unrecognized field itself legitimately appears later, in the
	// Unrecognized check's own finding line -- only the verbose block
	// (before Validation Results:) must omit it.
	verboseBlock := out[:resultsIdx]
	if strings.Contains(verboseBlock, "descripton") {
		t.Errorf("verbose block = %q, must not list the unrecognized field", verboseBlock)
	}

	outNoVerbose, err := execPluginValidate(t, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v (output: %s)", err, outNoVerbose)
	}
	beforeResults := outNoVerbose[:strings.Index(outNoVerbose, "Validation Results:")]
	if strings.Contains(beforeResults, "name") || strings.Contains(beforeResults, "version") {
		t.Errorf("without -v, no field lines should appear before Validation Results:; got %q", outNoVerbose)
	}
}

// Ordering: within one check, errors print before warnings, both in file
// order (data-model.md's Report.Findings order, rendered as-is).
func TestPluginValidate_Edge_ErrorsBeforeWarningsWithinCheck(t *testing.T) {
	dir := t.TempDir()
	// Fields check: 'author' present but non-object (error) plus
	// 'metadata' non-object (warning) both land in the Fields check.
	writeManifest(t, dir, "plugin.json", `{"name":"x","metadata":"a","author":"nope"}`)
	out, err := execPluginValidate(t, dir)
	if err == nil {
		t.Fatal("want an error from 'author' type mismatch")
	}
	errIdx := strings.Index(out, "Fields: 'author' must be an object")
	warnIdx := strings.Index(out, "Fields: 'metadata' should be an object")
	if errIdx < 0 || warnIdx < 0 {
		t.Fatalf("output = %q, want both the author error and metadata warning", out)
	}
	if errIdx > warnIdx {
		t.Errorf("output = %q, want the error line before the warning line within Fields", out)
	}
}

// ── WP03 review: read-boundary findings 1, 2, 3, 4, 6 ───────────────────

// execPluginValidateFull runs `plugin validate` through the REAL root
// command (buildRootCmd + ExecuteC + renderRootError), the only path that
// carries root's SilenceErrors + renderRootError wiring -- a bare
// pluginValidateCmd().Execute(), as execPluginValidate uses, has no
// SilenceErrors set and lets cobra's own default error handling print a
// second "Error: ..." line to the real os.Stderr, which is a test-harness
// artifact of calling the subcommand directly, never something a real
// invocation does. Pinning stdout AND stderr (WP03 review finding 5) needs
// the real path so an empty stderr actually proves what the contract
// claims: this command writes every status line through
// cmd.OutOrStdout(), never os.Stderr directly.
func execPluginValidateFull(t *testing.T, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	root := buildRootCmd()
	root.SetArgs(append([]string{"plugin", "validate"}, args...))
	stderr = captureStderr(t, func() {
		stdout = captureStdout(t, func() {
			cmd, err := root.ExecuteC()
			if err != nil {
				exitCode = renderRootError(cmd, err)
			}
		})
	})
	return stdout, stderr, exitCode
}

// TestPluginValidate_Finding1_ParentSymlinkEscapeRejected covers WP03
// review finding 1: dir/.claude-plugin is a symlink to a directory OUTSIDE
// dir, and the plugin.json inside it is an ordinary regular file. The
// pre-fix containment check only ran when the candidate's own Lstat showed
// a symlink, so a real file reached through a symlinked PARENT directory
// was never checked at all and would have been read.
func TestPluginValidate_Finding1_ParentSymlinkEscapeRejected(t *testing.T) {
	requireSymlinkSupport(t)
	outside := t.TempDir()
	writeManifest(t, outside, "plugin.json", `{"name":"escaped-content"}`)

	// chdirTemp (not t.TempDir directly) so the working directory is
	// projectRoot itself: displayManifestPath then relativizes the
	// escaping candidate to the short, deterministic ".claude-plugin/
	// plugin.json" on every platform. Passing the bare t.TempDir() result
	// as the CLI argument left the printed path a function of whether the
	// OS temp dir and the test binary's cwd share a volume -- true on the
	// Linux CI runner (relativization succeeds, yielding a long "../../.."
	// form) but not reliably true on Windows, where a cross-volume temp
	// dir falls back to the absolute path this test used to hardcode.
	projectRoot := chdirTemp(t)
	if err := os.Symlink(outside, filepath.Join(projectRoot, ".claude-plugin")); err != nil {
		t.Fatal(err)
	}

	out, err := execPluginValidate(t, projectRoot)
	if err == nil {
		t.Fatal("want the escaping .claude-plugin symlink to be rejected, not read")
	}
	if exitCodeOf(err) != 1 {
		t.Errorf("exit code = %d, want 1", exitCodeOf(err))
	}
	if strings.Contains(out, "escaped-content") {
		t.Fatalf("output = %q, must never surface content read from outside projectRoot", out)
	}
	wantCandidate := filepath.ToSlash(filepath.Join(projectRoot, ".claude-plugin", "plugin.json"))
	wantProgress := " > Validating plugin '.claude-plugin/plugin.json'..."
	wantErr := fmt.Sprintf("could not read '%s': outside the given path", wantCandidate)
	if !strings.Contains(out, wantProgress) {
		t.Errorf("output = %q, want the progress line naming the escaping candidate %q", out, wantProgress)
	}
	if !strings.Contains(out, wantErr) {
		t.Errorf("output = %q, want %q", out, wantErr)
	}
	if strings.Contains(out, "Validation Results:") || strings.Contains(out, "Summary:") {
		t.Errorf("output = %q, must not print Results/Summary for a read failure", out)
	}
}

// TestPluginValidate_Finding2_DirectSymlinkArgumentRejected covers WP03
// review finding 2: the pre-fix code trusted a path the caller named
// directly whenever Lstat showed "not a directory", admitting a symlink
// with no containment or regular-file check at all. NFR-003 authorizes no
// such exception.
func TestPluginValidate_Finding2_DirectSymlinkArgumentRejected(t *testing.T) {
	requireSymlinkSupport(t)
	real := t.TempDir()
	writeManifest(t, real, "real.json", `{"name":"x"}`)
	link := filepath.Join(t.TempDir(), "plugin.json")
	if err := os.Symlink(filepath.Join(real, "real.json"), link); err != nil {
		t.Fatal(err)
	}

	out, err := execPluginValidate(t, link)
	if err == nil {
		t.Fatal("want a symlink named directly to be rejected -- NFR-003 grants no exception for an explicit argument")
	}
	if exitCodeOf(err) != 1 {
		t.Errorf("exit code = %d, want 1", exitCodeOf(err))
	}
	want := fmt.Sprintf("could not read '%s': not a regular file", filepath.ToSlash(link))
	if !strings.Contains(out, want) {
		t.Errorf("output = %q, want %q", out, want)
	}
}

// TestPluginValidate_Finding2_ProbedSymlinkCandidateRejectedEvenWithinTree
// proves the policy has no "stays inside the tree" carve-out either: a
// directory-probed candidate that is itself a symlink is rejected purely
// for being a symlink, before the containment check ever runs, even though
// its target is a sibling file inside the same directory.
func TestPluginValidate_Finding2_ProbedSymlinkCandidateRejectedEvenWithinTree(t *testing.T) {
	requireSymlinkSupport(t)
	dir := t.TempDir()
	writeManifest(t, dir, "real.json", `{"name":"x"}`)
	if err := os.Symlink(filepath.Join(dir, "real.json"), filepath.Join(dir, "plugin.json")); err != nil {
		t.Fatal(err)
	}

	out, err := execPluginValidate(t, dir)
	if err == nil {
		t.Fatal("want a symlinked candidate to be rejected even when its target stays inside dir -- no exception for symlinks, full stop")
	}
	want := fmt.Sprintf("could not read '%s': not a regular file", filepath.ToSlash(filepath.Join(dir, "plugin.json")))
	if !strings.Contains(out, want) {
		t.Errorf("output = %q, want %q", out, want)
	}
}

// TestPluginValidate_Finding2_DirectFIFOArgumentRejectedWithoutBlocking
// covers the other non-regular type finding 2 calls out: a FIFO opened for
// reading with no writer attached blocks indefinitely, so the rejection
// must happen via Lstat, before any call that opens the file. The command
// runs in a goroutine under a hard timeout so a regression that reaches
// open() fails this test instead of hanging the suite. mkfifo is POSIX-only;
// skipped on windows outright (an MSYS/Git-Bash mkfifo.exe found on PATH
// there does not produce something Go's native os.Lstat recognizes as a
// file at all, so probing for the binary is not a reliable capability
// check), matching internal/manifest's TestParseDepString_AbsolutePath
// convention for a platform-gated case.
func TestPluginValidate_Finding2_DirectFIFOArgumentRejectedWithoutBlocking(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("mkfifo has no native equivalent on windows; FIFO rejection is exercised on unix")
	}
	dir := t.TempDir()
	fifoPath := filepath.Join(dir, "plugin.json")
	if err := exec.Command("mkfifo", fifoPath).Run(); err != nil {
		t.Skipf("mkfifo unavailable on %s (%v); FIFO rejection is exercised where mkfifo exists", runtime.GOOS, err)
	}

	type result struct {
		out string
		err error
	}
	done := make(chan result, 1)
	go func() {
		cmd := pluginValidateCmd()
		var buf bytes.Buffer
		cmd.SetOut(&buf)
		cmd.SetArgs([]string{fifoPath})
		err := cmd.Execute()
		done <- result{out: buf.String(), err: err}
	}()

	select {
	case r := <-done:
		if r.err == nil {
			t.Fatal("want a FIFO named directly to be rejected")
		}
		want := fmt.Sprintf("could not read '%s': not a regular file", filepath.ToSlash(fifoPath))
		if !strings.Contains(r.out, want) {
			t.Errorf("output = %q, want %q", r.out, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("plugin validate blocked opening a FIFO with no writer -- the regular-file check must reject it via Lstat before any open")
	}
}

// growingFile wraps a real, small *os.File so Stat/Close behave exactly
// like the genuine file the pre-open Lstat already vetted, while Read
// fabricates far more bytes than that file's true size -- deterministically
// simulating a manifest that grows between the size check and the read
// finishing (WP03 review finding 3), which a real filesystem race cannot
// be made to reproduce reliably across platforms in a test.
type growingFile struct {
	io.ReadCloser
	remaining int
}

func (g *growingFile) Read(p []byte) (int, error) {
	if g.remaining <= 0 {
		return 0, io.EOF
	}
	n := len(p)
	if n > g.remaining {
		n = g.remaining
	}
	for i := range p[:n] {
		p[i] = ' '
	}
	g.remaining -= n
	return n, nil
}

// TestPluginValidate_Finding3_BoundedReadCatchesGrowthAfterStatCheck is the
// review's requested replacement for the old oversized-file test, which
// proved nothing about the CLI: deleting the CLI's own Stat check let the
// full (still finite, on-disk) oversized content reach pluginjson.Validate,
// whose own defensive cap produced the identical message, so the old test
// could not tell the two apart.
//
// Here the fake file's Stat (used for the size check) genuinely reports a
// small size -- well under the cap, exactly like a real file right before
// it grows -- while Read is willing to fabricate far more than the cap.
// Only a caller that BOUNDS the actual read (io.LimitReader, not a full
// io.ReadAll) stops at maxManifestBytes+1 bytes; a caller that reads
// everything and delegates to pluginjson.Validate would consume the whole
// fabricated stream and render a completely different output shape (a
// Structure finding inside a Results/Summary block, per the pre-review
// contract, instead of the read-failure line): both the exact byte count
// and the absence of a Results block are asserted below so either
// regression fails this test.
func TestPluginValidate_Finding3_BoundedReadCatchesGrowthAfterStatCheck(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, "plugin.json", `{"name":"x"}`) // genuinely small; passes the regular-file check
	path := filepath.Join(dir, "plugin.json")

	orig := openRootFile
	t.Cleanup(func() { openRootFile = orig })
	openRootFile = func(root *os.Root, rel string) (manifestFile, os.FileInfo, error) {
		f, info, err := orig(root, rel)
		if err != nil {
			return nil, nil, err
		}
		return &growingFile{ReadCloser: f, remaining: maxManifestBytes + 4096}, info, nil
	}

	out, err := execPluginValidate(t, path)
	if err == nil {
		t.Fatal("want the bounded read to refuse a manifest that grows past the cap after the size check")
	}
	want := fmt.Sprintf("could not read '%s': file exceeds 5 MiB cap (%d bytes)", filepath.ToSlash(path), maxManifestBytes+1)
	if !strings.Contains(out, want) {
		t.Errorf("output = %q, want %q (cap+1 -- the bounded amount, not growingFile's larger fabricated total)", out, want)
	}
	if strings.Contains(out, "Validation Results:") || strings.Contains(out, "Summary:") {
		t.Errorf("output = %q, must not print Results/Summary for a read failure", out)
	}
}

// TestPluginValidate_Finding4_FirstLineRelativizesAbsoluteArgument covers
// WP03 review finding 4: only the path separator was normalized before,
// so an absolute directory argument produced an absolute first line,
// violating the contract's "<relative manifest path>".
func TestPluginValidate_Finding4_FirstLineRelativizesAbsoluteArgument(t *testing.T) {
	dir := chdirTemp(t)
	writeManifest(t, dir, "plugin.json", `{"name":"x"}`)

	out, err := execPluginValidate(t, dir) // dir (from t.TempDir()) is absolute
	if err != nil {
		t.Fatalf("unexpected error: %v (output: %s)", err, out)
	}
	firstLine := strings.SplitN(out, "\n", 2)[0]
	want := " > Validating plugin 'plugin.json'..."
	if firstLine != want {
		t.Errorf("first line = %q, want %q (an absolute directory argument must not leak an absolute path)", firstLine, want)
	}
}

// TestPluginValidate_Finding_RelativeArgumentsProduceRelativeFirstLine
// covers a real contract violation WP04's gate work found: displayManifestPath
// called filepath.Rel(cwd, manifestPath) with manifestPath left exactly as
// given. Rel requires both operands to be either absolute or relative, and
// cwd from os.Getwd() is always absolute, so Rel always errored for a
// relative manifestPath -- every ordinary relative argument a user types --
// and fell through to the cross-volume absolute-path fallback the contract
// reserves for the "different Windows volume" case only. The existing tests
// never caught this because they only ever passed an absolute manifest
// argument (e.g. Finding4 above uses dir from t.TempDir()), for which
// Rel(absolute, absolute) happens to succeed.
func TestPluginValidate_Finding_RelativeArgumentsProduceRelativeFirstLine(t *testing.T) {
	t.Run("relative directory argument", func(t *testing.T) {
		chdirTemp(t)
		writeManifest(t, "demo", "plugin.json", `{"name":"x"}`)
		out, err := execPluginValidate(t, "demo")
		if err != nil {
			t.Fatalf("unexpected error: %v (output: %s)", err, out)
		}
		want := " > Validating plugin 'demo/plugin.json'..."
		if firstLine := strings.SplitN(out, "\n", 2)[0]; firstLine != want {
			t.Errorf("first line = %q, want %q", firstLine, want)
		}
	})

	t.Run("relative file argument", func(t *testing.T) {
		chdirTemp(t)
		writeManifest(t, "demo", "plugin.json", `{"name":"x"}`)
		out, err := execPluginValidate(t, filepath.Join("demo", "plugin.json"))
		if err != nil {
			t.Fatalf("unexpected error: %v (output: %s)", err, out)
		}
		want := " > Validating plugin 'demo/plugin.json'..."
		if firstLine := strings.SplitN(out, "\n", 2)[0]; firstLine != want {
			t.Errorf("first line = %q, want %q", firstLine, want)
		}
	})

	t.Run("dot", func(t *testing.T) {
		chdirTemp(t)
		writeManifest(t, ".", "plugin.json", `{"name":"x"}`)
		out, err := execPluginValidate(t, ".")
		if err != nil {
			t.Fatalf("unexpected error: %v (output: %s)", err, out)
		}
		want := " > Validating plugin 'plugin.json'..."
		if firstLine := strings.SplitN(out, "\n", 2)[0]; firstLine != want {
			t.Errorf("first line = %q, want %q", firstLine, want)
		}
	})

	// A '..' segment that still resolves inside the working directory must
	// not survive into the display path either -- filepath.Abs's own Clean
	// removes it once manifestPath is made absolute before relativising.
	t.Run("dot-dot segment resolving inside the working directory", func(t *testing.T) {
		dir := chdirTemp(t)
		writeManifest(t, ".", "plugin.json", `{"name":"x"}`)
		if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
			t.Fatal(err)
		}
		out, err := execPluginValidate(t, filepath.FromSlash("sub/../plugin.json"))
		if err != nil {
			t.Fatalf("unexpected error: %v (output: %s)", err, out)
		}
		want := " > Validating plugin 'plugin.json'..."
		if firstLine := strings.SplitN(out, "\n", 2)[0]; firstLine != want {
			t.Errorf("first line = %q, want %q", firstLine, want)
		}
	})
}

// TestPluginValidate_Finding6_ReadFailureWording pins the read-failure
// template contracts/cli-plugin-validate.md's "無法讀取 manifest" row
// carries: could not read '<path>': <reason>. manifestPath is removed
// after locate succeeds so readAndValidate's own Lstat produces a genuine
// OS error, and the test captures that exact error text itself (Lstat on
// the same removed path, in the same process) rather than hardcoding
// platform-specific wording.
func TestPluginValidate_Finding6_ReadFailureWording(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, "plugin.json", `{"name":"x"}`)
	path := filepath.Join(dir, "plugin.json")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	_, statErr := os.Lstat(path)
	if statErr == nil {
		t.Fatal("want Lstat to fail for a removed file")
	}

	_, err := readAndValidate(path, "")
	if err == nil {
		t.Fatal("want a read error for a removed file")
	}
	want := fmt.Sprintf("could not read '%s': %s", filepath.ToSlash(path), statErr)
	if err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
}

// ── WP03 review finding 5: full stdout/stderr pins ───────────────────────

// TestPluginValidate_Finding5_FullOutput_Clean pins the complete stdout and
// stderr for a clean manifest, replacing the substring-only assertions the
// review found insufficient to catch a missing symbol, extra blank line, or
// stray output.
func TestPluginValidate_Finding5_FullOutput_Clean(t *testing.T) {
	dir := chdirTemp(t)
	if err := runPluginInit(t, "demo", "--yes", "--target", "claude"); err != nil {
		t.Fatalf("plugin init demo: %v", err)
	}
	stdout, stderr, exitCode := execPluginValidateFull(t, filepath.Join(dir, "demo"))
	if exitCode != 0 {
		t.Fatalf("plugin validate demo: exit %d (stdout: %s)", exitCode, stdout)
	}
	wantStdout := "" +
		" > Validating plugin 'plugin.json'...\n" +
		"\n" +
		" i Validation Results:\n" +
		" + Structure: passed\n" +
		" + Name: passed\n" +
		" + Fields: passed\n" +
		" + Paths: passed\n" +
		" + Unrecognized: passed\n" +
		"\n" +
		" i Summary: 5 passed, 0 warnings, 0 errors\n"
	if stdout != wantStdout {
		t.Errorf("stdout = %q, want %q", stdout, wantStdout)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}
}

// TestPluginValidate_Finding5_FullOutput_Mixed pins the contract's own
// mixed-findings example (contracts/cli-plugin-validate.md's stdout block)
// byte for byte.
func TestPluginValidate_Finding5_FullOutput_Mixed(t *testing.T) {
	dir := chdirTemp(t)
	writeManifest(t, dir, "plugin.json", `{"name":"x","metadata":"a","skills":"skills/","descripton":"d"}`)
	stdout, stderr, exitCode := execPluginValidateFull(t, dir)
	if exitCode != 1 {
		t.Errorf("exit code = %d, want 1", exitCode)
	}
	wantStdout := "" +
		" > Validating plugin 'plugin.json'...\n" +
		"\n" +
		" i Validation Results:\n" +
		" + Structure: passed\n" +
		" + Name: passed\n" +
		" ! Fields: 'metadata' should be an object; Claude Code ignores other values\n" +
		" x Paths: 'skills' must start with './'\n" +
		" ! Unrecognized: unrecognized field 'descripton' (did you mean 'description'?)\n" +
		"\n" +
		" i Summary: 2 passed, 2 warnings, 1 errors\n"
	if stdout != wantStdout {
		t.Errorf("stdout = %q, want %q", stdout, wantStdout)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}
}

// TestPluginValidate_Finding5_FullOutput_StructureFailure pins the
// Structure-failure shape: only the Structure line renders, no other
// check, then the Summary.
func TestPluginValidate_Finding5_FullOutput_StructureFailure(t *testing.T) {
	dir := chdirTemp(t)
	writeManifest(t, dir, "plugin.json", `{`)
	stdout, stderr, exitCode := execPluginValidateFull(t, dir)
	if exitCode != 1 {
		t.Errorf("exit code = %d, want 1", exitCode)
	}
	wantStdout := "" +
		" > Validating plugin 'plugin.json'...\n" +
		"\n" +
		" i Validation Results:\n" +
		" x Structure: invalid JSON: unexpected end of JSON input\n" +
		"\n" +
		" i Summary: 0 passed, 0 warnings, 1 errors\n"
	if stdout != wantStdout {
		t.Errorf("stdout = %q, want %q", stdout, wantStdout)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}
}

// TestPluginValidate_Finding5_FullOutput_Verbose pins -v's field-listing
// block: it appears between the progress line and Validation Results:,
// lists only recognized fields present, in file order, and does not
// duplicate the unrecognized field the Unrecognized check reports later.
func TestPluginValidate_Finding5_FullOutput_Verbose(t *testing.T) {
	dir := chdirTemp(t)
	writeManifest(t, dir, "plugin.json", `{"name":"x","version":"1.0.0","descripton":"d"}`)
	stdout, stderr, exitCode := execPluginValidateFull(t, dir, "-v")
	if exitCode != 0 {
		t.Fatalf("a warning alone must not fail without --strict: exit %d (stdout: %s)", exitCode, stdout)
	}
	wantStdout := "" +
		" > Validating plugin 'plugin.json'...\n" +
		" i name\n" +
		" i version\n" +
		"\n" +
		" i Validation Results:\n" +
		" + Structure: passed\n" +
		" + Name: passed\n" +
		" + Fields: passed\n" +
		" + Paths: passed\n" +
		" ! Unrecognized: unrecognized field 'descripton' (did you mean 'description'?)\n" +
		"\n" +
		" i Summary: 4 passed, 1 warnings, 0 errors\n"
	if stdout != wantStdout {
		t.Errorf("stdout = %q, want %q", stdout, wantStdout)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}
}

// TestPluginValidate_Finding5_FullOutput_NotFound pins the not-found line
// on its own, with no progress line and no Results/Summary block.
func TestPluginValidate_Finding5_FullOutput_NotFound(t *testing.T) {
	chdirTemp(t)
	stdout, stderr, exitCode := execPluginValidateFull(t, ".")
	if exitCode != 1 {
		t.Errorf("exit code = %d, want 1", exitCode)
	}
	wantStdout := " x no plugin.json found in . (looked in plugin.json, .github/plugin/plugin.json, .claude-plugin/plugin.json, .cursor-plugin/plugin.json)\n"
	if stdout != wantStdout {
		t.Errorf("stdout = %q, want %q", stdout, wantStdout)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}
}

// TestPluginValidate_Finding5_StrictVsNonStrict_SameStdout compares
// --strict's stdout against the same invocation without it directly: the
// contract requires the printed counts (and everything else) to be
// unchanged, only the exit code differs.
func TestPluginValidate_Finding5_StrictVsNonStrict_SameStdout(t *testing.T) {
	dir := chdirTemp(t)
	writeManifest(t, dir, "plugin.json", `{"name":"x","descripton":"d"}`)

	nonStrictOut, err := execPluginValidate(t, dir)
	if err != nil {
		t.Fatalf("unexpected error without --strict: %v", err)
	}
	strictOut, strictErr := execPluginValidate(t, dir, "--strict")
	if strictErr == nil {
		t.Fatal("want --strict to fail on a warning")
	}
	if exitCodeOf(strictErr) != 1 {
		t.Errorf("exit code = %d, want 1", exitCodeOf(strictErr))
	}
	if nonStrictOut != strictOut {
		t.Errorf("--strict changed the printed output:\nnon-strict = %q\nstrict     = %q", nonStrictOut, strictOut)
	}
}

// TestPluginValidate_Finding5_FullCandidateOrder covers the full probe
// order end to end, not only root beating .claude-plugin: with all four
// candidates present, root wins; removing the winner in turn exposes each
// next candidate, down to .cursor-plugin/plugin.json alone.
func TestPluginValidate_Finding5_FullCandidateOrder(t *testing.T) {
	dir := chdirTemp(t)
	candidates := []struct {
		rel  string
		name string
	}{
		{"plugin.json", "root"},
		{".github/plugin/plugin.json", "github"},
		{".claude-plugin/plugin.json", "claude-plugin"},
		{".cursor-plugin/plugin.json", "cursor-plugin"},
	}
	for _, c := range candidates {
		writeManifest(t, dir, c.rel, fmt.Sprintf(`{"name":%q}`, c.name))
	}

	for i, c := range candidates {
		out, err := execPluginValidate(t, dir)
		if err != nil {
			t.Fatalf("round %d (%s should win): unexpected error: %v (output: %s)", i, c.name, err, out)
		}
		wantFirstLine := fmt.Sprintf(" > Validating plugin '%s'...", filepath.ToSlash(c.rel))
		firstLine := strings.SplitN(out, "\n", 2)[0]
		if firstLine != wantFirstLine {
			t.Errorf("round %d: first line = %q, want %q", i, firstLine, wantFirstLine)
		}
		if err := os.Remove(filepath.Join(dir, c.rel)); err != nil {
			t.Fatal(err)
		}
	}
}

// ── WP03 round-2 review: findings 1, 2, 3 ───────────────────────────────

// requireFIFOSupport skips the calling test only if this host cannot
// produce a FIFO that Go's own os.Lstat recognizes as one -- mirroring
// requireSymlinkSupport's capability-probe style rather than an
// unconditional platform skip. mkfifo succeeding is not sufficient proof
// by itself: an MSYS/Git-Bash mkfifo.exe found on PATH creates something
// Go's native, Win32-backed os.Lstat cannot see at all (confirmed by hand:
// GetFileAttributesEx reports "cannot find the file"), which is exactly
// why TestPluginValidate_Finding2_DirectFIFOArgumentRejectedWithoutBlocking
// above probes with a GOOS check instead -- this probe checks the
// resulting Mode() bit through Go's own API so the skip reason is the
// actual missing capability, not an assumption about the OS name.
func requireFIFOSupport(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	fifoPath := filepath.Join(dir, "probe-fifo")
	if err := exec.Command("mkfifo", fifoPath).Run(); err != nil {
		t.Skipf("mkfifo unavailable on this host (%v)", err)
	}
	info, err := os.Lstat(fifoPath)
	if err != nil || info.Mode()&os.ModeNamedPipe == 0 {
		t.Skip("mkfifo produced something Go's os.Lstat does not recognize as a named pipe on this host")
	}
}

// TestPluginValidate_Finding1_FIFOSwappedInDuringOpenWindowDoesNotBlock
// covers the round-2 review's check-then-open race: readAndValidate's
// pre-open Lstat sees an ordinary regular file, then -- simulating the
// race window between that Lstat and the real open -- the path is removed
// and replaced with a FIFO that has no writer attached, before
// openManifestFile actually opens it. A plain os.Open blocks indefinitely
// reading a FIFO with no writer; the fix's O_NONBLOCK on unix must make
// this open return promptly instead of hanging, so the goroutine below
// finishes well inside the timeout regardless of the outcome. Gated by
// requireFIFOSupport (an actual capability probe), not an unconditional
// platform skip.
func TestPluginValidate_Finding1_FIFOSwappedInDuringOpenWindowDoesNotBlock(t *testing.T) {
	requireFIFOSupport(t)
	dir := t.TempDir()
	writeManifest(t, dir, "plugin.json", `{"name":"x"}`)
	path := filepath.Join(dir, "plugin.json")

	orig := openRootFile
	t.Cleanup(func() { openRootFile = orig })
	openRootFile = func(root *os.Root, rel string) (manifestFile, os.FileInfo, error) {
		if err := os.Remove(path); err != nil {
			return nil, nil, err
		}
		if err := exec.Command("mkfifo", path).Run(); err != nil {
			return nil, nil, err
		}
		return orig(root, rel)
	}

	done := make(chan error, 1)
	go func() {
		_, err := readAndValidate(path, "")
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("want the swapped-in FIFO to be refused, not read")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("readAndValidate blocked opening a FIFO swapped in during the check-then-open race window")
	}
}

// TestPluginValidate_Finding1_DirectoryProbedFIFOSwapDoesNotBlock is the
// round-4 review's counterpart to the test above for the OTHER branch: a
// directory-probed candidate (readManifestInRoot). Before the round-4 fix,
// that branch opened through root.Open -- plain O_RDONLY, no O_NONBLOCK --
// so a candidate swapped to a FIFO with no writer after root.Lstat's
// containment check would block that Open forever, before Fstat and
// os.SameFile ever got a chance to run. The test above cannot catch this:
// it calls readAndValidate with an empty boundary, which never reaches
// readManifestInRoot at all. Gated by requireFIFOSupport like the test
// above.
func TestPluginValidate_Finding1_DirectoryProbedFIFOSwapDoesNotBlock(t *testing.T) {
	requireFIFOSupport(t)
	dir := t.TempDir()
	writeManifest(t, dir, "plugin.json", `{"name":"x"}`)
	path := filepath.Join(dir, "plugin.json")

	orig := openRootFile
	t.Cleanup(func() { openRootFile = orig })
	openRootFile = func(root *os.Root, rel string) (manifestFile, os.FileInfo, error) {
		if err := os.Remove(path); err != nil {
			return nil, nil, err
		}
		if err := exec.Command("mkfifo", path).Run(); err != nil {
			return nil, nil, err
		}
		return orig(root, rel)
	}

	done := make(chan error, 1)
	go func() {
		_, err := readAndValidate(path, dir)
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("want the swapped-in FIFO to be refused, not read")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("readManifestInRoot blocked opening a FIFO swapped in during the check-then-open race window")
	}
}

// failReader always errors on Read without ever succeeding, so a caller
// that reaches Read at all -- rather than refusing beforehand from Stat
// alone -- fails visibly and differently from the size-cap message.
type failReader struct{}

func (failReader) Read([]byte) (int, error) {
	return 0, fmt.Errorf("read must not be called: the pre-read size check should have refused first")
}
func (failReader) Close() error { return nil }

// TestPluginValidate_Finding2_PreReadSizeCapRefusesWithoutReading covers
// the round-2 review's finding that the old oversized-file test could not
// tell the two size guards apart: it was exactly cap+1 bytes, so removing
// readAndValidate's pre-read Stat check (openInfo.Size() > maxManifestBytes)
// still produced the identical message via the bounded io.LimitReader
// read. Here Stat genuinely reports a size over the cap -- the fixture
// file is truncated to cap+1 bytes in place, so os.SameFile still
// recognizes it as the same file the pre-open Lstat saw -- while any Read
// call fails outright, so the ONLY way to pass is to refuse before ever
// calling Read. This proves the two guards independently:
//   - deleting the pre-read Stat check breaks this test (Read gets called,
//     fails with failReader's own message, not the size-cap message).
//   - deleting the bounded-read guard
//     (TestPluginValidate_Finding3_BoundedReadCatchesGrowthAfterStatCheck)
//     does not affect this test at all, since the Stat check above refuses
//     before that code path is ever reached.
func TestPluginValidate_Finding2_PreReadSizeCapRefusesWithoutReading(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, "plugin.json", `{"name":"x"}`)
	path := filepath.Join(dir, "plugin.json")

	orig := openRootFile
	t.Cleanup(func() { openRootFile = orig })
	openRootFile = func(root *os.Root, rel string) (manifestFile, os.FileInfo, error) {
		if err := os.Truncate(path, maxManifestBytes+1); err != nil {
			return nil, nil, err
		}
		info, err := os.Stat(path)
		if err != nil {
			return nil, nil, err
		}
		return failReader{}, info, nil
	}

	out, err := execPluginValidate(t, path)
	if err == nil {
		t.Fatal("want the pre-read size check to refuse an oversized Stat report before any Read")
	}
	want := fmt.Sprintf("could not read '%s': file exceeds 5 MiB cap (%d bytes)", filepath.ToSlash(path), maxManifestBytes+1)
	if !strings.Contains(out, want) {
		t.Errorf("output = %q, want %q (the pre-read check's message, not failReader's)", out, want)
	}
}

// findAlternateVolume returns a drive root (e.g. "E:\\") that exists on
// this host and is not cwd's own volume, or skips the calling test with a
// stated reason when no second volume exists: the cross-volume fallback
// below cannot be exercised without one.
func findAlternateVolume(t *testing.T, cwd string) string {
	t.Helper()
	excludeVol := strings.ToUpper(filepath.VolumeName(cwd))
	for _, letter := range "CDEFGHIJKLMNOPQRSTUVWXYZ" {
		root := string(letter) + `:\`
		if strings.ToUpper(filepath.VolumeName(root)) == excludeVol {
			continue
		}
		if _, err := os.Stat(root); err == nil {
			return root
		}
	}
	t.Skip("no second volume available on this host; the cross-volume fallback cannot be exercised")
	return ""
}

// TestPluginValidate_Finding3_CrossVolumeFallbackToCleanedAbsolutePath
// covers the contract amendment recorded in kitty-specs/plugin-manifest-
// validate-01M21E5Q/contracts/cli-plugin-validate.md (commit 5097ad2):
// filepath.Rel fails whenever manifestPath and the working directory sit
// on different windows volumes, and the contract's only sanctioned
// fallback is the CLEANED absolute path, still slash-converted -- not the
// raw, possibly-uncleaned absolute path the pre-fix code returned as-is.
// manifestPath is built with a redundant "foo\..\foo" segment, bypassing
// filepath.Join's own cleaning, specifically so an uncleaned fallback and a
// cleaned one produce distinguishable output. Skipped with a stated reason
// on a non-windows host (filepath.Rel cannot fail this way there) or when
// this host exposes only one drive letter.
func TestPluginValidate_Finding3_CrossVolumeFallbackToCleanedAbsolutePath(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("cross-volume paths are a windows-only concept; filepath.Rel cannot fail this way on unix")
	}
	cwd := chdirTemp(t)
	altRoot := findAlternateVolume(t, cwd)

	manifestPath := altRoot + `foo\..\foo\plugin.json`
	got := displayManifestPath(manifestPath)
	want := filepath.ToSlash(filepath.Clean(manifestPath))
	if got != want {
		t.Errorf("displayManifestPath(%q) = %q, want the cleaned absolute path %q", manifestPath, got, want)
	}
	if strings.Contains(got, "..") {
		t.Errorf("got = %q, must not leak the uncleaned '..' segment", got)
	}
}

// ── WP03 round-3 review: HIGH -- parent-directory symlink race ──────────

// TestPluginValidate_Finding_ParentSymlinkRaceAcrossLookups is the
// counterexample for the still-open HIGH finding: readAndValidate's Lstat,
// containment check (pathStaysWithin), and Open are three INDEPENDENT path
// lookups, each re-walking dir/.claude-plugin from scratch. A parent link
// that is OUTSIDE projectRoot during the Lstat, flips INSIDE for
// pathStaysWithin's own EvalSymlinks call, then flips back to the SAME
// outside target before Open, can pass the containment check (which
// observes "inside") and the os.SameFile comparison (checkInfo and openInfo
// are the identical outside file both times) without the escaping state
// ever being the one pathStaysWithin evaluated.
//
// A background goroutine drives the flip continuously (Remove+Symlink,
// not claimed atomic) while the foreground repeatedly calls readAndValidate
// for up to raceWindow -- this does not depend on hitting one exact instant,
// only on the window being hit at all within that time. The two candidate
// targets are distinguishable through the returned Report alone (an
// Unrecognized-field marker only the outside file carries), independent of
// readAndValidate's error return: that error reports only a read-boundary
// failure, not a validation outcome, so a nil error is unremarkable by
// itself -- reading the legitimately-inside target is supposed to succeed.
// Only a nil error whose Report carries the outside marker proves content
// from outside projectRoot was read after a containment check that had
// reported "inside".
func TestPluginValidate_Finding_ParentSymlinkRaceAcrossLookups(t *testing.T) {
	requireSymlinkSupport(t)

	const outsideMarkerField = "zzz-outside-marker"

	outside := t.TempDir()
	writeManifest(t, outside, "plugin.json", fmt.Sprintf(`{"name":"x","%s":"1"}`, outsideMarkerField))

	projectRoot := t.TempDir()
	insideTarget := filepath.Join(projectRoot, "safe-inside")
	writeManifest(t, insideTarget, "plugin.json", `{"name":"x"}`)

	link := filepath.Join(projectRoot, ".claude-plugin")
	manifestPath := filepath.Join(link, "plugin.json")

	flip := func(target string) {
		_ = os.Remove(link)
		_ = os.Symlink(target, link)
	}
	flip(outside)

	stop := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		for {
			select {
			case <-stop:
				return
			default:
			}
			flip(insideTarget)
			flip(outside)
		}
	}()
	t.Cleanup(func() {
		close(stop)
		<-stopped
	})

	const raceWindow = 5 * time.Second
	deadline := time.Now().Add(raceWindow)
	for time.Now().Before(deadline) {
		report, err := readAndValidate(manifestPath, projectRoot)
		if err != nil {
			continue
		}
		for _, f := range report.Findings {
			if strings.Contains(f.Message, outsideMarkerField) {
				t.Fatalf("read outside projectRoot's content after the containment check reported \"inside\": %+v", report.Findings)
			}
		}
	}
}
