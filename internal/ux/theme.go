package ux

import (
	"image/color"

	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
)

// Theme returns the huh theme used for all interactive forms, styled with
// the ux color palette (see colors.go).
func Theme() huh.Theme {
	return huh.ThemeFunc(themeFunc)
}

func themeFunc(isDark bool) *huh.Styles {
	t := huh.ThemeBase(isDark)

	brand := lipgloss.Color(ColorBrand)
	success := lipgloss.Color(ColorSuccess)
	errColor := lipgloss.Color(ColorError)
	muted := lipgloss.Color(ColorMuted)

	t.Focused.Title = t.Focused.Title.Foreground(brand)
	t.Focused.Description = t.Focused.Description.Foreground(muted)
	t.Focused.ErrorIndicator = t.Focused.ErrorIndicator.Foreground(errColor)
	t.Focused.ErrorMessage = t.Focused.ErrorMessage.Foreground(errColor)
	t.Focused.SelectSelector = t.Focused.SelectSelector.Foreground(brand)
	t.Focused.NextIndicator = t.Focused.NextIndicator.Foreground(brand)
	t.Focused.PrevIndicator = t.Focused.PrevIndicator.Foreground(brand)
	t.Focused.MultiSelectSelector = t.Focused.MultiSelectSelector.Foreground(brand)
	t.Focused.SelectedPrefix = t.Focused.SelectedPrefix.Foreground(success)
	t.Focused.UnselectedPrefix = t.Focused.UnselectedPrefix.Foreground(muted)
	t.Focused.FocusedButton = t.Focused.FocusedButton.Foreground(lipgloss.Color("0")).Background(brand)
	t.Focused.TextInput.Prompt = t.Focused.TextInput.Prompt.Foreground(brand)
	t.Focused.TextInput.Cursor = t.Focused.TextInput.Cursor.Foreground(brand)

	t.Blurred = t.Focused
	t.Blurred.Base = t.Blurred.Base.BorderStyle(lipgloss.HiddenBorder())
	t.Blurred.Card = t.Blurred.Base
	t.Blurred.NextIndicator = lipgloss.NewStyle()
	t.Blurred.PrevIndicator = lipgloss.NewStyle()

	return t
}

// clackTheme returns a variant of the shared theme whose field chrome sits on
// the same connecting line Clack draws its transcript with, so a prompt in
// progress lines up with the steps already recorded above it.
//
// It exists because huh's own chrome is visually unrelated to that line:
// ThemeBase gives a focused field a thick "┃" left border (theme.go:111) while
// this package's blurred variant hides the border entirely, and a Group's
// title carries no border at all -- so a grouped form (init's metadata step)
// renders with three different left edges, none of them the gutter.
//
// This is deliberately NOT folded into Theme(): the credential prompts in
// cmd/apm-go/mcp_prompt.go share that theme and are not part of a transcript.
func clackTheme(sym clackSymbols) huh.Theme {
	return huh.ThemeFunc(func(isDark bool) *huh.Styles {
		t := themeFunc(isDark)

		border := lipgloss.Border{Left: sym.Bar}
		brand := lipgloss.Color(ColorBrand)
		muted := lipgloss.Color(ColorMuted)

		// PaddingLeft(2) rather than ThemeBase's 1, so the text column lines up
		// with the transcript's "│  answer". PaddingBottom(1) closes each field
		// with a gutter-only line: padding sits inside the border, so the bar is
		// drawn on that line too, which is what keeps the connecting line
		// unbroken between fields (see FieldSeparator below).
		onGutter := func(s lipgloss.Style, c color.Color) lipgloss.Style {
			return s.BorderStyle(border).BorderLeft(true).BorderForeground(c).
				PaddingLeft(2).PaddingBottom(1)
		}

		t.Focused.Base = onGutter(t.Focused.Base, brand)
		t.Focused.Card = t.Focused.Base
		t.Blurred.Base = onGutter(t.Blurred.Base, muted)
		t.Blurred.Card = t.Blurred.Base

		// A Group renders its title above the fields with no border of its own,
		// which would leave the step heading floating off the line.
		t.Group.Title = onGutter(t.Group.Title, brand).
			Foreground(lipgloss.Color(ColorHeading)).Bold(true).PaddingBottom(0)
		t.Group.Description = onGutter(t.Group.Description, muted).
			Foreground(muted).PaddingBottom(0)

		// The default separator is "\n\n", whose blank line carries no gutter.
		// Putting the bar in the separator instead of in PaddingBottom above is
		// the obvious alternative but goes wrong twice: lipgloss pads every line
		// of a multi-line render out to the widest line (style.go:489-496), so
		// "\n<bar>\n" comes back with its trailing empty line padded to a space
		// that indents whatever follows, and once PaddingBottom is also present
		// the two stack into a doubled blank line between fields. A lone "\n"
		// is inert on both counts (its lines are all empty, so the padding
		// target width is zero).
		t.FieldSeparator = lipgloss.NewStyle().SetString("\n")

		return t
	})
}
