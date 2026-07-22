package ux

import (
	"bytes"
	"errors"
	"runtime"
	"strings"
	"testing"
	"time"

	"charm.land/huh/v2"
)

// withUnicodeSupport forces the terminal-Unicode decision for the duration of
// the test, so symbol-set selection can be exercised without depending on the
// test runner's own terminal.
func withUnicodeSupport(t *testing.T, ok bool) {
	t.Helper()
	prev := supportsUnicode
	supportsUnicode = func() bool { return ok }
	t.Cleanup(func() { supportsUnicode = prev })
}

func TestClack_StepRendersTitleAndAnswerOnGutter(t *testing.T) {
	// Arrange
	withUnicodeSupport(t, true)
	var buf bytes.Buffer
	c := NewClack(&buf)

	// Act
	c.Step("Project name", "apm-go")

	// Assert
	want := "◇  Project name\n│  apm-go\n"
	if buf.String() != want {
		t.Fatalf("Step() =\n%q\nwant\n%q", buf.String(), want)
	}
}

func TestClack_StepPrefixesEveryLineOfAMultiLineAnswer(t *testing.T) {
	// Arrange
	withUnicodeSupport(t, true)
	var buf bytes.Buffer
	c := NewClack(&buf)

	// Act
	c.Step("Select targets", "claude\ncodex")

	// Assert
	want := "◇  Select targets\n│  claude\n│  codex\n"
	if buf.String() != want {
		t.Fatalf("Step() =\n%q\nwant\n%q", buf.String(), want)
	}
}

func TestClack_StepOmitsAnswerLineWhenAnswerIsEmpty(t *testing.T) {
	// Arrange
	withUnicodeSupport(t, true)
	var buf bytes.Buffer
	c := NewClack(&buf)

	// Act
	c.Step("Installation complete", "")

	// Assert
	want := "◇  Installation complete\n"
	if buf.String() != want {
		t.Fatalf("Step() =\n%q\nwant\n%q", buf.String(), want)
	}
}

// TestClack_IntroDetailBarAndOutroDrawTheConnectingLine also pins Outro's own
// leading bar: upstream clack detaches the closing message from the last step
// with one length of line, so Outro emits two lines, not one.
func TestClack_IntroDetailBarAndOutroDrawTheConnectingLine(t *testing.T) {
	// Arrange
	withUnicodeSupport(t, true)
	var buf bytes.Buffer
	c := NewClack(&buf)

	// Act
	c.Intro("apm-go init")
	c.Detail("Press ^C at any time to quit.")
	c.Bar()
	c.Outro("Done!")

	// Assert
	want := "┌  apm-go init\n│  Press ^C at any time to quit.\n│\n│\n└  Done!\n"
	if buf.String() != want {
		t.Fatalf("Intro/Detail/Bar/Outro =\n%q\nwant\n%q", buf.String(), want)
	}
}

// TestClack_WarnKeepsBothItsSeverityAndTheGutter pins the severity mapping in
// terminal-ux-contract §4: a non-fatal warning raised mid-run must still carry
// the Warn symbol (calling ux.Warn directly would keep the symbol but break
// the connecting line; Detail would keep the line but silently demote the
// warning to muted prose).
func TestClack_WarnKeepsBothItsSeverityAndTheGutter(t *testing.T) {
	// Arrange
	withUnicodeSupport(t, true)
	var buf bytes.Buffer
	c := NewClack(&buf)

	// Act
	c.Warn("No targets selected. %s will auto-detect.", "APM")

	// Assert
	want := "│  " + SymbolWarn + " No targets selected. APM will auto-detect.\n"
	if buf.String() != want {
		t.Fatalf("Warn() =\n%q\nwant\n%q", buf.String(), want)
	}
}

// TestClack_NoteDrawsAClosedBoxWithAlignedRightEdge covers the summary box
// (init's "About to create"): the title line opens the box with a horizontal
// rule, every body line is padded to a common inner width, and the closing
// line reconnects to the gutter. All lines must share one visual width or the
// right edge visibly steps in and out.
func TestClack_NoteDrawsAClosedBoxWithAlignedRightEdge(t *testing.T) {
	// Arrange
	withUnicodeSupport(t, true)
	var buf bytes.Buffer
	c := NewClack(&buf)

	// Act
	c.Note("About to create", []string{"name:    apm-go", "targets: claude, codex"})

	// Assert
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("Note() produced %d lines, want 4 (title, 2 body, closing):\n%s", len(lines), buf.String())
	}
	width := runeWidth(lines[0])
	for i, line := range lines {
		if got := runeWidth(line); got != width {
			t.Fatalf("Note() line %d width = %d, want %d (right edge not aligned):\n%s", i, got, width, buf.String())
		}
	}
	if !strings.HasPrefix(lines[0], "◇  About to create ─") || !strings.HasSuffix(lines[0], "╮") {
		t.Fatalf("Note() title line = %q, want an opening rule ending in ╮", lines[0])
	}
	if !strings.HasPrefix(lines[1], "│  name:    apm-go") || !strings.HasSuffix(lines[1], "│") {
		t.Fatalf("Note() body line = %q, want gutter-prefixed content closed by │", lines[1])
	}
	if !strings.HasPrefix(lines[3], "├─") || !strings.HasSuffix(lines[3], "╯") {
		t.Fatalf("Note() closing line = %q, want ├─...─╯", lines[3])
	}
}

func TestClack_FallsBackToASCIISymbolsWithoutUnicodeSupport(t *testing.T) {
	// Arrange
	withUnicodeSupport(t, false)
	var buf bytes.Buffer
	c := NewClack(&buf)

	// Act
	c.Intro("apm-go init")
	c.Step("Project name", "apm-go")
	c.Note("About to create", []string{"name: apm-go"})
	c.Outro("Done!")

	// Assert
	out := buf.String()
	for _, glyph := range []string{"◇", "│", "┌", "└", "├", "╮", "╯", "─"} {
		if strings.Contains(out, glyph) {
			t.Fatalf("ASCII mode emitted Unicode glyph %q:\n%s", glyph, out)
		}
	}
	if !strings.Contains(out, "o  Project name") || !strings.Contains(out, "|  apm-go") {
		t.Fatalf("ASCII mode missing fallback step transcript:\n%s", out)
	}
}

// TestClack_BannerPrintsArtOnlyWhenUnicodeIsSupported guards the block-art
// logo: it is drawn from box-drawing/block glyphs with no ASCII equivalent, so
// a terminal that cannot render them must get no banner at all rather than a
// field of replacement characters.
func TestClack_BannerPrintsArtOnlyWhenUnicodeIsSupported(t *testing.T) {
	const art = "█████╗\n██╔══╝"

	t.Run("unicode terminal prints the art", func(t *testing.T) {
		withUnicodeSupport(t, true)
		var buf bytes.Buffer
		NewClack(&buf).Banner(art)
		if !strings.Contains(buf.String(), "█████╗") {
			t.Fatalf("Banner() = %q, want the art", buf.String())
		}
	})

	t.Run("plain terminal prints nothing", func(t *testing.T) {
		withUnicodeSupport(t, false)
		var buf bytes.Buffer
		NewClack(&buf).Banner(art)
		if buf.Len() != 0 {
			t.Fatalf("Banner() = %q, want no output", buf.String())
		}
	})
}

// TestClack_WritesNoANSIEscapesToANonTerminalWriter is the per-writer
// decolorization contract (terminal-ux-contract §6): every ux renderer must go
// through lipgloss.Fprint*, which strips styling for a non-TTY writer.
func TestClack_WritesNoANSIEscapesToANonTerminalWriter(t *testing.T) {
	// Arrange
	withUnicodeSupport(t, true)
	var buf bytes.Buffer
	c := NewClack(&buf)

	// Act
	c.Banner("█╗")
	c.Intro("apm-go init")
	c.Step("Project name", "apm-go")
	c.Note("About to create", []string{"name: apm-go"})
	c.Outro("Done!")

	// Assert
	if strings.Contains(buf.String(), "\x1b[") {
		t.Fatalf("Clack wrote ANSI escapes to a non-terminal writer:\n%q", buf.String())
	}
}

// TestClack_PromptsRecordTheirAnswerOnTheTranscript covers the pairing that
// Clack exists to guarantee: a prompt that completes leaves a step behind, so
// the answer survives huh clearing its own form.
func TestClack_PromptsRecordTheirAnswerOnTheTranscript(t *testing.T) {
	t.Run("Confirm records Yes/No", func(t *testing.T) {
		// Arrange
		withUnicodeSupport(t, true)
		setRichMode(t, true)
		stubRunField(t, func(huh.Field) error { return nil })
		var buf bytes.Buffer
		c := NewClack(&buf)

		// Act
		got, err := c.Confirm("Is this OK?", true)

		// Assert
		if err != nil || !got {
			t.Fatalf("Confirm() = %v, %v; want true, nil", got, err)
		}
		if want := "◇  Is this OK?\n│  Yes\n"; buf.String() != want {
			t.Fatalf("Confirm() transcript =\n%q\nwant\n%q", buf.String(), want)
		}
	})

	t.Run("Form records one line per field and masks passwords", func(t *testing.T) {
		// Arrange
		withUnicodeSupport(t, true)
		setRichMode(t, true)
		stubRunForm(t, func(*huh.Form) error { return nil })
		var buf bytes.Buffer
		c := NewClack(&buf)
		fields := []Field{
			{Key: "name", Label: "Project name", Default: "apm-go"},
			{Key: "token", Label: "Token", Default: "abcd", Password: true},
		}

		// Act
		values, err := c.Form("Project metadata", fields)

		// Assert
		if err != nil {
			t.Fatalf("Form() err = %v, want nil", err)
		}
		if values["name"] != "apm-go" || values["token"] != "abcd" {
			t.Fatalf("Form() = %v, want defaults preserved", values)
		}
		want := "◇  Project metadata\n│  Project name: apm-go\n│  Token: ****\n"
		if buf.String() != want {
			t.Fatalf("Form() transcript =\n%q\nwant\n%q", buf.String(), want)
		}
	})

	t.Run("MultiSelect records an empty selection as (none)", func(t *testing.T) {
		// Arrange
		withUnicodeSupport(t, true)
		setRichMode(t, true)
		stubRunMultiSelectField(t, func(huh.Field) error { return nil })
		var buf bytes.Buffer
		c := NewClack(&buf)

		// Act
		got, err := c.MultiSelect("Select targets", []Option{{Label: "claude", Value: "claude"}})

		// Assert
		if err != nil || len(got) != 0 {
			t.Fatalf("MultiSelect() = %v, %v; want empty, nil", got, err)
		}
		if want := "◇  Select targets\n│  (none)\n"; buf.String() != want {
			t.Fatalf("MultiSelect() transcript =\n%q\nwant\n%q", buf.String(), want)
		}
	})
}

// TestClack_PromptsWriteNothingWhenTheyCannotRun keeps the transcript honest:
// a prompt that never reached the user must not leave a step claiming they
// answered it.
func TestClack_PromptsWriteNothingWhenTheyCannotRun(t *testing.T) {
	tests := []struct {
		name string
		call func(*Clack) error
	}{
		{
			name: "Confirm",
			call: func(c *Clack) error {
				got, err := c.Confirm("Is this OK?", true)
				if err == nil && !got {
					return errors.New("Confirm did not return its default")
				}
				return err
			},
		},
		{
			name: "Form",
			call: func(c *Clack) error {
				got, err := c.Form("Project metadata", []Field{{Key: "name", Label: "Name", Default: "apm-go"}})
				if err == nil && got["name"] != "apm-go" {
					return errors.New("Form did not return its defaults")
				}
				return err
			},
		},
		{
			name: "MultiSelect",
			call: func(c *Clack) error {
				got, err := c.MultiSelect("Select targets", []Option{{Label: "claude", Value: "claude", Selected: true}})
				if err == nil && (len(got) != 1 || got[0] != "claude") {
					return errors.New("MultiSelect did not return its pre-selected defaults")
				}
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			withUnicodeSupport(t, true)
			setRichMode(t, false)
			var buf bytes.Buffer
			c := NewClack(&buf)

			// Act
			var err error
			runWithTimeout(t, time.Second, func() { err = tt.call(c) })

			// Assert
			if err != nil {
				t.Fatalf("%s err = %v, want nil", tt.name, err)
			}
			if buf.Len() != 0 {
				t.Fatalf("%s wrote a transcript without prompting:\n%s", tt.name, buf.String())
			}
		})
	}
}

// TestClack_PromptErrorsPropagateWithoutATranscript covers the abort path
// (Ctrl-C): the error reaches the caller and no step is recorded.
func TestClack_PromptErrorsPropagateWithoutATranscript(t *testing.T) {
	wantErr := errors.New("aborted")

	tests := []struct {
		name string
		stub func(*testing.T, error)
		call func(*Clack) error
	}{
		{
			name: "Confirm",
			stub: func(t *testing.T, err error) { stubRunField(t, func(huh.Field) error { return err }) },
			call: func(c *Clack) error { _, err := c.Confirm("Is this OK?", true); return err },
		},
		{
			name: "Form",
			stub: func(t *testing.T, err error) { stubRunForm(t, func(*huh.Form) error { return err }) },
			call: func(c *Clack) error {
				_, err := c.Form("Project metadata", []Field{{Key: "name", Label: "Name"}})
				return err
			},
		},
		{
			name: "MultiSelect",
			stub: func(t *testing.T, err error) {
				stubRunMultiSelectField(t, func(huh.Field) error { return err })
			},
			call: func(c *Clack) error {
				_, err := c.MultiSelect("Select targets", []Option{{Label: "claude", Value: "claude"}})
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			withUnicodeSupport(t, true)
			setRichMode(t, true)
			tt.stub(t, wantErr)
			var buf bytes.Buffer
			c := NewClack(&buf)

			// Act
			err := tt.call(c)

			// Assert
			if !errors.Is(err, wantErr) {
				t.Fatalf("%s err = %v, want %v", tt.name, err, wantErr)
			}
			if buf.Len() != 0 {
				t.Fatalf("%s recorded a step for a prompt that failed:\n%s", tt.name, buf.String())
			}
		})
	}
}

func TestSupportsUnicode_DecisionMatrix(t *testing.T) {
	tests := []struct {
		name        string
		env         map[string]string
		wantWindows bool
		wantPosix   bool
	}{
		{
			name:        "windows terminal session",
			env:         map[string]string{"WT_SESSION": "abc", "TERM": ""},
			wantWindows: true,
			wantPosix:   false,
		},
		{
			name:        "hosted terminal program",
			env:         map[string]string{"TERM_PROGRAM": "vscode", "TERM": ""},
			wantWindows: true,
			wantPosix:   false,
		},
		{
			name:        "xterm",
			env:         map[string]string{"TERM": "xterm-256color"},
			wantWindows: true,
			wantPosix:   true,
		},
		{
			name:        "linux console",
			env:         map[string]string{"TERM": "linux"},
			wantWindows: false,
			wantPosix:   false,
		},
		{
			name:        "dumb terminal",
			env:         map[string]string{"TERM": "dumb"},
			wantWindows: false,
			wantPosix:   false,
		},
		{
			name:        "bare console with nothing set",
			env:         map[string]string{"TERM": ""},
			wantWindows: false,
			wantPosix:   false,
		},
		{
			// NO_COLOR means "don't colorize", not "don't use Unicode" -- the
			// same reasoning CanPrompt applies to prompting.
			name:        "NO_COLOR does not disable Unicode",
			env:         map[string]string{"TERM": "xterm-256color", "NO_COLOR": "1"},
			wantWindows: true,
			wantPosix:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			for _, key := range []string{"WT_SESSION", "TERM_PROGRAM", "ConEmuANSI", "TERM", "NO_COLOR"} {
				t.Setenv(key, tt.env[key])
			}

			// Act
			got := defaultSupportsUnicode()

			// Assert
			want := tt.wantPosix
			if runtime.GOOS == "windows" {
				want = tt.wantWindows
			}
			if got != want {
				t.Fatalf("defaultSupportsUnicode() = %v, want %v on %s", got, want, runtime.GOOS)
			}
		})
	}
}
