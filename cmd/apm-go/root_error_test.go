package main

import (
	"strings"
	"testing"
)

// TestRootError_UnverifiedFlagErrorsMatchPreTicket13Shape is ticket 13
// attempt 2's regression test (eval-ticket-13.md finding 2): an unrelated
// cobra flag-parse error (an unknown flag, a shorthand missing its
// argument) must render EXACTLY the pre-ticket-13 shape -- plain
// withExitCode(2, ...), rendered via ux.Error ("[x] <message>" on stdout,
// nothing on stderr) -- byte-for-byte, because its own Oracle wording was
// never verified (probed directly for this ticket: `pack --bogus`'s
// Oracle message is "No such option: --bogus Did you mean --verbose?",
// nothing like cobra's own "unknown flag: --bogus"). Only the verified
// --format selector errors get the Usage-preamble treatment
// (TestPackCmd_FormatConflict_Exit2 and friends in pack_format_test.go
// already cover those).
//
// Expected bytes captured directly from the pre-ticket-13 binary (commit
// 843bbc5, the parent of f3ff78f) for each of these exact invocations,
// not re-derived from reading the code.
func TestRootError_UnverifiedFlagErrorsMatchPreTicket13Shape(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantStdout string
	}{
		{"pack unknown long flag", []string{"pack", "--bogus"}, " x unknown flag: --bogus\n"},
		{"pack shorthand missing argument", []string{"pack", "-m"}, " x Option ''m' in -m' requires an argument.\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chdirTemp(t)

			root := buildRootCmd()
			root.SetArgs(tt.args)

			var stdout string
			var exitCode int
			stderr := captureStderr(t, func() {
				stdout = captureStdout(t, func() {
					cmd, err := root.ExecuteC()
					if err == nil {
						t.Fatal("expected an error")
					}
					exitCode = renderRootError(cmd, err)
				})
			})

			if exitCode != 2 {
				t.Errorf("exit code = %d, want 2", exitCode)
			}
			if stdout != tt.wantStdout {
				t.Errorf("stdout = %q, want %q", stdout, tt.wantStdout)
			}
			if stderr != "" {
				t.Errorf("stderr = %q, want empty (pre-ticket-13 shape has no Usage preamble for this error)", stderr)
			}
			if strings.Contains(stdout, "Usage:") || strings.Contains(stderr, "Usage:") {
				t.Errorf("output must not contain a Usage preamble for an unverified flag error: stdout=%q stderr=%q", stdout, stderr)
			}
		})
	}
}

// TestRootError_InitUnknownFlagMatchesOracle covers ticket 19 Finding 2's
// one-off parse surface. Click renders this as a UsageError before init can
// touch the filesystem: stdout is empty, stderr carries the Usage preamble
// and "No such option", and the process exits 2. The preamble is cobra's own
// UseLine/CommandPath (apm-go's name, cobra's [flags] layout): the binary
// name is folded by the runner's rewrite_binary_name and the Click-vs-Cobra
// usage layout is a waived rendering class (init-unknown-flag's waiver),
// exactly as plugin-init-unknown is handled -- the Oracle's spelling is NOT
// hardcoded per command. The error line itself is byte-identical.
func TestRootError_InitUnknownFlagMatchesOracle(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantStderr string
	}{
		{
			name: "consumer init",
			args: []string{"init", "--name", "x"},
			wantStderr: "Usage: apm-go init [project-name] [flags]\n" +
				"Try 'apm-go init --help' for help.\n\n" +
				"Error: No such option: --name\n",
		},
		{
			name: "plugin init",
			args: []string{"plugin", "init", "--name", "x"},
			wantStderr: "Usage: apm-go plugin init [project-name] [flags]\n" +
				"Try 'apm-go plugin init --help' for help.\n\n" +
				"Error: No such option: --name\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chdirTemp(t)
			root := buildRootCmd()
			root.SetArgs(tt.args)

			var stdout string
			var exitCode int
			stderr := captureStderr(t, func() {
				stdout = captureStdout(t, func() {
					cmd, err := root.ExecuteC()
					if err == nil {
						t.Fatal("expected an error")
					}
					exitCode = renderRootError(cmd, err)
				})
			})

			if exitCode != 2 {
				t.Errorf("exit code = %d, want 2", exitCode)
			}
			if stdout != "" {
				t.Errorf("stdout = %q, want empty", stdout)
			}
			if stderr != tt.wantStderr {
				t.Errorf("stderr = %q, want %q", stderr, tt.wantStderr)
			}
		})
	}
}

// TestRootError_VerifiedFormatSelectorErrorsKeepPreambleShape is the
// counterpart of the test above: the --format selector errors THIS
// ticket's own attempt-1 verified against the Oracle (a conflicting
// selector, and the bare no-preamble missing-argument case) must still
// render the Usage-preamble/bare-error shapes through renderRootError,
// confirming the constrained hook did not also regress the cases it was
// verified to fix.
func TestRootError_VerifiedFormatSelectorErrorsKeepPreambleShape(t *testing.T) {
	tests := []struct {
		name          string
		args          []string
		wantPreamble  bool
		wantErrorLine string
	}{
		{
			"format conflict has the full preamble",
			[]string{"pack", "--format", "claude", "--claude-plugin"},
			true,
			"Error: Choose one bundle format selector; received: --format claude, --claude-plugin\n",
		},
		{
			"format missing argument is bare (no preamble)",
			[]string{"pack", "--format"},
			false,
			"Error: Option '--format' requires an argument.\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chdirTemp(t)

			root := buildRootCmd()
			root.SetArgs(tt.args)

			var stdout string
			var exitCode int
			stderr := captureStderr(t, func() {
				stdout = captureStdout(t, func() {
					cmd, err := root.ExecuteC()
					if err == nil {
						t.Fatal("expected an error")
					}
					exitCode = renderRootError(cmd, err)
				})
			})

			if exitCode != 2 {
				t.Errorf("exit code = %d, want 2", exitCode)
			}
			if stdout != "" {
				t.Errorf("stdout = %q, want empty (usage errors render on stderr)", stdout)
			}
			if !strings.HasSuffix(stderr, tt.wantErrorLine) {
				t.Errorf("stderr = %q, want it to end with %q", stderr, tt.wantErrorLine)
			}
			hasPreamble := strings.Contains(stderr, "Usage: ") && strings.Contains(stderr, "Try '")
			if hasPreamble != tt.wantPreamble {
				t.Errorf("stderr = %q, want preamble=%v", stderr, tt.wantPreamble)
			}
		})
	}
}
