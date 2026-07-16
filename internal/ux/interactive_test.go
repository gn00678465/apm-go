package ux

import (
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"charm.land/huh/v2"
)

// stubRunField replaces the huh field-execution seam with fn for the
// duration of the test, so rich-mode field construction can be exercised
// without a real interactive terminal.
func stubRunField(t *testing.T, fn func(huh.Field) error) {
	t.Helper()
	prev := runField
	runField = fn
	t.Cleanup(func() { runField = prev })
}

// stubRunForm replaces the huh form-execution seam with fn for the duration
// of the test, so InputForm's group construction can be exercised without a
// real interactive terminal.
func stubRunForm(t *testing.T, fn func(*huh.Form) error) {
	t.Helper()
	prev := runForm
	runForm = fn
	t.Cleanup(func() { runForm = prev })
}

// stubRunMultiSelectField replaces MultiSelect's own huh field-execution
// seam with fn for the duration of the test, so its field construction (R19:
// help footer + Height) can be exercised without a real interactive terminal.
func stubRunMultiSelectField(t *testing.T, fn func(huh.Field) error) {
	t.Helper()
	prev := runMultiSelectField
	runMultiSelectField = fn
	t.Cleanup(func() { runMultiSelectField = prev })
}

// setRichMode overrides the package's richMode decision directly (bypassing
// Init/TTY detection), and forces CanPrompt() to the same boolean via the
// stdinIsTTY/stderrIsTTY seams (plus clearing CI, which CanPrompt also
// checks), so interactive-function tests are deterministic and never depend
// on the test runner's actual stdin/stderr/environment. It also overrides
// the spinnerIsRich seam the same way, so Spin tests can exercise the live
// animation goroutine without a real terminal writer.
func setRichMode(t *testing.T, rich bool) {
	t.Helper()
	prevRich := richMode
	prevSpinnerIsRich := spinnerIsRich
	richMode = rich
	spinnerIsRich = func(io.Writer) bool { return rich }
	withStdinTTY(t, rich)
	withStderrTTY(t, rich)
	t.Setenv("CI", "")
	t.Cleanup(func() {
		richMode = prevRich
		spinnerIsRich = prevSpinnerIsRich
	})
}

// runWithTimeout fails the test if fn does not return within the given
// duration, guarding against accidentally invoking a blocking huh form.
func runWithTimeout(t *testing.T, d time.Duration, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		fn()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatal("function did not return within timeout; it likely blocked on a huh prompt")
	}
}

func TestConfirm_NonTTYReturnsDefaultWithoutBlocking(t *testing.T) {
	// Arrange
	setRichMode(t, false)
	var got bool
	var err error

	// Act
	runWithTimeout(t, time.Second, func() {
		got, err = Confirm("proceed?", true)
	})

	// Assert
	if err != nil {
		t.Fatalf("Confirm() err = %v, want nil", err)
	}
	if got != true {
		t.Fatalf("Confirm() = %v, want default true", got)
	}
}

func TestInputText_NonTTYReturnsDefaultWithoutBlocking(t *testing.T) {
	// Arrange
	setRichMode(t, false)
	var got string
	var err error

	// Act
	runWithTimeout(t, time.Second, func() {
		got, err = InputText("name", "my-plugin")
	})

	// Assert
	if err != nil {
		t.Fatalf("InputText() err = %v, want nil", err)
	}
	if got != "my-plugin" {
		t.Fatalf("InputText() = %q, want %q", got, "my-plugin")
	}
}

func TestPassword_NonTTYReturnsEmptyWithoutBlocking(t *testing.T) {
	// Arrange
	setRichMode(t, false)
	var got string
	var err error

	// Act
	runWithTimeout(t, time.Second, func() {
		got, err = Password("token")
	})

	// Assert
	if err != nil {
		t.Fatalf("Password() err = %v, want nil", err)
	}
	if got != "" {
		t.Fatalf("Password() = %q, want empty string", got)
	}
}

func TestMultiSelect_NonTTYReturnsSelectedDefaultsWithoutBlocking(t *testing.T) {
	// Arrange
	setRichMode(t, false)
	opts := []Option{
		{Label: "claude", Value: "claude", Selected: true},
		{Label: "cursor", Value: "cursor", Selected: false},
		{Label: "codex", Value: "codex", Selected: true},
	}
	var got []string
	var err error

	// Act
	runWithTimeout(t, time.Second, func() {
		got, err = MultiSelect("targets", opts)
	})

	// Assert
	if err != nil {
		t.Fatalf("MultiSelect() err = %v, want nil", err)
	}
	want := []string{"claude", "codex"}
	if len(got) != len(want) {
		t.Fatalf("MultiSelect() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("MultiSelect() = %v, want %v", got, want)
		}
	}
}

func TestMultiSelect_NonTTYReturnsEmptyWhenNoneSelected(t *testing.T) {
	// Arrange
	setRichMode(t, false)
	opts := []Option{
		{Label: "claude", Value: "claude"},
		{Label: "cursor", Value: "cursor"},
	}

	// Act
	got, err := MultiSelect("targets", opts)

	// Assert
	if err != nil {
		t.Fatalf("MultiSelect() err = %v, want nil", err)
	}
	if len(got) != 0 {
		t.Fatalf("MultiSelect() = %v, want empty", got)
	}
}

func TestInputForm_NonTTYReturnsDefaultsWithoutBlocking(t *testing.T) {
	// Arrange
	setRichMode(t, false)
	fields := []Field{
		{Key: "name", Label: "Name", Default: "apm-go"},
		{Key: "version", Label: "Version", Default: "1.0.0"},
	}
	var got map[string]string
	var err error

	// Act
	runWithTimeout(t, time.Second, func() {
		got, err = InputForm("metadata", fields)
	})

	// Assert
	if err != nil {
		t.Fatalf("InputForm() err = %v, want nil", err)
	}
	if got["name"] != "apm-go" || got["version"] != "1.0.0" {
		t.Fatalf("InputForm() = %v, want defaults", got)
	}
}

func TestConfirm_RichModeBuildsFieldAndPropagatesRunResult(t *testing.T) {
	// Arrange
	setRichMode(t, true)
	stubRunField(t, func(huh.Field) error { return nil })

	// Act
	got, err := Confirm("proceed?", true)

	// Assert
	if err != nil {
		t.Fatalf("Confirm() err = %v, want nil", err)
	}
	if got != true {
		t.Fatalf("Confirm() = %v, want true", got)
	}
}

func TestConfirm_RichModePropagatesRunError(t *testing.T) {
	// Arrange
	setRichMode(t, true)
	wantErr := errors.New("boom")
	stubRunField(t, func(huh.Field) error { return wantErr })

	// Act
	_, err := Confirm("proceed?", false)

	// Assert
	if !errors.Is(err, wantErr) {
		t.Fatalf("Confirm() err = %v, want %v", err, wantErr)
	}
}

func TestInputText_RichModeBuildsFieldAndPropagatesRunResult(t *testing.T) {
	// Arrange
	setRichMode(t, true)
	stubRunField(t, func(huh.Field) error { return nil })

	// Act
	got, err := InputText("name", "my-plugin")

	// Assert
	if err != nil {
		t.Fatalf("InputText() err = %v, want nil", err)
	}
	if got != "my-plugin" {
		t.Fatalf("InputText() = %q, want %q", got, "my-plugin")
	}
}

func TestPassword_RichModeBuildsFieldAndPropagatesRunResult(t *testing.T) {
	// Arrange
	setRichMode(t, true)
	stubRunField(t, func(huh.Field) error { return nil })

	// Act
	got, err := Password("token")

	// Assert
	if err != nil {
		t.Fatalf("Password() err = %v, want nil", err)
	}
	if got != "" {
		t.Fatalf("Password() = %q, want empty (stub never sets a value)", got)
	}
}

func TestMultiSelect_RichModePropagatesRunError(t *testing.T) {
	// Arrange
	setRichMode(t, true)
	wantErr := errors.New("aborted")
	stubRunMultiSelectField(t, func(huh.Field) error { return wantErr })
	opts := []Option{{Label: "claude", Value: "claude", Selected: true}}

	// Act
	_, err := MultiSelect("targets", opts)

	// Assert
	if !errors.Is(err, wantErr) {
		t.Fatalf("MultiSelect() err = %v, want %v", err, wantErr)
	}
}

func TestMultiSelect_RichModeBuildsOptionsAndPropagatesRunResult(t *testing.T) {
	// Arrange
	setRichMode(t, true)
	stubRunMultiSelectField(t, func(huh.Field) error { return nil })
	opts := []Option{
		{Label: "claude", Value: "claude", Selected: true},
		{Label: "cursor", Value: "cursor"},
	}

	// Act
	got, err := MultiSelect("targets", opts)

	// Assert
	if err != nil {
		t.Fatalf("MultiSelect() err = %v, want nil", err)
	}
	// The stub never mutates the bound value, so it stays at its zero value.
	if len(got) != 0 {
		t.Fatalf("MultiSelect() = %v, want empty (stub never sets a value)", got)
	}
}

func TestInputForm_RichModeBuildsGroupAndReturnsDefaults(t *testing.T) {
	// Arrange
	setRichMode(t, true)
	stubRunForm(t, func(*huh.Form) error { return nil })
	fields := []Field{
		{Key: "name", Label: "Name", Default: "apm-go"},
		{Key: "author", Label: "Author", Default: "", Password: false},
	}

	// Act
	got, err := InputForm("metadata", fields)

	// Assert
	if err != nil {
		t.Fatalf("InputForm() err = %v, want nil", err)
	}
	// The stub never mutates the bound values, so each stays at its Default.
	if got["name"] != "apm-go" || got["author"] != "" {
		t.Fatalf("InputForm() = %v, want defaults preserved", got)
	}
}

func TestInputForm_RichModePropagatesRunError(t *testing.T) {
	// Arrange
	setRichMode(t, true)
	wantErr := errors.New("aborted")
	stubRunForm(t, func(*huh.Form) error { return wantErr })
	fields := []Field{{Key: "name", Label: "Name", Default: "apm-go"}}

	// Act
	_, err := InputForm("metadata", fields)

	// Assert
	if !errors.Is(err, wantErr) {
		t.Fatalf("InputForm() err = %v, want %v", err, wantErr)
	}
}

// buildTestMultiSelectField mirrors MultiSelect's field construction (title,
// options, Height(len(opts)+1)) without going through the CanPrompt/
// runMultiSelectField seam, so tests below can render huh's real Group/Field
// views directly -- proving R19's actual huh behavior instead of merely
// exercising a stub.
func buildTestMultiSelectField(labels []string) *huh.MultiSelect[string] {
	huhOpts := make([]huh.Option[string], len(labels))
	for i, l := range labels {
		huhOpts[i] = huh.NewOption(l, l)
	}
	return huh.NewMultiSelect[string]().
		Title("Select targets for this project").
		Options(huhOpts...).
		Height(len(labels) + 1)
}

// TestMultiSelect_HeightFitsAllOptionsUnclipped is the R19(a) regression:
// with >=5 options and Height(len(opts)+1) (as MultiSelect sets), rendering
// the field must show every option, not just the ones a small default
// viewport would otherwise clip to.
func TestMultiSelect_HeightFitsAllOptionsUnclipped(t *testing.T) {
	// Arrange
	labels := []string{"copilot", "claude", "opencode", "codex", "antigravity"}
	field := buildTestMultiSelectField(labels)

	// Act
	out := field.View()

	// Assert
	for _, label := range labels {
		if !strings.Contains(out, label) {
			t.Fatalf("MultiSelect view missing option %q, view was clipped:\n%s", label, out)
		}
	}
}

// TestMultiSelect_GroupShowHelpTrueRendersFooter is the R19(b) regression:
// a Group wrapping a MultiSelect field, wired into a Form with
// WithShowHelp(true) (what runMultiSelectField now uses, in place of
// runField's WithShowHelp(false)), renders a non-empty keybinding help
// footer; WithShowHelp(false) renders none. The field must go through
// huh.NewForm (not a bare Group) because that's what applies the default
// keymap (NewDefaultKeyMap) fields need to have any visible key bindings at
// all -- a bare Group's fields keep a zero-value keymap with no keys, which
// would make this test pass/fail for the wrong reason (empty keymap, not
// respecting showHelp).
func TestMultiSelect_GroupShowHelpTrueRendersFooter(t *testing.T) {
	// Arrange
	withHelpGroup := huh.NewGroup(buildTestMultiSelectField([]string{"a", "b"}))
	huh.NewForm(withHelpGroup).WithShowHelp(true)

	withoutHelpGroup := huh.NewGroup(buildTestMultiSelectField([]string{"a", "b"}))
	huh.NewForm(withoutHelpGroup).WithShowHelp(false)

	// Act
	helpFooter := withHelpGroup.Footer()
	noHelpFooter := withoutHelpGroup.Footer()

	// Assert
	if helpFooter == "" {
		t.Fatal("WithShowHelp(true) group produced an empty footer, want the keybinding help text")
	}
	if noHelpFooter != "" {
		t.Fatalf("WithShowHelp(false) group produced a non-empty footer: %q", noHelpFooter)
	}
}

func TestInputForm_RichModeWithValidateFunc(t *testing.T) {
	// Arrange
	setRichMode(t, true)
	stubRunForm(t, func(*huh.Form) error { return nil })
	fields := []Field{
		{Key: "name", Label: "Name", Default: "apm-go", Validate: func(s string) error {
			if s == "" {
				return errors.New("required")
			}
			return nil
		}},
	}

	// Act
	got, err := InputForm("metadata", fields)

	// Assert
	if err != nil {
		t.Fatalf("InputForm() err = %v, want nil", err)
	}
	if got["name"] != "apm-go" {
		t.Fatalf("InputForm() = %v, want default preserved", got)
	}
}
