# Research: install --mcp parity 缺口（已實測驗證）

對照基準：microsoft/apm（Python 原版）。以下每項都有原始碼 / registry 回應 /
apm-go 端實測佐證。

## #1 apm.yml `mcp:` 寫成 flow style — 真差異，根因確定

- **現象**：`apm-go install --mcp io.github.github/github-mcp-server` 後 apm.yml 得到
  `mcp: [io.github.github/github-mcp-server]`（flow）；原版是 block：
  ```yaml
  mcp:
    - io.github.github/github-mcp-server
  ```
- **根因**：`cmd/apm/init.go:184` `buildManifestData` 寫 `"mcp": []any{}`，go-yaml 把
  空序列 render 成 flow（`Style=32`）。`apm install --mcp` 重讀時該 SequenceNode
  **繼承 flow style**，`upsertMCPEntry`（`cmd/apm/mcpinstall.go:418`）append 後
  重新 dump 仍 flow。
- **實測**（專案 module 內跑 go-yaml v4）：把該 seq 節點 `Style` 清掉 FlowStyle bit
  後，輸出即為 block style，與原版一致。
- **修法**：`upsertMCPEntry` 在 add/replace 分支清掉 `mcpSeq.Style` 的 FlowStyle。
  範圍僅 `dependencies.mcp` 序列；不動其他節點。

## #2 registry MCP server 不互動詢問 token — 真差異（缺功能）

### 原版行為（`src/apm_cli/registry/operations.py`）
- `collect_environment_variables`：掃 `packages[].environmentVariables[]`。
- `collect_runtime_variables`：掃 `packages[].runtimeArguments[].variables`。
- 兩者彙整出 required 變數後呼叫 `_prompt_for_environment_variables`：
  - 已在環境變數者顯示 `[+] NAME: using existing value` 直接沿用。
  - 非 required 且無值者跳過。
  - required 者互動詢問；名稱含 password/secret/key/token/api → 隱藏輸入。
  - **CI/E2E 跳過詢問**：`APM_E2E_TESTS` 或 `CI/GITHUB_ACTIONS/TRAVIS/JENKINS_URL/
    BUILDKITE` 存在時改用預設（github 特例走 `GitHubTokenManager`）。
- 使用者終端輸出（原版）：
  ```
  +- MCP Servers (1)
  [>] Looking up 1 MCP server in registry...
  Environment variables needed:
    token:
  ```

### `token` 的真正來源（registry 實測 GET）
`GET https://api.mcp.github.com/v0.1/servers/io.github.github%2Fgithub-mcp-server/versions/latest`
- **remotes[0]**（apm-go 唯一會部署的端點）：
  ```json
  { "type": "streamable-http", "url": "https://api.githubcopilot.com/mcp/",
    "headers": [ { "name": "Authorization", "isSecret": true,
      "description": "Authorization header with authentication token (PAT or App token)" } ] }
  ```
  → header **只有 name（無 `value` 模板、無 `variables`）**。
- **packages[0]**（OCI `ghcr.io/github/github-mcp-server:1.5.0`，stdio/docker 路徑）：
  ```json
  "runtimeArguments": [ ...,
    { "name": "-e", "value": "GITHUB_PERSONAL_ACCESS_TOKEN={token}",
      "variables": { "token": { "format": "string", "isSecret": true } } } ]
  ```
  → 原版 `token:` prompt 來自**這裡**（OCI 的 runtimeArguments.variables）。

### 核心分歧（設計必須定案）
- 原版 `token` 來自 **OCI/stdio** package 的 runtimeArguments，最終用途是 docker 的
  `GITHUB_PERSONAL_ACCESS_TOKEN` 環境變數。
- apm-go **只部署 remote**（`ResolveDeployable` 取 `info.Remotes[0]`，明確不支援
  package/stdio），remote 需要的是 `Authorization` header，且該 header 在 registry
  上**沒有可 prompt 的 variable**（只有 name+isSecret+description）。
- apm-go 端現況：`internal/mcpregistry/client.go` 的 `rawRemoteHeader` 只解析 `name`，
  丟掉 header 的 `value`/`variables`；`resolveFromRegistry`（`mcpinstall.go:322`）只把
  header 名稱當診斷印出，不 prompt、不注入。

## #3 `--header requires --url` — 非差異，apm-go 與原版一致

- 原版 `src/apm_cli/install/mcp/conflicts.py` **E9**：
  `if headers and not url: raise click.UsageError("--header requires --url")`。
- apm-go `cmd/apm/mcpinstall.go:180` 完全對應。
- 結論：原版同樣不允許對 registry server 用 `--header` 帶 token；registry server
  帶 token 的正途是 #2 的互動詢問。#3 **不需改碼**，補 #2 即涵蓋此需求。

## 安全性註記（#2 持久化）
- apm.yml 會被 commit；把 prompt 到的 secret 直接寫入 apm.yml 明碼有外洩風險。
- 原版文件範例用 `${ENV}` 佔位：`Authorization: "Bearer ${LINEAR_TOKEN}"`。
- 既有 apm-go 診斷字串（`resolveFromRegistry`）已教使用者 `--header KEY=VALUE`。
- 持久化策略（明碼 vs `${ENV}` 佔位 vs 只部署不寫 apm.yml）須於 design.md 定案。
