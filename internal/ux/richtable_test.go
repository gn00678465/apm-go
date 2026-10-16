package ux

import (
	"bytes"
	"regexp"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// depsTable builds the table the Oracle's `apm deps list` prints
// (commands/deps/cli.py:503-536), with an Origin column when origin is set.
func depsTable(title string, origin bool, rows [][]string) RichTable {
	style := func(c string) lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color(c)) }
	cols := []RichColumn{
		{Header: "Package", Style: style("15").Bold(true)},
		{Header: "Version", Style: style("3")},
		{Header: "Source", Style: style("4")},
	}
	if origin {
		cols = append(cols, RichColumn{Header: "Origin", Style: style("1").Bold(true)})
	}
	for _, h := range []string{"Prompts", "Instructions", "Agents", "Skills", "Hooks"} {
		cols = append(cols, RichColumn{Header: h, Justify: RichCenter, Style: style("5")})
	}
	return RichTable{
		Title:       title,
		HeaderStyle: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6")),
		Columns:     cols,
		Rows:        rows,
	}
}

func (t RichTable) withColumns(n int) RichTable {
	t.Columns = t.Columns[:n]
	return t
}

var globalRows = [][]string{
	{"_local/pkg", "1.0.0", "local", "-", "-", "1", "1", "-"},
	{"microsoft/apm-sample-package", "1.0.0", "github", "2", "1", "1", "1", "-"},
	{"github/awesome-copilot/skills/review-and-refactor", "unknown", "github", "-", "-", "-", "1", "-"},
}

// Every expected table is Rich 15.0.0 output: console.print(table) on
// Console(width=<width>, force_terminal=False, color_system=None).
func TestRenderRichTable_MatchesRich(t *testing.T) {
	plainOutput(t)
	cases := []struct {
		name  string
		table RichTable
		width int
		want  []string
	}{
		{"project table at its natural width", depsTable(" APM Dependencies (Project)", false, [][]string{
			{"_local/dep", "2.3.4", "local", "1", "1", "1", "1", "1"},
		}), 120, []string{
			"                             APM Dependencies (Project)                             ",
			"┏━━━━━━━━━━━━┳━━━━━━━━━┳━━━━━━━━┳━━━━━━━━━┳━━━━━━━━━━━━━━┳━━━━━━━━┳━━━━━━━━┳━━━━━━━┓",
			"┃ Package    ┃ Version ┃ Source ┃ Prompts ┃ Instructions ┃ Agents ┃ Skills ┃ Hooks ┃",
			"┡━━━━━━━━━━━━╇━━━━━━━━━╇━━━━━━━━╇━━━━━━━━━╇━━━━━━━━━━━━━━╇━━━━━━━━╇━━━━━━━━╇━━━━━━━┩",
			"│ _local/dep │ 2.3.4   │ local  │    1    │      1       │   1    │   1    │   1   │",
			"└────────────┴─────────┴────────┴─────────┴──────────────┴────────┴────────┴───────┘",
		}},
		{"global table shrunk to 120 cells", depsTable(" APM Dependencies (Global)", false, globalRows), 120, []string{
			"                                                APM Dependencies (Global)                                               ",
			"┏━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┳━━━━━━━━━┳━━━━━━━━┳━━━━━━━━━┳━━━━━━━━━━━━━━┳━━━━━━━━┳━━━━━━━━┳━━━━━━━┓",
			"┃ Package                                        ┃ Version ┃ Source ┃ Prompts ┃ Instructions ┃ Agents ┃ Skills ┃ Hooks ┃",
			"┡━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━╇━━━━━━━━━╇━━━━━━━━╇━━━━━━━━━╇━━━━━━━━━━━━━━╇━━━━━━━━╇━━━━━━━━╇━━━━━━━┩",
			"│ _local/pkg                                     │ 1.0.0   │ local  │    -    │      -       │   1    │   1    │   -   │",
			"│ microsoft/apm-sample-package                   │ 1.0.0   │ github │    2    │      1       │   1    │   1    │   -   │",
			"│ github/awesome-copilot/skills/review-and-refa… │ unknown │ github │    -    │      -       │   -    │   1    │   -   │",
			"└────────────────────────────────────────────────┴─────────┴────────┴─────────┴──────────────┴────────┴────────┴───────┘",
		}},
		{"origin column, a long cell cut and a cell wrapped", depsTable(" Insecure APM Dependencies (Global)", true, [][]string{
			{"acme/child", "unknown", "github", "via acme/pkg", "-", "-", "-", "1", "-"},
			{"github/awesome-copilot/skills/review-and-refactor", "unknown", "github", "via microsoft/apm-sample-package", "-", "-", "-", "1", "-"},
		}), 120, []string{
			"                                           Insecure APM Dependencies (Global)                                           ",
			"┏━━━━━━━━━━━━━━━━━━━━━━━━┳━━━━━━━━━┳━━━━━━━━┳━━━━━━━━━━━━━━━━━━━━━━━┳━━━━━━━━━┳━━━━━━━━━━━━━━┳━━━━━━━━┳━━━━━━━━┳━━━━━━━┓",
			"┃ Package                ┃ Version ┃ Source ┃ Origin                ┃ Prompts ┃ Instructions ┃ Agents ┃ Skills ┃ Hooks ┃",
			"┡━━━━━━━━━━━━━━━━━━━━━━━━╇━━━━━━━━━╇━━━━━━━━╇━━━━━━━━━━━━━━━━━━━━━━━╇━━━━━━━━━╇━━━━━━━━━━━━━━╇━━━━━━━━╇━━━━━━━━╇━━━━━━━┩",
			"│ acme/child             │ unknown │ github │ via acme/pkg          │    -    │      -       │   -    │   1    │   -   │",
			"│ github/awesome-copilo… │ unknown │ github │ via                   │    -    │      -       │   -    │   1    │   -   │",
			"│                        │         │        │ microsoft/apm-sample… │         │              │        │        │       │",
			"└────────────────────────┴─────────┴────────┴───────────────────────┴─────────┴──────────────┴────────┴────────┴───────┘",
		}},
		{"cells with spaces wrap onto more lines of the row", depsTable(" Insecure APM Dependencies (Project)", true, [][]string{
			{"acme/child", "unknown", "github", "via acme/pkg and acme/other-package", "-", "-", "-", "1", "-"},
			{"acme/x", "1.0.0", "github", "via acme/pkg", "10", "-", "-", "1", "-"},
		}), 80, []string{
			"                       Insecure APM Dependencies (Project)                      ",
			"┏━━━━━━━━┳━━━━━━━━┳━━━━━━━━┳━━━━━━━━┳━━━━━━━━┳━━━━━━━━┳━━━━━━━━┳━━━━━━━┳━━━━━━━┓",
			"┃ Packa… ┃ Versi… ┃ Source ┃ Origin ┃ Promp… ┃ Instr… ┃ Agents ┃ Skil… ┃ Hooks ┃",
			"┡━━━━━━━━╇━━━━━━━━╇━━━━━━━━╇━━━━━━━━╇━━━━━━━━╇━━━━━━━━╇━━━━━━━━╇━━━━━━━╇━━━━━━━┩",
			"│ acme/… │ unkno… │ github │ via    │   -    │   -    │   -    │   1   │   -   │",
			"│        │        │        │ acme/… │        │        │        │       │       │",
			"│        │        │        │ and    │        │        │        │       │       │",
			"│        │        │        │ acme/… │        │        │        │       │       │",
			"│ acme/x │ 1.0.0  │ github │ via    │   10   │   -    │   -    │   1   │   -   │",
			"│        │        │        │ acme/… │        │        │        │       │       │",
			"└────────┴────────┴────────┴────────┴────────┴────────┴────────┴───────┴───────┘",
		}},
		{"columns reduced evenly once collapsing is not enough", depsTable(" APM Dependencies (Global)", false, globalRows), 40, []string{
			"        APM Dependencies (Global)       ",
			"┏━━━━┳━━━━┳━━━━┳━━━━┳━━━━┳━━━━┳━━━┳━━━━┓",
			"┃ P… ┃ V… ┃ S… ┃ P… ┃ I… ┃ A… ┃ … ┃ H… ┃",
			"┡━━━━╇━━━━╇━━━━╇━━━━╇━━━━╇━━━━╇━━━╇━━━━┩",
			"│ _… │ 1… │ l… │ -  │ -  │ 1  │ 1 │ -  │",
			"│ m… │ 1… │ g… │ 2  │ 1  │ 1  │ 1 │ -  │",
			"│ g… │ u… │ g… │ -  │ -  │ -  │ 1 │ -  │",
			"└────┴────┴────┴────┴────┴────┴───┴────┘",
		}},
		{"a title wider than the table folds by words", RichTable{
			Title:   " A title much wider than this small table is",
			Columns: []RichColumn{{Header: "A"}, {Header: "B", Justify: RichCenter}},
			Rows:    [][]string{{"x", "yy"}},
		}, 30, []string{
			"  A title ",
			"much wider",
			"than this ",
			"  small   ",
			" table is ",
			"┏━━━┳━━━━┓",
			"┃ A ┃ B  ┃",
			"┡━━━╇━━━━┩",
			"│ x │ yy │",
			"└───┴────┘",
		}},
		{"a double-width character cut in half becomes a space", depsTable(" APM Dependencies (Project)", false, [][]string{
			{"套件名稱很長很長很長很長很長很長", "1.0.0", "local"},
			{"a b", "2", "x"},
		}).withColumns(3), 28, []string{
			" APM Dependencies (Project) ",
			"┏━━━━━━━━┳━━━━━━━━┳━━━━━━━━┓",
			"┃ Packa… ┃ Versi… ┃ Source ┃",
			"┡━━━━━━━━╇━━━━━━━━╇━━━━━━━━┩",
			"│ 套件 … │ 1.0.0  │ local  │",
			"│ a b    │ 2      │ x      │",
			"└────────┴────────┴────────┘",
		}},
		{"whitespace past the column width is dropped, not cut with an ellipsis", RichTable{
			Columns: []RichColumn{{Header: "H"}, {Header: "C", Justify: RichCenter}},
			Rows:    [][]string{{"abc     x", "ab     y"}},
		}, 15, []string{
			"┏━━━━━━┳━━━━━━┓",
			"┃ H    ┃  C   ┃",
			"┡━━━━━━╇━━━━━━┩",
			"│ abc  │  ab  │",
			"│ x    │  y   │",
			"└──────┴──────┘",
		}},
		{"a column with no room inside its padding adds no line", RichTable{
			Columns: []RichColumn{{Header: "Package"}, {Header: "Version"}, {Header: "Source"}},
			Rows:    [][]string{{"a b c d", "1 2 3", "x y"}},
		}, 11, []string{
			"┏━━┳━━━┳━━┓",
			"┃  ┃ … ┃  ┃",
			"┡━━╇━━━╇━━┩",
			"│  │ 1 │  │",
			"│  │ 2 │  │",
			"│  │ 3 │  │",
			"└──┴───┴──┘",
		}},
		{"a console narrower than the borders crops every line", RichTable{
			Title:   " T",
			Columns: []RichColumn{{Header: "Package"}, {Header: "Version"}, {Header: "Source"}},
			Rows:    [][]string{{"a b c d", "1 2 3", "x y"}},
		}, 3, []string{
			"  T",
			"┏┳┳",
			"┃┃┃",
			"┡╇╇",
			"│││",
			"└┴┴",
		}},
		{"no rows", depsTable(" APM Dependencies (Project)", false, nil).withColumns(3), 80, []string{
			"  APM Dependencies (Project)  ",
			"┏━━━━━━━━━┳━━━━━━━━━┳━━━━━━━━┓",
			"┃ Package ┃ Version ┃ Source ┃",
			"┡━━━━━━━━━╇━━━━━━━━━╇━━━━━━━━┩",
			"└─────────┴─────────┴────────┘",
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			RenderRichTable(&buf, tc.table, tc.width)
			want := strings.Join(tc.want, "\n") + "\n"
			if got := buf.String(); got != want {
				t.Errorf("table mismatch\n got:\n%s\nwant:\n%s", got, want)
			}
		})
	}
}

// TTY_FORCE makes lipgloss treat a buffer as a terminal: the styles add
// escape codes and nothing else. NO_COLOR removes the colors and
// keeps bold and italic, as Rich and the other ux printers do.
func TestRenderRichTable_ColorOnlyWhenAllowed(t *testing.T) {
	plainOutput(t)
	table := depsTable(" APM Dependencies (Project)", false, [][]string{
		{"_local/dep", "2.3.4", "local", "1", "1", "1", "1", "1"},
	})
	var plain bytes.Buffer
	RenderRichTable(&plain, table, 120)
	if strings.Contains(plain.String(), "\x1b") {
		t.Fatalf("table written to a buffer has escape codes:\n%q", plain.String())
	}

	t.Setenv("TERM", "xterm-256color")
	t.Setenv("TTY_FORCE", "1")
	var colored bytes.Buffer
	RenderRichTable(&colored, table, 120)
	if !sgrColor.MatchString(colored.String()) {
		t.Fatalf("forced color printed no color code:\n%q", colored.String())
	}
	if got := ansi.Strip(colored.String()); got != plain.String() {
		t.Errorf("colored table differs from the plain one once stripped\n got:\n%s\nwant:\n%s", got, plain.String())
	}

	t.Setenv("NO_COLOR", "1")
	var noColor bytes.Buffer
	RenderRichTable(&noColor, table, 120)
	if sgrColor.MatchString(noColor.String()) {
		t.Errorf("NO_COLOR table has color codes:\n%q", noColor.String())
	}
	if got := ansi.Strip(noColor.String()); got != plain.String() {
		t.Errorf("NO_COLOR table differs from the plain one once stripped:\n%q", got)
	}
}

// plainOutput pins what decides whether lipgloss colors a buffer, so the
// shell that runs the tests does not decide it.
func plainOutput(t *testing.T) {
	t.Helper()
	t.Setenv("TTY_FORCE", "")
	t.Setenv("NO_COLOR", "")
}

// sgrColor matches an SGR sequence that sets a foreground color.
var sgrColor = regexp.MustCompile(`\x1b\[(?:[0-9]+;)*(?:3[0-79]|9[0-7])(?:;[0-9]+)*m`)
