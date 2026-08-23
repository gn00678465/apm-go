package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/apm-go/apm/internal/gitops"
	"github.com/apm-go/apm/internal/marketplace/authoring"
	"github.com/apm-go/apm/internal/ux"
	"github.com/spf13/cobra"
)

// doctorTimeout is upstream's per-probe subprocess timeout
// (commands/marketplace/doctor.py:120,150).
const doctorTimeout = 5 * time.Second

// gitResult is one scripted/observed `git` invocation. notFound mirrors
// Python's FileNotFoundError (git absent from PATH); timedOut mirrors
// subprocess.TimeoutExpired.
type gitResult struct {
	stdout, stderr string
	exitCode       int
	notFound       bool
	timedOut       bool
	err            error
}

// doctorDeps are doctor's external seams (同 installDeps 的注入模式):
// runGit shells out to git with a deadline; getenv reads the environment.
type doctorDeps struct {
	runGit func(ctx context.Context, args ...string) gitResult
	getenv func(string) string
}

func defaultDoctorDeps() doctorDeps {
	return doctorDeps{runGit: execGit, getenv: os.Getenv}
}

// execGit is the production runGit: `git <args>` under ctx's deadline with
// the hardened environment gitops already uses for clones.
func execGit(ctx context.Context, args ...string) gitResult {
	cmd := exec.CommandContext(ctx, "git", args...)
	// Finding 7 (F07): same hardening as every other git subprocess in the
	// project -- no credential prompts, no remote-helper transports.
	gitops.ApplySecureGitEnv(cmd)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	res := gitResult{stdout: stdout.String(), stderr: stderr.String()}
	switch {
	case err == nil:
		return res
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		res.timedOut = true
	case errors.Is(err, exec.ErrNotFound):
		res.notFound = true
	default:
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			res.exitCode = ee.ExitCode()
		} else {
			res.err = err
		}
	}
	return res
}

// doctorCheck mirrors _DoctorCheck (commands/marketplace/__init__.py:
// 1299-1308). informational checks never affect the exit code.
type doctorCheck struct {
	name, detail  string
	passed        bool
	informational bool
}

func (c doctorCheck) icon() string {
	switch {
	case c.informational:
		return "[i]"
	case c.passed:
		return "[+]"
	}
	return "[x]"
}

// doctorCmd is the top-level `apm-go doctor` (commands/doctor.py:18-30).
func doctorCmd() *cobra.Command { return doctorCmdWith(defaultDoctorDeps()) }

func doctorCmdWith(deps doctorDeps) *cobra.Command {
	var verbose bool
	cmd := &cobra.Command{
		Use:          "doctor",
		Short:        "Run environment diagnostics (git, network, auth, marketplace config). Reports a pass/fail table and exits non-zero if a critical check fails.",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDoctor(deps, verbose)
		},
	}
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Show detailed output")
	return cmd
}

// runDoctor mirrors run_doctor (commands/marketplace/doctor.py:103-329)
// for the checks apm-go can back today: git, network, auth, marketplace
// config, duplicate names. format coverage / version alignment /
// executable trust have no apm-go subsystem yet and are omitted (spec
// "Out of Scope").
func runDoctor(deps doctorDeps, verbose bool) error {
	_ = verbose // accepted for surface parity; no verbose-only output yet
	checks := []doctorCheck{
		checkGit(deps),
		checkNetwork(deps),
		checkAuth(deps),
	}
	cfgCheck, cfg := checkMarketplaceConfig()
	checks = append(checks, cfgCheck)
	if cfg != nil {
		checks = append(checks, checkDuplicateNames(cfg))
	}

	renderDoctorTable(checks)

	for _, c := range checks {
		if !c.informational && !c.passed {
			return withExitCode(1, errors.New("critical environment check failed"))
		}
	}
	return nil
}

// checkGit: doctor.py:112-140.
func checkGit(deps doctorDeps) doctorCheck {
	ctx, cancel := context.WithTimeout(context.Background(), doctorTimeout)
	defer cancel()
	r := deps.runGit(ctx, "--version")
	c := doctorCheck{name: "git"}
	switch {
	case r.notFound:
		c.detail = "git not found on PATH"
	case r.timedOut:
		c.detail = "git --version timed out"
	case r.err != nil:
		c.detail = truncate(r.err.Error(), 60)
	case r.exitCode != 0:
		c.detail = "git returned non-zero exit code"
	default:
		c.passed = true
		c.detail = strings.TrimSpace(r.stdout)
	}
	return c
}

// checkNetwork: doctor.py:142-176.
func checkNetwork(deps doctorDeps) doctorCheck {
	ctx, cancel := context.WithTimeout(context.Background(), doctorTimeout)
	defer cancel()
	r := deps.runGit(ctx, "ls-remote", "https://github.com/git/git.git", "HEAD")
	c := doctorCheck{name: "network"}
	switch {
	case r.timedOut:
		c.detail = "Network check timed out (5s)"
	case r.notFound:
		c.detail = "git not found; cannot test network"
	case r.err != nil:
		c.detail = truncate(r.err.Error(), 60)
	case r.exitCode != 0:
		t := gitops.TranslateGitStderr(r.stderr, r.exitCode, "ls-remote", "github.com")
		c.detail = truncate(t.Hint, 80)
	default:
		c.passed = true
		c.detail = "github.com reachable"
	}
	return c
}

// checkAuth: doctor.py:178-196. Informational; the token value itself is
// never rendered. Only env-var detection for github.com (spec Out of
// Scope: full AuthResolver parity).
func checkAuth(deps doctorDeps) doctorCheck {
	has := deps.getenv("GH_TOKEN") != "" || deps.getenv("GITHUB_TOKEN") != ""
	detail := "No token; unauthenticated rate limits apply"
	if has {
		detail = "Token detected"
	}
	return doctorCheck{name: "auth", passed: true, detail: detail, informational: true}
}

// checkMarketplaceConfig: doctor.py:198-238. Returns the loaded config
// (nil when absent/invalid) so config-dependent checks can follow.
func checkMarketplaceConfig() (doctorCheck, *authoring.AuthoringConfig) {
	c := doctorCheck{name: "marketplace config", passed: true, informational: true}
	cfg, src, err := authoring.LoadAuthoringConfig(".")
	switch {
	case err == nil && src == authoring.ConfigSourceApmYML:
		c.detail = "apm.yml 'marketplace:' block found and valid"
	case err == nil:
		c.detail = "marketplace.yml found (legacy). Run 'apm-go marketplace migrate' to fold it into apm.yml."
	case authoring.IsNoConfigError(err):
		c.detail = "No marketplace authoring config in current directory"
	case authoring.IsConfigsMutuallyExclusiveError(err):
		c.passed = false
		c.detail = "Both apm.yml (with a 'marketplace:' block) and marketplace.yml exist. Remove marketplace.yml or run 'apm-go marketplace migrate --force' to consolidate."
	default:
		// doctor.py:212-214 (apm.yml) / :222-224 (legacy), attributed to the
		// source LoadAuthoringConfig was actually reading (Round-2 F8).
		c.passed = false
		if src == authoring.ConfigSourceLegacy {
			c.detail = "marketplace.yml has errors: " + truncate(err.Error(), 60)
		} else {
			c.detail = "apm.yml marketplace block has errors: " + truncate(err.Error(), 60)
		}
	}
	return c, cfg
}

// checkDuplicateNames: doctor.py:266-286 + _find_duplicate_names
// (__init__.py:186-198): case-insensitive, reports the later entry's
// spelling with both indices.
func checkDuplicateNames(cfg *authoring.AuthoringConfig) doctorCheck {
	seen := map[string]int{}
	var dups []string
	for i, p := range cfg.Packages {
		k := strings.ToLower(p.Name)
		if j, ok := seen[k]; ok {
			dups = append(dups, fmt.Sprintf("'%s' (packages[%d] and packages[%d])", p.Name, j, i))
			continue
		}
		seen[k] = i
	}
	c := doctorCheck{name: "duplicate names", passed: true, informational: true, detail: "No duplicate package names"}
	if len(dups) > 0 {
		c.passed = false
		c.detail = "Duplicate names: " + strings.Join(dups, ", ")
	}
	return c
}

// renderDoctorTable mirrors _render_doctor_table (__init__.py:1311-1348):
// a titled table in a rich terminal, `  [i]/[+]/[x] name: detail` lines
// otherwise.
func renderDoctorTable(checks []doctorCheck) {
	w := os.Stderr
	if !ux.IsRich() {
		for _, c := range checks {
			ux.Plain(w, "  %s %s: %s", c.icon(), c.name, c.detail)
		}
		return
	}
	rows := make([][]string, 0, len(checks))
	for _, c := range checks {
		rows = append(rows, []string{c.name, c.icon(), c.detail})
	}
	ux.Plain(w, "")
	ux.Section(w, "Environment Diagnostics")
	ux.Table(w, []string{"Check", "Status", "Detail"}, rows)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
