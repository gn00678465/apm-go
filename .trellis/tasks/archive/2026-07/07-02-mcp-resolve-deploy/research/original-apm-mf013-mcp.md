# Original apm CLI — mf-013 placeholder resolution + MCP deploy oracle

Research for task `07-02-mcp-resolve-deploy`. Behavioral oracle extracted from the
original Microsoft **apm** CLI source. Read-only research; no project source modified.

All `file:line` citations below refer to the unpacked sdist under the scratchpad
(`.../scratchpad/apm-src/apm_cli-0.21.0/src/apm_cli/...`). Paths are given relative to
`src/apm_cli/`.

---

## 1. Source analyzed

- **Package:** `apm-cli` **0.21.0** (import package `apm_cli`), the exact version the
  task pins for A/B comparison.
- **How obtained:** `pip download apm-cli==0.21.0 --no-deps --no-binary :all:` → sdist
  `apm_cli-0.21.0.tar.gz` (1.3 MB), unpacked with `tar -xzf`. Full Python source
  present under `src/apm_cli/`. No decompilation needed.
- **Relevant subsystems:**
  - `install/mcp/` — the `apm install --mcp <pkg>` *manifest writer* (adds a server to
    `apm.yml`; NOT the target-config writer). Only writer.py is relevant to precedence.
  - `integration/mcp_integrator.py` + `integration/mcp_integrator_install.py` — the
    `apm install` **deploy orchestrator** (collect transitive MCP deps, dedup, per-target
    dispatch, stale cleanup, lockfile reconcile).
  - `adapters/client/*.py` — the **per-target config writers + placeholder resolution**.
    Both `apm mcp install` and `apm install` deploy converge here (see §5).

### Adapter class hierarchy (critical — behavior is inherited)

```
MCPClientAdapter (base.py)              # defines resolution + the dispatch flag
└── CopilotClientAdapter (copilot.py)   # _supports_runtime_env_substitution = True
    ├── ClaudeClientAdapter (claude.py) # OVERRIDES flag -> False
    └── GeminiClientAdapter (gemini.py) # OVERRIDES flag -> False
        └── AntigravityClientAdapter (antigravity.py)  # inherits Gemini (flag=False)
```
Codex is a separate subclass. `claude`/`gemini`/`antigravity` all subclass Copilot but
**turn the runtime-substitution flag back OFF**, so they use legacy install-time
resolution, not Copilot's verbatim-placeholder behavior.

---

## 2. Placeholder resolution semantics

### The single dispatch flag

`MCPClientAdapter._supports_runtime_env_substitution: bool = False` — `base.py:165`.
Its docstring (`base.py:160-165`) states the contract:

- **`True` = "translate mode"** (Copilot CLI, IntelliJ, Kiro): placeholders emitted
  **verbatim** as `${VAR}`; the target runtime resolves them at server-start. Even
  *literal* env values authored in `apm.yml` are rewritten to `${NAME}` placeholders so
  secrets never touch disk (`base.py:454-458`, issue #1152).
- **`False` = "legacy mode"** (claude, codex, gemini, antigravity, cursor, opencode,
  windsurf, hermes): placeholders **resolved to literal values at install time** via
  `env_overrides → os.environ → optional interactive prompt` (`base.py:160-165, 410-585`).

Per-adapter flag values (grep `_supports_runtime_env_substitution`):
`base=False`, `claude.py:49=False`, `copilot.py:63=True`, `gemini.py:62=False`,
`antigravity`=inherited False, `codex`=inherited base False, `cursor=False`,
`intellij.py:83=True`, `kiro.py:33=True`, `opencode=False`, `windsurf=False`, `hermes=False`.

### The regexes (`base.py:12-30`)

```
_INPUT_VAR_RE       = \$\{input:([^}]+)\}
_ENV_VAR_RE         = \$\{(?:env:)?([A-Za-z_][A-Za-z0-9_]*)\}
_ENV_PLACEHOLDER_RE = <([A-Z_][A-Z0-9_]*)>| + _ENV_VAR_RE   # adds legacy <VAR>
_LEGACY_ANGLE_VAR_RE= <([A-Z_][A-Z0-9_]*)>
```
There is **no dedicated GitHub-Actions `${{...}}` regex**. `${{...}}` is preserved purely
because `_ENV_VAR_RE` cannot match it: after `${` the next char is `{`, which is neither
`env:` nor a valid var-name start `[A-Za-z_]`, so no match at any offset → left verbatim.
`${input:...}` is likewise excluded from `_ENV_VAR_RE` (the `:` and `input:` prefix don't
satisfy `(?:env:)?[A-Za-z_]...`), so it is never resolved by the env path — it only
triggers a *warning* (see below).

### Semantics table — behavior at NON-INTERACTIVE install (`_should_skip_env_prompts` True: no TTY, or env_overrides pre-supplied, or `APM_E2E_TESTS=1`; `base.py:393-408`)

| Placeholder form | Source | Undefined behavior | Install-time behavior | Citation |
|---|---|---|---|---|
| `${VAR}` / `${env:VAR}` in **env dict** (self-defined stdio `env:`), **legacy** adapters | `env_overrides` → `os.environ` | **left literal `${VAR}`** (returns `match.group(0)`) | resolved to literal if defined; else the raw placeholder text is written to disk unchanged | `base.py:493-504, 543-585` (`_resolve_env_variable._replace` line 583 returns `match.group(0)`) |
| `${VAR}` / `${env:VAR}` in **registry env schema list**, **legacy** adapters | `env_overrides` → `os.environ` | **var OMITTED** from output (unless `_DEFAULT_GITHUB_ENV`) | resolved to literal if defined; if empty/undefined and not required or skip-prompting → key not written | `base.py:506-541` (line 534 `if value and value.strip()`) |
| `${VAR}` / `${env:VAR}` in **args**, **legacy** adapters | — | **left verbatim** | args resolver in legacy mode resolves ONLY legacy `<VAR>`; `${VAR}`/`${env:VAR}` are preserved untouched even when defined | `base.py:587-634` (lines 615-621) |
| `${VAR}` / `${env:VAR}` (any position), **translate** adapters (copilot/intellij/kiro) | not read at install | placeholder still emitted; unset vars **warned**, never baked | `${env:VAR}`→`${VAR}`, `<VAR>`→`${VAR}`, literals→`${NAME}`; verbatim to disk | `base.py:430-478, 558-564`; `_translate_env_placeholder` `base.py:47-76`; unset-warn `copilot.py:286-304, 340-352` |
| `${input:<id>}` (env or headers), **all non-VSCode** adapters | never resolved | **left literal `${input:<id>}`** + a **warning** is emitted per referenced id | passthrough (regex excludes it); adapters that can't prompt call `_warn_input_variables` → `"${input:X} in server '..' will not be resolved -- <runtime> does not support input variable prompts"` | warn fn `base.py:344-372`; call sites: `codex.py:212,276`, `gemini.py:158,201`, `cursor.py:143`, `kiro.py:125,159`, `copilot.py:433`, base args `base.py:792`. **No `raise`, no interactive prompt at install.** |
| `${input:<id>}`, **VS Code** | VS Code runtime | preserved | left `${input:X}` verbatim (VS Code natively prompts); no warning | `vscode.py:519` |
| `${{ ... }}` (GitHub Actions) | — | preserved | **verbatim** — no regex matches it (see mechanism above); passes through every resolver unchanged | `base.py:19` (`_ENV_VAR_RE` shape), `base.py:61-63` ("`${VAR:-default}`/`$VAR`/`${input:...}` passthrough") |
| legacy `<VAR>` | `env_overrides`→`os.environ`→prompt (legacy); →`${VAR}` (translate) | resolved to literal, else left `<VAR>`; deprecation warned once per run | resolved at install (legacy) or migrated to `${VAR}` (translate) | `base.py:575-585, 618-621`; deprecation aggregation `base.py:79-88` |

### "No silent literal passthrough" for unsupported placeholders

The original does **NOT hard-refuse** to write for legacy adapters. Its posture:
- `${input:<id>}` on a non-prompting target → **left as literal AND a rich warning is
  emitted** (`_warn_input_variables`, `base.py:344-372`). This is the "diagnostic, may
  refuse" behavior — it warns, still writes the literal.
- VS Code alone *raises* on an unresolvable form (bare `$VAR`): `vscode.py:~540-553`
  (`"cannot resolve. Use ${VAR} or ${env:VAR} instead ..."`). No other target raises.
- `${{...}}` is preserved **silently** (no diagnostic) — treated as intentional.

> **Interpretation for req-mf-013:** the original's actual "unsupported → not silently
> literal" mechanism is a **warning at deploy time** (for `${input:}`), not a parse-time
> rejection and not a refusal to write. apm-go may legitimately choose a stricter
> "refuse to write" posture, but the original oracle is *warn + still write literal*.

---

## 3. Per-target dispatch matrix

"Resolve-at-install?" = does the target bake literal values into the config at
`apm install` time (legacy mode) vs. emit placeholders for the runtime to resolve
(translate mode)?

| Target | Resolve at install? | MCP config file (project scope) | Top-level key | HTTP/SSE field(s) | Notes / citations |
|---|---|---|---|---|---|
| **claude** | **Yes (legacy)** but see caveat | `<root>/.mcp.json` (opt-in: `.claude/` must exist); user scope `~/.claude.json` | `mcpServers` | remote: `type` + `url` + `headers`; stdio: `type:"stdio"` + `command`/`args`/`env` | flag=False `claude.py:49`; paths `claude.py:140-152`; shape `claude.py:73-106` |
| **codex** | **Yes (legacy)** | `<root>/.codex/config.toml`; user `~/.codex/config.toml` | `mcp_servers` (TOML) | remote: `url` + `id` + `http_headers`; **SSE skipped** (warns), **non-https skipped** | flag=inherited False; `codex.py:29,55-64,230-278` |
| **copilot** | **No (translate)** | `~/.copilot/mcp-config.json` (user path) | `mcpServers` | remote: `type:"http"` + `url` | flag=True `copilot.py:63`; remote `copilot.py:471-476`; stale path `mcp_integrator.py:626` |
| **antigravity** | **Yes (legacy)** | `<root>/.agents/mcp_config.json` (opt-in: `.agents/` must exist); user `~/.gemini/config/mcp_config.json` | `mcpServers` | inherits Gemini: **`httpUrl`** (http/streamable-http) or **`url`** (sse) + `headers` | flag=inherited False; `antigravity.py:33-54`; shape via `gemini.py:170-206` |
| **gemini** | **Yes (legacy)** | `<root>/.gemini/settings.json` (opt-in); user `~/.gemini/settings.json` | `mcpServers` | **`httpUrl`** (http/streamable-http) or **`url`** (sse) + `headers` | flag=False `gemini.py:62`; shape `gemini.py:122-206` |
| (contrast) vscode | No (preserve) | `.vscode/mcp.json` | **`servers`** | translates `${VAR}`→`${env:VAR}`; preserves `${input:}` | `mcp_integrator.py:615-622`; `vscode.py:503-528` |
| (contrast) intellij / kiro | No (translate) | intellij `~/.../mcp.json` key `servers`; kiro scope-resolved | `servers` / scope | flags True | `intellij.py:83`, `kiro.py:33` |

**Key correction vs. the apm-go checklist:** antigravity being "no runtime interpolation"
is TRUE, and it *is* the legacy/resolve-at-install branch — but legacy mode does **not
fully resolve everything**. In legacy mode, an **undefined** `${VAR}` in an env value is
left as the literal `${VAR}` on disk (`base.py:583`), and `${VAR}` inside **args** is left
verbatim regardless (`base.py:615-621`). So antigravity/gemini can still ship an
unresolved `${VAR}` to disk that their runtime will not interpolate — a real gap the
original does not close (it only warns for `${input:}`, not for undefined `${VAR}`).

**Opt-in gating:** project-scope writes for claude/gemini/antigravity/opencode/codex are
**silently skipped** unless the target's signal directory exists (`.claude/`, `.gemini/`,
`.agents/`, `.opencode/`, `.codex/`). Target selection itself is gated by
`_gate_project_scoped_runtimes` against `--target` > `targets:` field > directory signals,
**failing closed** (no MCP writes) on a malformed `targets:` field
(`mcp_integrator.py:976-1077`).

---

## 4. MCP writer — per-server JSON/TOML shapes (deploy path)

Self-defined dep → synthetic `server_info` via `_build_self_defined_info`
(`mcp_integrator.py:290-353`): stdio deps become `_raw_stdio {command,args,env}`;
http/sse/streamable-http deps become `remotes:[{transport_type,url,headers}]`.

**Gemini / Antigravity** (`gemini.py:122-258`) — file `mcpServers` map, entry examples:
```jsonc
// stdio (self-defined)
"my-server": { "command": "npx", "args": ["-y","pkg","--flag"], "env": { "TOKEN": "<resolved-literal>" } }
// streamable-http / http
"remote": { "httpUrl": "https://api.example/mcp", "headers": { "Authorization": "<resolved>" } }
// sse
"remote": { "url": "https://api.example/sse" }
```
**Claude** (`claude.py:73-106`) — `mcpServers` map:
```jsonc
"my-server": { "type": "stdio", "command": "npx", "args": [...], "env": {...} }   // stdio
"remote":    { "type": "http",  "url": "https://...", "headers": {...} }           // remote
```
**Copilot** (`copilot.py:405-476`) — `mcpServers` map, **placeholders verbatim**:
```jsonc
"my-server": { "type": "local", "command": "npx", "args": [...], "env": { "TOKEN": "${TOKEN}" } }
"remote":    { "type": "http",  "url": "https://..." }
```
**Codex** (`codex.py:230-360`) — TOML `[mcp_servers.<name>]`:
```toml
[mcp_servers.remote]     # remote (must be https, not sse)
url = "https://api.example/mcp"
id  = "..."
[mcp_servers.remote.http_headers]
Authorization = "<resolved>"
```

> **`serverUrl` does NOT exist** anywhere in apm-cli 0.21.0 (grep of `src/` + `tests/`
> for `serverUrl` / `"serverUrl"` = zero hits). The task brief's "HTTP servers use field
> `serverUrl`" does not match the original. Real HTTP field names by target: gemini/
> antigravity `httpUrl`|`url`; claude `url`(+`type`); copilot `url`(+`type:http`); codex
> `url`. If apm-go emits `serverUrl` it diverges from every original target.

**Merge vs overwrite (Claude, representative):** reads existing file, **shallow-merges
per server** `{**old, **new}` (new keys win on conflict, foreign keys like OAuth blocks
survive), preserves all other top-level keys, then normalizes each touched entry
(`claude.py:108-135, 166-211`). Gemini merges into existing `mcpServers`, preserving other
top-level keys, and `chmod 0o600` (`gemini.py:74-108`). Stale servers no longer in the
manifest are removed by `remove_stale` per target (`mcp_integrator.py:538-767`).

---

## 5. Precedence / override for MCP

- **Assembly (root-first):** `mcp_deps = get_all_mcp_dependencies()` (root prod+dev) then
  `MCPIntegrator.deduplicate(mcp_deps + transitive_mcp)` — root list is prepended before
  transitive (`commands/install.py:1675, 1849-1863`).
- **Dedup = first-declared-wins by name:** `deduplicate_deps` keeps the first occurrence of
  each `name` (`integration/_shared.py:14-37`). Because root is first, **root/local
  declarations override dependency-declared servers of the same name** (= apm-go
  req-pr-002 local-over-dependency), and among deps first-declared wins (req-pr-003).
- **Overlay (root refines a registry server):** `_apply_overlay` lets a dep override
  transport/package/headers/args/tools/env on a cached `server_info`
  (`mcp_integrator.py:355-424`); `version` overlay is warned as not-yet-applied.
- **Transitive trust:** self-defined (`registry: false`) servers from **direct** deps
  (depth==1) are auto-trusted; from **transitive** deps (depth>1) they are **skipped with
  a warning** unless `--trust-transitive-mcp` (`mcp_integrator.py:205-271`).
- **Manifest writer precedence** (`apm install --mcp`, separate path): existing entry +
  non-TTY → `click.UsageError` exit 2; +`--force` → replace; +TTY → prompt
  (`install/mcp/writer.py:96-127`).

---

## 6. Delta vs apm-go current code (what must be added)

apm-go today (read): `internal/manifest/mcp.go` recognizes placeholders only; the regexes
`EnvVarRe` / `InputVarRe` are **byte-identical** to the original's `_ENV_VAR_RE` /
`_INPUT_VAR_RE`. `ActionsRe = \$\{\{.*?\}\}` is an apm-go addition (original has none;
original relies on `_ENV_VAR_RE` naturally not matching `${{...}}`). Recognition semantics
therefore already align; **resolution + deploy are entirely absent**.

Gaps to close:

1. **Store parsed MCP on Manifest.** `internal/manifest/manifest.go:355-367` parses `mcp:`
   then discards it. Must persist the `[]MCPDependency` (prod + dev) on the Manifest so
   the resolver/deployer can consume it.
2. **Per-target resolve-vs-preserve dispatch (the flag).** Implement the equivalent of
   `_supports_runtime_env_substitution`: **resolve-at-install (bake)** for claude, codex,
   gemini, antigravity, cursor, opencode, windsurf; **preserve-verbatim** for copilot,
   intellij, kiro. Match the *branch-specific* rules: env-dict undefined → keep literal
   `${VAR}`; registry-list undefined → omit; args → only legacy `<VAR>` resolved, `${VAR}`
   left verbatim. (Or consciously diverge — but document it.)
3. **`${input:<id>}` handling.** Original = leave literal + emit a warning on non-prompt
   targets; never prompts at install, never raises. apm-go's mf-013 "no silent literal"
   can be satisfied by the same warning (or a stricter refuse-to-write, a deliberate
   divergence to flag).
4. **Per-target MCP writers.** `internal/deploy/` deploys instructions/agents/skills/
   commands/hooks/prompts but **no mcp**. Add writers with correct paths + keys + HTTP
   field names from §3/§4. **Do not use `serverUrl`** — use `httpUrl`/`url`/`type` per
   target. Respect opt-in dir gating and per-server shallow-merge / preserve-foreign-keys.
5. **Precedence.** Reuse the file-primitive override model (`internal/deploy/conflict.go`
   pr-002/003): assemble root-first, dedup by name first-wins; transitive self-defined
   servers gated behind a trust flag.
6. **`${{...}}` verbatim.** Ensure the resolver never rewrites Actions expressions — the
   existing `ActionsRe` recognition plus "don't touch matched Actions spans" is enough;
   confirm the env resolver's regex (already matching the original) skips them.

---

## 7. Open questions / low-confidence items

- **Codex `user_scope` support** — not individually verified here (LOW CONFIDENCE); does
  not affect the resolution oracle.
- **Exact `_process_arguments` / docker `-e` injection for codex/gemini stdio args** — read
  at a high level (`gemini.py:230-255`, `codex.py:300-360`); the precise arg ordering for
  docker packages was not exhaustively traced (MEDIUM CONFIDENCE) but is orthogonal to
  mf-013 placeholder semantics.
- **Whether apm-go intends the original's *warn-and-write-literal* or a stricter
  *refuse-to-write*** for `${input:}` — this is a product decision, not discoverable from
  the original (which warns and writes). Flagged for the PRD.
- **`serverUrl`** — the task brief names it; the original never emits it. Confirm with the
  apm-go team whether `serverUrl` is an intentional apm-go convention (e.g. matching a
  newer Antigravity schema) before writing acceptance criteria against it. **HIGH-IMPACT
  divergence.**
