package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func TestPluginValidate_US3_AS3_OversizedFile(t *testing.T) {
	dir := t.TempDir()
	big := make([]byte, 5*1024*1024+1)
	for i := range big {
		big[i] = ' '
	}
	writeManifest(t, dir, "plugin.json", string(big))
	out, err := execPluginValidate(t, dir)
	if err == nil {
		t.Fatal("want Structure error for oversized file")
	}
	if !strings.Contains(out, fmt.Sprintf("Structure: file exceeds 5 MiB cap (%d bytes)", len(big))) {
		t.Errorf("output = %q, want the size-cap message", out)
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
