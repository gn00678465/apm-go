package ux

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"golang.org/x/term"
)

// The expected tree is Rich 15.0.0 output: console.print(tree) on
// Console(width=200, force_terminal=False, color_system=None).
func TestRenderRichTree_MatchesRich(t *testing.T) {
	green := lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	dim := lipgloss.NewStyle().Faint(true)
	root := RichNode{
		Label: "proj (local)",
		Style: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6")),
		Children: []RichNode{
			{Label: "acme/a@1.0.0", Style: green, Children: []RichNode{
				{Label: "1 prompts, 2 skills", Style: dim},
				{Label: "acme/b@2.0.0", Style: green, Children: []RichNode{
					{Label: "acme/c@unknown", Style: green},
					{Label: "acme/d@3.1.0", Style: green},
				}},
				{Label: "acme/e@0.1.0", Style: green},
			}},
			{Label: "acme/f@1.2.3", Style: green, Children: []RichNode{
				{Label: "3 agents", Style: dim},
			}},
			{Label: "acme/g@9.9.9", Style: green},
		},
	}
	want := strings.Join([]string{
		"proj (local)",
		"├── acme/a@1.0.0",
		"│   ├── 1 prompts, 2 skills",
		"│   ├── acme/b@2.0.0",
		"│   │   ├── acme/c@unknown",
		"│   │   └── acme/d@3.1.0",
		"│   └── acme/e@0.1.0",
		"├── acme/f@1.2.3",
		"│   └── 3 agents",
		"└── acme/g@9.9.9",
	}, "\n") + "\n"

	var buf bytes.Buffer
	RenderRichTree(&buf, root)
	if got := buf.String(); got != want {
		t.Errorf("tree mismatch\n got:\n%s\nwant:\n%s", got, want)
	}
}

// Rich 15.0.0 output, as above.
func TestRenderRichTree_MultiLineLabels(t *testing.T) {
	root := RichNode{Label: "root\nsecond", Children: []RichNode{
		{Label: "a\nb", Children: []RichNode{{Label: "c"}}},
		{Label: "d\ne"},
	}}
	want := strings.Join([]string{
		"root",
		"second",
		"├── a",
		"│   b",
		"│   └── c",
		"└── d",
		"    e",
	}, "\n") + "\n"
	var buf bytes.Buffer
	RenderRichTree(&buf, root)
	if got := buf.String(); got != want {
		t.Errorf("tree mismatch\n got:\n%s\nwant:\n%s", got, want)
	}
}

func TestRenderRichTree_RootAlone(t *testing.T) {
	var buf bytes.Buffer
	RenderRichTree(&buf, RichNode{Label: "my-project (local)"})
	if got, want := buf.String(), "my-project (local)\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestTerminalColumns(t *testing.T) {
	stdoutIsTerminal := term.IsTerminal(int(os.Stdout.Fd()))
	cases := []struct {
		name    string
		columns string
		unset   bool
		want    int
	}{
		{"positive COLUMNS wins", "120", false, 120},
		{"zero COLUMNS falls through", "0", false, 80},
		{"negative COLUMNS falls through", "-5", false, 80},
		{"non-numeric COLUMNS falls through", "wide", false, 80},
		{"unset COLUMNS falls through", "", true, 80},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("COLUMNS", tc.columns)
			if tc.unset {
				os.Unsetenv("COLUMNS")
			}
			if tc.want == 80 && stdoutIsTerminal {
				t.Skip("stdout is a terminal, so its width wins over the fallback")
			}
			if got := TerminalColumns(80); got != tc.want {
				t.Errorf("TerminalColumns(80) = %d, want %d", got, tc.want)
			}
		})
	}
}
