package main

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/ux"
	"github.com/spf13/cobra"
)

// captureStderr redirects os.Stderr for the duration of fn and returns what
// was written. The old terminal-ux-contract §3 citation that described init
// success output as stderr-only is retired: ticket 19 Finding 2 aligns init
// and plugin init with the Oracle's stdout-only success block. This helper
// remains for the interactive Clack transcript and other stderr assertions.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() err = %v", err)
	}
	orig := os.Stderr
	os.Stderr = w

	fn()

	os.Stderr = orig
	if err := w.Close(); err != nil {
		t.Fatalf("close pipe writer: %v", err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read captured stderr: %v", err)
	}
	return string(out)
}

// clackGlyphs are the transcript/banner characters that must never reach a
// non-interactive run's output. The shared Oracle-aligned success table/panel
// legitimately uses box-drawing vertical bars, so "│" is not a Clack-only
// marker anymore.
var clackGlyphs = []string{"█", "╗", "┌", "◇", "└"}

// TestInitCmd_NonInteractiveRunsPrintNoBannerOrTranscript pins the gating in
// PRD R1/R4: the clack transcript and the block-art banner belong to
// interactive runs only. A --yes run and a non-TTY run must keep the plain
// prefix output they had before issue #14, so scripts and CI logs are
// unchanged.
func TestInitCmd_NonInteractiveRunsPrintNoBannerOrTranscript(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "--yes", args: []string{"--yes", "--target", "claude"}},
		{name: "non-TTY without --yes", args: []string{"--target", "claude"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			dir := t.TempDir()
			origDir, _ := os.Getwd()
			if err := os.Chdir(dir); err != nil {
				t.Fatalf("chdir: %v", err)
			}
			t.Cleanup(func() { os.Chdir(origDir) })

			restore := ux.SetTTYSeamsForTest(false, false, false)
			t.Cleanup(restore)

			// Act
			var runErr error
			var stdout string
			stderr := captureStderr(t, func() {
				stdout = captureStdout(t, func() {
					cmd := initCmd()
					cmd.SetArgs(tt.args)
					runErr = cmd.Execute()
				})
			})

			// Assert
			if runErr != nil {
				t.Fatalf("init failed: %v", runErr)
			}
			for _, glyph := range clackGlyphs {
				if strings.Contains(stdout, glyph) || strings.Contains(stderr, glyph) {
					t.Fatalf("non-interactive init emitted clack glyph %q:\nstdout=%s\nstderr=%s", glyph, stdout, stderr)
				}
			}
			if !strings.Contains(stdout, "APM project initialized successfully!") {
				t.Fatalf("non-interactive init lost its plain success output:\n%s", stdout)
			}
			if stderr != "" {
				t.Fatalf("non-interactive init success wrote to stderr:\n%s", stderr)
			}
		})
	}
}

// captureInteractiveInit drives the production init path with the existing
// prompt seams while capturing both process streams. Clack writes its
// transcript to os.Stderr, so this proves the command does not bypass the
// frame through a direct stdout printer.
func captureInteractiveInit(t *testing.T, cmd *cobra.Command, args []string) (stdout, transcript string, cap *interactiveCapture) {
	t.Helper()
	stdout, transcript = "", ""
	stdout, transcript = captureInitOutput(t, func() {
		cap = driveInteractiveInit(t, cmd, args, true)
	})
	return stdout, transcript, cap
}

// assertClackTranscript checks the frame rather than a particular Unicode
// capability. NewClack deliberately has an ASCII fallback, so both its ASCII
// and Unicode clack glyphs are accepted while every line between Intro and
// Outro must still belong to the connected transcript.
func assertClackTranscript(t *testing.T, transcript string, want []string) {
	t.Helper()
	lines := strings.Split(strings.TrimSuffix(transcript, "\n"), "\n")
	intro, outro := -1, -1
	for i, line := range lines {
		if strings.Contains(line, "Setting up your APM") && (strings.HasPrefix(line, "┌  ") || strings.HasPrefix(line, "T  ")) {
			intro = i
		}
		if strings.HasSuffix(line, "  Done!") && (strings.HasPrefix(line, "└  ") || strings.HasPrefix(line, "-  ")) {
			outro = i
			break
		}
	}
	if intro < 0 || outro <= intro {
		t.Fatalf("transcript has no ordered Intro/Outro frame:\n%s", transcript)
	}

	for i := intro + 1; i < outro; i++ {
		line := lines[i]
		if line == "" {
			t.Errorf("transcript line %d is empty inside Intro/Outro frame", i+1)
			continue
		}
		if !strings.ContainsAny(string([]rune(line)[:1]), "│|◇o├+╮╯─-") {
			t.Errorf("transcript line %d escapes the clack frame: %q", i+1, line)
		}
	}
	for _, text := range want {
		if !strings.Contains(transcript, text) {
			t.Errorf("transcript missing %q:\n%s", text, transcript)
		}
	}
}

func TestInteractiveInitSuccessStaysInsideClackFrame(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	tests := []struct {
		name string
		cmd  func() *cobra.Command
		args []string
		mode initMode
	}{
		{name: "init", cmd: initCmd, args: []string{"demo"}, mode: consumerMode},
		{name: "plugin init", cmd: pluginInitCmd, args: []string{"pf"}, mode: pluginMode},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, transcript, cap := captureInteractiveInit(t, tt.cmd(), tt.args)
			if cap.runErr != nil {
				t.Fatalf("interactive command failed: %v", cap.runErr)
			}
			if stdout != "" {
				t.Fatalf("interactive success leaked outside the clack frame to stdout:\n%s", stdout)
			}

			want := append([]string{
				"[*] " + tt.mode.successTitle,
				"    Created Files",
				"File",
				"Description",
				"Next Steps",
				"  Docs: https://microsoft.github.io/apm  |  Star: https://github.com/microsoft/apm",
				"[>] Created project directory: " + tt.args[0],
				"[>] Initializing APM project: " + tt.args[0],
			}, tt.mode.nextSteps...)
			assertClackTranscript(t, transcript, want)
			// The Oracle block keeps its own glyphs and box-drawing, but every
			// one of its lines must hang off the gutter -- a "[>]", "[*]" or
			// box line at column 0 is the exact defect this test guards.
			for i, line := range strings.Split(transcript, "\n") {
				if strings.HasPrefix(line, "[") || strings.HasPrefix(line, "╭") || strings.HasPrefix(line, "╰") || strings.HasPrefix(line, "    Created Files") {
					t.Errorf("transcript line %d is outside the clack gutter: %q", i+1, line)
				}
			}
		})
	}
}

func TestClackRendererIncludesCodexTip(t *testing.T) {
	_, transcript := captureInitOutput(t, func() {
		ck := ux.NewClack(os.Stderr)
		ck.Intro("Setting up your APM project")
		clackRenderer(ck, "APM project initialized successfully!", initSuccessContent{
			files:     []string{"apm.yml"},
			nextSteps: []string{"Install a package: apm-go install <owner>/<repo>"},
			codexTip:  "Tip: Use '--target agent-skills' to also deploy skills to .agents/skills/ for other clients.",
			docsLine:  "Docs: https://microsoft.github.io/apm  |  Star: https://github.com/microsoft/apm",
		})
		ck.Outro("Done!")
	})
	assertClackTranscript(t, transcript, []string{
		"Tip: Use '--target agent-skills' to also deploy skills to .agents/skills/ for other clients.",
	})
}
