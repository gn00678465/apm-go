# Research: MCP 憑證佔位 per-target 處理（bake vs translate）

對照 Python 原版 + 官方文件，逐項附證據。

## 問題現象（實測）

`install --mcp github --url … --header 'Authorization=Bearer ${MY_TOKEN}'` 後：
- **copilot** `.github/mcp-config.json`：`Bearer ${MY_TOKEN}`（保留佔位 ✅）
- **claude** `.mcp.json`：MY_TOKEN 未設→**警告+省略 header**；已設→**烘明碼 `Bearer <secret>`**
- **opencode** `opencode.json`：同上 bake
- **codex** `.codex/config.toml`：同上 bake

3 個 bake target 把 secret 烘進被 commit 的設定檔（外洩）或未設時丟 header。

## apm-go 現況（`internal/deploy`）

各 adapter `MCPResolveMode()`：
- claude=**Bake**（`mcp_claude.go:9`）、codex=**Bake**（`mcp_codex.go:11`）、
  opencode=**Bake**（`mcp_opencode.go:9`）、antigravity=Bake、**copilot=Translate**（唯一）
- `resolveMCPServer`（`mcp_common.go:39`）對 headers/env/url 統一套用同一 mode：
  bake → `ResolvePlaceholders` 從 `os.LookupEnv` 展開（`mcpresolve.go:103`），undefined header
  → 警告 + omit（`mcpresolve.go:135`）。**mode 是 per-server，headers 與 env 不分開。**

## 官方文件確認的變數語法

- **Claude Code `.mcp.json`**（https://code.claude.com/docs/zh-TW/mcp#environment-variable-expansion-in-mcp-json）：
  語法 `${VAR}` 與 `${VAR:-default}`；可用於 **command/args/env/url/headers**。
  範例 `"Authorization": "Bearer ${API_KEY}"`。
- **OpenCode `opencode.json`**（https://opencode.ai/docs/zh-tw/mcp-servers/）：
  語法 **`{env:VAR}`**（不同！）；可用於 **headers**/oauth/env。
  範例 `"Authorization": "Bearer {env:MY_API_KEY}"`。
- **Codex `config.toml`**（https://developers.openai.com/codex/mcp，使用者提供）：
  **不支援 `${VAR}` inline**。env-backed 憑證用專屬欄位（給 env var **名稱**，非佔位）：
  - `bearer_token_env_var = "MY_TOKEN"`（Authorization bearer token 專用）
  - `env_http_headers = { "X-Custom" = "ENV_VAR_NAME" }`（header 名→env var 名）
  - `http_headers = { "X-Region" = "us-east-1" }`（**只放靜態值**）
  → 因此把 `Bearer ${MY_TOKEN}` 寫進 `http_headers` 對 codex **可能無效**（送出字面字串）。

## 原版行為（關鍵：header 一律保留 `${VAR}`）

`src/apm_cli/adapters/client/base.py`：
- `_translate_env_placeholder`（L47）：**純文字**把 `${env:VAR}`/`<VAR>` → `${VAR}`，
  **「MUST NOT read os.environ、MUST NOT resolve 成 literal」（issue #1152 安全設計）**。
- `_resolve_variable_placeholders`（L587）：translate 模式全部改寫成 `${VAR}`；
  **legacy 模式「只解析 legacy `<VAR>`，`${VAR}`/`${env:VAR}` 一律不動（保留）」**。
- `_supports_runtime_env_substitution`（L165 預設 False）**只影響 `env` dict**
  （`_resolve_environment_variables` L410：True→保留佔位、False→烘 literal）。

各 adapter：
- **claude.py** L49 `_supports_runtime_env_substitution=False`（**只影響 env dict**；
  寫 `.mcp.json`，L141）。**header 走 `_resolve_variable_placeholders` → 保留 `${VAR}`**。
- **codex.py** 無覆寫（用預設 False）；header L281 走 `_resolve_variable_placeholders`
  → 保留 `${VAR}`（註解：Codex 執行期解析 `<VAR>`/`${VAR}`/`${env:VAR}`）。
- **opencode.py** L12 False；header 由 copilot-format `dict(headers)` 帶入。
- **copilot.py** L63 True（env 亦保留佔位）。

### 結論：真正的分歧在 **header**
- 原版 **header 對所有 adapter 保留 `${VAR}`**（不烘）；apm-go **header bake**（烘 literal）。
  → **claude/codex/opencode 的 header bake 都偏離原版 + 烘明碼 secret**。
- `env` dict：原版 claude/codex/opencode = bake（False）、copilot = 保留（True）。
  apm-go 同（bake）→ env dict 部分是 parity。

## 修正方向（待 design 定案）

- **claude**：header 保留 `${VAR}`（`.mcp.json` 原生支援；bake→translate）。
- **codex**：兩個選項（codex `http_headers` 只吃靜態值，見上）：
  - **A（parity）**：保留 `${VAR}` 在 `http_headers`（對齊原版；但 codex 執行期可能不解析）。
  - **B（正確）**：`Bearer ${VAR}` → `bearer_token_env_var="VAR"`；其他 header → `env_http_headers`。
    偏離原版、較複雜，但實際可用。
- **opencode**：header 需 **`{env:VAR}`**（bake→translate + 語法轉換 `${VAR}`→`{env:VAR}`）。
- **env dict 取捨**：apm-go mode 是 per-server，若整包 translate 會連 env 也保留 `${VAR}`
  （原版 env 是 bake）。兩個實作選項：
  - **A（簡單）**：整 server translate + opencode 語法轉換。env 也保留 `${VAR}`
    —— claude `.mcp.json` env 官方支援 `${VAR}`；codex/opencode env 需確認。
  - **B（精準 parity）**：拆開——header 一律 translate、env dict 維持 per-adapter bake/keep。
    最貼原版但改動較大（需改 `resolveMCPServer` 分欄位套 mode）。

## 待驗證（P0，實作前）
- codex `config.toml` 的 `http_headers` 是否真的支援 `${VAR}` runtime 代換（原版假設 yes）。
- opencode `{env:VAR}` 是否也適用於 `environment`（stdio env），或僅 headers。
- claude/codex/opencode 的 `env` dict 在保留 `${VAR}` 時是否可用（選項 A 的前提）。
