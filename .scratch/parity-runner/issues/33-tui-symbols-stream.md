# Ticket 33 — stream TUI symbols and parity glyph-shape normalization

Status: CLOSED (2026-08-29)

## What

Use the project TUI status vocabulary for every stream status record:

| Class | Symbol | Rendered form |
|---|---:|---:|
| success | `+` | ` + ` |
| info | `i` | ` i ` |
| warning | `!` | ` ! ` |
| error | `x` | ` x ` |
| progress | `>` | ` > ` |
| list | `*` | ` * ` |

The stream renderer centers each symbol in a width-3 column and never emits
Oracle bracket glyphs. Oracle `[#]`, `[~]`, `[-]`, and `[=]` classes are mapped
to narrow ASCII project equivalents for comparison as needed.

## Why

The user ruling reverses the glyph-shape half of tickets 10 and 31. The
channel decision from ticket 10 is unchanged: ordinary errors and warnings
still land on stdout. Parity must compare status meaning without making the
Oracle's bracket syntax a product-output requirement.

## Acceptance criteria

- [x] `internal/ux` renders success, info, warning, error, progress, and list
  status records with centered width-3 TUI symbols and per-writer styling.
- [x] `doctor` table status cells use the same symbol vocabulary; no
  bracketed status cell is emitted by apm-go.
- [x] `tools/parity` always normalizes exact line-leading Oracle bracket forms
  and apm-go centered forms to one canonical glyph token before diffing stdout,
  stderr, and `error_body`; mid-line bracket text remains literal.
- [x] Existing Go assertions and the interactive init checks cover the new
  stream forms; normalization tests cover both directions and every corpus
  class.
- [x] The pinned 96-case corpus reports 0 unwaived diffs, with no new waiver
  and unchanged or narrower `(fields, waived)` tuples.
- [x] `AGENTS.md` documents the TUI vocabulary, stdout channel contract, and
  always-on parity normalization.

## Implementation

`internal/ux/colors.go` owns the vocabulary. `internal/ux/printer.go` routes
all stream records through `printLine` and the existing stdout channel policy.
`cmd/apm-go/init.go` uses one stream renderer for non-interactive success
output and keeps clack's interactive renderer on the same vocabulary.
`tools/parity/normalize.go` performs the F0x glyph-shape normalization and
`tools/parity/diff.go` applies it to `error_body` and wrapped status records.

## Tests

Focused package tests, the full Go suite, vet, race tests, real CLI execution,
the pinned Oracle corpus, and a target-output scan for line-leading Oracle
brackets are part of the verifier evidence in `/tmp/verifier-report-11.md`.
