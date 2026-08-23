# 14 — Marketplace command wording parity

**What to build:** Close the message-wording gaps ticket 10 attempt 2 surfaced (and deliberately left unwaived) on `marketplace browse <unknown>` and `marketplace list` (empty registry): apm-go's error/info text differs from the Oracle's in content, not just channel/prefix (ticket 10 already fixed channel/prefix).

**Blocked by:** none — channel/prefix parity (ticket 10) already landed; this is pure wording.

**Status:** ready-for-agent

**Origin:** ticket 10 attempt 2 (eval-ticket-10.md): `browse-unknown-marketplace` and `list-empty` still show wording diffs after the channel/prefix fix. Per direct instruction, these are NOT waived in `waivers.json` (only the pre-existing `tree` field is) so the runner's gate keeps them visibly open. Evidence below is the exact old/new from `/tmp/p12/diff/browse-unknown-marketplace.json` and `/tmp/p12/diff/list-empty.json` (full-corpus run against the pinned Oracle, oracle_commit `c8d6cdec596e773a84b0839c33c28b6b0a217637`).

## Findings to close

### 1. `marketplace browse <unknown-marketplace>` — `error_body` wording

- Oracle: `Failed to browse marketplace: Marketplace 'nonexistent' is not registered.`
- apm-go: `marketplace "nonexistent" is not registered (no marketplaces registered; add one with: apm-go marketplace add SOURCE)`

Full stdout (normalized, `apm`/`apm-go` names left as each side printed them):

```
# Oracle
[x] Failed to browse marketplace: Marketplace 'nonexistent' is not registered.
Run 'apm marketplace add https://github.com/OWNER/REPO' or 'apm marketplace add
OWNER/REPO' to register it, or 'apm marketplace list' to see registered
marketplaces.

# apm-go
[x] marketplace "nonexistent" is not registered (no marketplaces registered; add one with: apm-go marketplace add SOURCE)
Run `apm-go marketplace list` to see registered marketplaces, or `apm-go marketplace add SOURCE` to register a new one.
```

apm-go's message is `marketplaceNotRegisteredErr`'s own phrasing (quoting style, parenthetical, backtick-quoted hint commands) rather than a transliteration of the Oracle's `Failed to browse marketplace: Marketplace '...' is not registered.` + `Run '...' or '...' to register it, or '...' to see registered marketplaces.` two-sentence form. Decide whether to match the Oracle's exact wording (structure it as two sentences, single-quoted hints) or record a deliberate, dated wording deviation the way `search.go`'s package doc comment records its own (ticket 05's precedent).

### 2. `marketplace list` (empty registry) — `stdout` wording

- Oracle: `[i] No marketplaces registered. Use 'apm marketplace add SOURCE' to register one (OWNER/REPO, HTTPS URL, SSH URL, or local path).`
- apm-go: ` i No marketplaces registered. Add one with: apm-go marketplace add SOURCE`

Two gaps, not one:
- Prefix: apm-go's `Info` still uses the bare centered `" i "` (`colors.go`'s `SymbolInfo`) — ticket 10 decision (A) only moved `Warn`/`Error` to the Oracle's bracketed `[!] `/`[x] `; `Info`/`Success` were left on the existing convention because no oracle/apm-go difference had been found for them at the time. Decide whether `Info` should also switch to `[i] ` for exact parity (the Oracle's `STATUS_SYMBOLS` prefixes `[i]` the same as `[x]`/`[!]`), or whether that's an intentional, recorded deviation.
- Body wording: apm-go's shorter `Add one with: apm-go marketplace add SOURCE` vs. the Oracle's longer `Use 'apm marketplace add SOURCE' to register one (OWNER/REPO, HTTPS URL, SSH URL, or local path).` — the Oracle names the four accepted source forms; apm-go's hint doesn't.

## Acceptance criteria

- [ ] Decide, for each of the two messages, whether apm-go adopts the Oracle's exact wording or records a dated, reasoned deviation (style: `search.go`'s top-of-file comment on its three hint strings).
- [ ] If adopting Oracle wording: update the message strings, and any others in `marketplace.go`/`marketplace_browse.go` (or wherever these live) that share the same pattern.
- [ ] Decide `Info`'s prefix (`" i "` vs `[i] `) once, for all commands — not just `list` — consistent with ticket 10's "no command chooses its own" principle. If changed, `ux.Info`'s doc comment and every existing test asserting the current `" i "` glyph must be updated in the same commit.
- [ ] `waivers.json`'s `browse-unknown-marketplace` and `list-empty` entries drop back to wildcard-free, field-precise waivers ONLY for whatever gap (if any) remains a deliberate, dated deviation after this ticket's decision — or drop the waiver entirely if apm-go now matches the Oracle byte-for-byte.
- [ ] Runner: both cases show no unwaived diff (clean pass) or a single, newly-dated waiver reflecting the recorded deviation.
