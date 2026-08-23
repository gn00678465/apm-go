//go:build unix

package main

import (
	"errors"
	"os/exec"
	"testing"
	"time"
)

// TestRunSelfTest_Passes is the shipped self-test actually passing: proof
// that the diff/waiver-gate pipeline this ticket adds genuinely detects
// the differences S1-S5 inject, using in-process stub scripts and never
// the real Oracle.
func TestRunSelfTest_Passes(t *testing.T) {
	if err := runSelfTest(5 * time.Second); err != nil {
		t.Fatalf("runSelfTest: %v", err)
	}
}

func TestRunSelfTest_FailureIsASelfTestErrorWithExitCode3(t *testing.T) {
	// A broken check (S1 expecting a real diff from identical stubs) must
	// surface as *selfTestError so main() exits 3, not some other code.
	err := (&selfTestError{errors.New("s1 expected a diff but got none")}).ExitCode()
	if err != 3 {
		t.Errorf("selfTestError.ExitCode() = %d, want 3", err)
	}
}

func TestSelfTestS1_IdenticalStubsProduceNoDiff(t *testing.T) {
	if err := selfTestS1(t.TempDir(), 5*time.Second); err != nil {
		t.Errorf("selfTestS1: %v", err)
	}
}

func TestSelfTestS2_OneByteStdoutDiff(t *testing.T) {
	if err := selfTestS2(t.TempDir(), 5*time.Second); err != nil {
		t.Errorf("selfTestS2: %v", err)
	}
}

func TestSelfTestS3_ExitCodeOnlyDiff(t *testing.T) {
	if err := selfTestS3(t.TempDir(), 5*time.Second); err != nil {
		t.Errorf("selfTestS3: %v", err)
	}
}

func TestSelfTestS4_ExtraTargetFile(t *testing.T) {
	if err := selfTestS4(t.TempDir(), 5*time.Second); err != nil {
		t.Errorf("selfTestS4: %v", err)
	}
}

func TestSelfTestS5_WaiverCoverageMatchesAndPartialCases(t *testing.T) {
	if err := selfTestS5(t.TempDir(), 5*time.Second); err != nil {
		t.Errorf("selfTestS5: %v", err)
	}
}

// TestSelfTestOnlyFlag_ExitsZeroOnSuccess drives the actual compiled
// -selftest-only entry point end to end, proving the CLI surface (not just
// runSelfTest's Go-level contract) exits 0 with no -cases/-out required.
func TestSelfTestOnlyFlag_ExitsZeroOnSuccess(t *testing.T) {
	cmd := exec.Command("go", "run", ".", "-selftest-only")
	cmd.Dir = "."
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("parity -selftest-only failed: %v\n%s", err, out)
	}
}
