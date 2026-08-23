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
	CasesDir  string
	OutDir    string
	OracleCmd []string
	TargetBin []string
	Timeout   time.Duration
}

func main() {
	cases := flag.String("cases", "", "directory of case subdirectories, each with a case.json")
	out := flag.String("out", "", "directory to write evidence into")
	flag.Parse()

	if *cases == "" || *out == "" {
		fmt.Fprintln(os.Stderr, "parity: -cases and -out are both required")
		os.Exit(1)
	}

	cfg := Config{
		CasesDir:  *cases,
		OutDir:    *out,
		OracleCmd: resolveCmd("APM_ORACLE_CMD", defaultOracleCmd),
		TargetBin: resolveCmd("APM_TARGET_BIN", defaultTargetBin),
		Timeout:   defaultTimeout,
	}

	if err := Run(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "parity: %v\n", err)
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

// Run executes every case in cfg.CasesDir against both sides and writes the
// evidence tree under cfg.OutDir. Nothing is compared: a case's non-zero
// exit code is captured, not treated as a runner failure.
func Run(cfg Config) error {
	cases, err := LoadCases(cfg.CasesDir)
	if err != nil {
		return err
	}

	for _, side := range []string{"oracle", "target"} {
		if err := os.MkdirAll(filepath.Join(cfg.OutDir, side), 0o755); err != nil {
			return fmt.Errorf("creating %s output dir: %w", side, err)
		}
	}

	oracleVersion := getVersion(cfg.OracleCmd, cfg.Timeout)
	targetVersion := getVersion(cfg.TargetBin, cfg.Timeout)
	if err := writeRunHeader(cfg, oracleVersion, targetVersion); err != nil {
		return err
	}

	for _, c := range cases {
		if err := runCaseAllSides(cfg, c); err != nil {
			return err
		}
	}

	return nil
}

func runCaseAllSides(cfg Config, c Case) error {
	sides := []struct {
		name string
		bin  []string
	}{
		{"oracle", cfg.OracleCmd},
		{"target", cfg.TargetBin},
	}

	for _, s := range sides {
		rec, err := runCaseSide(s.bin, c, cfg.OutDir, s.name, cfg.Timeout)
		if err != nil {
			return err
		}

		jsonlPath := filepath.Join(cfg.OutDir, s.name+".jsonl")
		caseDir := filepath.Join(s.name, c.ID)
		if err := appendJSONLRecord(jsonlPath, rec, caseDir); err != nil {
			return fmt.Errorf("case %s (%s): %w", c.ID, s.name, err)
		}
	}

	return nil
}

// runHeader is <out>/run.json: what ran, against which binaries, when.
type runHeader struct {
	Timestamp     string `json:"timestamp"`
	OracleCmd     string `json:"apm_oracle_cmd"`
	TargetBin     string `json:"apm_target_bin"`
	OracleVersion string `json:"oracle_version"`
	TargetVersion string `json:"target_version"`
}

func writeRunHeader(cfg Config, oracleVersion, targetVersion string) error {
	header := runHeader{
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
		OracleCmd:     strings.Join(cfg.OracleCmd, " "),
		TargetBin:     strings.Join(cfg.TargetBin, " "),
		OracleVersion: oracleVersion,
		TargetVersion: targetVersion,
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
	env := buildEnv(nil, sb.Home, sb.ConfigDir)
	res := runProcess(argv, env, "", sb.Cwd, timeout)
	return strings.TrimSpace(string(res.Stdout))
}
