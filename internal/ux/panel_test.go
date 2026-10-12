package ux

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"golang.org/x/term"
)

// The expected panels are the Oracle's own stdout (apm 0.33.0, `apm
// marketplace init --name demo --no-gitignore-check`, NO_COLOR=1) at the
// COLUMNS value each case names.
func TestPanel_MatchesOraclePanelBytes(t *testing.T) {
	body := []string{
		"  1. Edit the 'marketplace:' block in apm.yml to add your packages",
		"  2. Run 'apm pack' to generate .claude-plugin/marketplace.json",
		"  3. Add 'codex' to marketplace.outputs to also generate .agents/plugins/marketplace.json",
		"  4. Commit apm.yml and the generated marketplace file(s)",
	}
	cases := []struct {
		columns string
		want    string
	}{
		{"80", `
╭────────────────────────────────  Next Steps ─────────────────────────────────╮
│   1. Edit the 'marketplace:' block in apm.yml to add your packages           │
│   2. Run 'apm pack' to generate .claude-plugin/marketplace.json              │
│   3. Add 'codex' to marketplace.outputs to also generate                     │
│ .agents/plugins/marketplace.json                                             │
│   4. Commit apm.yml and the generated marketplace file(s)                    │
╰──────────────────────────────────────────────────────────────────────────────╯
`},
		{"60", `
╭──────────────────────  Next Steps ───────────────────────╮
│   1. Edit the 'marketplace:' block in apm.yml to add     │
│ your packages                                            │
│   2. Run 'apm pack' to generate                          │
│ .claude-plugin/marketplace.json                          │
│   3. Add 'codex' to marketplace.outputs to also generate │
│ .agents/plugins/marketplace.json                         │
│   4. Commit apm.yml and the generated marketplace        │
│ file(s)                                                  │
╰──────────────────────────────────────────────────────────╯
`},
		{"120", `
╭────────────────────────────────────────────────────  Next Steps ─────────────────────────────────────────────────────╮
│   1. Edit the 'marketplace:' block in apm.yml to add your packages                                                   │
│   2. Run 'apm pack' to generate .claude-plugin/marketplace.json                                                      │
│   3. Add 'codex' to marketplace.outputs to also generate .agents/plugins/marketplace.json                            │
│   4. Commit apm.yml and the generated marketplace file(s)                                                            │
╰──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────╯
`},
		{"20", `
╭──  Next Steps ───╮
│   1. Edit the    │
│ 'marketplace:'   │
│ block in apm.yml │
│ to add your      │
│ packages         │
│   2. Run 'apm    │
│ pack' to         │
│ generate         │
│ .claude-plugin/m │
│ arketplace.json  │
│   3. Add 'codex' │
│ to               │
│ marketplace.outp │
│ uts to also      │
│ generate         │
│ .agents/plugins/ │
│ marketplace.json │
│   4. Commit      │
│ apm.yml and the  │
│ generated        │
│ marketplace      │
│ file(s)          │
╰──────────────────╯
`},
	}
	for _, tc := range cases {
		t.Run("COLUMNS="+tc.columns, func(t *testing.T) {
			t.Setenv("COLUMNS", tc.columns)
			var buf bytes.Buffer

			Panel(&buf, " Next Steps", body)

			if got, want := buf.String(), strings.TrimPrefix(tc.want, "\n"); got != want {
				t.Errorf("Panel output differs from the Oracle's\n got:\n%s\nwant:\n%s", got, want)
			}
		})
	}
}

func TestPanel_NonNumericColumnsFallsBackToWidth80OffTerminal(t *testing.T) {
	for _, f := range []*os.File{os.Stdin, os.Stdout, os.Stderr} {
		if term.IsTerminal(int(f.Fd())) {
			t.Skip("a standard stream is a terminal; its width decides")
		}
	}
	t.Setenv("COLUMNS", "wide")
	var buf bytes.Buffer

	Panel(&buf, "", []string{"x"})

	want := "╭" + strings.Repeat("─", 78) + "╮\n│ x" + strings.Repeat(" ", 76) + "│\n╰" + strings.Repeat("─", 78) + "╯\n"
	if got := buf.String(); got != want {
		t.Errorf("Panel output = %q, want %q", got, want)
	}
}
