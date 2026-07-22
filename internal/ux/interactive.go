package ux

import (
	"os"

	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
)

// Option is a single choice for MultiSelect. Selected marks the option as
// pre-selected, which also acts as the default returned when prompting
// isn't possible (see MultiSelect).
type Option struct {
	Label    string
	Value    string
	Selected bool
}

// Field describes a single field of an InputForm group.
type Field struct {
	Key      string
	Label    string
	Default  string
	Password bool
	Validate func(string) error
}

// runField executes a huh field. It is a swappable seam so tests can
// exercise field construction (Title/Value/Options/theme wiring) without
// needing a real interactive terminal.
//
// It reproduces huh.Run(field)'s single-field wrapping (NewForm(NewGroup(f))
// with help hidden) but binds output to stderr explicitly: without WithOutput,
// huh's accessible mode (triggered by TERM=dumb) defaults prompts to stdout,
// which would contaminate a command's redirected stdout and appear to hang.
var runField = func(f huh.Field) error {
	return huh.NewForm(huh.NewGroup(f)).
		WithShowHelp(false).
		WithOutput(os.Stderr).
		Run()
}

// runForm executes a huh form. It is a swappable seam so tests can exercise
// form construction without needing a real interactive terminal.
var runForm = func(f *huh.Form) error {
	return f.Run()
}

// runMultiSelectField executes a huh MultiSelect field with its own form
// wrapping, separate from runField's single-field wrapper. It is a swappable
// seam so tests can exercise field construction without needing a real
// interactive terminal.
//
// R19: runField's WithShowHelp(false) (added for the accessible-output fix)
// also silently disabled MultiSelect's toggle/move/confirm keybinding help
// footer -- MultiSelect alone needs it back, so it gets its own wrapper
// (WithShowHelp(true)) instead of changing runField for every field kind.
// WithOutput(os.Stderr) is kept for the same reason as runField (see its
// doc comment): huh's accessible mode (TERM=dumb) defaults prompts to
// stdout otherwise.
var runMultiSelectField = func(f huh.Field) error {
	return huh.NewForm(huh.NewGroup(f)).
		WithShowHelp(true).
		WithOutput(os.Stderr).
		Run()
}

// Confirm asks a yes/no question with the given default. When prompting
// isn't possible (non-TTY stdin/stderr, or CI -- see CanPrompt) it returns
// def immediately without prompting.
func Confirm(prompt string, def bool) (bool, error) {
	return confirmWith(Theme(), prompt, def)
}

// confirmWith is Confirm with the theme left to the caller, so Clack can put
// the prompt on its connecting line without restyling every other command's
// confirmations.
func confirmWith(theme huh.Theme, prompt string, def bool) (bool, error) {
	if !CanPrompt() {
		return def, nil
	}

	val := def
	field := huh.NewConfirm().
		Title(prompt).
		Value(&val).
		// huh defaults buttonAlignment to lipgloss.Center and renders the
		// button row in a box as wide as max(titleWidth, buttonsWidth), so any
		// question longer than "Yes  No" pushes the buttons toward the middle,
		// visually detached from the question (issue #14). Left-align them so
		// they start in the question's own column. This must be chained before
		// WithTheme, which returns the Field interface rather than *Confirm.
		WithButtonAlignment(lipgloss.Left).
		WithTheme(theme)
	err := runField(field)
	return val, err
}

// InputText asks for a single line of text, prefilled with def. When
// prompting isn't possible (see CanPrompt) it returns def immediately
// without prompting.
func InputText(label, def string) (string, error) {
	if !CanPrompt() {
		return def, nil
	}

	val := def
	field := huh.NewInput().
		Title(label).
		Value(&val).
		WithTheme(Theme())
	err := runField(field)
	return val, err
}

// Password asks for a masked secret value. When prompting isn't possible
// (see CanPrompt) it returns an empty string immediately without prompting.
func Password(label string) (string, error) {
	if !CanPrompt() {
		return "", nil
	}

	var val string
	field := huh.NewInput().
		Title(label).
		Password(true).
		Value(&val).
		WithTheme(Theme())
	err := runField(field)
	return val, err
}

// MultiSelect lets the user toggle any number of opts (space to toggle).
// When prompting isn't possible (see CanPrompt) it returns the values of
// options pre-marked Selected, without prompting.
func MultiSelect(title string, opts []Option) ([]string, error) {
	return multiSelectWith(Theme(), title, opts)
}

// multiSelectWith is MultiSelect with a caller-supplied theme; see confirmWith.
func multiSelectWith(theme huh.Theme, title string, opts []Option) ([]string, error) {
	if !CanPrompt() {
		var defaults []string
		for _, o := range opts {
			if o.Selected {
				defaults = append(defaults, o.Value)
			}
		}
		return defaults, nil
	}

	huhOpts := make([]huh.Option[string], len(opts))
	for i, o := range opts {
		huhOpts[i] = huh.NewOption(o.Label, o.Value).Selected(o.Selected)
	}

	var selected []string
	field := huh.NewMultiSelect[string]().
		Title(title).
		Options(huhOpts...).
		// R19: force the field's own height to fit its title line plus every
		// option, so a long option list (e.g. init's >=5 targets) is shown
		// in full rather than left to the terminal's reported window size.
		Height(len(opts) + 1).
		Value(&selected).
		WithTheme(theme)
	err := runMultiSelectField(field)
	return selected, err
}

// InputForm renders every field of fields in a single huh Group -- all
// fields are visible at once and the user can move back and forth between
// them with Tab/Shift+Tab before submitting. This replaces calling
// InputText N times (which only shows one field at a time and can't revisit
// an earlier answer). When prompting isn't possible (see CanPrompt) it
// returns each field's Default without prompting.
func InputForm(title string, fields []Field) (map[string]string, error) {
	return inputFormWith(Theme(), title, fields)
}

// inputFormWith is InputForm with a caller-supplied theme; see confirmWith.
func inputFormWith(theme huh.Theme, title string, fields []Field) (map[string]string, error) {
	values := make(map[string]string, len(fields))

	if !CanPrompt() {
		for _, f := range fields {
			values[f.Key] = f.Default
		}
		return values, nil
	}

	holders := make([]*string, len(fields))
	huhFields := make([]huh.Field, len(fields))
	for i, f := range fields {
		val := f.Default
		holders[i] = &val

		input := huh.NewInput().
			Title(f.Label).
			Password(f.Password).
			Value(&val)
		if f.Validate != nil {
			input = input.Validate(f.Validate)
		}
		huhFields[i] = input.WithTheme(theme)
	}

	group := huh.NewGroup(huhFields...)
	if title != "" {
		group = group.Title(title)
	}
	form := huh.NewForm(group).WithTheme(theme).WithOutput(os.Stderr)
	if err := runForm(form); err != nil {
		return nil, err
	}

	for i, f := range fields {
		values[f.Key] = *holders[i]
	}
	return values, nil
}
