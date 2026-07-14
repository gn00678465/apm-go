# Codex adversarial verification — Phase 2–5 pack parity

Date: 2026-07-13  
Commit under test: `f547b4b` (`feat(pack): Phase 2-5 pack 三 producer 完整 parity`)  
Verifier posture: prior agent claims and implementation comments were treated as untrusted. Conclusions below come from direct Python-source comparison, freshly built Go code, independent TEMP fixtures, and Python/Go A/B runs.

## Verdict

**VERDICT: PASS**

No Gate B failure was reproduced. The only red result during verification was a verifier-harness error: a PowerShell helper named its parameter `$args`, so `--force` was not forwarded. Renaming it to `$packArgs` made the same fixture pass (`dep-two` won), proving this was not a product failure.

## What was verified

Only Phase 2–5 pack scope was judged:

- `internal/pack/detect.go`
- `internal/pack/bundle/{collect,merge,mcpjson,lockfile_pack,pluginjson,producer}.go`
- `internal/pack/pluginmanifest/*`
- `cmd/apm/pack.go`

No product code, spec, commit, marketplace configuration, or Python oracle repository was changed. TEMP fixtures were deleted after each run. The optional stash red-light exercise was not used: `f547b4b` is already committed at HEAD and the initial worktree was clean, so `git stash push -- <file>` would have no implementation delta to remove; manufacturing one would have exceeded the authorized mutation sequence.

## Gate B results

### 1. `.mcp.json` redaction regexes — PASS

Direct source comparison:

- Python authority: `D:/Projects/apm-dev/apm/src/apm_cli/core/plugin_manifest.py:73-278`
- Go: `internal/pack/bundle/mcpjson.go:20-160`

The six Go patterns and their application order match Python exactly, allowing only syntax-equivalent flag spelling (`(?i)` in Go versus `re.IGNORECASE` in Python):

1. URL userinfo: `\b([a-zA-Z][\w+.-]*://)([^/?#\s@]+)@`
2. Inline flag: `(--?[\w.-]*(?:token|secret|password|credential|apikey|key)[\w.-]*=)(\S+)`
3. Space-separated flag in one string: `(--?[\w.-]*(?:token|secret|password|credential|apikey|api-key|key)[\w.-]*\s+)(\S+)`
4. Environment assignment: `\b([A-Za-z0-9_]*(?:token|secret|password|credential|apikey|api_key|key)[A-Za-z0-9_]*=)(\S+)`
5. Auth scheme: `\b(Bearer|Basic)\s+([A-Za-z0-9._~+/=-]{8,})`
6. Known provider tokens: all 14 alternatives match, including their case sensitivity and minimum lengths.

The key rules also match: exact `env`, `environment`, `headers`, `authorization`; lowercase-and-remove-underscore normalization; substring checks for `token`, `secret`, `password`, `credential`, `apikey`, `key`. `sanitizeObject`/`sanitizeArray` recurse without a depth cap, list-context `['--token', 'value']` redacts the following value, and `SanitizeServers` deliberately does not sensitivity-test server names.

Independent A/B fixture results:

- Go exit 0; Python exit 0.
- Exercised all six value-regex classes.
- Exercised all 14 alternatives in the provider-token regex.
- Exercised all four exact keys and all six required substring classes, including `privateKey`/`signingKey` at arbitrary depth.
- `my-keychain` survived as a server name on both sides.
- Safe deep sibling content survived.
- Go and Python produced equal sanitized argument values for every tested item.
- Both emitted the same dropped/redacted paths (presentation wrapping/prefix differed only because Python uses Rich).

### 2. Merge direction table — PASS

Direct source comparison:

- Python: `bundle/plugin_exporter.py:475-536`, `_deep_merge` at `:215-234`, `_merge_file_map` at `:683-708`
- Go: `internal/pack/bundle/merge.go:59-79`, `internal/pack/bundle/jsonvalue.go:300-339`, orchestration in `internal/pack/bundle/producer.go:89-149`

Independent fixtures proved each direction:

| Collision | No `--force` result | `--force` result |
|---|---|---|
| dep one vs dep two `agents/shared.md` | `dep-one` (first wins) | `dep-two` (last wins) |
| dependency vs root `agents/shared.md` | dependency wins | not used for this rule |
| dependency vs root hooks key | root wins; non-conflicting dep/root members both remain | not applicable |
| dependency vs root MCP server field | root wins; non-conflicting dep/root members both remain | not applicable |

The root-vs-dependency fixture was also run through Python. Both CLIs chose dependency content for the file map and root content for hooks/MCP, and emitted the same bundle file list.

### 3. `bundle_files` hash format — PASS

Direct source comparison:

- Python bare digest: `bundle/plugin_exporter.py:632-660` (`hashlib.sha256(...).hexdigest()`)
- Go bare digest: `internal/pack/bundle/producer.go:472-508` (`hex.EncodeToString(sum[:])`)
- Pack serialization: `internal/pack/bundle/lockfile_pack.go:47-109`
- Contrasting normal lockfile convention: `internal/lockfile/hash.go:14-49` (`HashFileBytes` returns a `sha256:` envelope)

The scratch bundle contained four `pack.bundle_files` entries. Every value matched `^[0-9a-f]{64}$`; none contained `sha256:`. Keys were emitted deterministically. Format only was judged, as requested; install consumption is outside this round.

### 4. Trigger matrix — PASS

Direct source comparison:

- Python: `core/build_orchestrator.py:346-393`
- Go routing: `internal/pack/detect.go:36-44`
- Go input derivation and producer order: `cmd/apm/pack.go:120-168`
- Nine-row executable table: `internal/pack/detect_test.go:8-52`

`DetectOutputs` independently derives bundle, marketplace, and plugin-manifest outputs, and returns `ErrNothingToPack` when all are false. `runPack` sets `hasDeps` only from `len(m.ParsedDeps) > 0`; it does not count `ParsedDevDeps`, MCP servers, or MCP dev servers. Producer order is Bundle → Marketplace → PluginManifest with immediate error return.

Fresh CLI probes against the rebuilt `bin/apm-go.exe`:

| Fixture | Exit | Required effect |
|---|---:|---|
| empty `apm.yml` | 1 | nothing-to-do fails |
| `target: codex` only | 1 | non-plugin ecosystem does not trigger |
| pure `devDependencies.apm` | 1 | dev dependency does not trigger bundle |
| `dependencies.apm` only | 0 | real `build/deps-only-1.0.0/plugin.json` produced |
| `targets: [claude, copilot]` only | 0 | both standalone plugin manifests produced |

The 14-case marketplace A/B regression separately covered marketplace-only and multi-output marketplace behavior.

### 5. `plugin.json` synthesis — PASS

Direct source comparison:

- Python synthesis: `deps/plugin_parser.py:930-990`
- Python ecosystem behavior: `core/plugin_manifest.py:338-380`
- Python bundle key stripping: `bundle/plugin_exporter.py:365-397`
- Go synthesis/serialization: `internal/pack/bundle/pluginjson.go:57-201`
- Go ecosystem orchestration: `internal/pack/pluginmanifest/producer.go:32-63`
- Go bundle key stripping: `internal/pack/bundle/producer.go:420-448`

Independent fixtures proved:

- Author dict preserved `name`, `email`, `url`.
- Author string `Bob` became `{ "name": "Bob" }`.
- Scalar keyword `one` became `["one"]`; keyword list `[one, two]` remained a two-element string list.
- `agents`, `skills`, `commands`, `instructions` were removed from an authored bundle `plugin.json`; unrelated `custom` remained.
- Claude standalone output included sanitized `mcpServers`; Copilot standalone output omitted it.
- A/B standalone field set matched exactly: `author, description, homepage, keywords, license, mcpServers, name, repository, version`.
- A/B bundle `plugin.json` field keys matched in both oracle fixtures.

### 6. dry-run / `--force` / WARN_POLICY / P0 warning removal — PASS

Direct code path:

- `internal/pack/bundle/producer.go:89-219` returns from the dry-run branch before `scanBundleSources` and before `os.RemoveAll`/`MkdirAll`/writes.
- `scanBundleSources` is at `internal/pack/bundle/producer.go:313-337` and uses `security.WarnPolicy`.
- `--force` wiring is in `cmd/apm/pack.go:35-87`; it affects file-map collisions/plugin overwrite, while scan remains warn-only.

Independent critical-character fixture (`U+202E`) proved:

- `pack --dry-run`: exit 0, no `build/`, no hidden-character warning (zero scanner invocation observable at the CLI boundary).
- normal `pack`: exit 0, hidden-character warning present, bundle written.
- `pack --force`: exit 0, hidden-character warning still present, bundle written.
- Therefore WARN_POLICY did not block, and `--force` did not suppress or bypass scanning.

The retired Phase-1 messages containing `apm-go pack only builds marketplace.json` were absent from deps-only and target-only runtime output. Graph-backed literal search found zero occurrences of that text in scoped Go files.

## Python/Go oracle A/B evidence

Both A/B runs used TEMP copies of the same authored fixture and the exact commands:

```text
uv --project D:/Projects/apm-dev/apm run apm pack
D:/Projects/apm-dev/apm-go/bin/apm-go.exe pack
```

Run A (deps + claude + redaction):

- exits: Python 0 / Go 0
- bundle file list on both: `.mcp.json`, `agents/probe.md`, `plugin.json`
- standalone plugin fields matched exactly
- bundle plugin fields matched
- sanitized MCP values matched item-by-item

Run B (locked dependency + root merge):

- exits: Python 0 / Go 0
- bundle file list on both: `.mcp.json`, `agents/shared.md`, `apm.lock.yaml`, `hooks.json`, `plugin.json`
- winners on both: dependency file, root hook, root MCP
- bundle plugin fields matched

No oracle marketplace add/remove/update command was invoked.

## Required command results

Fresh binary first:

```text
go build ./internal/pack/... ./cmd/apm/...                 PASS
go build -o bin/apm-go.exe ./cmd/apm                     PASS
go vet ./internal/pack/... ./cmd/apm/...                  PASS
go test ./internal/pack/... ./cmd/apm/... -count=1        PASS
```

Test package results:

```text
ok github.com/apm-go/apm/internal/pack
ok github.com/apm-go/apm/internal/pack/bundle
ok github.com/apm-go/apm/internal/pack/pluginmanifest
ok github.com/apm-go/apm/cmd/apm
```

Marketplace A/B regression:

```text
python D:/Projects/apm-dev/evals/ab_marketplace_pack.py
total: 14 passed, 0 failed
```

## Final assessment

The Phase 2–5 implementation at `f547b4b` satisfies all six requested Review Gate B checks. No `file:line` failure list follows because no product failure was found.
