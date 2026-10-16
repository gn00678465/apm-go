package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// bootstrapProject makes root/<proj> the working directory, next to a local
// package root/pkg with one skill, and pins git's user.name lookup so the
// author of a created apm.yml is the Oracle's fallback, Developer.
func bootstrapProject(t *testing.T, proj string) string {
	t.Helper()
	root := t.TempDir()
	writeCompileFile(t, root, "pkg/apm.yml", "name: pkg\nversion: 1.0.0\n")
	writeCompileFile(t, root, "pkg/.apm/skills/sk/SKILL.md", "---\nname: sk\ndescription: d\n---\nx\n")
	writeCompileFile(t, root, "gitconfig", "")
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(root, "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("HOME", filepath.Join(root, "home"))
	if err := os.MkdirAll(filepath.Join(root, proj), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(filepath.Join(root, proj))
	return root
}

func readFileString(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func assertNoFile(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Errorf("%s exists after the run (err %v), want it absent", path, err)
	}
}

// oracleBootstrapTargets is the apm.yml Oracle 0.33.0 writes for
// `install ../pkg --target codex,claude` in a directory named projA with no
// apm.yml (or-p/projA/apm.yml).
const oracleBootstrapTargets = "name: projA\n" +
	"version: 1.0.0\n" +
	"description: APM project for projA\n" +
	"author: Developer\n" +
	"# Which agent platforms to deploy to.\n" +
	"# Resolution order: --target flag > this field > auto-detect from filesystem.\n" +
	"# Accepted values: agent-skills, antigravity, claude, codex, copilot, cursor, gemini, grok-build, hermes, kiro, opencode, vscode, windsurf\n" +
	"targets:\n" +
	"  - claude\n" +
	"  - codex\n" +
	"dependencies:\n" +
	"  apm:\n" +
	"    - ../pkg\n" +
	"  mcp: []\n" +
	"includes: auto\n" +
	"scripts: {}\n"

// TestInstall_NoManifest_CreatesItWithTargets: the issue's adjacent case. As
// in Oracle 0.33.0 (or-p.out, projA) the install creates apm.yml with the
// --target value, says so before validating, and a later install with no
// flag deploys to those targets.
func TestInstall_NoManifest_CreatesItWithTargets(t *testing.T) {
	root := bootstrapProject(t, "projA")

	stdout, _, code := runCompileCLI(t, "install", "../pkg", "--target", "codex,claude")
	if code != 0 {
		t.Fatalf("exit %d, stdout:\n%s", code, stdout)
	}
	wantHead := " + Created apm.yml\n i Targets set: claude, codex (persisted to apm.yml)\n + Validating 1 package...\n"
	if !strings.HasPrefix(stdout, wantHead) {
		t.Errorf("stdout:\n%s\nwant it to start with:\n%s", stdout, wantHead)
	}
	proj := filepath.Join(root, "projA")
	if got := readFileString(t, filepath.Join(proj, "apm.yml")); got != oracleBootstrapTargets {
		t.Errorf("apm.yml:\n%s\nwant:\n%s", got, oracleBootstrapTargets)
	}

	for _, dir := range []string{".claude", ".agents"} {
		if err := os.RemoveAll(filepath.Join(proj, dir)); err != nil {
			t.Fatal(err)
		}
	}
	stdout, _, code = runCompileCLI(t, "install")
	if code != 0 {
		t.Fatalf("second install: exit %d, stdout:\n%s", code, stdout)
	}
	for _, f := range []string{".claude/skills/sk/SKILL.md", ".agents/skills/sk/SKILL.md"} {
		if _, err := os.Stat(filepath.Join(proj, f)); err != nil {
			t.Errorf("second install did not deploy %s: %v", f, err)
		}
	}
}

// TestManifestTargetsFromTargetFlag: what an install that creates apm.yml
// writes under targets: for a --target value, measured with Oracle 0.33.0's
// manifest_targets_from_target_option (core/target_detection.py:481-499).
func TestManifestTargetsFromTargetFlag(t *testing.T) {
	all := []string{"claude", "codex", "copilot", "cursor", "gemini", "grok-build", "kiro", "opencode", "windsurf"}
	for _, tt := range []struct {
		flag string
		want []string
	}{
		{"", nil},
		{"claude", []string{"claude"}},
		{"codex,claude", []string{"claude", "codex"}},
		{"claude, codex", []string{"claude", "codex"}},
		{"vscode", []string{"copilot"}},
		{"agents", []string{"copilot"}},
		{"copilot,vscode", []string{"copilot"}},
		{"agy", nil},
		{"antigravity", []string{"antigravity"}},
		{"agent-skills", []string{"agent-skills"}},
		{"cursor", []string{"cursor"}},
		{"gemini", []string{"gemini"}},
		{"windsurf", []string{"windsurf"}},
		{"kiro", []string{"kiro"}},
		{"opencode", []string{"opencode"}},
		{"grok-build", []string{"grok-build"}},
		{"all", all},
		{"x-acme-foo", nil},
	} {
		if got := manifestTargetsFromTargetFlag(tt.flag); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("manifestTargetsFromTargetFlag(%q) = %v, want %v", tt.flag, got, tt.want)
		}
	}
}

// TestInstall_NoManifest_AutoDetect_CommentedSkeleton: with no --target the
// created apm.yml has the commented targets skeleton and no Targets set
// line (Oracle 0.33.0, or-p/projC/apm.yml).
func TestInstall_NoManifest_AutoDetect_CommentedSkeleton(t *testing.T) {
	root := bootstrapProject(t, "projC")
	writeCompileFile(t, root, "projC/.claude/keep", "")

	stdout, _, code := runCompileCLI(t, "install", "../pkg")
	if code != 0 {
		t.Fatalf("exit %d, stdout:\n%s", code, stdout)
	}
	wantHead := " + Created apm.yml\n + Validating 1 package...\n"
	if !strings.HasPrefix(stdout, wantHead) {
		t.Errorf("stdout:\n%s\nwant it to start with:\n%s", stdout, wantHead)
	}
	const want = "name: projC\n" +
		"version: 1.0.0\n" +
		"description: APM project for projC\n" +
		"author: Developer\n" +
		"# Which agent platforms to deploy to (uncomment to pin):\n" +
		"# targets:\n" +
		"#   - copilot\n" +
		"#   - claude\n" +
		"\n" +
		"dependencies:\n" +
		"  apm:\n" +
		"    - ../pkg\n" +
		"  mcp: []\n" +
		"includes: auto\n" +
		"scripts: {}\n"
	if got := readFileString(t, filepath.Join(root, "projC", "apm.yml")); got != want {
		t.Errorf("apm.yml:\n%s\nwant:\n%s", got, want)
	}
}

// TestInstall_NoManifest_FailureRemovesIt: an install that created apm.yml
// and then fails removes it and says so before the error, as Oracle 0.33.0
// does (or-p.out, projE: a path that does not exist; projB: no target).
func TestInstall_NoManifest_FailureRemovesIt(t *testing.T) {
	for _, tt := range []struct {
		name     string
		args     []string
		wantCode int
	}{
		{"missing package path", []string{"install", "../nope", "--target", "claude"}, 1},
		{"no deployment target", []string{"install", "../pkg"}, 2},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := bootstrapProject(t, "proj")

			stdout, _, code := runCompileCLI(t, tt.args...)
			if code != tt.wantCode {
				t.Errorf("exit %d, want %d; stdout:\n%s", code, tt.wantCode, stdout)
			}
			removed := strings.Index(stdout, " i Removed apm.yml created by the failed install.\n")
			failed := strings.Index(stdout, " x ")
			if removed < 0 || failed < 0 || removed > failed {
				t.Errorf("stdout:\n%s\nwant the Removed line before the first error line", stdout)
			}
			assertNoFile(t, filepath.Join(root, "proj", "apm.yml"))
		})
	}
}

// TestInstall_ExistingManifest_TargetFlagNotPersisted: an apm.yml that
// exists, with no targets:, gets none from --target (Oracle 0.33.0,
// or-p/projD/apm.yml).
func TestInstall_ExistingManifest_TargetFlagNotPersisted(t *testing.T) {
	root := bootstrapProject(t, "projD")
	writeCompileFile(t, root, "projD/apm.yml", "name: d\nversion: 1.0.0\n")

	stdout, _, code := runCompileCLI(t, "install", "../pkg", "--target", "claude")
	if code != 0 {
		t.Fatalf("exit %d, stdout:\n%s", code, stdout)
	}
	if strings.Contains(stdout, "Created") || strings.Contains(stdout, "Targets set") {
		t.Errorf("stdout:\n%s\nwant no Created or Targets set line", stdout)
	}
	const want = "name: d\nversion: 1.0.0\ndependencies:\n  apm:\n    - ../pkg\n"
	if got := readFileString(t, filepath.Join(root, "projD", "apm.yml")); got != want {
		t.Errorf("apm.yml:\n%s\nwant:\n%s", got, want)
	}
}

// TestInstall_NoManifest_LockKept_ManifestGetsPackage: the lock of an earlier
// install is left and apm.yml removed; the apm.yml the next install creates
// still declares the package (Oracle 0.33.0, or-e/l1/apm.yml), though the
// lock does not change.
func TestInstall_NoManifest_LockKept_ManifestGetsPackage(t *testing.T) {
	root := bootstrapProject(t, "l1")
	if _, _, code := runCompileCLI(t, "install", "../pkg", "--target", "claude"); code != 0 {
		t.Fatalf("first install: exit %d", code)
	}
	if err := os.Remove("apm.yml"); err != nil {
		t.Fatal(err)
	}

	stdout, _, code := runCompileCLI(t, "install", "../pkg", "--target", "claude")
	if code != 0 {
		t.Fatalf("exit %d, stdout:\n%s", code, stdout)
	}
	const want = "name: l1\n" +
		"version: 1.0.0\n" +
		"description: APM project for l1\n" +
		"author: Developer\n" +
		"# Which agent platforms to deploy to.\n" +
		"# Resolution order: --target flag > this field > auto-detect from filesystem.\n" +
		"# Accepted values: agent-skills, antigravity, claude, codex, copilot, cursor, gemini, grok-build, hermes, kiro, opencode, vscode, windsurf\n" +
		"targets:\n" +
		"  - claude\n" +
		"dependencies:\n" +
		"  apm:\n" +
		"    - ../pkg\n" +
		"  mcp: []\n" +
		"includes: auto\n" +
		"scripts: {}\n"
	if got := readFileString(t, filepath.Join(root, "l1", "apm.yml")); got != want {
		t.Errorf("apm.yml:\n%s\nwant:\n%s", got, want)
	}
}

// TestInstall_NoManifestNoPackage_Refused: with neither apm.yml nor a
// package the install creates nothing and prints the Oracle's lines once
// (Oracle 0.33.0: or-p.out projG, or-e.out g0), apm-go in the hints.
func TestInstall_NoManifestNoPackage_Refused(t *testing.T) {
	t.Run("project", func(t *testing.T) {
		root := bootstrapProject(t, "projG")
		stdout, stderr, code := runCompileCLI(t, "install")
		const want = " x No apm.yml found\n" +
			" i Run 'apm-go init' to create one, or:\n" +
			" i   apm-go install <org/repo> to auto-create + install\n"
		if code != 1 || stdout != want || stderr != "" {
			t.Errorf("exit %d, stdout:\n%s\nstderr:\n%s\nwant exit 1, stdout:\n%s", code, stdout, stderr, want)
		}
		assertNoFile(t, filepath.Join(root, "projG", "apm.yml"))
	})
	t.Run("global", func(t *testing.T) {
		root := bootstrapProject(t, "proj")
		manifestPath := filepath.Join(root, "home", ".apm", "apm.yml")
		stdout, stderr, code := runCompileCLI(t, "install", "-g")
		want := " x No " + manifestPath + " found\n" +
			" i Run 'apm-go install -g <org/repo>' to auto-create + install\n"
		if code != 1 || stdout != want || stderr != "" {
			t.Errorf("exit %d, stdout:\n%s\nstderr:\n%s\nwant exit 1, stdout:\n%s", code, stdout, stderr, want)
		}
		assertNoFile(t, manifestPath)
	})
}

// TestInstallGlobal_NoManifest_TargetsPersisted is the issue: the first
// `install -g <pkg> --target codex,claude` writes the targets into
// ~/.apm/apm.yml, a later `install -g` deploys to them, and a later
// --target leaves them as they are (Oracle 0.33.0, or-g-abs.out).
func TestInstallGlobal_NoManifest_TargetsPersisted(t *testing.T) {
	root := bootstrapProject(t, "proj")
	writeCompileFile(t, root, "pkg2/apm.yml", "name: pkg2\nversion: 1.0.0\n")
	writeCompileFile(t, root, "pkg2/.apm/skills/sk2/SKILL.md", "---\nname: sk2\ndescription: d\n---\nx\n")
	home := filepath.Join(root, "home")
	manifestPath := filepath.Join(home, ".apm", "apm.yml")
	pkg, pkg2 := filepath.Join(root, "pkg"), filepath.Join(root, "pkg2")
	wantManifest := func(deps ...string) string {
		s := "name: home\n" +
			"version: 1.0.0\n" +
			"description: APM project for home\n" +
			"author: Developer\n" +
			"# Which agent platforms to deploy to.\n" +
			"# Resolution order: --target flag > this field > auto-detect from filesystem.\n" +
			"# Accepted values: agent-skills, antigravity, claude, codex, copilot, cursor, gemini, grok-build, hermes, kiro, opencode, vscode, windsurf\n" +
			"targets:\n" +
			"  - claude\n" +
			"  - codex\n" +
			"dependencies:\n" +
			"  apm:\n"
		for _, d := range deps {
			s += "    - " + d + "\n"
		}
		return s + "  mcp: []\n" +
			"includes: auto\n" +
			"scripts: {}\n"
	}

	stdout, _, code := runCompileCLI(t, "install", "-g", pkg, "--target", "codex,claude")
	if code != 0 {
		t.Fatalf("first install: exit %d, stdout:\n%s", code, stdout)
	}
	wantHead := " + Created " + manifestPath + "\n" +
		" i Targets set: claude, codex (persisted to " + manifestPath + ")\n" +
		" + Validating 1 package...\n"
	if !strings.HasPrefix(stdout, wantHead) {
		t.Errorf("stdout:\n%s\nwant it to start with:\n%s", stdout, wantHead)
	}
	if got, want := readFileString(t, manifestPath), wantManifest(pkg); got != want {
		t.Errorf("~/.apm/apm.yml:\n%s\nwant:\n%s", got, want)
	}

	for _, dir := range []string{".claude", ".agents"} {
		if err := os.RemoveAll(filepath.Join(home, dir)); err != nil {
			t.Fatal(err)
		}
	}
	stdout, _, code = runCompileCLI(t, "install", "-g")
	if code != 0 {
		t.Fatalf("second install: exit %d, stdout:\n%s", code, stdout)
	}
	for _, f := range []string{".claude/skills/sk/SKILL.md", ".agents/skills/sk/SKILL.md"} {
		if _, err := os.Stat(filepath.Join(home, f)); err != nil {
			t.Errorf("second install did not deploy %s: %v", f, err)
		}
	}

	stdout, _, code = runCompileCLI(t, "install", "-g", pkg2, "--target", "claude")
	if code != 0 {
		t.Fatalf("third install: exit %d, stdout:\n%s", code, stdout)
	}
	if got, want := readFileString(t, manifestPath), wantManifest(pkg, pkg2); got != want {
		t.Errorf("~/.apm/apm.yml after the third install:\n%s\nwant:\n%s", got, want)
	}
}

// TestInstallGlobalMCP_NoManifest_CreatesIt: `install -g --mcp` creates
// ~/.apm/apm.yml with the bytes Oracle 0.33.0 ends with (mg-codex,claude and
// mg-none): no comments, and targets in the order --target gives them.
func TestInstallGlobalMCP_NoManifest_CreatesIt(t *testing.T) {
	const mcpTail = "dependencies:\n" +
		"  apm: []\n" +
		"  mcp:\n" +
		"  - name: srv\n" +
		"    registry: false\n" +
		"    transport: stdio\n" +
		"    command: echo\n" +
		"    args:\n" +
		"    - hi\n" +
		"includes: auto\n" +
		"scripts: {}\n"
	const head = "name: home\nversion: 1.0.0\ndescription: APM project for home\nauthor: Developer\n"
	for _, tt := range []struct {
		name string
		args []string
		want string
	}{
		{"target flag", []string{"--target", "codex,claude"}, head + "targets:\n- codex\n- claude\n" + mcpTail},
		{"repeated target", []string{"--target", "codex,claude,codex"}, head + "targets:\n- codex\n- claude\n" + mcpTail},
		// apm-go's MCP targets for all at user scope; Oracle 0.33.0 writes
		// claude, codex, gemini, kiro, windsurf (rv5/m-or-all), a recorded
		// deviation.
		{"all", []string{"--target", "all"}, head + "targets:\n- claude\n- codex\n- copilot\n- opencode\n" + mcpTail},
		{"alias", []string{"--target", "agy"}, head + "targets:\n- antigravity\n" + mcpTail},
		{"target without MCP config", []string{"--target", "claude,agent-skills"}, head + "targets:\n- claude\n" + mcpTail},
		{"auto-detect", nil, head + mcpTail},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := bootstrapProject(t, "proj")
			writeCompileFile(t, root, "home/.claude/keep", "")
			manifestPath := filepath.Join(root, "home", ".apm", "apm.yml")

			args := append([]string{"install", "-g", "--mcp", "srv"}, tt.args...)
			stdout, _, code := runCompileCLI(t, append(args, "--", "echo", "hi")...)
			if code != 0 {
				t.Fatalf("exit %d, stdout:\n%s", code, stdout)
			}
			created := strings.Index(stdout, " + Created "+manifestPath+"\n")
			block := strings.Index(stdout, "+- MCP Servers (1)\n")
			if created < 0 || block < 0 || created > block {
				t.Errorf("stdout:\n%s\nwant the Created line before the MCP block", stdout)
			}
			if strings.Contains(stdout, "Targets set") {
				t.Errorf("stdout:\n%s\nwant no Targets set line", stdout)
			}
			if got := readFileString(t, manifestPath); got != tt.want {
				t.Errorf("~/.apm/apm.yml:\n%s\nwant:\n%s", got, tt.want)
			}
		})
	}
}

// TestInstallGlobalMCP_NoManifest_FailureRemovesIt: an `install -g --mcp`
// that created ~/.apm/apm.yml and then fails removes it.
func TestInstallGlobalMCP_NoManifest_FailureRemovesIt(t *testing.T) {
	root := bootstrapProject(t, "proj")
	// A lock that cannot be read fails the install after apm.yml is written.
	if err := os.MkdirAll(filepath.Join(root, "home", ".apm", "apm.lock.yaml"), 0o755); err != nil {
		t.Fatal(err)
	}

	stdout, _, code := runCompileCLI(t, "install", "-g", "--mcp", "srv", "--target", "claude", "--", "echo", "hi")
	if code != 1 {
		t.Errorf("exit %d, want 1; stdout:\n%s", code, stdout)
	}
	removed := strings.Index(stdout, " i Removed apm.yml created by the failed install.\n")
	failed := strings.Index(stdout, " x ")
	if removed < 0 || failed < 0 || removed > failed {
		t.Errorf("stdout:\n%s\nwant the Removed line before the error line", stdout)
	}
	assertNoFile(t, filepath.Join(root, "home", ".apm", "apm.yml"))
}

// TestInstallHelp_TargetRepeatKeepsLast: the --target help says what the
// Oracle's says (commands/install.py:923-924): a repeated flag keeps only
// its last value.
func TestInstallHelp_TargetRepeatKeepsLast(t *testing.T) {
	stdout, _, code := runCompileCLI(t, "install", "--help")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	const want = "repeating the flag keeps only the last value (use commas instead)"
	if !strings.Contains(stdout, want) {
		t.Errorf("install --help:\n%s\nwant it to contain %q", stdout, want)
	}
}

// TestRemoveCreatedManifest_FailureWarns: an apm.yml the failed install
// cannot remove is the Oracle's warning (install/transaction.py:57-60).
func TestRemoveCreatedManifest_FailureWarns(t *testing.T) {
	dir := chdirTemp(t)
	writeCompileFile(t, dir, "apm.yml/keep", "")

	stdout := captureStdout(t, removeCreatedManifest)

	const want = " ! Failed to remove apm.yml created by this install. Delete apm.yml manually before retrying.\n"
	if stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
}
