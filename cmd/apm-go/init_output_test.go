package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/huh/v2"

	"github.com/apm-go/apm/internal/ux"
)

func captureInitOutput(t *testing.T, fn func()) (stdout, stderr string) {
	t.Helper()
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stderr pipe: %v", err)
	}
	origOut, origErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outW, errW
	fn()
	os.Stdout, os.Stderr = origOut, origErr
	if err := outW.Close(); err != nil {
		t.Fatalf("close stdout pipe: %v", err)
	}
	if err := errW.Close(); err != nil {
		t.Fatalf("close stderr pipe: %v", err)
	}
	out, err := io.ReadAll(outR)
	if err != nil {
		t.Fatalf("read stdout pipe: %v", err)
	}
	errOut, err := io.ReadAll(errR)
	if err != nil {
		t.Fatalf("read stderr pipe: %v", err)
	}
	return string(out), string(errOut)
}

func assertContainsAll(t *testing.T, output string, want []string) {
	t.Helper()
	for _, line := range want {
		if !strings.Contains(output, line) {
			t.Errorf("output missing %q:\n%s", line, output)
		}
	}
}

func TestInitSuccessOutput_ConsumerAndPlugin(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		success    string
		files      []string
		nextSteps  []string
		agentrcTip bool
	}{
		{
			name:    "consumer",
			args:    []string{"init", "consumer", "--yes"},
			success: " + APM project initialized successfully!",
			files:   []string{"│ *    │ apm.yml"},
			nextSteps: []string{
				"* Install a package:               apm-go install <owner>/<repo>",
				"* Run a script:                    apm-go run <script>",
				"* Build a plugin? Scaffold one:    apm-go plugin init",
				"* Publishing a marketplace?:       apm-go marketplace init",
			},
			agentrcTip: true,
		},
		{
			name:    "claude plugin",
			args:    []string{"plugin", "init", "plugin", "--yes"},
			success: " + APM project initialized successfully!",
			files:   []string{"│ *    │ apm.yml", "│ *    │ plugin.json"},
			nextSteps: []string{
				"* Add dev dependencies:    apm-go install --dev <owner>/<repo>",
				"* Pack as Agent Plugins v1:             apm-go pack --format agent-plugin",
				"* Pack as Claude plugin:                apm-go pack --format claude-plugin",
			},
		},
		{
			name:    "agent plugin",
			args:    []string{"plugin", "init", "agent-plugin", "--format", "agent-plugin", "--yes"},
			success: " + APM project initialized successfully!",
			files:   []string{"│ *    │ apm.yml", "│ *    │ plugin.json", "│ *    │ mcp.json"},
			nextSteps: []string{
				"* Add dev dependencies:    apm-go install --dev <owner>/<repo>",
				"* Pack as Agent Plugins v1:             apm-go pack --format agent-plugin",
				"* Pack as Claude plugin:                apm-go pack --format claude-plugin",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			origDir, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Chdir(dir); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chdir(origDir) })
			t.Setenv("PATH", t.TempDir())
			restoreTTY := ux.SetTTYSeamsForTest(false, false, false)
			t.Cleanup(restoreTTY)

			stdout, stderr := captureInitOutput(t, func() {
				root := buildRootCmd()
				root.SetArgs(tt.args)
				if err := root.Execute(); err != nil {
					t.Fatalf("init failed: %v", err)
				}
			})

			assertContainsAll(t, stdout, append([]string{
				" > ",
				tt.success,
				" Created Files",
				"File",
				"Description",
				"Next Steps",
			}, append(tt.files, tt.nextSteps...)...))
			if tt.agentrcTip && !strings.Contains(stdout, "Tip: Use agentrc to generate tailored agent instructions from your codebase.") {
				t.Errorf("consumer output missing Oracle agentrc tip:\n%s", stdout)
			}
			if !tt.agentrcTip && strings.Contains(stdout, "agentrc") {
				t.Errorf("plugin output unexpectedly contains agentrc guidance:\n%s", stdout)
			}
			if stderr != "" {
				t.Errorf("success output leaked to stderr:\n%s", stderr)
			}
		})
	}
}

func TestInitSuccessOutput_AgentrcAndInstructionBranches(t *testing.T) {
	tests := []struct {
		name          string
		withAgentrc   bool
		withInstrFile bool
		withCodex     bool
		want          string
		reject        string
	}{
		{
			name:        "agentrc installed without instructions",
			withAgentrc: true,
			want:        "* Generate agent instructions:     agentrc init",
			reject:      "Tip: Use agentrc to generate tailored agent instructions",
		},
		{
			name:          "existing instructions suppress suggestion",
			withAgentrc:   true,
			withInstrFile: true,
			reject:        "agentrc",
		},
		{
			name:   "agentrc absent",
			want:   "Tip: Use agentrc to generate tailored agent instructions from your codebase.",
			reject: "* Generate agent instructions:     agentrc init",
		},
		{
			name:      "codex target tip",
			withCodex: true,
			want:      "Tip: Use '--target agent-skills' to also deploy skills to .agents/skills/ for other clients.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			origDir, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Chdir(dir); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chdir(origDir) })

			pathDir := t.TempDir()
			if tt.withAgentrc {
				agentrc := filepath.Join(pathDir, "agentrc")
				if err := os.WriteFile(agentrc, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("PATH", pathDir)
			if tt.withInstrFile {
				if err := os.WriteFile("AGENTS.md", []byte("existing\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if tt.withCodex {
				if err := os.Mkdir(".codex", 0o755); err != nil {
					t.Fatal(err)
				}
			}
			restoreTTY := ux.SetTTYSeamsForTest(false, false, false)
			t.Cleanup(restoreTTY)

			stdout, stderr := captureInitOutput(t, func() {
				root := buildRootCmd()
				root.SetArgs([]string{"init", "--yes"})
				if err := root.Execute(); err != nil {
					t.Fatalf("init failed: %v", err)
				}
			})
			if tt.want != "" && !strings.Contains(stdout, tt.want) {
				t.Errorf("output missing %q:\n%s", tt.want, stdout)
			}
			if tt.reject != "" && strings.Contains(stdout, tt.reject) {
				t.Errorf("output contains suppressed guidance %q:\n%s", tt.reject, stdout)
			}
			if stderr != "" {
				t.Errorf("success output leaked to stderr:\n%s", stderr)
			}
		})
	}
}

func TestInitInteractiveFinalSuccessBlockUsesClackTranscript(t *testing.T) {
	dir := t.TempDir()
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	t.Setenv("PATH", t.TempDir())
	t.Setenv("CI", "")
	if err := os.WriteFile("hooks.json", []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	restoreTTY := ux.SetTTYSeamsForTest(true, true, true)
	t.Cleanup(restoreTTY)
	restorePrompts := ux.SetPromptSeamsForTest(
		func(_ huh.Theme, _ string, def bool) (bool, error) { return true, nil },
		func(_ huh.Theme, _ string, _ string, _ bool, _ []ux.Option) ([]string, error) {
			return []string{"claude"}, nil
		},
		func(_ huh.Theme, _ string, _ bool, fields []ux.Field) (map[string]string, error) {
			values := make(map[string]string, len(fields))
			for _, field := range fields {
				values[field.Key] = field.Default
			}
			return values, nil
		},
	)
	t.Cleanup(restorePrompts)

	stdout, stderr := captureInitOutput(t, func() {
		root := buildRootCmd()
		root.SetArgs([]string{"init", "interactive", "--target", "claude"})
		if err := root.Execute(); err != nil {
			t.Fatalf("interactive init failed: %v", err)
		}
	})

	if stdout != "" {
		t.Fatalf("interactive success block leaked to stdout outside the clack frame:\n%s", stdout)
	}
	if !strings.Contains(stdout+stderr, "plugin-native") {
		t.Errorf("interactive native-source warning missing:\nstdout=%s\nstderr=%s", stdout, stderr)
	}
	assertClackTranscript(t, stderr, []string{
		"  Initializing ",
		" Initializing APM project: interactive",
		" APM project initialized successfully!",
		" Created Files",
		"│ File",
		"apm.yml",
		" Next Steps",
		"* Install a package:               apm-go install <owner>/<repo>",
	})
	// The Oracle block is embedded verbatim -- glyphs and box-drawing kept --
	// but every one of its lines hangs off the gutter. A "[>]", "[*]" or box
	// corner at column 0 is the frame break this test guards against.
	for i, line := range framedLines(stderr) {
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "╭") || strings.HasPrefix(line, "╰") {
			t.Errorf("transcript line %d is outside the clack gutter: %q", i+1, line)
		}
	}
	for _, bracketed := range []string{"[>] ", "[*] ", "[i] "} {
		if strings.Contains(stderr, bracketed) {
			t.Errorf("interactive transcript uses a bracketed status form %q:\n%s", bracketed, stderr)
		}
	}
}
