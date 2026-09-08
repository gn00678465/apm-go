//go:build unix

package main

import (
	_ "embed"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/apm-go/apm/internal/gitops"
)

// oraclePinData is tools/parity/oracle.pin: the ONE place the pinned Oracle
// commit lives (acceptance criterion 1). Everything else -- the pin
// mismatch check here, and the waiver validator in ticket 02 that compares
// waivers[].oracle_commit against preflight.oracle_commit -- reads it from
// here rather than carrying its own copy.
//
//go:embed oracle.pin
var oraclePinData string

// preflightError marks a preflight failure that must fail the run closed
// with exit code 2 (a wrong Oracle pin or an unusable Target binary),
// distinct from an ordinary runner failure (exit 1, exec_run.go / record.go
// setup/write errors). main() type-switches on this via errors.As to pick
// the exit code.
type preflightError struct{ err error }

func (e *preflightError) Error() string { return e.err.Error() }
func (e *preflightError) Unwrap() error { return e.err }
func (e *preflightError) ExitCode() int { return 2 }

// Preflight is the evidence proving a run compared the right two binaries:
// the Oracle checkout is at the pinned commit and the Target binary is an
// explicit, existing executable. It is written into run.json verbatim
// (main.go's runHeader.Preflight) so a reviewer -- or ticket 02's waiver
// validator -- never has to re-derive it.
type Preflight struct {
	OraclePin       string `json:"oracle_pin"`
	OracleCommit    string `json:"oracle_commit"`
	OracleDirty     bool   `json:"oracle_dirty"`
	OracleVersion   string `json:"oracle_version"`
	TargetBinPath   string `json:"target_bin_path"`
	TargetBinSHA256 string `json:"target_bin_sha256"`
	TargetBinMtime  string `json:"target_bin_mtime"`
	TargetVersion   string `json:"target_version"`
	TargetCommit    string `json:"target_commit"`
	TargetDirty     bool   `json:"target_dirty"`
	UvVersion       string `json:"uv_version"`
}

// pinnedOracleCommit reads and validates oracle.pin's embedded content: the
// full 40-char hex commit SHA every run's Oracle checkout is checked
// against. Run() calls this indirectly through pinnedOracleCommitFunc so a
// test can exercise Run()'s full pin-lookup -> preflight -> runCases wiring
// against a throwaway repo instead of the real, embedded Oracle checkout.
func pinnedOracleCommit() (string, error) {
	pin := strings.TrimSpace(oraclePinData)
	if !isHexSHA(pin) {
		return "", fmt.Errorf("tools/parity/oracle.pin: expected a 40-char hex commit SHA, got %q", pin)
	}
	return pin, nil
}

// pinnedOracleCommitFunc is the seam Run() calls through; production code
// never reassigns it. Test-only override: SetPinnedOracleCommitForTest.
var pinnedOracleCommitFunc = pinnedOracleCommit

// SetPinnedOracleCommitForTest overrides the pin Run() checks the Oracle
// checkout against, returning a restore func. Same pattern as
// internal/pluginjson/testhooks.go's SetCommitHookForTest.
func SetPinnedOracleCommitForTest(pin string) (restore func()) {
	prev := pinnedOracleCommitFunc
	pinnedOracleCommitFunc = func() (string, error) { return pin, nil }
	return func() { pinnedOracleCommitFunc = prev }
}

func isHexSHA(s string) bool {
	if len(s) != 40 {
		return false
	}
	for _, r := range s {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return true
}

// runPreflight proves the runner is about to compare the right two binaries
// before any case executes. pin is the Oracle commit the checkout must be
// at (production callers pass pinnedOracleCommit(); tests inject an
// arbitrary value against a throwaway repo). An Oracle pin mismatch or an
// unusable Target binary fails the whole run closed (wrapped in
// *preflightError); an Oracle dirty worktree is recorded and warned about,
// not fatal -- reviewers decide whether it invalidates the run. The
// preflight never fetches, checks out, or otherwise mutates the Oracle
// repo: only read-only `git rev-parse` / `git status --porcelain`.
func runPreflight(cfg Config, pin string) (Preflight, error) {
	oracleDir, err := oracleProjectDir(cfg.OracleCmd)
	if err != nil {
		return Preflight{}, &preflightError{fmt.Errorf("resolving Oracle repo from APM_ORACLE_CMD: %w", err)}
	}

	oracleCommit, err := gitRevParseHEAD(oracleDir)
	if err != nil {
		return Preflight{}, &preflightError{fmt.Errorf("Oracle repo %s: %w", oracleDir, err)}
	}
	if oracleCommit != pin {
		return Preflight{}, &preflightError{fmt.Errorf(
			"Oracle pin mismatch: pinned commit %s, checkout %s is at %s", pin, oracleDir, oracleCommit)}
	}

	oracleDirty, err := gitDirty(oracleDir)
	if err != nil {
		return Preflight{}, &preflightError{fmt.Errorf("Oracle repo %s: checking worktree status: %w", oracleDir, err)}
	}
	if oracleDirty {
		fmt.Fprintf(os.Stderr, "parity: warning: Oracle checkout %s has uncommitted changes\n", oracleDir)
	}

	targetArg := ""
	if len(cfg.TargetBin) > 0 {
		targetArg = cfg.TargetBin[0]
	}
	targetAbs, err := resolveTargetBin(targetArg)
	if err != nil {
		return Preflight{}, &preflightError{err}
	}
	targetSHA, targetMtime, err := targetBinEvidence(targetAbs)
	if err != nil {
		return Preflight{}, &preflightError{fmt.Errorf("Target binary %s: %w", targetAbs, err)}
	}

	targetCmd := append([]string{targetAbs}, cfg.TargetBin[1:]...)
	oracleVersion := getVersion(cfg.OracleCmd, cfg.Timeout)
	targetVersion := getVersion(targetCmd, cfg.Timeout)

	// The Target repo (the invoking process's cwd) is recorded best-effort:
	// unlike the Oracle pin, an unresolvable commit here (e.g. running a
	// released binary with no .git ancestor) doesn't invalidate the run.
	targetCommit, targetDirty := targetRepoStatus(".")

	return Preflight{
		OraclePin:       pin,
		OracleCommit:    oracleCommit,
		OracleDirty:     oracleDirty,
		OracleVersion:   oracleVersion,
		TargetBinPath:   targetAbs,
		TargetBinSHA256: targetSHA,
		TargetBinMtime:  targetMtime.UTC().Format(time.RFC3339),
		TargetVersion:   targetVersion,
		TargetCommit:    targetCommit,
		TargetDirty:     targetDirty,
		UvVersion:       uvVersion(),
	}, nil
}

// oracleProjectDir extracts the `--project <dir>` (or `--project=<dir>`)
// argument from an Oracle command such as
// "uv run --project /path/to/apm apm" -- the directory the Oracle checkout
// the pin is verified against lives in.
func oracleProjectDir(oracleCmd []string) (string, error) {
	for i, arg := range oracleCmd {
		if arg == "--project" {
			if i+1 >= len(oracleCmd) {
				return "", fmt.Errorf("--project has no value in %q", strings.Join(oracleCmd, " "))
			}
			return oracleCmd[i+1], nil
		}
		if v, ok := strings.CutPrefix(arg, "--project="); ok {
			return v, nil
		}
	}
	return "", fmt.Errorf("no --project argument in %q", strings.Join(oracleCmd, " "))
}

// resolveTargetBin validates and resolves APM_TARGET_BIN's binary path
// (cfg.TargetBin[0]). A bare name such as "apm-go" -- one with no path
// separator, i.e. one the shell would resolve via PATH -- is refused so a
// stray apm-go on the invoker's PATH can never be silently compared instead
// of the intended build; an explicit path (relative or absolute) is
// required.
func resolveTargetBin(pathArg string) (string, error) {
	if pathArg == "" {
		return "", errors.New("APM_TARGET_BIN is empty")
	}
	if !strings.ContainsRune(pathArg, '/') {
		return "", fmt.Errorf(
			"APM_TARGET_BIN must be an explicit path (e.g. ./bin/apm-go), not a bare name resolved via PATH: %q", pathArg)
	}

	abs, err := filepath.Abs(pathArg)
	if err != nil {
		return "", fmt.Errorf("resolving APM_TARGET_BIN %q: %w", pathArg, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("APM_TARGET_BIN %q: %w", pathArg, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("APM_TARGET_BIN %q is a directory, not an executable", pathArg)
	}
	if info.Mode()&0o111 == 0 {
		return "", fmt.Errorf("APM_TARGET_BIN %q is not executable", pathArg)
	}
	return abs, nil
}

// targetBinEvidence derives the sha256 (via record.go's sha256Hex, the same
// helper stdout/stderr evidence uses) and mtime evidence from a single open
// file descriptor, so a concurrent rebuild of the Target binary between a
// separate ReadFile and Stat call can't make the recorded sha256/mtime pair
// describe two different states of the file.
func targetBinEvidence(path string) (sha256hex string, mtime time.Time, err error) {
	f, err := os.Open(path)
	if err != nil {
		return "", time.Time{}, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return "", time.Time{}, err
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return "", time.Time{}, err
	}
	return sha256Hex(data), info.ModTime(), nil
}

// targetRepoStatus is gitRevParseHEAD/gitDirty for the Target repo, but
// best-effort: any error (not a git checkout, git not installed, ...)
// yields an empty/false result rather than propagating, since -- unlike the
// Oracle -- the Target repo isn't pinned or gated on.
func targetRepoStatus(repoDir string) (commit string, dirty bool) {
	commit, err := gitRevParseHEAD(repoDir)
	if err != nil {
		return "", false
	}
	dirty, err = gitDirty(repoDir)
	if err != nil {
		return commit, false
	}
	return commit, dirty
}

// uvVersion runs `uv --version` and returns its trimmed stdout, or "" if uv
// isn't available -- non-fatal, like getVersion's Oracle/Target failures:
// the evidence's absence is itself informative to a reviewer.
func uvVersion() string {
	out, err := exec.Command("uv", "--version").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// gitRevParseHEAD resolves repoDir's HEAD commit. Delegates to the already
// hardened gitops.ResolveCommit (internal/gitops/clone.go) -- which runs
// `git rev-parse HEAD` under gitops.ApplySecureGitEnv, the environment
// every git subprocess in this project uses (AGENTS.md: "every call site,
// including diagnostics") -- rather than keeping a second, drifting copy of
// that call here. Read-only: never fetches, checks out, or otherwise
// mutates repoDir.
func gitRevParseHEAD(repoDir string) (string, error) {
	return gitops.ResolveCommit(repoDir)
}

// gitDirty runs `git status --porcelain` in repoDir and reports whether it
// produced any output. Read-only.
func gitDirty(repoDir string) (bool, error) {
	out, err := runGit(repoDir, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

func runGit(repoDir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = repoDir
	gitops.ApplySecureGitEnv(cmd)

	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(string(exitErr.Stderr)))
		}
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}
