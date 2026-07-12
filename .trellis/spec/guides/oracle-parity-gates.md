# Oracle Parity Gates

> **Purpose**: Catch the "same name, different behavior" class of bug — where an
> apm-go CLI command/flag reuses a name that already means something different (or
> nothing) in the Python `apm` oracle — before it ships, not after a user gets
> silently misled by matching exit codes.

---

## The Problem: Reused-Name Blind Spot

Three separate defects (`codex agents` byte-copy fake TOML, `pack` same-name/
different-function, `audit` same-name/different-function) turned out to share one
structural root cause, documented in
`.trellis/tasks/07-12-cli-surface-parity-audit/research/root-cause-analysis.md`.
None were caused by low skill or missing tests in general — each task had research,
a design doc, and at least one round of adversarial review. The common failure was
narrower and easy to miss:

> Every "align with Python" task, in practice, decided its own verification scope
> case-by-case, and every scope boundary was drawn around "what this task intends to
> implement" — never around "what this CLI surface (command name, file extension,
> format) actually means on the Python side, end to end." Research, design, and
> review all inherited the same boundary, so nothing outside it was ever designed
> to be looked at.

| Aspect | Case 1: codex TOML | Case 2: `pack` | Case 3: `audit` |
|---|---|---|---|
| Borrowed surface | `.toml` extension (implies a target format) | Python's existing `pack` command name | Python's existing `audit` command name |
| Was Python behavior researched? | Yes, but only recorded that `format_id: codex_agent` existed — never expanded the transform logic | Yes, and the **in-scope** part got two adversarial review rounds | **No** — the task frame excluded this comparison axis entirely |
| Where the scope narrowed | `design.md` used one broad sentence ("format transforms deferred") to cover a structurally different `format_id` | Parent task split named the child `pack` and scoped it to "marketplace.json only" at split time | PRD requirement text translated spec wording ("re-verify on audit") straight into a command name, unchecked |
| Why verification missed it | Verification only checked file **placement**, never file **content validity** | A/B tests + adversarial review scope == implementation scope, never covering out-of-scope input | The verification contract itself never listed "Python same-name command behavior" as a comparison target |
| How the user actually hit it | Deployed files were fake TOML; Codex CLI couldn't parse them | `pack` on a dependencies-only/target-only project: exit 0, no output, no warning | Bare `audit` did non-overlapping checks on each side; tampering could pass on one side |

Evidence: `research/root-cause-analysis.md` Case 1 (lines 20-81), Case 2 (lines
84-139), Case 3 (lines 142-198), common-pattern table (lines 201-219).

---

## Gate 1 (PRD gate): Same-Name CLI Surface Inventory

**Trigger**: A new/changed apm-go CLI command or subcommand name (case-insensitive)
matches an existing Python `apm <verb>` command name.

**Required action** — the PRD must add a section that lists, for the matched
Python command:

1. Its **complete** behavior surface (every sub-function/branch, with file:line),
   not just the part you intend to build. Use
   `.trellis/spec/evals/cli-surface-parity-register.md` `group-packaging.md` /
   `group-integrity.md`-style inventories as the depth bar.
2. For every piece of that surface you are **not** covering this round, pick one
   explicitly and write it into the PRD:
   - (i) full parity this round
   - (ii) not this round, but the CLI **must warn/error** when it detects input
     that would trigger the uncovered branch — silent no-op / exit 0 is forbidden
   - (iii) explicitly deferred, with a tracking task reference
3. If Python has no same-name command (e.g. the name came from spec wording, see
   Gate 4), write one line confirming that was checked — "confirmed: no Python
   command named X" — not silence.

**Verification**: reviewer checks the PRD contains this section whenever a matched
name exists; a PRD that reuses a Python command name with no inventory section
fails review.

**Real case this prevents**: Case 2 (`pack`) — the parent task named a child
`07-03-marketplace-pack` and scoped it to "marketplace.json only" at split time
(`research/root-cause-analysis.md:88-97`), without ever inventorying what
`BundleProducer` / `PluginManifestProducer` do. Two adversarial review rounds
verified the in-scope algorithm but never asked "is the scope decision itself
safe" — see Gate 1 item 2(ii): had this gate existed, `pack` on a
`dependencies:`-only manifest would have to print a warning instead of silently
exiting 0 with no output (`.trellis/spec/evals/cli-surface-parity-register.md`
§3.2, Probe A/C transcripts).

---

## Gate 2 (Verification gate): Out-Of-Scope Input Must Fail Loud; Output Format Must Be Parsed

**Trigger**: Any task that deliberately narrows Python's original scope for a
command (Gate 1 outcome (ii)), or deploys/writes output to a path whose extension
or well-known location implies a structured format (TOML/JSON/YAML/...).

**Required action**:

- Scope-narrowing tasks: A/B tests or oracle fixtures must include **at least one**
  fixture that constructs input which would trigger the deferred branch, and assert
  a warning/error is produced. Asserting "exit 0, no output" is **not** a pass
  condition for that fixture.
- Structured-format output: verification must parse the produced bytes with that
  format's actual parser and assert success. Asserting only "file exists at the
  oracle-recorded path" is insufficient.

**Verification**: check.jsonl / review must confirm both fixture types exist before
marking a scope-narrowed or format-writing task complete.

**Real cases this prevents**:
- Case 1 (codex TOML) — both review rounds only used length/existence assertions
  on deployed files (`research/root-cause-analysis.md:60-73`); nobody ran a TOML
  parser against `.codex/agents/*.toml`, so a byte-copied markdown file with a
  `.toml` extension passed every check until a user's live probe found it
  unparseable 13 days later.
- Case 2 (`pack`) — AC4's A/B scope was limited to the scaffolded example project,
  which by construction only has a `marketplace:` block
  (`research/root-cause-analysis.md:117-120`), so the fixture set could never
  exercise the "has `dependencies:`/`target:` but no `marketplace:`" input that
  turned out to be the highest-risk case.

---

## Gate 3 (Research gate): A Recorded `format_id`/Transformer Key Is Not Enough — Expand It

**Trigger**: Research records a named transformer or `format_id` value from the
Python side (e.g. a column in a primitive-mapping table).

**Required action**: Either expand the transformer's actual logic with file:line
evidence for its input/output shape, or explicitly annotate "transform logic not
yet researched — design must not assume plain copy." Do not let a recorded key
value stand in for having read the code.

**Verification**: any research file listing `format_id` (or equivalent named-key)
columns must have a corresponding expansion section for every listed value, or an
explicit "not researched" flag next to it.

**Real case this prevents**: Case 1 — research correctly recorded
`format_id = codex_agent` in the primitive-mapping table
(`06-29-phase4-target-deploy/research/python-apm-research.md:159-163`), and even
defined what the `format_id` column meant (line 120), but §7 "Format Transforms"
only expanded the 5 `format_id` values under the *instructions* primitive type and
skipped `codex_agent` (an *agents* primitive type) entirely
(`research/root-cause-analysis.md:44-48`). The unexpanded key silently became
"plain copy" in `design.md:221`.

---

## Gate 4 (PRD gate): Spec-Wording Collision Check

**Trigger**: Requirements text is drawn directly from spec wording (OpenAPM spec,
RFC-style prose, etc.) rather than from a direct Python CLI comparison, **and**
that wording is a common verb/noun that could plausibly become a CLI command name.

**Required action**: Before finalizing the PRD, run one low-cost check: does the
Python CLI tree already have a command with that name?
(`grep -r "^def <name>" src/apm_cli/commands/` or `apm <name> --help`.) Write the
result into the PRD per Gate 1 item 3. This check applies **even to
spec-conformance-driven tasks** that don't frame themselves as Python-parity work —
that framing is exactly what let this defect through.

**Verification**: same as Gate 1 — PRD must show the check was run when spec
wording could plausibly collide with an existing Python command name.

**Real case this prevents**: Case 3 (`audit`) — `req-sc-001` translated the OpenAPM
spec phrase "re-verify on **audit**" directly into a command literally named
`audit`, without checking whether Python already had a same-named command
(`research/root-cause-analysis.md:171-178`). The task's own "Authority" section
(`design.md:3-9`) scoped verification to spec text + immutable oracle only —
Python CLI comparison was never in the comparison-object list, so neither of the
two external review rounds (opus + codex) was ever asked to check it
(`research/root-cause-analysis.md:182-190`). This is the deepest of the three
cases: not under-researched, but never in scope to research at all, because
spec-driven work and Python-parity work were treated as two disjoint checking
paths.

---

## Gate 5 (Living-doc gate): Register Update Rule

**Trigger**: Any PRD that adds or modifies apm-go CLI surface (new command, new
flag, changed behavior of an existing command).

**Required action**:

1. Before writing the PRD, check
   `.trellis/spec/evals/cli-surface-parity-register.md` for an existing entry for
   the command being touched. If none exists, note the surface as new (to be added
   on completion).
2. When the task completes, update that register row's status/evidence (category,
   severity, resolution note) to reflect the change.

This turns the register from a one-time audit snapshot into the standard starting
point every time apm-go's CLI surface is touched, instead of a file only read/
written once by the audit task that produced it.

**Note on the register's location**: the register lives at
`.trellis/spec/evals/cli-surface-parity-register.md`, **not**
`.trellis/spec/conformance/`. `conformance/` is gitignored (`.gitignore:44`,
"Conformance authority — local-only, not pushed to remote") and holds
`cli-verification-checklist.md` / `openapm-v0.1.md` under that established,
intentional convention. The register was moved to `spec/evals/` specifically so it
stays version-controlled and this gate is enforceable across clones/CI — do not
move it back into `conformance/`.

---

## Checklist: Before Writing a PRD That Touches CLI Surface

- [ ] Checked whether the new/changed apm-go command name matches an existing
      Python `apm <verb>` name (case-insensitive) — Gate 1
- [ ] If matched: added the full Python behavior inventory + per-item
      (i)/(ii)/(iii) disposition to the PRD — Gate 1
- [ ] If requirement wording is drawn from spec text rather than direct Python
      comparison: ran the same-name check anyway — Gate 4
- [ ] If scope is deliberately narrower than Python: added an out-of-scope-input
      fixture that asserts warn/error, not silent exit 0 — Gate 2
- [ ] If output is written to a path implying a structured format: verification
      parses the output with that format's real parser — Gate 2
- [ ] If research records a `format_id`/transformer key: expanded its logic or
      explicitly flagged it unresearched — Gate 3
- [ ] Checked `.trellis/spec/evals/cli-surface-parity-register.md` for an existing
      row before starting, and updated it on completion — Gate 5

---

## Real-World Examples

**`codex agents` byte-copy fake TOML** (Case 1): `internal/deploy/codex.go`
byte-copied markdown source to `.codex/agents/*.toml` for 13 days and one review
round before a user's live probe against the `evals/test1` fixture found the Codex
CLI couldn't parse the output. Fixed in commit `197fe98` by adding
`transformCodexAgent`/`deployCodexAgentTOML` mirroring Python
`agent_integrator.py:302 _write_codex_agent`. See Gate 2 and Gate 3.

**`pack` same name, different function** (Case 2): `cmd/apm/pack.go:24-36`
knowingly implements only `MarketplaceProducer`; Python's `BuildOrchestrator`
routes to up to three producers (`BundleProducer`, `MarketplaceProducer`,
`PluginManifestProducer`). On a `dependencies:`-only or `target:`-only manifest,
apm-go prints `nothing to do` and exits 0 while Python produces a plugin bundle or
project-root `plugin.json`. See Gate 1 and Gate 2.

**`audit` same name, different function** (Case 3): apm-go `audit` (bare)
re-verifies lockfile SHA-256 hashes; Python `apm audit` (bare) runs a hidden-Unicode
scan and never touches SHA-256 — Python's equivalent hash check is buried inside
`apm audit --ci` as its 7th check (`content-integrity`, `ci_checks.py:280-375`),
behind 6 fail-fast checks by default. A tampered file can be silently accepted by
Python bare `audit` while apm-go correctly flags it (or vice versa depending on
which check order applies). See Gate 4.

Full evidence and transcripts:
`.trellis/tasks/07-12-cli-surface-parity-audit/research/root-cause-analysis.md`
and `.trellis/spec/evals/cli-surface-parity-register.md` §3.
