//go:build unix

package main

import (
	"encoding/json"
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

func TestRunPreflight_RecordsOracleCommit(t *testing.T) {
	oracleDir, head := initGitRepo(t)
	cfg := testPreflightConfig(t, oracleDir)

	pf, err := runPreflight(cfg)
	if err != nil {
		t.Fatalf("runPreflight: %v", err)
	}
	if pf.OracleCommit != head {
		t.Errorf("OracleCommit = %q, want %q", pf.OracleCommit, head)
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

func TestRunPreflight_DirtyOracleTreeIsRecordedNotFatal(t *testing.T) {
	oracleDir, head := initGitRepo(t)
	cfg := testPreflightConfig(t, oracleDir)

	// An untracked file is enough for `git status --porcelain` to report
	// something -- no need to touch a tracked file.
	if err := os.WriteFile(filepath.Join(oracleDir, "untracked.txt"), []byte("dirty"), 0o644); err != nil {
		t.Fatalf("writing untracked file: %v", err)
	}

	pf, err := runPreflight(cfg)
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

// TestRun_PreflightSuccessDrivesRunCases proves Run() actually wires
// runPreflight's result into runCases (not just that each function works in
// isolation), and that the Oracle checkout may be at any commit: against a
// throwaway Oracle repo whose HEAD is not a commit of microsoft/apm, a full
// Run() call must produce run.json recording that commit, with no
// oracle_pin key, AND per-case evidence for the case it loaded.
func TestRun_PreflightSuccessDrivesRunCases(t *testing.T) {
	oracleDir, head := initGitRepo(t)
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
	var raw struct {
		Preflight map[string]json.RawMessage `json:"preflight"`
	}
	if err := json.Unmarshal(headerData, &raw); err != nil {
		t.Fatalf("unmarshalling run.json: %v", err)
	}
	if _, ok := raw.Preflight["oracle_pin"]; ok {
		t.Error("run.json preflight has an oracle_pin key, want none")
	}

	for _, side := range []string{"oracle", "target"} {
		if _, err := os.Stat(filepath.Join(cfg.OutDir, side, "some-case", "record.json")); err != nil {
			t.Errorf("%s: %v", side, err)
		}
	}
}

// TestRun_RelativeTargetBinIsResolvedToAbsoluteBeforeCaseExecution is a
// regression test for a latent bug: every case runs with cmd.Dir set to its
// own sandbox cwd (sandbox.go), and a relative argv[0] containing a path
// separator resolves against cmd.Dir, not this process's cwd -- so a
// relative -target-bin (like the real default, "./bin/apm-go") would fail
// to exec in every single case with "no such file or directory", forever,
// regardless of actual Oracle/Target parity. Run() must resolve it to the
// absolute path runPreflight already validated before any case executes.
func TestRun_RelativeTargetBinIsResolvedToAbsoluteBeforeCaseExecution(t *testing.T) {
	oracleDir, _ := initGitRepo(t)
	scriptDir := t.TempDir()
	oracleBin := writeStubScript(t, scriptDir, "oracle.sh", `
if [ "$1" = "--version" ]; then echo "oracle 1.0"; exit 0; fi
echo "ran"; exit 0
`)
	writeStubScript(t, scriptDir, "target.sh", `
if [ "$1" = "--version" ]; then echo "target 1.0"; exit 0; fi
echo "ran"; exit 0
`)

	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(scriptDir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	defer func() {
		if err := os.Chdir(origWD); err != nil {
			t.Fatalf("restoring cwd: %v", err)
		}
	}()

	cfg := Config{
		OracleCmd: []string{oracleBin, "--project", oracleDir},
		TargetBin: []string{"./target.sh"}, // relative, on purpose
		Timeout:   defaultTimeout,
	}
	casesDir := t.TempDir()
	writeCase(t, casesDir, "c1", `{"id": "c1", "argv": []}`)
	cfg.CasesDir = casesDir
	cfg.OutDir = t.TempDir()

	if err := Run(cfg); err != nil {
		t.Fatalf("Run: %v, want the relative -target-bin to resolve and the case to actually run", err)
	}

	targetRec := readSoleJSONLRecord(t, filepath.Join(cfg.OutDir, "target.jsonl"))
	if targetRec.ExitCode != 0 || targetRec.Stdout == nil || !strings.Contains(*targetRec.Stdout, "ran") {
		t.Errorf("target record = %+v, want the stub to have actually executed", targetRec)
	}
}
