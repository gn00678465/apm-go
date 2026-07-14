package ux

import (
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
