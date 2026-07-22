# Research: huh v2 internals for clack-style `apm-go init` restyle (issue #14)

- **Query**: See task brief — restyle `apm-go init` like @clack/prompts / Vercel CLI (ASCII logo, connected-gutter transcript, summary box) and fix the current huh Confirm left-border + right-offset Yes/No rendering bug.
- **Scope**: mixed (internal Go source + huh/lipgloss/bubbletea module-cache source + external go-clack library + Python oracle parity check)
- **Date**: 2026-07-23

> Note on "???" in the task brief: those are almost certainly mangled Unicode box-drawing/bullet
> glyphs (│ ◆ ◇ ┌ └ etc., the exact set @clack/prompts and its Go port `go-clack` use — see
> "External References" below) that got corrupted somewhere in transit into this prompt. This
> research treats every "???" occurrence as a placeholder for one of those glyphs and cites the
> real characters where relevant. This is a garbling observation, not a verified 1:1 mapping of
> every single "???" instance in the brief to a specific glyph — flagged as unverified where the
> exact intended glyph could not be inferred from context.

## Findings

### Files Found (project source read)

| File Path | Description |
|---|---|
| `internal/ux/interactive.go` | Confirm/InputText/Password/MultiSelect/InputForm wrapping huh/v2; `runField`/`runForm`/`runMultiSelectField` seams |
| `internal/ux/theme.go` | `Theme()` / `themeFunc` — builds `huh.Theme` from `huh.ThemeBase`, overriding only foreground colors |
| `internal/ux/output.go` | `Table`/`BulletList`/`Tree`/`Section`/`Box`/`Diff` — lipgloss-based non-interactive rendering (`Box` at line 134 is the current "About to create" summary renderer) |
| `internal/ux/printer.go` | `Success`/`Info`/`Warn`/`Error` — fixed-width-3 centered ASCII symbol prefix (`printLine`, line 47) |
| `internal/ux/colors.go` | Color palette + `Symbol*` ASCII-only glyph set, with an explicit rationale comment (lines 19-37) for avoiding East-Asian-Ambiguous-width Unicode glyphs |
| `internal/ux/spinner.go` | Custom spinner built directly on `charm.land/bubbles/v2/spinner`, not huh's own spinner |
| `internal/ux/ux.go` | `CanPrompt()` / `IsRich()` / `Init()` — TTY/NO_COLOR/CI gating |
| `cmd/apm-go/init.go` | `apm-go init` flow: overwrite Confirm → InputForm metadata → MultiSelect targets → Box summary → Confirm → success lines (lines 57-197) |
| `cmd/apm-go/mcp_prompt.go` | Second call site for `ux.Password`/`ux.InputText`/`ux.Confirm` (lines 101-127) |
| `go.mod` | `charm.land/huh/v2 v2.0.3`, `charm.land/lipgloss/v2 v2.0.5`, `charm.land/bubbletea/v2 v2.0.2` (indirect), `charm.land/bubbles/v2 v2.0.0` |

### huh v2.0.3 module-cache source (`C:\Users\gn006\go\pkg\mod\charm.land\huh\v2@v2.0.3`)

Located via `go env GOMODCACHE` → `C:\Users\gn006\go\pkg\mod`; read directly (not from training data).

#### Q1 — What produces the left border and the right-offset Yes/No?

**Left border ("???" gutter):**
- `theme.go:111` (`ThemeBase`): `t.Focused.Base = lipgloss.NewStyle().PaddingLeft(1).BorderStyle(lipgloss.ThickBorder()).BorderLeft(true)` — only the **left** edge of a `ThickBorder()` is enabled (no top/right/bottom).
- `charm.land/lipgloss/v2@v2.0.5/borders.go:140-154`: `thickBorder.Left = "┃"` (U+2503 HEAVY VERTICAL, a Narrow-width ASCII-adjacent-but-non-ASCII glyph). This is almost certainly the char the task brief's "???" stands in for — a single heavy vertical bar to the left of every field, which is structurally the same idea as clack's `BAR`/`│` connector, just styled differently (thick vs. light, no bullet glyph before the title).
- `internal/ux/theme.go:37` only changes this for `Blurred` (`t.Blurred.Base = t.Blurred.Base.BorderStyle(lipgloss.HiddenBorder())`), leaving `Focused.Base` (and thus the visible field, i.e. every prompt while it's on-screen since huh only ever shows the focused field one at a time) with the inherited `ThickBorder` left-only border. ux/theme.go never touches `Focused.Base`'s border at all — it only recolors `Title`/`Description`/etc. (theme.go:22-34).
- `field_confirm.go:294-296` (`Confirm.View()`): the whole rendered block (title + description + button row) is wrapped in `styles.Base.Width(c.width).Height(c.height).Render(sb.String())` — i.e. every Confirm render goes through this same left-bordered `Base` style. Same holds for other fields (`field_input.go:414` renders through `styles.Base` too), so the "???" left border is not Confirm-specific — it's the shared default `Focused.Base` border applied to every huh field type in this codebase (Confirm, Input, MultiSelect all reuse `ThemeBase`'s left-border convention; `ux/theme.go` never overrides it).

**Right-offset Yes/No buttons:**
- `field_confirm.go:52-53` (`NewConfirm()`): `buttonAlignment: lipgloss.Center` is the **default** button alignment (not `lipgloss.Left`).
- `field_confirm.go:284-291` (`Confirm.View()`):
  ```go
  buttonsRow := lipgloss.JoinHorizontal(c.buttonAlignment, affirmative, negative)
  promptWidth := lipgloss.Width(sb.String())
  buttonsWidth := lipgloss.Width(buttonsRow)
  renderWidth := max(buttonsWidth, promptWidth)
  style := lipgloss.NewStyle().Width(renderWidth).Align(c.buttonAlignment)
  sb.WriteString(style.Render(buttonsRow))
  ```
  The button row is rendered inside a box whose width is `max(promptWidth, buttonsWidth)` and aligned per `c.buttonAlignment` (Center by default). Since the title/question text (`promptWidth`) is normally wider than the two short buttons (`buttonsWidth`), centering pushes "Yes  No" right of the left edge — this is exactly the "buttons appear offset to the right" symptom, not a bug in `ux`'s theme; it's huh's own default button alignment.
- `field_confirm.go:361-364`: `func (c *Confirm) WithButtonAlignment(p lipgloss.Position) *Confirm` exists and returns `*Confirm` (not the `Field` interface), so it must be chained **before** `.WithTheme(...)` (which returns `Field`, `field_confirm.go:326-332`) in `ux/interactive.go`'s `Confirm()` builder. **This is a per-field method, not a `huh.Styles`/theme field** — theming alone (in `themeFunc`, `internal/ux/theme.go`) cannot fix the button alignment; it requires an `internal/ux/interactive.go` code change (e.g. `huh.NewConfirm().Title(prompt).Value(&val).WithButtonAlignment(lipgloss.Left).WithTheme(Theme())`).

**Full `huh.Styles`/`FieldStyles` inventory (`theme.go:24-90`)** — every field a theme (`huh.ThemeFunc`) can set:
```
Styles{ Form FormStyles{Base}, Group GroupStyles{Base,Title,Description}, FieldSeparator, Blurred FieldStyles, Focused FieldStyles, Help help.Styles }

FieldStyles{
  Base, Title, Description, ErrorIndicator, ErrorMessage,
  SelectSelector, Option, NextIndicator, PrevIndicator,     // select
  Directory, File,                                          // filepicker
  MultiSelectSelector, SelectedOption, SelectedPrefix,
  UnselectedOption, UnselectedPrefix,                        // multiselect
  TextInput TextInputStyles{Cursor, CursorText, Placeholder, Prompt, Text},
  FocusedButton, BlurredButton,                              // confirm buttons
  Card, NoteTitle, Next,                                     // note/card
}
```
`ux/theme.go` currently sets only: `Focused.Title`, `Focused.Description`, `Focused.ErrorIndicator`, `Focused.ErrorMessage`, `Focused.SelectSelector`, `Focused.NextIndicator`, `Focused.PrevIndicator`, `Focused.MultiSelectSelector`, `Focused.SelectedPrefix`, `Focused.UnselectedPrefix`, `Focused.FocusedButton`, `Focused.TextInput.{Prompt,Cursor}`, plus `Blurred = Focused` with `Blurred.Base` hidden-bordered. It never touches `Focused.Base`'s `BorderStyle`/`BorderLeft`, so the gutter character/shape (theme-changeable: you *can* set `t.Focused.Base = t.Focused.Base.BorderStyle(customBorder)` with a custom `lipgloss.Border{Left: "│"}` to swap `┃`→`│`) and never touches `FocusedButton`'s alignment (not theme-changeable at all — alignment is a `*Confirm` builder method, `WithButtonAlignment`, not a `Styles` field).

**Can the theme alone put a bullet/prefix (e.g. clack's `◆`) before titles?** No — verified in `field_confirm.go:243-244`: `sb.WriteString(styles.Title.Render(wrap(c.title.val, maxWidth)))` always calls `Render()` with an explicit string argument (the field's own title text). `lipgloss.Style.Render(s)` renders exactly `s` styled; it ignores any `SetString()` default content the style might carry. Contrast with `ErrorIndicator`, which *is* theme-injectable because the field calls `styles.ErrorIndicator.String()` (`field_confirm.go:248`) — `.String()` *does* return the style's own `SetString()` default content. So: no field-added prefix glyph is possible from `themeFunc` alone for Title; it would require either (a) prefixing the string apm-go itself passes to `.Title(...)` in `interactive.go`/`init.go` (e.g. `.Title("◆  " + prompt)`), or (b) the manual-transcript-printing approach (see Q3).

#### Q2 — Does huh leave anything on screen after `Run()` returns, or does it clear itself?

- `form.go:654-660` (`Form.View()`):
  ```go
  func (f *Form) View() string {
      if f.quitting {
          return ""
      }
      return f.styles().Base.Render(f.layout.View(f))
  }
  ```
  Once the form transitions to quitting (submitted **or** aborted — `form.go:583-584` sets `f.quitting = true` on submit, `form.go:566-568` on Ctrl-C/quit-key), `View()` returns the **empty string** as the final frame.
- Traced into bubbletea's renderer (`charm.land/bubbletea/v2@v2.0.2/cursed_renderer.go:256-311`, `flush()`): `if len(view.Content) == 0 { frameArea.Max.Y = 0 }` then `s.cellbuf.Clear()` and `content.Draw(...)`. An empty final view collapses the rendered frame area to zero height, which — per the diff-based screen buffer resize/redraw at lines 289-311 — erases the previously drawn lines rather than leaving them on screen.
- `run.go:1-8` (top-level `huh.Run(field)`): wraps a single field in `NewGroup`+`NewForm` and calls `.Run()` — same lifecycle, same clearing behavior.
- `README.md` (huh's own, lines ~121-392) shows the expected usage pattern after `form.Run()` returns: plain `fmt.Println(...)`/`fmt.Printf(...)` calls to print a summary — i.e. **huh's own documented pattern is "prompt clears itself, caller manually prints whatever should persist afterward."** `cmd/apm-go/init.go`'s Phase 7 (lines 191-197: `ux.Success(...)`, `ux.Section(...)`, `ux.Info(...)`) already follows exactly this pattern for the post-form success block — it's just not yet done per-step (only at the very end).
- **Conclusion**: huh v2 has **no** built-in "leave the completed step visible" feature analogous to clack's persistent transcript. Every field/form visually disappears from the terminal once `Run()`/`RunWithContext()` returns (submit or abort), by design (`Form.View()` returning `""` while quitting, confirmed at the source line above — not inferred).

#### Q3 — Manual "print the transcript after each field" pattern: gotchas

Not run experimentally in this session (no interactive TTY available in this environment), so the following is source-derived reasoning, not an observed repro — flagged as such:
- Because `Form.View()` returns `""` on quit and the renderer treats that as "collapse to 0 height" (`cursed_renderer.go:262-264`), the prompt's own rendered lines are erased by bubbletea itself before control returns to caller code — the caller does **not** need to manually erase anything; by the time `runField`/`runForm` returns, the field is already gone from the screen. This removes the most obvious feared gotcha (double-erasure / cursor math) for the "run huh field, then print transcript line" pattern.
- Remaining gotchas to verify in implementation (not verified here — no live TTY test performed):
  1. `Form.WithOutput(os.Stderr)` (`interactive.go:38,63`) means huh's own program owns stderr's cursor state while running; the manual transcript print (also to stderr, per existing convention) must happen only *after* `runField`/`runForm` returns, never interleaved — current code structure (`init.go`'s sequential calls) already satisfies this.
  2. huh's bubbletea program takes over raw/cbreak terminal mode for its duration (`tea.NewProgram(...).Run()`, `form.go:695-707`); rapid-fire manual `fmt.Fprintln` calls between consecutive huh fields (as clack's transcript would require) should be safe since each `runField` call fully starts and stops its own `tea.Program`, but this sequencing has not been verified with a live terminal in this session.
  3. Windows-specific: this project runs on Windows (see `internal/ux/colors.go:19-30`'s explicit rationale for avoiding non-Narrow Unicode glyphs); a manual transcript printer choosing clack's `◆ ◇ │ ┌ └` glyphs would reintroduce exactly the East-Asian-Ambiguous-width risk that `colors.go`'s `Symbol*` constants were designed to avoid (see Q5/Q6 caveat).

#### Q4 — Existing Go clack-style libraries

- `github.com/orochaa/go-clack` — confirmed to exist and be resolvable (`go list -m -versions github.com/orochaa/go-clack` succeeded from this environment, returning versions `v0.1.0`...`v0.1.21`). Module `github.com/orochaa/go-clack` (single go.mod at repo root, `go 1.22.1` — compatible with this project's `go 1.26.3`), subpackages `.../core` (unstyled primitives) and `.../prompts` (styled, ready-to-use — this is the direct analog of `@clack/prompts`).
- GitHub API (`api.github.com/repos/orochaa/go-clack`, fetched live this session): **8 stars, 1 fork, 1 open issue, MIT license, not archived, last push 2025-12-14** (i.e. actively maintained, not stale, but a small/low-adoption project — 8 stars is objectively low; this is the actual number, not an estimate).
- README (fetched live, `raw.githubusercontent.com/orochaa/go-clack/master/README.md`): explicitly "a complete rewrite of bombshell-dev/clack" (the upstream JS `@clack/prompts`), confirming it targets the same visual/UX contract the issue asks for.
- `prompts/confirm.go` (fetched live) shows a real `Confirm(params ConfirmParams) (bool, error)` API using `core.NewConfirmPrompt` + `picocolors` + a themeable `symbols` package — a genuine, working port, not a stub.
- `prompts/symbols/symbols.go` (fetched live) is the strongest evidence for what the task brief's "???" glyphs are:
  ```go
  STEP_ACTIVE Symbol = s("◆", "*")   STEP_SUBMIT Symbol = s("◇", "o")
  STEP_CANCEL Symbol = s("■", "x")   STEP_ERROR  Symbol = s("▲", "x")
  BAR_START   Symbol = s("┌", "T")   BAR    Symbol = s("│", "|")   BAR_END Symbol = s("└", "—")
  BAR_H Symbol = s("─","-")  CORNER_TOP_RIGHT Symbol = s("╮","+")  CONNECT_LEFT Symbol = s("├","+")  CORNER_BOTTOM_RIGHT Symbol = s("╯","+")
  ```
  and every symbol is gated through `s(unicodeGlyph, asciiFallback)` keyed on `isunicodesupported.IsUnicodeSupported()` — i.e. **upstream clack's own Go port already treats Unicode-vs-ASCII terminal support as a first-class fork**, which validates that this project's existing ASCII-only `Symbol*` policy (`internal/ux/colors.go:19-37`) is not overcautious; it is the same tradeoff clack itself makes, just resolved permanently to ASCII rather than at runtime.
- **Adopt vs. coexist**: `go-clack/prompts` is a full replacement surface for huh (its own `Confirm`/`Text`/`Password`/`MultiSelect`/`Spinner` etc.), not a theme/plugin for huh — adopting it for `init` only would mean `apm-go` depends on two different prompt stacks (huh for `mcp_prompt.go`'s `ttyAsk`/`Confirm` call sites, go-clack for `init.go`), unless `internal/ux`'s public API (`Confirm`/`InputText`/`MultiSelect`/`InputForm`/`Password`) is reimplemented on top of go-clack globally, which would touch `cmd/apm-go/mcp_prompt.go`'s two call sites too (see Q6).
- **Caveat**: no further due-diligence (license compatibility review beyond reading "MIT" from the API response, dependency graph, code quality/security read of the vendored core package, or a build/import trial in this repo) was performed — flagged as unverified for anything beyond "it exists, is live, is MIT-licensed, 8 stars, last pushed 2025-12-14, and its `prompts/confirm.go`+`symbols.go` source was read directly."

#### Q5 — ASCII art logo / figlet

- No figlet-style rendering dependency exists anywhere in `go.mod`/`go.sum` (`grep -i "figlet|banner|ascii|clack" go.sum` → no matches).
- `internal/ux/colors.go:19-37` is the load-bearing precedent here: the project **already made and documented** a deliberate choice to replace every East-Asian-Ambiguous-width Unicode glyph (✓ ℹ ✗ ▸ •) with ASCII equivalents (`+ i x > *`), specifically because "some terminal fonts render them two columns wide, which breaks printLine/newBulletList's fixed-width-3 centered alignment." A hardcoded ANSI-Shadow-style banner using heavy Unicode box-drawing characters (as in the issue) would need to pass through the *same* terminal-compatibility lens this project already applies elsewhere — this is context for the PRD to weigh, not a finding that the banner is infeasible.
- No test in `internal/ux/*_test.go` (per `Glob internal/ux/*.go`) was found to assert exact Unicode glyphs for a banner (none exists yet — the banner is new for this issue), so there's no existing test-suite constraint to work around, only the `colors.go` design precedent above.
- Simplest implementation path per the files actually read: a hardcoded raw string constant (Go raw string literal) printed via the existing `lipgloss.Fprintln`-based helpers (`internal/ux/printer.go`/`output.go` pattern) — no new dependency needed to render a static banner; the only open question (not resolved here) is whether the exact glyphs in issue #14's banner text survive Windows console output, which the project's own `colors.go` precedent suggests is worth verifying rather than assuming.

#### Q6 — Call sites outside `init.go` using `ux.Confirm`/`MultiSelect`/`InputForm`

Grep across the whole repo (`ux\.(Confirm|MultiSelect|InputForm|InputText|Password)\(`):

| Call site | Function | Notes |
|---|---|---|
| `cmd/apm-go/init.go:61` | `ux.Confirm("apm.yml already exists...")` | overwrite confirm |
| `cmd/apm-go/init.go:96` | `ux.InputForm("Setting up your APM project", fields)` | metadata group |
| `cmd/apm-go/init.go:160` | `ux.Confirm("Is this OK?", true)` | summary confirm |
| `cmd/apm-go/init.go:266` | `ux.MultiSelect("Select targets for this project", opts)` | target selection |
| `cmd/apm-go/init.go:275` | `ux.Confirm("Continue without pinning targets?", true)` | fallback confirm |
| `cmd/apm-go/mcp_prompt.go:103` | `ux.Password(label)` | `ttyAsk`, masked credential input |
| `cmd/apm-go/mcp_prompt.go:109` | `ux.InputText(label, "")` | `ttyAsk`, non-secret credential input |
| `cmd/apm-go/mcp_prompt.go:127` | `ux.Confirm("Replace MCP server %q?...", false)` | `promptReplaceMCP` |

`internal/ux/testhooks.go:10` references `ux.CanPrompt()`/`ux.Confirm()` only in a doc comment about test seams, not a real call site. `.trellis/spec/backend/terminal-ux-contract.md:175,193` documents the `ux.Confirm`/`IsRich`/`CanPrompt` contract (spec, not code).

**Implication for the PRD**: a global `themeFunc` change (e.g. re-bordering `Focused.Base`, fixing button alignment inside `ux.Confirm` itself) affects `mcp_prompt.go`'s credential/replace prompts too, not just `init`. If the clack-style gutter/transcript is meant to be `init`-only, it must be built as an `init`-local wrapper (e.g. new functions in `cmd/apm-go/init.go` or a new `internal/ux` helper invoked only from `init.go`), not a change to the shared `Theme()`/`themeFunc` in `internal/ux/theme.go`, or the credential prompts in `mcp_prompt.go` would inherit the same restyle.

### Python oracle parity (`D:\Projects\apm-dev\apm\src\apm_cli\commands\init.py`)

Read once for context only (not a gate — issue #14 is an explicit stylistic divergence per the task brief):
- Uses `rich.prompt.Prompt`/`rich.prompt.Confirm`/`rich.panel.Panel` (with a `click`/plain-text fallback when `rich` isn't importable) — no ASCII-art banner, no clack-style gutter/connector, no persistent per-step transcript beyond what `rich`/`click` naturally leaves on screen (unlike huh, `Prompt.ask`/`click.prompt` do **not** clear their own line after the user answers — the Python CLI's transcript persistence is incidental to the library, not a designed feature).
- `_confirm_setup_summary` (lines 417-453) renders the same "About to create" panel/summary shape `apm-go`'s `Box` (`internal/ux/output.go:134`) already mirrors — `name/version/description/author/targets`.
- No figlet/ASCII-art anywhere in this file or its imports.

### Related Specs

- `.trellis/spec/backend/terminal-ux-contract.md:175,193` — documents `ux.IsRich()`/`ux.CanPrompt()`/`ux.Confirm()` usage contract; would need updating if `init`'s prompt behavior diverges from the documented pattern.

## Implementation Options (effort / risk, not a recommendation of scope — PRD decides)

| Option | What it covers | Effort | Risk |
|---|---|---|---|
| (a) Theme-only tweaks | Fix button alignment is **not** achievable via theme (verified: `WithButtonAlignment` is a `*Confirm` builder method, not a `Styles` field, `field_confirm.go:361-364`) — so "theme-only" cannot fully fix even the reported Confirm bug. Theme *can* recolor the gutter char shape/color (`Focused.Base.BorderStyle`) but cannot add per-title bullet glyphs (verified: `Title.Render(text)` always uses the caller-supplied string, `field_confirm.go:243-244`) or a persistent transcript (verified: `Form.View()` returns `""` on quit, `form.go:655-657`). | Low (border/color only) | Low, but does not deliver items 1-2 of the issue (logo, connected transcript) at all — only a partial fix to item 3, and even item 3 needs an `interactive.go` code change (not pure theme) for button alignment. |
| (b) huh + manual transcript printing | Keep huh for input, add: (i) `interactive.go` `WithButtonAlignment(lipgloss.Left)` for the Confirm bug, (ii) a custom minimal/no-border theme variant for `init`'s fields so huh's own chrome doesn't visually double up with a hand-printed transcript, (iii) after each `runField`/`runForm` call in `init.go`, print the clack-style `<bullet> title` / `<bullet> answer` lines manually via existing `internal/ux` primitives, (iv) hardcode the ASCII logo string, (v) build the summary box via `internal/ux/output.go`'s existing `Box`/lipgloss primitives with clack-style corner glyphs. Confirmed huh does NOT need active erasure workarounds (it already clears itself, `form.go:655-657` + `cursed_renderer.go:262-264`), which removes the most-feared gotcha. | Medium — touches `interactive.go`, `theme.go` (an `init`-local theme, not the global one, per Q6), `init.go`, plus new banner/transcript helper code. | Medium — untested in this session whether huh's per-field program start/stop sequencing (Q3) has any visible flicker/race when interleaved with manual prints; needs a live-TTY smoke test before shipping. |
| (c) Adopt a clack-style library (`github.com/orochaa/go-clack`) | Full clack-native transcript/gutter/symbols out of the box (verified: `prompts/confirm.go` + `prompts/symbols/symbols.go` read live), correct button/alignment semantics by construction, ASCII-fallback already built in (`isunicodesupported`). | Medium-High if scoped to `init` only (new dependency, must coexist with huh at `mcp_prompt.go`'s 3 call sites, Q6); High if scoped globally (reimplement all of `internal/ux`'s public API on go-clack, migrating both call sites). | Medium — small library (8 stars, 1 open issue, single maintainer per GitHub API) though actively pushed (2025-12-14) and MIT-licensed; adds a second prompt-rendering stack alongside huh/bubbletea unless huh is fully replaced; deeper due diligence (security/code read of `core` package, transitive deps) not done in this session. |
| (d) Hand-rolled bubbletea | Full custom control over gutter/transcript/logo/summary box, reusing the `charm.land/bubbletea/v2`/`lipgloss/v2` deps already in `go.mod` (no new dependency). | High — reimplements form/field/focus/validation machinery huh already provides for `init`'s 6 fields (name/version/description/author/targets/confirms). | Highest effort-to-value ratio of the four for this specific issue; only clearly justified if (b) and (c) both prove unworkable in a spike. |

## Caveats / Not Found

- No live interactive-TTY test was run in this session (Bash tool sessions here are not real terminals); every claim about "what would print"/"what would flicker" for options (b)/(d) is derived from reading huh/bubbletea/lipgloss source, not from an observed run — flagged inline above wherever this applies.
- `github.com/orochaa/go-clack` maturity assessment is limited to what the GitHub REST API and raw file fetches returned live this session (8 stars, 1 fork, 1 open issue, MIT, pushed 2025-12-14, `go 1.22.1`); no dependency-graph/security audit of its transitive deps (`bradleyjkemp/cupaloy`, `stretchr/testify` are test-only per its `go.mod`) or of its `core`/`third_party/picocolors`/`third_party/is-unicode-supported` packages was performed.
- The exact glyph-for-glyph mapping of every "???" occurrence in the task brief (e.g. the precise summary-box corner/edge glyphs implied by `??? Title ???????????? ... ??????????????畔`) was not individually reverse-engineered character-by-character — only the general hypothesis (mangled Unicode box-drawing/bullet glyphs, most likely matching clack's own `symbols.go` set) is offered, backed by the live-fetched `symbols.go` evidence above; treat any 1:1 character guess beyond that as unverified.
- Whether Windows' default console codepage in this project's actual CI/user environments renders `┃`/`◆`/`│` etc. correctly (vs. `internal/ux/colors.go`'s stated East-Asian-Ambiguous-width breakage) was not tested here — no repro was run, this is a carried-over risk already documented in existing project code, not a new finding this session verified end-to-end.
