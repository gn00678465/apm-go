package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/pluginjson"
)

func runPluginInit(t *testing.T, args ...string) error {
	t.Helper()
	cmd := pluginInitCmd()
	cmd.SetArgs(args)
	return cmd.Execute()
}

func TestPluginInit_AgentFormat_WritesPluginAndMCPJSON(t *testing.T) {
	dir := chdirTemp(t)
	if err := runPluginInit(t, "my-plugin", "--yes", "--format", "agent-plugin"); err != nil {
		t.Fatalf("plugin init --format agent-plugin: %v", err)
	}
	for _, f := range []string{"apm.yml", "plugin.json", "mcp.json"} {
		if _, err := os.Stat(filepath.Join(dir, "my-plugin", f)); err != nil {
			t.Errorf("expected %s to exist: %v", f, err)
		}
	}
	// Upstream commands/init.py:417 + _helpers.py:649-660: sort_keys=True
	// for mcp.json, insertion order for plugin.json.
	mcp, _ := os.ReadFile(filepath.Join(dir, "my-plugin", "mcp.json"))
	wantMCP := "{\n  \"$schema\": \"https://agent-plugins.org/schemas/1.0.0/mcp.schema.json\",\n  \"mcpServers\": {}\n}\n"
	if string(mcp) != wantMCP {
		t.Errorf("mcp.json =\n%s\nwant\n%s", mcp, wantMCP)
	}
	pj, _ := os.ReadFile(filepath.Join(dir, "my-plugin", "plugin.json"))
	for _, want := range []string{
		`"$schema": "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json"`,
		`"extensions": {`,
		`"com.microsoft.apm": {`,
		`"schemaVersion": "1"`,
	} {
		if !strings.Contains(string(pj), want) {
			t.Errorf("agent plugin.json missing %s:\n%s", want, pj)
		}
	}
	if !strings.HasPrefix(string(pj), "{\n  \"$schema\"") {
		t.Errorf("$schema must be the first key:\n%s", pj)
	}
}

func TestPluginInit_ClaudeFormat_NoMCPJSON_MatchesDefault(t *testing.T) {
	dir := chdirTemp(t)
	if err := runPluginInit(t, "a", "--yes", "--format", "claude-plugin"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "a", "mcp.json")); !os.IsNotExist(err) {
		t.Errorf("claude format must not write mcp.json")
	}

	// --claude-plugin and no-flag produce byte-identical plugin.json.
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	if err := runPluginInit(t, "b", "--yes", "--claude-plugin"); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	if err := runPluginInit(t, "c", "--yes"); err != nil {
		t.Fatal(err)
	}
	pb, _ := os.ReadFile(filepath.Join(dir, "b", "plugin.json"))
	pc, _ := os.ReadFile(filepath.Join(dir, "c", "plugin.json"))
	pb = bytes.ReplaceAll(pb, []byte(`"b"`), []byte(`"X"`))
	pb = bytes.ReplaceAll(pb, []byte(`for b"`), []byte(`for X"`))
	pc = bytes.ReplaceAll(pc, []byte(`"c"`), []byte(`"X"`))
	pc = bytes.ReplaceAll(pc, []byte(`for c"`), []byte(`for X"`))
	if !bytes.Equal(pb, pc) {
		t.Errorf("--claude-plugin and default plugin.json differ:\n%s\n---\n%s", pb, pc)
	}
}

func TestPluginInit_ConflictingSelectors_Exit2_NoFiles(t *testing.T) {
	dir := chdirTemp(t)
	err := runPluginInit(t, "p", "--yes", "--format", "agent-plugin", "--claude-plugin")
	if err == nil {
		t.Fatal("want usage error")
	}
	if code := exitCodeOf(err); code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if !strings.Contains(err.Error(), "--format agent-plugin, --claude-plugin") {
		t.Errorf("error = %q", err.Error())
	}
	if _, statErr := os.Stat(filepath.Join(dir, "p")); !os.IsNotExist(statErr) {
		t.Errorf("usage error must not create project directory")
	}
}

// Round-2 F1: Click's Choice validates BEFORE resolve_bundle_format runs,
// so the user-visible text for any non-alias value (including "apm") is
// Click's, and a missing argument is Click's "requires an argument"
// usage error -- both exit 2.
func TestPluginInit_UnknownFormat_Exit2_ClickWording(t *testing.T) {
	chdirTemp(t)
	for _, bad := range []string{"apm", "nope"} {
		err := runPluginInit(t, "p", "--yes", "--format", bad)
		if exitCodeOf(err) != 2 {
			t.Fatalf("--format %s: want exit 2, got err=%v", bad, err)
		}
		want := "Invalid value for '--format': '" + bad + "' is not one of 'plugin', 'agent-plugin', 'claude', 'claude-plugin'."
		if err.Error() != want {
			t.Errorf("--format %s:\n got %q\nwant %q", bad, err.Error(), want)
		}
	}
}

func TestPluginInit_FormatMissingArgument_Exit2(t *testing.T) {
	dir := chdirTemp(t)
	err := runPluginInit(t, "p", "--yes", "--format")
	if exitCodeOf(err) != 2 {
		t.Fatalf("want exit 2, got err=%v", err)
	}
	if err.Error() != "Option '--format' requires an argument." {
		t.Errorf("got %q", err.Error())
	}
	if _, statErr := os.Stat(filepath.Join(dir, "p")); !os.IsNotExist(statErr) {
		t.Error("usage error must not create project directory")
	}
}

func TestPluginInit_Help_ListsFormatFlags(t *testing.T) {
	cmd := pluginInitCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	// Finding 2 (F01): Click renders a Choice as
	// "--format [plugin|agent-plugin|claude|claude-plugin]" and the help
	// text ends at "default." with no extra "One of:" suffix.
	for _, want := range []string{
		"--format [plugin|agent-plugin|claude|claude-plugin]",
		"'plugin', 'claude', and 'claude-plugin' select the current Claude-compatible default.",
		"--claude-plugin",
		"Scaffold the legacy Claude-compatible layout (current no-flag default)",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("help missing %q:\n%s", want, out.String())
		}
	}
	for _, reject := range []string{"--format string", "One of:"} {
		if strings.Contains(out.String(), reject) {
			t.Errorf("help must not contain %q:\n%s", reject, out.String())
		}
	}
}

// Upstream commands/init.py:195-215: every generated file that already
// exists is listed in the overwrite notice; in agent mode that set
// includes mcp.json.
func TestPluginInit_AgentFormat_ExistingMCPJSON_ListedInOverwriteNotice(t *testing.T) {
	dir := chdirTemp(t)
	proj := filepath.Join(dir, "my-plugin")
	if err := os.Mkdir(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"apm.yml", "mcp.json"} {
		if err := os.WriteFile(filepath.Join(proj, f), []byte("{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out := captureStderr(t, func() {
		if err := runPluginInit(t, "my-plugin", "--yes", "--format", "agent-plugin"); err != nil {
			t.Errorf("plugin init: %v", err)
		}
	})
	if !strings.Contains(out, "apm.yml, mcp.json") {
		t.Errorf("overwrite notice should list apm.yml and mcp.json, got:\n%s", out)
	}
}

// Finding 4 (F09): upstream commands/init.py:205-215 gates the overwrite
// notice/prompt on ANY generated file existing, not just apm.yml.
func TestPluginInit_PluginJSONOnly_StillWarnsAndRequiresYes(t *testing.T) {
	dir := chdirTemp(t)
	proj := filepath.Join(dir, "my-plugin")
	if err := os.Mkdir(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, "plugin.json"), []byte("OLD"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Non-interactive without --yes must refuse, leaving plugin.json intact.
	err := runPluginInit(t, "my-plugin", "--format", "claude-plugin")
	if err == nil {
		t.Fatal("want refusal without --yes")
	}
	if got, _ := os.ReadFile(filepath.Join(proj, "plugin.json")); string(got) != "OLD" {
		t.Fatalf("plugin.json was overwritten without consent: %q", got)
	}

	// --yes overwrites and names the file in the notice.
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	out := captureStderr(t, func() {
		if err := runPluginInit(t, "my-plugin", "--yes", "--format", "claude-plugin"); err != nil {
			t.Errorf("plugin init --yes: %v", err)
		}
	})
	// Round-2 F4: upstream prints the warning line first
	// (commands/init.py:205-209), THEN the --yes overwrite progress line.
	warnIdx := strings.Index(out, "Generated files already exist: plugin.json")
	infoIdx := strings.Index(out, "--yes specified, overwriting: plugin.json")
	if warnIdx < 0 || infoIdx < 0 || warnIdx > infoIdx {
		t.Errorf("want warning line before overwrite line:\n%s", out)
	}
	if got, _ := os.ReadFile(filepath.Join(proj, "plugin.json")); string(got) == "OLD" {
		t.Error("--yes should have overwritten plugin.json")
	}
}

// Finding 5 (F03/F09): upstream commands/init.py:407-444 stages the agent
// scaffold and commits all files at once, restoring prior files on failure.
// Simulate a commit failure by making the project dir read-only AFTER
// staging (via a seam on the scaffold commit), so backup-then-commit has
// something to roll back.
func TestPluginInit_AgentFormat_CommitFailure_RestoresPriorFiles(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	dir := chdirTemp(t)
	proj := filepath.Join(dir, "my-plugin")
	if err := os.Mkdir(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	prior := map[string]string{
		"apm.yml":     "name: keep-me\n",
		"plugin.json": "OLD-PLUGIN",
	}
	for f, c := range prior {
		if err := os.WriteFile(filepath.Join(proj, f), []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Fail the second commit step (plugin.json) so apm.yml is already
	// committed and must be rolled back.
	restore := pluginjson.SetCommitHookForTest(func(name string) error {
		if name == "plugin.json" {
			return os.ErrPermission
		}
		return nil
	})
	defer restore()

	err := runPluginInit(t, "my-plugin", "--yes", "--format", "agent-plugin")
	if err == nil {
		t.Fatal("want commit failure")
	}
	for f, want := range prior {
		got, _ := os.ReadFile(filepath.Join(proj, f))
		if string(got) != want {
			t.Errorf("%s must be restored, got %q", f, got)
		}
	}
	if _, statErr := os.Stat(filepath.Join(proj, "mcp.json")); !os.IsNotExist(statErr) {
		t.Error("mcp.json must not be left behind on failure")
	}
	entries, _ := os.ReadDir(proj)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".apm-plugin-init-") {
			t.Errorf("staging dir %s leaked", e.Name())
		}
	}
}

// Finding 1 (F01/F08): Click rejects an explicit empty --format= (exit 2, no
// files); Target must not treat it as "flag absent".
func TestPluginInit_ExplicitEmptyFormat_Exit2_NoFiles(t *testing.T) {
	dir := chdirTemp(t)
	err := runPluginInit(t, "p", "--yes", "--format=")
	if exitCodeOf(err) != 2 {
		t.Fatalf("want exit 2, got err=%v", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "p")); !os.IsNotExist(statErr) {
		t.Error("usage error must not create project directory")
	}
}

// Finding 3 (F10/F01): upstream prints the same plugin-author next steps
// for both modes (commands/init.py:322-327). Every command those steps name
// must actually exist in this binary -- no dead `pack --format` until pack
// grows that flag (spec Out of Scope).
func TestPluginInit_NextSteps_SameForBothModes_AndRunnable(t *testing.T) {
	if pluginMode.nextSteps == nil || len(pluginMode.nextSteps) != len(agentPluginMode.nextSteps) {
		t.Fatalf("next steps differ in length: %v vs %v", pluginMode.nextSteps, agentPluginMode.nextSteps)
	}
	for i := range pluginMode.nextSteps {
		if pluginMode.nextSteps[i] != agentPluginMode.nextSteps[i] {
			t.Errorf("step %d differs:\n  claude: %s\n  agent:  %s", i, pluginMode.nextSteps[i], agentPluginMode.nextSteps[i])
		}
	}
	root := pluginCmd()
	root.AddCommand(packCmd(), installCmd())
	for _, step := range pluginMode.nextSteps {
		// "<label>:   apm-go <sub> <flags...>" -> check every --flag exists on <sub>.
		idx := strings.Index(step, "apm-go ")
		if idx < 0 {
			t.Errorf("step does not name an apm-go command: %q", step)
			continue
		}
		fields := strings.Fields(step[idx+len("apm-go "):])
		sub, _, err := root.Find(fields[:1])
		if err != nil || sub == nil || sub == root {
			t.Errorf("step names unknown subcommand %q: %q", fields[0], step)
			continue
		}
		for _, f := range fields[1:] {
			if !strings.HasPrefix(f, "--") {
				continue
			}
			if sub.Flags().Lookup(strings.TrimPrefix(f, "--")) == nil {
				t.Errorf("step names flag %s that `apm-go %s` does not have: %q", f, fields[0], step)
			}
		}
	}
}

// Round-2 F5: a rollback that itself fails must surface (errors.Join), not
// be swallowed -- and the backup must survive so the user can recover.
func TestPluginInit_AgentFormat_RollbackFailure_IsReported_BackupKept(t *testing.T) {
	dir := chdirTemp(t)
	proj := filepath.Join(dir, "my-plugin")
	if err := os.Mkdir(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	prior := "name: keep-me\n"
	if err := os.WriteFile(filepath.Join(proj, "apm.yml"), []byte(prior), 0o644); err != nil {
		t.Fatal(err)
	}

	restore := pluginjson.SetCommitHookForTest(func(name string) error {
		if name == "plugin.json" {
			// apm.yml is already committed; simulate an external race that
			// makes both rollback steps for it fail (non-empty dir in the way).
			os.Remove(filepath.Join(proj, "apm.yml"))
			os.MkdirAll(filepath.Join(proj, "apm.yml", "sentinel"), 0o755)
			return errors.New("injected commit failure")
		}
		return nil
	})
	defer restore()

	err := runPluginInit(t, "my-plugin", "--yes", "--format", "agent-plugin")
	if err == nil {
		t.Fatal("want failure")
	}
	msg := err.Error()
	if !strings.Contains(msg, "injected commit failure") || !strings.Contains(msg, "rollback") {
		t.Errorf("error must report both the commit failure and the rollback failure, got: %v", err)
	}
	// The prior apm.yml bytes must still exist somewhere under the project
	// (the kept backup) so nothing is lost.
	var found bool
	filepath.Walk(proj, func(p string, info os.FileInfo, _ error) error {
		if info != nil && !info.IsDir() {
			if b, _ := os.ReadFile(p); string(b) == prior {
				found = true
			}
		}
		return nil
	})
	if !found {
		t.Error("prior apm.yml bytes were lost; backup must be preserved when rollback fails")
	}
}
