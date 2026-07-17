# PRD — 修復 git ext:: transport 供應鏈 RCE（C1）

> 安全 hotfix，獨立於 `feat/install-parity-bugfix`。分支 `fix/git-ext-transport-rce`（自 main 切出）。
> 觸發：2026-07-17 全專案安全審查（`ecc:security-reviewer` + 主會話定向 + codex 對抗），
> 確認一個 CRITICAL 供應鏈 RCE。

## 漏洞（CRITICAL，PoC 確認）

`internal/manifest/depref.go` 的 `{name: ...}` 依賴分支是**唯一未做 charset 驗證**的解析分支，
原封存 `RepoURL=kv["name"]`、Owner/Repo/Source 皆空 → `resolver` 判為 `KindGitLiteral`
→ `gitops.resolveCloneURL` 對 Owner/Repo 空的 ref **原值返回** → `exec.Command("git","clone", <值>, dir)`。

git 的 `protocol.ext.allow` 預設 `user`，直接 clone 會執行 `ext::` remote-helper。
惡意套件宣告 `- name: "ext::sh -c '<cmd>'"`，受害者只要 `apm-go install` 一個含此宣告的套件
（含**遞移**依賴），即在解析時執行任意命令，無需 `--force`/`--allow-insecure`、無需使用者輸入。

PoC：apm-go 確實把 `ext::sh -c '...'` 傳抵 `git clone`（Windows 因目錄名非法偶然未落地；
POSIX 字元合法 → 必中）。

## 修復需求（三層 + file 收窄）

1. **parse 層**：`{name:}` 走 `ParseDepString` 驗證為 git shorthand（owner/repo），拒非法字串。
2. **validateCloneURL**：clone 前拒 `::`（remote-helper）與 `-` 開頭（option 注入）。
3. **GIT_ALLOW_PROTOCOL**：`gitops.SecureGitEnv` 併入白名單（預設 `https:ssh:git`，**無 file**）
   + `GIT_PROTOCOL_FROM_USER=0`；所有 git 子行程走此 env（git 自身拒任何非白名單 transport）。
4. **file 收窄**：`file` 僅在 clone/ls-remote URL 為本機路徑時開放
   （`isLocalCloneURL` 保守鏡像 git：`://`、UNC 含 mixed-slash、SCP `[user@]host:path` 皆判遠端；
   Windows 磁碼例外僅 `GOOS=windows`）。gitops 與 marketplace 全 clone/ls-remote 統一經
   URL 感知 env（`ApplyCloneEnv`/`cloneCommandFor`）；加 `--` 分隔符；錯誤過 `SanitizeGitOutput`。

## 驗收（全部達成）

- [x] `{name: "ext::..."}` 於 parse 報錯（`TestParseDepDict_NameBranch_RejectsExtTransport`）。
- [x] `{name: "owner/repo"}` 仍正常（`..._AcceptsShorthand`）。
- [x] `LoadPackage` 直餵 ext:: payload（繞過 parse）→ 報錯且無 marker 檔
      （`TestLoadPackage_RejectsExtTransportRCE`，證明底層防禦獨立生效）。
- [x] `isLocalCloneURL` 拒 UNC/mixed-slash/SCP-without-user/backslash-SCP，磁碼僅 Windows
      （`TestIsLocalCloneURL`）。
- [x] `SecureGitEnv` 白名單不含 ext（`TestSecureGitEnv_RestrictsProtocols`）。
- [x] 合法 `git: ./path` 本機 clone 仍運作（實跑驗證）。
- [x] 原始 PoC 對修復後二進位：parse 報錯、無 marker（實跑驗證）。
- [x] `go build/vet`、`go test ./... -count=1` 全綠。
- [x] codex 對抗閘門 5 輪至無 CRITICAL/HIGH。

## 附帶關閉

- **H1**（`git: ./path` 本機分支同型注入）：由 validateCloneURL + GIT_ALLOW_PROTOCOL 一併覆蓋。

## HIGH-B（已評估並封閉可注入切片，commit f583183）

原記為「架構性殘留、延後」。實際追程式碼後，威脅模型如下：

- **Marketplace source（won't-fix，結構上不可注入）**：`resolve_plugin.go` mkt-022
  只查 `~/.apm/marketplaces.json`（使用者親自 `marketplace add`），**絕不**讀專案/相依
  apm.yml 的 `marketplace:` 區塊。故一個本機路徑 marketplace source 只可能來自使用者
  自己的註冊，file transport 指向使用者自己的檔案系統——非漏洞。
- **依賴安裝路徑（實際可注入切片，已修）**：resolver 會遞迴進遞移相依且原本未拒本機 dep。
  遞移套件 apm.yml 宣告 `git: <本機路徑>`（經 depref 設 IsLocal=false/Source=git，
  resolveCloneURL 原值返回）→ file transport clone。去毒事實：`git: ../x`（相對逃逸）
  已被 `containsEscape` 擋、`git: file://` 於 parseShorthand 即拒、clone 無
  `--recurse-submodules`（submodule `.gitmodules file://` 放大不適用）、內容落在受害者
  自己的 apm_modules 不回傳攻擊者。實質 MEDIUM。
- **修法（非架構變更）**：resolver 子相依迴圈（depth≥2）以 `isLocalFilesystemDep` 拒絕
  本機 dep，判定式採 resolveCloneURL 同一 local 不變式。原以為需要的「parse 層
  trusted-local 型別系統」是 overkill——depth-based guard 即封閉可注入切片。
- 驗收：遞移 git-local/path 拒、root local 允許、真實 parser 的 SCP/https/shorthand
  遠端不誤判、解析層 file:// 拒。codex 對抗閘門 2 輪至無 CRITICAL/HIGH。

## 其他安全審查發現（另立任務，非本 hotfix）

同次審查另發現（詳見 `research/security-review.md`）：
- **C2/MEDIUM**：deploy `copyDirRecursive`（adapter.go）跟隨 symlink（任意檔案讀取）——與已防護的
  plugin.json/codex_agent 路徑不對稱。修：加 `os.Lstat` 拒 symlink。
- **H2**：registry/marketplace github/gitlab HTTP body 無 `io.LimitReader` 上限（記憶體 DoS）。
- **M1**：mcp registry JSON decode 無 cap。
- **M2**：self-defined MCP `--header` 明文寫 apm.yml（無警告）。
- **M3**：YAML 無巢狀深度上限（alias 已擋，低風險）。
