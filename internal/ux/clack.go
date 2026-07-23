package ux

import (
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"

	"charm.land/lipgloss/v2"
)

// clackSymbols is the glyph set the Clack renderer draws its connecting line
// with. Two sets exist for the same reason colors.go replaced the original
// message symbols with ASCII: box-drawing and geometric glyphs are East-Asian
// Ambiguous width, so a terminal that renders them two columns wide (or cannot
// render them at all) would break the alignment the box relies on. The ASCII
// mapping mirrors upstream clack's own fallback set.
type clackSymbols struct {
	Step        string // completed step marker
	BarStart    string // opening corner of the connecting line
	Bar         string // the connecting line itself
	BarEnd      string // closing corner
	BarH        string // horizontal rule
	CornerRight string // top-right corner of a Note box
	ConnectLeft string // bottom-left corner, reconnecting a Note to the gutter
	CornerEnd   string // bottom-right corner of a Note box
}

var (
	unicodeClackSymbols = clackSymbols{
		Step:        "◇",
		BarStart:    "┌",
		Bar:         "│",
		BarEnd:      "└",
		BarH:        "─",
		CornerRight: "╮",
		ConnectLeft: "├",
		CornerEnd:   "╯",
	}

	asciiClackSymbols = clackSymbols{
		Step:        "o",
		BarStart:    "T",
		Bar:         "|",
		BarEnd:      "-",
		BarH:        "-",
		CornerRight: "+",
		ConnectLeft: "+",
		CornerEnd:   "+",
	}
)

// brandStyle colors the transcript's step markers and the banner. It is kept
// separate from printer.go's infoStyle, which carries the same color but a
// message-severity meaning.
var brandStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ColorBrand))

// supportsUnicode reports whether the terminal can be expected to render the
// Unicode symbol set. It is a swappable seam for tests; production code always
// uses defaultSupportsUnicode.
var supportsUnicode = defaultSupportsUnicode

// defaultSupportsUnicode infers Unicode capability from the environment. It
// deliberately ignores NO_COLOR: that variable means "don't colorize", not
// "don't use Unicode" -- the same distinction CanPrompt draws for prompting.
func defaultSupportsUnicode() bool {
	term := os.Getenv("TERM")
	if term == "dumb" || term == "linux" {
		return false
	}
	if runtime.GOOS != "windows" {
		return term != ""
	}
	// Windows has no TERM by default. Modern hosts (Windows Terminal, VS Code,
	// ConEmu) advertise themselves, and anything that does set TERM (Git Bash,
	// MSYS) is a Unicode-capable emulator; the bare conhost that remains
	// cannot be relied on for box drawing.
	return os.Getenv("WT_SESSION") != "" ||
		os.Getenv("TERM_PROGRAM") != "" ||
		os.Getenv("ConEmuANSI") == "ON" ||
		term != ""
}

// Clack renders the connected-gutter transcript used by `apm-go init`, in the
// style of @clack/prompts: each answered step stays on screen as a "◇ title /
// │ answer" pair, joined into one vertical line that opens with Intro and
// closes with Outro.
//
// It exists because huh clears its own prompt when a form finishes (Form.View
// returns an empty string once quitting, which the renderer collapses to zero
// height), so nothing a prompt drew survives it. The transcript is therefore
// printed by the caller, after each prompt returns -- which is also huh's own
// documented pattern for keeping a summary on screen.
type Clack struct {
	w   io.Writer
	sym clackSymbols
}

// NewClack returns a Clack that writes to w, choosing its symbol set from the
// terminal's Unicode capability.
func NewClack(w io.Writer) *Clack {
	sym := asciiClackSymbols
	if supportsUnicode() {
		sym = unicodeClackSymbols
	}
	return &Clack{w: w, sym: sym}
}

// Banner prints ASCII art (the APM-GO logo) above the transcript, without a
// gutter, followed by a blank line separating it from the run. The art is
// drawn from block and box-drawing glyphs that have no meaningful ASCII
// rendering, so on a terminal that can't display them nothing is printed at
// all -- a field of replacement characters is worse than no logo, and callers
// get no stray blank line either.
func (c *Clack) Banner(art string) {
	if c.sym != unicodeClackSymbols {
		return
	}
	lipgloss.Fprintln(c.w, brandStyle.Render(art)+"\n")
}

// Intro opens the transcript with the run's title.
func (c *Clack) Intro(title string) {
	lipgloss.Fprintln(c.w, mutedStyle.Render(c.sym.BarStart)+"  "+headingStyle.Render(title))
}

// Bar prints a bare connecting line, used to space steps apart.
func (c *Clack) Bar() {
	lipgloss.Fprintln(c.w, mutedStyle.Render(c.sym.Bar))
}

// Detail prints one line of supporting text on the connecting line, for
// context that belongs to the run rather than to any single step (e.g. init's
// "Press ^C at any time to quit." hint).
func (c *Clack) Detail(text string) {
	lipgloss.Fprintln(c.w, mutedStyle.Render(c.sym.Bar)+"  "+mutedStyle.Render(text))
}

// Warn prints a non-fatal warning on the connecting line. It exists so a
// warning raised mid-run keeps both its severity (the Warn symbol and color
// that §4 of the terminal-UX contract maps it to) and its place in the
// transcript -- calling ux.Warn directly would print a line with no gutter and
// visibly break the connecting line.
func (c *Clack) Warn(format string, a ...any) {
	msg := warnStyle.Bold(true).Render(SymbolWarn) + " " + fmt.Sprintf(format, a...)
	lipgloss.Fprintln(c.w, mutedStyle.Render(c.sym.Bar)+"  "+msg)
}

// Step records a completed prompt: its question, then the answer the user
// gave, each line hanging off the connecting line. A multi-line answer gets
// the gutter on every line; an empty answer prints the title alone.
func (c *Clack) Step(title, answer string) {
	lipgloss.Fprintln(c.w, brandStyle.Render(c.sym.Step)+"  "+title)
	if answer == "" {
		return
	}
	bar := mutedStyle.Render(c.sym.Bar)
	for _, line := range strings.Split(answer, "\n") {
		lipgloss.Fprintln(c.w, bar+"  "+line)
	}
}

// Note renders body inside a box that hangs off the connecting line, used for
// init's "About to create" summary. Every line is padded to one inner width so
// the right edge stays straight.
func (c *Clack) Note(title string, body []string) {
	inner := runeWidth(title) + 1
	for _, line := range body {
		inner = max(inner, runeWidth(line))
	}

	bar := mutedStyle.Render(c.sym.Bar)
	rule := func(n int) string { return strings.Repeat(c.sym.BarH, n) }

	// The title line opens the box: "◇  <title> ───╮", sized so it ends in the
	// same column as every body line's closing bar.
	head := brandStyle.Render(c.sym.Step) + "  " + headingStyle.Render(title) + " " +
		mutedStyle.Render(rule(inner-runeWidth(title)+1)+c.sym.CornerRight)
	lipgloss.Fprintln(c.w, head)

	for _, line := range body {
		padded := line + strings.Repeat(" ", inner-runeWidth(line))
		lipgloss.Fprintln(c.w, bar+"  "+padded+"  "+bar)
	}

	lipgloss.Fprintln(c.w, mutedStyle.Render(c.sym.ConnectLeft+rule(inner+4)+c.sym.CornerEnd))
}

// Outro closes the transcript, detaching the final message from the last step
// with one length of connecting line first (as upstream clack's outro does).
func (c *Clack) Outro(msg string) {
	c.Bar()
	lipgloss.Fprintln(c.w, mutedStyle.Render(c.sym.BarEnd)+"  "+msg)
}

// Confirm asks a yes/no question and, once answered, records it on the
// transcript. Pairing the prompt with its transcript line here (rather than
// leaving the caller to print it) keeps the two from drifting apart.
// When prompting isn't possible it returns def without printing anything.
func (c *Clack) Confirm(title string, def bool) (bool, error) {
	if !CanPrompt() {
		return def, nil
	}
	val, err := confirmWith(clackTheme(c.sym), title, def)
	if err != nil {
		return val, err
	}
	c.Step(title, yesNo(val))
	return val, nil
}

// Form asks for every field in one grouped prompt and records the answers,
// one "label: value" line per field, in the order the fields were given.
// When prompting isn't possible it returns each field's default without
// printing anything.
func (c *Clack) Form(title string, fields []Field) (map[string]string, error) {
	if !CanPrompt() {
		return InputForm(title, fields)
	}
	values, err := inputFormWith(clackTheme(c.sym), title, false, fields)
	if err != nil {
		return nil, err
	}

	answers := make([]string, 0, len(fields))
	for _, f := range fields {
		val := values[f.Key]
		if f.Password {
			val = strings.Repeat("*", len(val))
		}
		answers = append(answers, f.Label+": "+val)
	}
	c.Step(title, strings.Join(answers, "\n"))
	return values, nil
}

// multiSelectKeyHint replaces huh's keybinding footer for a MultiSelect shown
// inside a transcript. Toggling with the space bar is the one binding a user
// cannot guess (R19), and huh's own footer sits below a blank separator line
// that Group.View hardcodes, off the connecting line; a field description
// renders inside the field's border, on it.
const multiSelectKeyHint = "space to toggle, enter to confirm"

// MultiSelect asks the user to toggle options and records the chosen values.
// When prompting isn't possible it returns the pre-selected defaults without
// printing anything.
func (c *Clack) MultiSelect(title string, opts []Option) ([]string, error) {
	if !CanPrompt() {
		return MultiSelect(title, opts)
	}
	selected, err := multiSelectWith(clackTheme(c.sym), title, multiSelectKeyHint, false, opts)
	if err != nil {
		return nil, err
	}

	answer := strings.Join(selected, ", ")
	if answer == "" {
		answer = "(none)"
	}
	c.Step(title, answer)
	return selected, nil
}

func yesNo(v bool) string {
	if v {
		return "Yes"
	}
	return "No"
}

// runeWidth reports how many terminal columns s occupies, so box padding
// accounts for wide runes rather than counting bytes.
func runeWidth(s string) int {
	return lipgloss.Width(s)
}
