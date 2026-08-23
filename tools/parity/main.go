//go:build unix

// Command parity runs every case under -cases against both the Oracle
// (apm) and the Target (apm-go), isolated from the invoking user's real
// environment, and writes one raw evidence record per side per case under
// -out. It performs no normalisation, diffing, or gating: that is a later
// ticket's job. The runner's own exit status is 0 if every case ran to
// completion (regardless of the case's own exit code), and 1 only when the
// runner itself failed to set up or write evidence.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	defaultOracleCmd = "uv run --project /home/madao/projects/apm-mesh/apm apm"
	defaultTargetBin = "./bin/apm-go"
)

// Config is main()'s parsed input, factored out so Run is testable without
// touching flag.CommandLine or the real environment.
type Config struct {
	CasesDir    string
	OutDir      string
	OracleCmd   []string
	TargetBin   []string
	Timeout     time.Duration
	WaiversPath string // "" means the default sibling of CasesDir; see loadAndValidateWaivers.
}

func main() {
	cases := flag.String("cases", "", "directory of case subdirectories, each with a case.json")
	out := flag.String("out", "", "directory to write evidence into")
	waivers := flag.String("waivers", "", "path to waivers.json (default: a \"waivers.json\" sibling of -cases)")
	selftestOnly := flag.Bool("selftest-only", false, "run only the fault-injection self-test and exit (3 on failure)")
	flag.Parse()

	if *selftestOnly {
		if err := runSelfTestFunc(selfTestTimeout); err != nil {
			fmt.Fprintf(os.Stderr, "parity: %v\n", err)
			os.Exit(3)
		}
		fmt.Println("parity: self-test passed")
		return
	}

	if *cases == "" || *out == "" {
		fmt.Fprintln(os.Stderr, "parity: -cases and -out are both required")
		os.Exit(1)
	}

	cfg := Config{
		CasesDir:    *cases,
		OutDir:      *out,
		OracleCmd:   resolveCmd("APM_ORACLE_CMD", defaultOracleCmd),
		TargetBin:   resolveCmd("APM_TARGET_BIN", defaultTargetBin),
		Timeout:     defaultTimeout,
		WaiversPath: *waivers,
	}

	if err := Run(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "parity: %v\n", err)
		var ec interface{ ExitCode() int }
		if errors.As(err, &ec) {
			os.Exit(ec.ExitCode())
		}
		os.Exit(1)
	}
}

// resolveCmd splits an env var (or its default) on whitespace into command
// parts. Neither default value nor documented overrides need quoting, so a
// plain Fields split is sufficient here. A whitespace-only override falls
// back to def too, rather than resolving to an empty argv that would later
// panic on argv[0].
func resolveCmd(envVar, def string) []string {
	v := strings.TrimSpace(os.Getenv(envVar))
	if v == "" {
		v = def
	}
	return strings.Fields(v)
}

// Run executes every case in cfg.CasesDir against both sides, writes the
// evidence tree under cfg.OutDir, and gates on the result. A case's own
// non-zero exit code is captured, not treated as a runner failure; an
// unwaived Oracle/Target diff is.
//
// Before touching cfg.CasesDir or cfg.OutDir at all, it runs the Oracle/
// Target pin preflight (ticket 03): a pin mismatch or unusable Target
// binary returns a *preflightError and NO case runs, no output directory is
// created, and no run.json is written. After preflight and before any real
// case, it runs the fault-injection self-test (ticket 02): a failure there
// returns a *selfTestError and, likewise, no real case runs.
func Run(cfg Config) error {
	pin, err := pinnedOracleCommitFunc()
	if err != nil {
		return &preflightError{err}
	}
	preflight, err := runPreflight(cfg, pin)
	if err != nil {
		return err
	}

	// Every case runs with cmd.Dir set to its own sandbox cwd (sandbox.go),
	// and a relative argv[0] containing a path separator resolves against
	// cmd.Dir, not this process's own cwd -- so the default relative
	// -target-bin (defaultTargetBin, "./bin/apm-go") would fail to exec in
	// every case. runPreflight already validated and resolved this to an
	// absolute path (Preflight.TargetBinPath); reuse it here instead of
	// cfg.TargetBin[0]'s original, possibly-relative form.
	cfg.TargetBin = append([]string{preflight.TargetBinPath}, cfg.TargetBin[1:]...)

	if err := runSelfTestFunc(selfTestTimeout); err != nil {
		return err
	}

	return runCases(cfg, preflight)
}

// CasePair is one case's captured evidence from both sides, threaded from
// captureRun into runCases' diff stage.
type CasePair struct {
	Case   Case
	Oracle Record
	Target Record
}

// captureRun loads cfg.CasesDir, runs every case against both sides, and
// writes run.json (embedding the already-computed preflight evidence) plus
// per-case evidence under cfg.OutDir -- ticket 01's original capture
// pipeline, unchanged in behaviour. Split out from runCases so ticket 01's
// capture-pipeline tests (main_test.go) can drive the JSONL/run.json
// writers directly without also going through ticket 02's normalise/diff/
// waiver-gate stage, and so runCases can layer that stage on top of exactly
// the evidence this produces.
func captureRun(cfg Config, preflight Preflight) ([]CasePair, error) {
	cases, err := LoadCases(cfg.CasesDir)
	if err != nil {
		return nil, err
	}

	for _, side := range []string{"oracle", "target"} {
		if err := os.MkdirAll(filepath.Join(cfg.OutDir, side), 0o755); err != nil {
			return nil, fmt.Errorf("creating %s output dir: %w", side, err)
		}
	}

	if err := writeRunHeader(cfg, preflight); err != nil {
		return nil, err
	}

	pairs := make([]CasePair, 0, len(cases))
	for _, c := range cases {
		pair, err := runCaseAllSides(cfg, c)
		if err != nil {
			return nil, err
		}
		pairs = append(pairs, pair)
	}
	return pairs, nil
}

// runCases layers ticket 02's normalise/diff/waiver-gate stage on top of
// captureRun. It validates waivers.json against the case IDs LoadCases
// finds and this run's preflight.OracleCommit BEFORE calling captureRun at
// all (ticket 02 attempt 2, W2: "LoadCases -> validate -> ONLY THEN execute
// any case") -- an invalid waivers.json fails the run with
// *waiverValidationError having triggered zero Oracle/Target invocations.
// Once evidence is captured, it diffs each case's Oracle/Target records,
// writes diff.jsonl/diff/<id>.json/summary.txt, and fails the run (exit 1,
// via a plain error) if any case has an unwaived diff.
func runCases(cfg Config, preflight Preflight) error {
	cases, err := LoadCases(cfg.CasesDir)
	if err != nil {
		return err
	}
	casesByID := make(map[string]Case, len(cases))
	for _, c := range cases {
		casesByID[c.ID] = c
	}

	waivers, err := loadAndValidateWaivers(cfg.CasesDir, cfg.WaiversPath, preflight.OracleCommit, casesByID)
	if err != nil {
		return err
	}

	pairs, err := captureRun(cfg, preflight)
	if err != nil {
		return err
	}

	diffs := make([]CaseDiff, 0, len(pairs))
	for _, p := range pairs {
		cd, detail, err := diffCase(cfg.OutDir, p.Case, p.Oracle, p.Target)
		if err != nil {
			return err
		}
		cd = applyWaiver(cd, waivers)
		diffs = append(diffs, cd)

		if len(cd.Fields) > 0 {
			if err := writeDiffDetail(cfg.OutDir, cd.ID, detail); err != nil {
				return err
			}
		}
	}

	if err := writeDiffJSONL(cfg.OutDir, diffs); err != nil {
		return err
	}
	if err := writeSummary(cfg.OutDir, diffs); err != nil {
		return err
	}

	if unwaived := countUnwaived(diffs); unwaived > 0 {
		return fmt.Errorf("%d of %d case(s) have unwaived diffs -- see %s/diff.jsonl", unwaived, len(diffs), cfg.OutDir)
	}
	return nil
}

func runCaseAllSides(cfg Config, c Case) (CasePair, error) {
	sides := []struct {
		name string
		bin  []string
	}{
		{"oracle", cfg.OracleCmd},
		{"target", cfg.TargetBin},
	}

	pair := CasePair{Case: c}
	for _, s := range sides {
		rec, err := runCaseSide(s.bin, c, cfg.OutDir, s.name, cfg.Timeout)
		if err != nil {
			return CasePair{}, err
		}

		jsonlPath := filepath.Join(cfg.OutDir, s.name+".jsonl")
		caseDir := filepath.Join(s.name, c.ID)
		if err := appendJSONLRecord(jsonlPath, rec, caseDir); err != nil {
			return CasePair{}, fmt.Errorf("case %s (%s): %w", c.ID, s.name, err)
		}

		if s.name == "oracle" {
			pair.Oracle = rec
		} else {
			pair.Target = rec
		}
	}

	return pair, nil
}

// runHeader is <out>/run.json: what ran, against which binaries, when.
// Preflight is ticket 03's Oracle/Target pin evidence; OracleVersion/
// TargetVersion are kept as their own top-level fields (not just nested
// under Preflight) for ticket 01's existing consumers.
type runHeader struct {
	Timestamp     string    `json:"timestamp"`
	OracleCmd     string    `json:"apm_oracle_cmd"`
	TargetBin     string    `json:"apm_target_bin"`
	OracleVersion string    `json:"oracle_version"`
	TargetVersion string    `json:"target_version"`
	Preflight     Preflight `json:"preflight"`
}

func writeRunHeader(cfg Config, preflight Preflight) error {
	header := runHeader{
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
		OracleCmd:     strings.Join(cfg.OracleCmd, " "),
		TargetBin:     strings.Join(cfg.TargetBin, " "),
		OracleVersion: preflight.OracleVersion,
		TargetVersion: preflight.TargetVersion,
		Preflight:     preflight,
	}
	data, err := json.MarshalIndent(header, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling run.json: %w", err)
	}
	return os.WriteFile(filepath.Join(cfg.OutDir, "run.json"), data, 0o644)
}

// getVersion runs binPath with a trailing --version in its own throwaway
// sandbox and returns its stdout, trimmed. A failure to obtain a version is
// recorded as an empty string rather than aborting the whole run — the
// evidence's absence is itself informative to a reviewer.
func getVersion(binPath []string, timeout time.Duration) string {
	sb, err := newSandbox("")
	if err != nil {
		return ""
	}
	defer sb.cleanup()

	argv := append(append([]string{}, binPath...), "--version")
	env := buildEnv(nil, sb.Home, sb.ConfigDir, sb.LauncherCache)
	res := runProcess(argv, env, "", sb.Cwd, timeout)
	return strings.TrimSpace(string(res.Stdout))
}
