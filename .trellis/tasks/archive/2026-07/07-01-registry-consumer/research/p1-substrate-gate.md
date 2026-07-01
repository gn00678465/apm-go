# P1 gate — local conformant-server A/B substrate is viable (VERIFIED)

Decision **B**: A/B both clients (apm-go + original apm-cli) against a local server
implementing the Registry HTTP API §3.1/§3.2. Advisor flagged the gate: prove the
original apm-cli can run headless against a local http registry, else B collapses to
single-sided. **Verified empirically 2026-07-01.**

## Substrate
- Minimal stdlib server (`scratchpad/reg-probe/server.py`): serves
  `GET /v1/packages/acme/sample/versions` and `.../1.0.0/download`, builds the
  fixture tar.gz in-memory (deterministic mtime=0) so advertised digest == served
  bytes, records `Authorization`/`Accept` per request to `requests.log`.
- Fixture = flat APM archive: `apm.yml` (name+version) + `.apm/skills/probe/SKILL.md`.
- Original client: `abtest-venv` apm.exe **0.21.0**.

## Result — original apm-cli 0.21.0 completes full registry install headless
```
apm experimental enable registries      # flag persisted, headless OK
apm install --target claude             # http://127.0.0.1:7777
  /versions -> pick 1.0.0 -> /download -> verify sha256 -> deploy -> lockfile v2
```
Lockfile written: `source: registry`, `resolved_url: http://127.0.0.1:7777/.../download`,
`resolved_hash: sha256:8aca0401…` == server digest == sha256(archive). Byte-exact.

## Credsec observability confirmed on the real download path (server-side)
| env | `/download` Authorization recorded |
|---|---|
| (none) | `<none>` — anonymous first |
| `APM_REGISTRY_TOKEN_LOCAL=probe-secret-token-abc123` | `Bearer probe-secret-token-abc123` |

Registry name `local` → `APM_REGISTRY_TOKEN_LOCAL` (uppercase, `-`/`.`→`_`). Bearer on
both `/versions` and `/download`. Anonymous-first, then tokened — matches spec §2.

## Gotchas found
- **Port 8799 bind failed (WinError 10013)** — inside reserved range 8734–8833
  (`netsh interface ipv4 show excludedportrange protocol=tcp`). Use a port outside
  all reserved ranges (7777 worked). Design's test harness must pick a free port
  (bind :0 and read back, or avoid reserved ranges).
- Server bind needs sandbox disabled in this harness; client connect is fine sandboxed.

## Consequence for design
- B is valid: original apm-cli is a real external oracle. ACs = server-side header
  assertions + resolved_hash parity, both clients same server.
- Reuse `server.py` + fixture as the seed for the Go test harness (build server to
  §9 fixtures, validate, THEN run both clients — no echo chamber).
