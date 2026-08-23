//go:build unix

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// selfTestTimeout bounds each self-test stub invocation. The stubs return
// immediately, so this only guards against a genuinely broken shell rather
// than pacing real work.
const selfTestTimeout = 10 * time.Second

// selfTestError marks a self-test failure. Acceptance requires the runner
// to exit 3 and run no real case when its own fault-injection proof that
// the diff/waiver-gate pipeline actually detects differences fails.
type selfTestError struct{ err error }

func (e *selfTestError) Error() string { return "self-test: " + e.err.Error() }
func (e *selfTestError) Unwrap() error { return e.err }
func (e *selfTestError) ExitCode() int { return 3 }

// runSelfTestFunc is the seam Run() and main()'s -selftest-only path call
// through; production code never reassigns it.
var runSelfTestFunc = runSelfTest

// runSelfTest proves the normalise/diff/waiver-gate pipeline actually
// detects the differences it claims to, using stub shell scripts as both
// "Oracle" and "Target" -- run in-process by this binary exactly like a
// real case, never the real Oracle. Called automatically before any real
// case; a failure here must stop the run before a real case executes
// (acceptance: exit 3, "does not run real cases").
func runSelfTest(timeout time.Duration) error {
	dir, err := os.MkdirTemp("", "apm-parity-selftest-*")
	if err != nil {
		return &selfTestError{fmt.Errorf("creating self-test scratch dir: %w", err)}
	}
	defer os.RemoveAll(dir)

	checks := []struct {
		name string
		fn   func(dir string, timeout time.Duration) error
	}{
		{"S1", selfTestS1},
		{"S2", selfTestS2},
		{"S3", selfTestS3},
		{"S4", selfTestS4},
		{"S5", selfTestS5},
	}
	for _, c := range checks {
		if err := c.fn(dir, timeout); err != nil {
			return &selfTestError{fmt.Errorf("%s: %w", c.name, err)}
		}
	}
	return nil
}

// selfTestDiff runs one synthetic case (oracleBody/targetBody as stub shell
// scripts) through the exact same runCaseSide -> diffCase path a real case
// takes, and returns the resulting CaseDiff.
func selfTestDiff(dir, id, oracleBody, targetBody string, timeout time.Duration) (CaseDiff, error) {
	caseDir := filepath.Join(dir, id)
	oracleBin, err := writeSelfTestScript(caseDir, "oracle.sh", oracleBody)
	if err != nil {
		return CaseDiff{}, err
	}
	targetBin, err := writeSelfTestScript(caseDir, "target.sh", targetBody)
	if err != nil {
		return CaseDiff{}, err
	}

	c := Case{ID: id}

	oracleRec, err := runCaseSide([]string{oracleBin}, c, caseDir, "oracle", timeout)
	if err != nil {
		return CaseDiff{}, err
	}
	targetRec, err := runCaseSide([]string{targetBin}, c, caseDir, "target", timeout)
	if err != nil {
		return CaseDiff{}, err
	}

	cd, _, err := diffCase(caseDir, c, oracleRec, targetRec)
	if err != nil {
		return CaseDiff{}, err
	}
	return cd, nil
}

func writeSelfTestScript(dir, name, body string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("creating self-test script dir: %w", err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		return "", fmt.Errorf("writing self-test script: %w", err)
	}
	return path, nil
}

// selfTestS1: identical stubs must produce zero diff fields.
func selfTestS1(dir string, timeout time.Duration) error {
	cd, err := selfTestDiff(dir, "s1", "echo hi\nexit 0\n", "echo hi\nexit 0\n", timeout)
	if err != nil {
		return err
	}
	if len(cd.Fields) != 0 {
		return fmt.Errorf("identical stubs produced diff fields %v, want none", cd.Fields)
	}
	return nil
}

// selfTestS2: a one-byte stdout difference must produce exactly ["stdout"].
func selfTestS2(dir string, timeout time.Duration) error {
	cd, err := selfTestDiff(dir, "s2", "echo hello\nexit 0\n", "echo hellp\nexit 0\n", timeout)
	if err != nil {
		return err
	}
	if !fieldsEqual(cd.Fields, []string{"stdout"}) {
		return fmt.Errorf("one-byte stdout diff produced fields %v, want [stdout]", cd.Fields)
	}
	return nil
}

// selfTestS3: an exit-code-only difference must produce exactly
// ["exit_code"].
func selfTestS3(dir string, timeout time.Duration) error {
	cd, err := selfTestDiff(dir, "s3", "exit 0\n", "exit 1\n", timeout)
	if err != nil {
		return err
	}
	if !fieldsEqual(cd.Fields, []string{"exit_code"}) {
		return fmt.Errorf("exit-code-only diff produced fields %v, want [exit_code]", cd.Fields)
	}
	return nil
}

// selfTestS4: Target writing one extra file must produce exactly ["tree"].
func selfTestS4(dir string, timeout time.Duration) error {
	cd, err := selfTestDiff(dir, "s4", "exit 0\n", "echo extra > extra.txt\nexit 0\n", timeout)
	if err != nil {
		return err
	}
	if !fieldsEqual(cd.Fields, []string{"tree"}) {
		return fmt.Errorf("target-only extra file produced fields %v, want [tree]", cd.Fields)
	}
	return nil
}

// selfTestS5: S2's diff, waived by a matching waiver, must report
// Waived=true; the same diff against a waiver that only lists exit_code
// must report Waived=false (partial coverage is not a waiver).
func selfTestS5(dir string, timeout time.Duration) error {
	cd, err := selfTestDiff(dir, "s5", "echo hello\nexit 0\n", "echo hellp\nexit 0\n", timeout)
	if err != nil {
		return err
	}
	if !fieldsEqual(cd.Fields, []string{"stdout"}) {
		return fmt.Errorf("s5 base case produced fields %v, want [stdout]", cd.Fields)
	}

	matching := applyWaiver(cd, nil, []Waiver{{ID: "s5", Fields: []string{"stdout"}, Reason: "self-test S5"}})
	if !matching.Waived {
		return fmt.Errorf("a waiver covering [stdout] did not waive a [stdout] diff")
	}

	partial := applyWaiver(cd, nil, []Waiver{{ID: "s5", Fields: []string{"exit_code"}, Reason: "self-test S5"}})
	if partial.Waived {
		return fmt.Errorf("a waiver covering only [exit_code] incorrectly waived a [stdout] diff")
	}
	return nil
}

func fieldsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
