package main

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fakeGit scripts `git <args...>` by the first argument ("--version",
// "ls-remote"). Unscripted subcommands behave like a healthy git.
type fakeGit struct {
	version  gitResult
	lsRemote gitResult
	calls    [][]string
}

func (f *fakeGit) run(_ context.Context, args ...string) gitResult {
	f.calls = append(f.calls, args)
	switch args[0] {
	case "--version":
		return f.version
	case "ls-remote":
		return f.lsRemote
	}
	return gitResult{stdout: "", exitCode: 0}
}

func healthyGit() *fakeGit {
	return &fakeGit{
		version:  gitResult{stdout: "git version 2.45.0\n", exitCode: 0},
		lsRemote: gitResult{stdout: "abc\tHEAD\n", exitCode: 0},
	}
}

// runDoctorWith runs doctor under NO_COLOR=1 CI=1 with no TTY (captureStdout's
// os.Pipe is never a terminal): the exact "rich renderers stay on, ANSI
// strips" scenario ticket 10 decision (A) requires -- doctor's table must
// render unconditionally, never the old IsRich()-gated plain fallback. The
// table itself is captured from stdout, not stderr: the runner case
// doctor-healthy (ticket 10 attempt 2) proved renderDoctorTable was still on
// os.Stderr while the pinned Oracle's own Console defaults to stdout for ANY
// output, so doctor.go moved w to os.Stdout in the same commit.
func runDoctorWith(t *testing.T, git *fakeGit, env map[string]string, args ...string) (string, error) {
	t.Helper()
	t.Setenv("NO_COLOR", "1")
	t.Setenv("CI", "1")
	deps := doctorDeps{
		runGit: git.run,
		getenv: func(k string) string { return env[k] },
	}
	var err error
	out := captureStdout(t, func() {
		cmd := doctorCmdWith(deps)
		cmd.SetArgs(args)
		err = cmd.Execute()
	})
	return out, err
}

// captureStdout redirects os.Stdout for the duration of fn and returns what
// was written -- doctor's table (post ticket 10 attempt 2) writes straight to
// os.Stdout, so the process-level stream is what has to be inspected, same
// rationale as captureStderr (init_clack_test.go) for init's stderr output.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() err = %v", err)
	}
	orig := os.Stdout
	os.Stdout = w

	fn()

	os.Stdout = orig
	if err := w.Close(); err != nil {
		t.Fatalf("close pipe writer: %v", err)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read captured stdout: %v", err)
	}
	return string(data)
}

// assertNoANSI fails the test if s contains any ANSI escape sequence.
// ux.Table strips color automatically for a non-terminal writer, so table
// output under NO_COLOR=1 CI=1 no-TTY must carry box-drawing characters but
// no ANSI codes.
func assertNoANSI(t *testing.T, name, s string) {
	t.Helper()
	if strings.Contains(s, "\x1b[") {
		t.Fatalf("%s output contains ANSI escape: %q", name, s)
	}
}

// assertBoxDrawing fails the test if s contains no box-drawing border
// characters, proving the table path rendered instead of the old
// IsRich()-gated plain fallback.
func assertBoxDrawing(t *testing.T, name, s string) {
	t.Helper()
	if !strings.ContainsRune(s, '─') {
		t.Fatalf("%s output has no box-drawing characters, want a table: %q", name, s)
	}
}

func TestDoctor_AllCriticalPass_Exit0_ReportsGitAndNetwork(t *testing.T) {
	chdirTemp(t)
	out, err := runDoctorWith(t, healthyGit(), nil)
	if err != nil {
		t.Fatalf("want exit 0, got %v\n%s", err, out)
	}
	assertBoxDrawing(t, "doctor", out)
	assertNoANSI(t, "doctor", out)
	for _, want := range []string{
		"git", "[+]", "git version 2.45.0",
		"network", "[+]", "github.com reachable",
		"auth", "[i]", "No token; unauthenticated rate limits apply",
		"marketplace config", "[i]", "No marketplace authoring config in current directory",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestDoctor_GitMissing_Exit1(t *testing.T) {
	chdirTemp(t)
	git := healthyGit()
	git.version = gitResult{notFound: true}
	git.lsRemote = gitResult{notFound: true}
	out, err := runDoctorWith(t, git, nil)
	if exitCodeOf(err) != 1 {
		t.Fatalf("want exit 1, got %v", err)
	}
	assertBoxDrawing(t, "doctor", out)
	assertNoANSI(t, "doctor", out)
	for _, want := range []string{
		"git", "[x]", "git not found on PATH",
		"network", "git not found; cannot test network",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestDoctor_NetworkTimeout_Exit1(t *testing.T) {
	chdirTemp(t)
	git := healthyGit()
	git.lsRemote = gitResult{timedOut: true}
	out, err := runDoctorWith(t, git, nil)
	if exitCodeOf(err) != 1 {
		t.Fatalf("want exit 1, got %v", err)
	}
	if !strings.Contains(out, "network") || !strings.Contains(out, "Network check timed out (5s)") {
		t.Errorf("got:\n%s", out)
	}
}

func TestDoctor_NetworkFailure_TranslatedHint(t *testing.T) {
	chdirTemp(t)
	git := healthyGit()
	git.lsRemote = gitResult{
		stderr:   "fatal: unable to access 'https://github.com/git/git.git/': Could not resolve host: github.com\n",
		exitCode: 128,
	}
	out, err := runDoctorWith(t, git, nil)
	if exitCodeOf(err) != 1 {
		t.Fatalf("want exit 1, got %v", err)
	}
	if !strings.Contains(out, "network") || strings.Contains(out, "fatal: unable to access") {
		t.Errorf("detail should be a translated hint, not raw stderr:\n%s", out)
	}
	// git_stderr.py:151-152 TIMEOUT hint ("could not resolve host" classifies
	// as TIMEOUT, :84,:116-117).
	if !strings.Contains(out, "Network issue contacting the remote. Retry or check your connection.") {
		t.Errorf("got:\n%s", out)
	}
}

func TestDoctor_TokenDetected_NeverPrinted(t *testing.T) {
	chdirTemp(t)
	const secret = "ghp_SUPERSECRET123"
	out, err := runDoctorWith(t, healthyGit(), map[string]string{"GITHUB_TOKEN": secret})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "auth") || !strings.Contains(out, "Token detected") {
		t.Errorf("got:\n%s", out)
	}
	if strings.Contains(out, secret) {
		t.Fatalf("token value leaked into output:\n%s", out)
	}
}

func TestDoctor_MarketplaceConfig_ApmYml(t *testing.T) {
	chdirTemp(t)
	os.WriteFile("apm.yml", []byte("name: m\nversion: 1.0.0\nmarketplace:\n  name: m\n  packages:\n    - name: a\n      source: owner/a\n    - name: A\n      source: owner/b\n"), 0o644)
	out, err := runDoctorWith(t, healthyGit(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"marketplace config", "apm.yml 'marketplace:' block found and valid",
		"duplicate names", "Duplicate names: 'A' (packages[0] and packages[1])",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestDoctor_MarketplaceConfig_Legacy_PointsAtMigrate(t *testing.T) {
	chdirTemp(t)
	os.WriteFile("marketplace.yml", []byte("name: m\npackages:\n  - name: a\n    source: owner/a\n"), 0o644)
	out, err := runDoctorWith(t, healthyGit(), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := "marketplace.yml found (legacy). Run 'apm-go marketplace migrate' to fold it into apm.yml."
	if !strings.Contains(out, want) {
		t.Errorf("missing %q in:\n%s", want, out)
	}
	if !strings.Contains(out, "duplicate names") || !strings.Contains(out, "No duplicate package names") {
		t.Errorf("got:\n%s", out)
	}
}

func TestDoctor_MarketplaceConfig_BothExist_Flagged_StillExit0(t *testing.T) {
	dir := chdirTemp(t)
	os.WriteFile(filepath.Join(dir, "apm.yml"), []byte("name: m\nversion: 1.0.0\nmarketplace:\n  name: m\n  packages: []\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "marketplace.yml"), []byte("name: m\npackages: []\n"), 0o644)
	out, err := runDoctorWith(t, healthyGit(), nil)
	if err != nil {
		t.Fatalf("informational failure must not change exit code: %v", err)
	}
	if !strings.Contains(out, "marketplace config") || !strings.Contains(out, "Both apm.yml") {
		t.Errorf("got:\n%s", out)
	}
}

func TestDoctor_Help_MatchesUpstream(t *testing.T) {
	cmd := doctorCmd()
	var sb strings.Builder
	cmd.SetOut(&sb)
	cmd.SetArgs([]string{"--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Run environment diagnostics", "--verbose", "-v"} {
		if !strings.Contains(sb.String(), want) {
			t.Errorf("help missing %q:\n%s", want, sb.String())
		}
	}
}

// Finding 7 (F07): doctor's git probes must run under gitops' hardened
// environment (secure_env.go) so they can neither prompt nor hang on
// credentials, nor be steered onto a remote-helper transport.
func TestDoctor_ExecGit_UsesSecureGitEnv(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("needs sh")
	}
	bin := t.TempDir()
	envFile := filepath.Join(bin, "env.txt")
	script := "#!/bin/sh\nenv > " + envFile + "\necho git version 9.9.9\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	r := execGit(context.Background(), "--version")
	if r.notFound || r.exitCode != 0 {
		t.Fatalf("fake git not used: %+v", r)
	}
	env, _ := os.ReadFile(envFile)
	for _, want := range []string{
		"GIT_TERMINAL_PROMPT=0",
		"GIT_ASKPASS=",
		"GCM_INTERACTIVE=never",
		"GIT_ALLOW_PROTOCOL=",
		"GIT_PROTOCOL_FROM_USER=0",
	} {
		if !strings.Contains(string(env), want+"\n") && !strings.Contains(string(env), want) {
			t.Errorf("git env missing %s:\n%s", want, env)
		}
	}
}

// Finding 8 (F08): doctor.py:212-214 / :222-224 wrap a parse failure as
// "<file> marketplace block has errors: <err>" (apm.yml) or
// "<file> has errors: <err>" (legacy), truncating err to 60 chars.
func TestDoctor_MalformedConfig_UpstreamPrefix_StillExit0(t *testing.T) {
	chdirTemp(t)
	os.WriteFile("apm.yml", []byte("name: x\nmarketplace:\n  packages: [\n"), 0o644)
	out, err := runDoctorWith(t, healthyGit(), nil)
	if err != nil {
		t.Fatalf("informational parse failure must keep exit 0: %v", err)
	}
	if !strings.Contains(out, "marketplace config") || !strings.Contains(out, "apm.yml marketplace block has errors: ") {
		t.Errorf("got:\n%s", out)
	}
	if strings.Contains(out, "marketplace config has errors:") {
		t.Errorf("non-upstream prefix leaked:\n%s", out)
	}

	os.Remove("apm.yml")
	os.WriteFile("marketplace.yml", []byte("packages: [\n"), 0o644)
	out, err = runDoctorWith(t, healthyGit(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "marketplace config") || !strings.Contains(out, "marketplace.yml has errors: ") {
		t.Errorf("got:\n%s", out)
	}
}

// Round-2 F8: when apm.yml exists WITHOUT a marketplace: block, the
// active source is the legacy file (detect_config_source), so a broken
// marketplace.yml must be reported under the legacy prefix.
func TestDoctor_ApmYmlWithoutBlock_BrokenLegacy_UsesLegacyPrefix(t *testing.T) {
	chdirTemp(t)
	os.WriteFile("apm.yml", []byte("name: demo\nversion: 1.0.0\n"), 0o644)
	os.WriteFile("marketplace.yml", []byte("name: broken\npackages: [\n"), 0o644)
	out, err := runDoctorWith(t, healthyGit(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "marketplace config") || !strings.Contains(out, "marketplace.yml has errors: ") {
		t.Errorf("got:\n%s", out)
	}
	if strings.Contains(out, "apm.yml marketplace block has errors") {
		t.Errorf("wrong source attributed:\n%s", out)
	}
}
