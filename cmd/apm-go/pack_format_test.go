// Tests for pack.go's --format/--claude-plugin bundle-format selector
// (ticket 07): the shared resolver from bundle_format.go (ticket 04) wired
// onto `apm-go pack`, resolution happening before any write, and the
// embedded lockfile's pack.format field carrying the resolved canonical
// value.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// ── flag surface ──────────────────────────────────────────────────────────

func TestPackCmd_FormatFlagsWired(t *testing.T) {
	cmd := packCmd()
	if cmd.Flags().Lookup("format") == nil {
		t.Error("pack is missing --format")
	}
	if cmd.Flags().Lookup("claude-plugin") == nil {
		t.Error("pack is missing --claude-plugin")
	}
}

func TestPackCmd_Help_ListsFormatFlags(t *testing.T) {
	out, err := runPackCmd(t, "--help")
	if err != nil {
		t.Fatalf("pack --help returned error: %v", err)
	}
	for _, want := range []string{
		"--format [plugin|agent-plugin|claude|claude-plugin|apm]",
		"apm-go currently implements only the Claude plugin bundle; agent-plugin and apm are accepted but refused.",
		"--claude-plugin",
		"Select the legacy Claude plugin bundle output (current no-flag default).",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("help missing %q:\n%s", want, out)
		}
	}
}

// ── conflict / unknown / empty / missing-argument: same as `plugin init` ──

func TestPackCmd_FormatConflict_Exit2(t *testing.T) {
	chdirTemp(t)
	_, err := runPackCmd(t, "--format", "agent-plugin", "--claude-plugin")
	if exitCodeOf(err) != 2 {
		t.Fatalf("want exit 2, got err=%v", err)
	}
	want := "Choose one bundle format selector; received: --format agent-plugin, --claude-plugin"
	if err.Error() != want {
		t.Errorf("err = %q, want %q", err.Error(), want)
	}
}

func TestPackCmd_FormatMissingArgument_Exit2(t *testing.T) {
	chdirTemp(t)
	_, err := runPackCmd(t, "--format")
	if exitCodeOf(err) != 2 {
		t.Fatalf("want exit 2, got err=%v", err)
	}
	if err.Error() != "Option '--format' requires an argument." {
		t.Errorf("got %q", err.Error())
	}
}

func TestPackCmd_ExplicitEmptyFormat_Exit2(t *testing.T) {
	chdirTemp(t)
	_, err := runPackCmd(t, "--format=")
	if exitCodeOf(err) != 2 {
		t.Fatalf("want exit 2, got err=%v", err)
	}
}

func TestPackCmd_UnknownFormat_Exit2_ClickWording(t *testing.T) {
	chdirTemp(t)
	_, err := runPackCmd(t, "--format", "nope")
	if exitCodeOf(err) != 2 {
		t.Fatalf("want exit 2, got err=%v", err)
	}
	want := "Invalid value for '--format': 'nope' is not one of 'plugin', 'agent-plugin', 'claude', 'claude-plugin', 'apm'."
	if err.Error() != want {
		t.Errorf("got %q, want %q", err.Error(), want)
	}
}

// ── refusal: agent-plugin/apm parse fine, then refuse, before any write ───

// treeSnapshot walks dir and returns a map of slash-relative path -> sha256
// hex digest, so a refused invocation can be proven to leave the tree
// byte-identical (AC: "resolution ... before ANY producer, lockfile, or
// output-dir write").
func treeSnapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	snap := map[string]string{}
	err := filepath.Walk(dir, func(p string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		rel, rerr := filepath.Rel(dir, p)
		if rerr != nil {
			return rerr
		}
		data, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		sum := sha256.Sum256(data)
		snap[filepath.ToSlash(rel)] = hex.EncodeToString(sum[:])
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	return snap
}

func assertTreeUnchanged(t *testing.T, dir string, before map[string]string) {
	t.Helper()
	after := treeSnapshot(t, dir)
	if len(before) != len(after) {
		t.Fatalf("tree changed: before had %d file(s), after has %d\nbefore=%v\nafter=%v", len(before), len(after), before, after)
	}
	for path, sum := range before {
		if after[path] != sum {
			t.Errorf("tree changed: %s hash before=%s after=%s", path, sum, after[path])
		}
	}
}

// writePackFormatFixture writes a project that WOULD build a real bundle if
// packing were allowed to proceed -- a dependencies: block plus local .apm/
// content -- so a refusal test proves nothing was written despite valid
// producer input being present.
func writePackFormatFixture(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, ".apm", "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".apm", "agents", "foo.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	writePackApmYML(t, "name: demo\nversion: 1.0.0\ndependencies:\n  apm:\n    - acme/tool\n")
}

func TestPackCmd_RefuseAgentPlugin_Exit2_NoWrite(t *testing.T) {
	dir := chdirTemp(t)
	writePackFormatFixture(t, dir)

	before := treeSnapshot(t, dir)
	_, err := runPackCmd(t, "--format", "agent-plugin")
	if err == nil {
		t.Fatal("want refusal for --format agent-plugin")
	}
	if exitCodeOf(err) != 2 {
		t.Errorf("exitCodeOf(err) = %d, want 2", exitCodeOf(err))
	}
	want := "bundle format 'agent-plugin' is not yet supported by apm-go; use --format claude-plugin"
	if err.Error() != want {
		t.Errorf("err = %q, want %q", err.Error(), want)
	}
	assertTreeUnchanged(t, dir, before)
}

func TestPackCmd_RefuseApm_Exit2_NoWrite(t *testing.T) {
	dir := chdirTemp(t)
	writePackFormatFixture(t, dir)

	before := treeSnapshot(t, dir)
	_, err := runPackCmd(t, "--format", "apm")
	if err == nil {
		t.Fatal("want refusal for --format apm")
	}
	if exitCodeOf(err) != 2 {
		t.Errorf("exitCodeOf(err) = %d, want 2", exitCodeOf(err))
	}
	want := "bundle format 'apm' is not yet supported by apm-go; use --format claude-plugin"
	if err.Error() != want {
		t.Errorf("err = %q, want %q", err.Error(), want)
	}
	assertTreeUnchanged(t, dir, before)
}

// ── Claude path: five selector spellings produce byte-identical bundles ───

var packFormatPackedAtRe = regexp.MustCompile(`packed_at: .*`)

// normalizedBundleTree runs `pack <extraArgs...>` against a fresh fixture
// (dependencies: block plus a local .apm/skills/ file and a minimal
// apm.lock.yaml, so the embedded-lockfile step also runs) and returns the
// produced bundle's file tree, each file's packed_at line normalised to
// "<TS>" so byte comparisons across invocations aren't defeated by the
// wall-clock timestamp alone.
func normalizedBundleTree(t *testing.T, extraArgs ...string) map[string]string {
	t.Helper()
	dir := chdirTemp(t)
	if err := os.MkdirAll(filepath.Join(dir, ".apm", "skills", "hello"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".apm", "skills", "hello", "SKILL.md"), []byte("---\nname: hello\n---\n\nHello.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writePackApmYML(t, "name: demo\nversion: 1.0.0\ndependencies:\n  apm: []\n  mcp: []\nincludes: auto\nscripts: {}\n")
	if err := os.WriteFile(filepath.Join(dir, "apm.lock.yaml"), []byte("lockfile_version: \"1\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := runPackCmd(t, extraArgs...)
	if err != nil {
		t.Fatalf("pack %v returned error: %v (output: %s)", extraArgs, err, out)
	}

	bundleDir := filepath.Join(dir, "build", "demo-1.0.0")
	tree := map[string]string{}
	walkErr := filepath.Walk(bundleDir, func(p string, info os.FileInfo, werr error) error {
		if werr != nil {
			return werr
		}
		if info.IsDir() {
			return nil
		}
		rel, rerr := filepath.Rel(bundleDir, p)
		if rerr != nil {
			return rerr
		}
		data, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		data = packFormatPackedAtRe.ReplaceAll(data, []byte("packed_at: <TS>"))
		tree[filepath.ToSlash(rel)] = string(data)
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk %s: %v", bundleDir, walkErr)
	}
	return tree
}

func TestPackCmd_FiveFormatSelectors_ByteIdenticalBundle(t *testing.T) {
	baseline := normalizedBundleTree(t)

	cases := map[string][]string{
		"format-plugin":        {"--format", "plugin"},
		"format-claude":        {"--format", "claude"},
		"format-claude-plugin": {"--format", "claude-plugin"},
		"claude-plugin-flag":   {"--claude-plugin"},
	}
	for name, args := range cases {
		got := normalizedBundleTree(t, args...)
		if len(got) != len(baseline) {
			t.Errorf("%s: file count = %d, want %d (got=%v, baseline=%v)", name, len(got), len(baseline), got, baseline)
			continue
		}
		for path, content := range baseline {
			if got[path] != content {
				t.Errorf("%s: %s differs from the no-flag baseline\ngot:\n%s\nwant:\n%s", name, path, got[path], content)
			}
		}
	}
}

// ── embedded lockfile carries pack.format: claude-plugin ──────────────────

func TestRunPack_LockfileEmbedsClaudePluginFormat(t *testing.T) {
	dir := chdirTemp(t)
	if err := os.WriteFile(filepath.Join(dir, "apm.lock.yaml"), []byte("lockfile_version: \"1\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".apm", "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".apm", "agents", "foo.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	writePackApmYML(t, "name: demo\nversion: 1.0.0\ndependencies:\n  apm:\n    - acme/tool\n")

	if _, err := runPackCmd(t); err != nil {
		t.Fatalf("pack returned error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "build", "demo-1.0.0", "apm.lock.yaml"))
	if err != nil {
		t.Fatalf("expected an embedded apm.lock.yaml: %v", err)
	}
	if !strings.Contains(string(data), "format: claude-plugin") {
		t.Errorf("embedded lockfile missing \"format: claude-plugin\":\n%s", data)
	}
}
