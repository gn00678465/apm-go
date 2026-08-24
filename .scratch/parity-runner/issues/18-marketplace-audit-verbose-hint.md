# 18 — `marketplace audit` missing "Run with --verbose for details." hint

**What to build:** `apm-go marketplace audit NAME` against an unregistered NAME is missing a trailing `[i] Run with --verbose for details.` line the pinned Oracle always prints after its `MarketplaceNotFoundError` wrap.

**Blocked by:** none. **Status:** open (spun out of ticket 14 attempt 2's evaluator review).

**Origin:** eval-ticket-14.md's verified-items section, reviewing `marketplaceNotRegisteredErr(verb, name)`'s ticket-14 rewrite: "The Oracle's audit command additionally emits `[i] Run with --verbose for details.` after that error while apm-go does not; this predates this helper rewrite and is outside Ticket 14's two written browse/list findings. It should be recorded as a separate marketplace-output follow-up, not waived or used to conceal the wrap regressions above." Per the project's Scope rule (`.scratch/parity-runner/README.md`), a real finding outside a ticket's written acceptance criteria is spun into its own ticket rather than fixed inline or waived away.

## The finding

Live-probed against the pinned Oracle (`c8d6cdec596e773a84b0839c33c28b6b0a217637`):

```
$ apm marketplace audit nonexistent
[x] Failed to audit marketplace: Marketplace 'nonexistent' is not registered. Run 'apm marketplace add ...' ...
[i] Run with --verbose for details.
```

apm-go's `marketplaceAuditCmd` (`cmd/apm-go/marketplace_authoring_audit.go`) returns `marketplaceNotRegisteredErr("audit", name)` directly and prints nothing else — the trailing `[i] Run with --verbose for details.` line never appears, regardless of whether `--verbose`/`--strict` is passed.

Needs verification before fixing: whether this hint line is specific to `audit`'s own error handler (`commands/marketplace/audit.py`) catching `MarketplaceNotFoundError` and appending the hint itself, or a broader pattern shared with other commands that also accept `--verbose` (in which case the fix belongs in a shared helper, not a one-off).

## Acceptance criteria

- [ ] Confirm live against the pinned Oracle whether the hint line is audit-specific or shared, and cite the exact oracle file:line.
- [ ] Add apm-go's own runner case (`marketplace audit <unregistered-name>`) if one doesn't already exist, capturing this exact gap.
- [ ] Implement the hint line at the verified call site(s) only.
- [ ] Fresh corpus evidence: the new/updated case shows no unwaived diff on this specific line; zero `(fields, waived)` regression elsewhere.
