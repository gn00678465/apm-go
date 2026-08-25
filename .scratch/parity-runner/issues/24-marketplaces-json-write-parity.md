# 24 — `marketplaces.json`: local sources stored as a bare path, and the file is never created when empty

**What to build:** the registry file `~/.apm/marketplaces.json` must match the Oracle's bytes: a local source is stored as a `file://` URI, and the file exists (as `{"marketplaces": []}`) after any command that reads the registry, even when nothing is registered.

**Blocked by:** none. **Status:** open. **Clears:** the `tree` field of 11 of the 18 backlog cases — every `search-*` hit case, both `validate-checkrefs-*`, `list-empty`, and `browse-unknown-marketplace`.

**Origin:** orchestrator triage of the standing 18 unwaived parity cases, 2026-08-25. Sub-gap (a) is the item recorded against ticket 05 as "the `home/.apm/marketplaces.json` tree gap (Oracle stores a `file://` URI, apm-go a bare path)"; sub-gap (b) surfaced in the same triage.

## The two sub-gaps

**(a) `file://` URI vs bare path** — 9 cases show this as a `changed` tree entry:

```
oracle: { "marketplaces": [ { "name": "skills", "url": "file:///tmp/.../cwd/fixture" } ] }
target: { "marketplaces": [ { "name": "skills", "url": "/tmp/.../cwd/fixture" } ] }
                                                        ^^^^^^^ missing scheme
```

**(b) the file is never created when the registry is empty** — `list-empty` and `browse-unknown-marketplace` show it as `only-in-oracle`:

```
oracle wrote: home/.apm/marketplaces.json  ->  { "marketplaces": [] }
target wrote: (nothing)
```

Both commands only *read* the registry, so the Oracle is creating-on-read (or creating-on-load-miss). Determine which, from the Oracle source, before implementing — "write an empty file whenever anything reads the registry" and "materialise the default on a load miss" are different behaviours that happen to agree on these two cases, and picking the wrong one will diverge on some case not in the corpus.

## Compatibility — read this before changing the write path

`marketplaces.json` is a **user file that already exists on disk** for anyone running an earlier apm-go, containing bare paths. Changing the write format must not orphan those entries:

- The reader must keep accepting a bare path **and** a `file://` URI, indefinitely. This is not a migration with an end date.
- Do not rewrite a user's existing file as a side effect of an unrelated read. If a rewrite happens it must be because the user ran a command that legitimately mutates the registry.
- Round-trip: after `marketplace add ./x` then any read, the entry must resolve to the same directory it did before this change.

## Acceptance criteria

- [ ] AC1 — Cite the Oracle file:line for (i) how the `url` field is produced for a local source and (ii) exactly when the file is created. Quote the code, do not infer the rule from the two corpus cases alone.
- [ ] AC2 — A local source registered via `marketplace add ./path` is stored as a `file://` URI byte-identical to the Oracle's for the same input, including how an absolute path with no host is spelled (`file:///abs/path`, three slashes) and how any character needing escaping is encoded.
- [ ] AC3 — The reader accepts bare paths and `file://` URIs alike; an existing bare-path registry keeps resolving. Regression test with a hand-written bare-path `marketplaces.json` fixture.
- [ ] AC4 — The file-creation behaviour matches whatever AC1(ii) established, at the same trigger point.
- [ ] AC5 — Non-local sources (`OWNER/REPO`, HTTPS, SSH) are untouched — no scheme added, no re-encoding. Regression test each shape.
- [ ] AC6 — In a fresh runner corpus, the 11 named cases report **no `tree` diff**. Several will still differ on `stdout`/`error_body`; those are tickets 22/23/25 and may remain.
- [ ] AC7 — Zero regression elsewhere: every other case's `(fields, waived)` tuple unchanged from the pre-change baseline. Note `registry-explicit-config-dir` has its own, different tree diff (apm-go honours `APM_CONFIG_DIR`, the Oracle ignores it) — it is ticket 25's waiver, not this ticket's, and must not be "fixed" by removing `APM_CONFIG_DIR` support.
- [ ] AC8 — Go tests for AC2/AC3/AC4/AC5.
