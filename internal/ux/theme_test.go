package ux

import "testing"

func TestTheme_ReturnsUsableStylesForLightAndDark(t *testing.T) {
	tests := []struct {
		name   string
		isDark bool
	}{
		{name: "dark", isDark: true},
		{name: "light", isDark: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			theme := Theme()

			// Act
			styles := theme.Theme(tt.isDark)

			// Assert
			if styles == nil {
				t.Fatal("Theme().Theme() returned nil styles")
			}
			if styles.Blurred.MultiSelectSelector.GetForeground() != styles.Focused.MultiSelectSelector.GetForeground() {
				t.Fatal("Blurred styles should inherit Focused's color overrides")
			}
		})
	}
}
