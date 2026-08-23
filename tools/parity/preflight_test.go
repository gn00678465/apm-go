//go:build unix

package main

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// initGitRepo creates a fresh git repo in a t.TempDir() with a single
// commit and returns its directory and that commit's HEAD SHA.
func initGitRepo(t *testing.T) (dir, head string) {
	t.Helper()
	dir = t.TempDir()

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	run("init", "-q")
	run("config", "user.email", "parity-test@example.com")
	run("config", "user.name", "parity test")
	run("commit", "--allow-empty", "-q", "-m", "init")

	head, err := gitRevParseHEAD(dir)
	if err != nil {
		t.Fatalf("gitRevParseHEAD: %v", err)
	}
	return dir, head
}

func testPreflightConfig(t *testing.T, oracleDir string) Config {
	t.Helper()
	scriptDir := t.TempDir()
	target := writeStubScript(t, scriptDir, "target.sh", `
if [ "$1" = "--version" ]; then echo "target 1.0"; exit 0; fi
exit 0
`)
	oracleBin := writeStubScript(t, scriptDir, "oracle.sh", `
if [ "$1" = "--version" ]; then echo "oracle 1.0"; exit 0; fi
exit 0
`)
	return Config{
		OracleCmd: []string{oracleBin, "--project", oracleDir},
		TargetBin: []string{target},
		Timeout:   defaultTimeout,
	}
}

func TestRunPreflight_PinMatch(t *testing.T) {
	oracleDir, head := initGitRepo(t)
	cfg := testPreflightConfig(t, oracleDir)

	pf, err := runPreflight(cfg, head)
	if err != nil {
		t.Fatalf("runPreflight: %v", err)
	}
	if pf.OracleCommit != head {
		t.Errorf("OracleCommit = %q, want %q", pf.OracleCommit, head)
	}
	if pf.OraclePin != head {
		t.Errorf("OraclePin = %q, want %q", pf.OraclePin, head)
	}
	if pf.OracleDirty {
		t.Error("OracleDirty = true, want false for a freshly committed repo")
	}
	if pf.TargetVersion != "target 1.0" {
		t.Errorf("TargetVersion = %q, want %q", pf.TargetVersion, "target 1.0")
	}
	if pf.TargetBinSHA256 == "" {
		t.Error("TargetBinSHA256 is empty")
	}
	if !filepath.IsAbs(pf.TargetBinPath) {
		t.Errorf("TargetBinPath = %q, want an absolute path", pf.TargetBinPath)
	}
}

func TestRunPreflight_PinMismatchFailsClosedWithBothSHAs(t *testing.T) {
	oracleDir, head := initGitRepo(t)
	cfg := testPreflightConfig(t, oracleDir)

	wrongPin := strings.Repeat("a", 40)
	if wrongPin == head {
		t.Fatal("test setup: wrongPin accidentally equals the repo's real HEAD")
	}

	_, err := runPreflight(cfg, wrongPin)
	if err == nil {
		t.Fatal("runPreflight: expected a pin-mismatch error, got nil")
	}

	var pf *preflightError
	if !errors.As(err, &pf) {
		t.Fatalf("runPreflight error %v is not a *preflightError, so main() would exit 1 instead of 2", err)
	}
	msg := err.Error()
	if !strings.Contains(msg, wrongPin) {
		t.Errorf("error %q does not contain the pinned SHA %q", msg, wrongPin)
	}
	if !strings.Contains(msg, head) {
		t.Errorf("error %q does not contain the checkout's actual SHA %q", msg, head)
	}
}

func TestRunPreflight_DirtyOracleTreeIsRecordedNotFatal(t *testing.T) {
	oracleDir, head := initGitRepo(t)
	cfg := testPreflightConfig(t, oracleDir)

	// An untracked file is enough for `git status --porcelain` to report
	// something -- no need to touch a tracked file.
	if err := os.WriteFile(filepath.Join(oracleDir, "untracked.txt"), []byte("dirty"), 0o644); err != nil {
		t.Fatalf("writing untracked file: %v", err)
	}

	pf, err := runPreflight(cfg, head)
	if err != nil {
		t.Fatalf("runPreflight: %v, want a dirty tree to be non-fatal", err)
	}
	if !pf.OracleDirty {
		t.Error("OracleDirty = false, want true for a repo with an untracked file")
	}
	// A dirty tree does not change which commit was actually checked out.
	if pf.OracleCommit != head {
		t.Errorf("OracleCommit = %q, want %q", pf.OracleCommit, head)
	}
}

func TestResolveTargetBin_BareNameRefused(t *testing.T) {
	if _, err := resolveTargetBin("apm-go"); err == nil {
		t.Fatal("resolveTargetBin(\"apm-go\"): expected an error for a bare PATH-resolved name, got nil")
	}

	scriptDir := t.TempDir()
	explicit := writeStubScript(t, scriptDir, "apm-go", "exit 0\n")
	abs, err := resolveTargetBin(explicit)
	if err != nil {
		t.Fatalf("resolveTargetBin(%q): %v", explicit, err)
	}
	if !filepath.IsAbs(abs) {
		t.Errorf("resolveTargetBin(%q) = %q, want an absolute path", explicit, abs)
	}
}

func TestResolveTargetBin_MissingPathIsError(t *testing.T) {
	if _, err := resolveTargetBin("./does/not/exist/apm-go"); err == nil {
		t.Fatal("resolveTargetBin: expected an error for a nonexistent path, got nil")
	}
}

func TestOracleProjectDir_ExtractsProjectFlag(t *testing.T) {
	got, err := oracleProjectDir([]string{"uv", "run", "--project", "/x/apm", "apm"})
	if err != nil {
		t.Fatalf("oracleProjectDir: %v", err)
	}
	if got != "/x/apm" {
		t.Errorf("oracleProjectDir = %q, want %q", got, "/x/apm")
	}

	got, err = oracleProjectDir([]string{"uv", "run", "--project=/y/apm", "apm"})
	if err != nil {
		t.Fatalf("oracleProjectDir: %v", err)
	}
	if got != "/y/apm" {
		t.Errorf("oracleProjectDir = %q, want %q", got, "/y/apm")
	}

	if _, err := oracleProjectDir([]string{"apm"}); err == nil {
		t.Fatal("oracleProjectDir: expected an error when --project is absent, got nil")
	}
}

// TestRun_PinMismatchBlocksAllCasesAndWritesNoOutput exercises Run()'s real
// wiring end to end: pin lookup -> runPreflight -> exit-2-worthy error, with
// no override, so it also proves the real oracle.pin genuinely does not
// match a freshly created throwaway repo (i.e. this isn't a vacuous check).
func TestRun_PinMismatchBlocksAllCasesAndWritesNoOutput(t *testing.T) {
	oracleDir, _ := initGitRepo(t)
	cfg := testPreflightConfig(t, oracleDir)

	casesDir := t.TempDir()
	writeCase(t, casesDir, "some-case", `{"id": "some-case", "argv": []}`)
	cfg.CasesDir = casesDir
	outDir := filepath.Join(t.TempDir(), "out")
	cfg.OutDir = outDir

	err := Run(cfg)
	if err == nil {
		t.Fatal("Run: expected a pin-mismatch error, got nil")
	}
	var pf *preflightError
	if !errors.As(err, &pf) {
		t.Fatalf("Run error %v is not a *preflightError, so main() would exit 1 instead of 2", err)
	}
	if _, statErr := os.Stat(outDir); !os.IsNotExist(statErr) {
		t.Errorf("Run: %s exists after a preflight failure, want no output directory created", outDir)
	}
}

// TestRun_PreflightSuccessDrivesRunCases proves Run() actually wires
// runPreflight's result into runCases (not just that each function works in
// isolation): with the pin lookup overridden to match a throwaway Oracle
// repo, a full Run() call must produce run.json with that preflight
// evidence AND per-case evidence for the case it loaded.
func TestRun_PreflightSuccessDrivesRunCases(t *testing.T) {
	oracleDir, head := initGitRepo(t)
	restore := SetPinnedOracleCommitForTest(head)
	defer restore()

	cfg := testPreflightConfig(t, oracleDir)
	casesDir := t.TempDir()
	writeCase(t, casesDir, "some-case", `{"id": "some-case", "argv": []}`)
	cfg.CasesDir = casesDir
	cfg.OutDir = t.TempDir()

	if err := Run(cfg); err != nil {
		t.Fatalf("Run: %v", err)
	}

	headerData, err := os.ReadFile(filepath.Join(cfg.OutDir, "run.json"))
	if err != nil {
		t.Fatalf("reading run.json: %v", err)
	}
	var header runHeader
	if err := json.Unmarshal(headerData, &header); err != nil {
		t.Fatalf("unmarshalling run.json: %v", err)
	}
	if header.Preflight.OracleCommit != head {
		t.Errorf("run.json preflight.oracle_commit = %q, want %q", header.Preflight.OracleCommit, head)
	}

	for _, side := range []string{"oracle", "target"} {
		if _, err := os.Stat(filepath.Join(cfg.OutDir, side, "some-case", "record.json")); err != nil {
			t.Errorf("%s: %v", side, err)
		}
	}
}

func TestPinnedOracleCommit_ValidatesEmbeddedPin(t *testing.T) {
	pin, err := pinnedOracleCommit()
	if err != nil {
		t.Fatalf("pinnedOracleCommit: %v", err)
	}
	if !isHexSHA(pin) {
		t.Errorf("pinnedOracleCommit() = %q, not a 40-char hex SHA", pin)
	}
}
