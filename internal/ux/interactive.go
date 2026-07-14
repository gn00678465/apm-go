package ux

import (
	"charm.land/huh/v2"
)

// Option is a single choice for MultiSelect. Selected marks the option as
// pre-selected, which also acts as the default returned when prompting
// isn't possible (see MultiSelect).
type Option struct {
	Label    string
	Value    string
	Selected bool
}

// runField executes a huh field. It is a swappable seam so tests can
// exercise field construction (Title/Value/Options/theme wiring) without
// needing a real interactive terminal.
var runField = func(f huh.Field) error {
	return f.Run()
}

// Confirm asks a yes/no question with the given default. When prompting
// isn't possible (non-TTY stdin/stderr, or CI -- see CanPrompt) it returns
// def immediately without prompting.
func Confirm(prompt string, def bool) (bool, error) {
	if !CanPrompt() {
		return def, nil
	}

	val := def
	field := huh.NewConfirm().
		Title(prompt).
		Value(&val).
		WithTheme(Theme())
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
		Value(&selected).
		WithTheme(Theme())
	err := runField(field)
	return selected, err
}
