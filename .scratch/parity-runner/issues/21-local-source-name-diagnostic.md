# 21 — trailing-separator rejection blames the derived name, not the source the user typed

**What to build:** when `marketplace package add` rejects a package name that was *derived* from a local source, the error must name the source the user actually typed, and must not render a single backslash as two.

**Blocked by:** ticket 20 (`99bfbd0`). **Status:** open.

**Origin:** `.review/eval-ticket-20.md`, "Diagnostic follow-up (not an AC blocker)". Ticket 20 PASSed on all seven ACs; this is the quality item it explicitly deferred.

## The finding

Ticket 20 stops the user-reported bad state from ever being written. But for the exact reported input, the message points at the wrong thing:

```
$ apm-go marketplace package add './llm-wiki\'
[x] package name "llm-wiki\\" must not contain a path separator
```

Two problems:

1. **Wrong subject.** The user typed a *source*, not a `--name`. `resolveLocalSourceAgainstRoot` normalises `\` → `/`, so `./llm-wiki\` resolves to the existing `llm-wiki` directory and clears AC1's existence check; the rejection then lands on AC3's name guard. Correct outcome, unhelpful diagnostic — nothing tells the user their `./llm-wiki\` argument is what to fix. This only affects the *derived*-name path; an explicit `--name 'bad\name'` is already blaming the right thing.
2. **`%q` double-backslash.** `"llm-wiki\\"` is a faithful Go quoted form of a one-backslash string, but it makes the user decode Go escaping to recognise their own input. Print the name in a form that matches what they typed.

The evaluator also flagged an inaccurate test comment: one of ticket 20's tests is labelled as an AC1 case and its comment claims the normalised resolved path does not exist. It does exist — that test actually exercises AC3.

## Acceptance criteria

- [x] AC1 — when the rejected name came from `defaultNameFromSource` (not from an explicit `--name`), the error names the source argument as given, e.g. `local source "./llm-wiki\" derives package name "llm-wiki\", which contains a path separator; pass --name to set it explicitly`. Exact wording is yours to choose; it must contain the raw source string and point at a remedy.
- [x] AC2 — an explicit `--name` rejection keeps blaming the name (its current message is already correct); do not regress it.
- [x] AC3 — neither message renders a single backslash as two. Note `ux.Error` takes a format string, so a `%`-bearing name must still be handled safely.
- [x] AC4 — fix the mislabelled/inaccurate ticket-20 test comment identified above.
- [x] AC5 — exit code stays 2, `apm.yml` stays byte-identical on the failure path, and every ticket-20 test still passes. Tests for both AC1 and AC2 message shapes.
- [x] AC6 — no parity-corpus drift: `(fields, waived)` tuple-identical across all 69 cases (there are still no `marketplace package add` cases; confirm rather than assume).

## Implementation

`internal/marketplace/authoring/editor.go`:
- `validatePackageName` split into a pure classifier, `packageNameIssue(name) packageNameProblem`, and a message builder, `packageNameError(name, problem) error` — both the explicit-`--name` path (AC2, unchanged wording) and the new derived-name path share the same classification instead of AddPackage re-deriving or string-matching validatePackageName's error text.
- New `packageNameDiagnosticQuote(s string) string`: wraps in plain `"..."` with **no escaping at all** — this is what fixes AC3's `%q` double-backslash (`\` → `\\`). Used everywhere a source/name string is rendered into one of these diagnostics.
- New `packageNameDerivedError(source, name, problem) error` (AC1): built with `fmt.Errorf("... %s ...", quoted-source, quoted-name, problem)` — the untrusted strings are always `%s` *arguments*, never the format string itself, which is what makes AC3's "`ux.Error` takes a format string" concern moot here (confirmed with a `%`/`\`-bearing fixture, `TestAddPackage_LocalSource_DerivedNameControlChar_SafeWithPercentSign`).
- `AddPackage` now tracks `derivedFromSource := opts.Name == ""` and picks `packageNameDerivedError` vs `packageNameError` accordingly.

Live reproduction of the exact ticket 20/21 input:
```
$ apm-go marketplace package add './llm-wiki\'
[x] local source "./llm-wiki\" derives package name "llm-wiki\", which must not contain a path separator; pass --name to set it explicitly
exit: 2
```
and the explicit-`--name` path, confirmed unaffected:
```
$ apm-go marketplace package add ./pkgs/tool --name 'bad\name'
[x] package name "bad\name" must not contain a path separator
exit: 2
```

AC4: `TestAddPackage_LocalSource_TrailingSeparator_Rejected`'s doc comment (`internal/marketplace/authoring/editor_test.go`) rewritten to state the actual mechanism (the normalised path exists and clears AC1; the derived name still contains the literal `\` and is rejected by AC3) instead of the inaccurate "resolved path does not exist" claim the evaluator flagged. This is also the test now locking ticket 21's fix (asserts the source is named and no `\\` appears).

## Tests

New: `TestAddPackage_LocalSource_DerivedNameControlChar_SafeWithPercentSign` (AC3, `%`-safety). Tightened: `TestAddPackage_LocalSource_TrailingSeparator_Rejected` (AC1/AC3/AC4, comment + message-content assertions), `TestAddPackage_NameFlag_TrailingSeparator_Rejected` (AC2, asserts the name — not the source — is blamed), `TestMarketplacePackageAdd_LocalSourceTrailingSeparator_RejectedEndToEnd` (CLI end-to-end, AC1/AC3). No ticket-20 test needed a message-shape change beyond these (none asserted exact wording before).

## Evidence

- `go build ./...`: clean. `go test ./...`: all packages `ok`. `go test ./cmd/apm-go/... ./internal/marketplace/authoring/... -race -count=1`: both `ok`.
- Parity corpus: built the pre-fix (`99bfbd0`, stashed) and post-fix binaries, ran all 69 `tools/parity` cases against the pinned Oracle (`c8d6cdec...`) for each. Both report the same pre-existing "18 of 69 unwaived". A tuple-level `(fields, waived)` diff across all 69 case ids: **zero changes**. Confirmed (not assumed): no case invokes `marketplace package add` or `marketplace check`.

## Non-goals

- Re-litigating ticket 20's AC1/AC3 boundary. The rejection point is correct; only the message changes.
- Changing which inputs are accepted or rejected. This ticket is diagnostics only — behaviour must be identical before and after.
