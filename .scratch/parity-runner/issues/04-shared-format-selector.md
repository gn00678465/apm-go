# 04 — Shared bundle-format selector resolver (prefactor)

**What to build:** The `--format` / `--claude-plugin` resolution that `plugin init` already has becomes a shared helper that any command can use with its own choice list, so that `pack` (ticket 07) gets byte-identical Click-style behaviour without copy-paste. `plugin init` behaves exactly as before.

**Blocked by:** None — can start immediately.

**Status:** done — be88962 + 2ddd2fc; verified .review/eval-ticket-04.md (AC1 reworded to behavioural; 7/7)

**Oracle:** `bundle/formats.py:38-115` — `_CLI_CHOICES`, `_SELECTOR_ALIASES`, `coerce_bundle_format`, `resolve_bundle_format`, `format_selection_text`. Evaluator guardrails: `.review/ticket-review.md` §F "02".

## Acceptance criteria

- [ ] One resolver takes `(value string, valueSet bool, claudePlugin bool, choices []string)` and returns one of three modes: `claude`, `agent`, `apm`. Every input upstream's `_SELECTOR_ALIASES` accepts (including the `agent_plugin` / `claude_plugin` underscore spellings) resolves to the same mode here — whether via a literal table entry or via normalisation (trim+lower+space/underscore→hyphen) first is an implementation choice. Behavioural equivalence is the criterion.
- [ ] `plugin init` passes the 4-choice list (`plugin, agent-plugin, claude, claude-plugin`). A test proves `plugin init --format apm` is still rejected with the Click Choice text listing exactly those four.
- [ ] The 5-choice list (`…, apm`) exists as a named value ready for `pack`, and a unit test proves `apm` resolves to mode `apm` through it.
- [ ] Every error string is unchanged from today: Click Choice "Invalid value for '--format': 'X' is not one of …" (the list rendered from the caller's choices), "Choose one bundle format selector; received: …", "Option '--format' requires an argument.".
- [ ] The `[a|b|c]` metavar `pflag.Value` and the exit-2 `SetFlagErrorFunc` mapper are shared helpers taking the choice list / command, with no behaviour change for `plugin init`.
- [ ] All existing `plugin init` tests pass unmodified (table test, command tests, help test). No other command's flag parsing changes — `go test ./cmd/apm-go/` is green.
- [ ] A static guard: a test asserts there is exactly one definition of the alias table in the package (e.g. via `go/ast` or a grep-style check) so `init` and `pack` cannot fork it later.
