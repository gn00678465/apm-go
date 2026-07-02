# Design — 修復 Phase 0-5 驗證確認的 FAIL/MISSING 缺口

三組工作彼此獨立，分別設計。

## A. `apm update` 指令

### A.1 CLI 骨架

新增 `cmd/apm/update.go`，仿 `install.go` 的 `installCmd()`/`runInstall()` 結構：

```go
func updateCmd() *cobra.Command {
    var frozen bool
    cmd := &cobra.Command{
        Use:   "update [package]",
        Short: "Re-resolve dependencies to their newest matching version",
        Args:  cobra.MaximumNArgs(1),
        RunE: func(cmd *cobra.Command, args []string) error {
            deps := &installDeps{ tags: &gitops.RealTagLister{}, loader: &gitops.RealPackageLoader{ModulesDir: "apm_modules"} }
            pkg := ""
            if len(args) == 1 { pkg = args[0] }
            return runUpdate(deps, frozen, pkg)
        },
    }
    cmd.Flags().BoolVar(&frozen, "frozen", false, "refuse to update a frozen install without this override")
    return cmd
}
```

`--frozen` 語意在此指令**反過來**：預設（無旗標）視為「非 frozen，允許 update」；只有當現有 lockfile 本身處於 frozen 語境（例如 CI 環境或使用者主動加 `--frozen`）時才需要 override——沿用 `PlanScopedUpdate(..., frozen bool)` 現成參數，`runUpdate` 直接把 `frozen` 傳進去，`req-rs-012` 的「無 override flag 時拒絕」由 `PlanScopedUpdate` 已有的錯誤路徑處理，不必重新設計。

在 `cmd/apm/main.go` 加一行 `root.AddCommand(updateCmd())`。

### A.2 `runUpdate` 流程

```
1. 讀 apm.yml（缺檔案直接報錯，update 不支援 frozen-only-no-manifest 模式）
2. 讀既有 apm.lock.yaml（不存在則報錯：update 需要既有 lockfile 才有東西可更新）
3. 依 pkg 是否為空，呼叫 resolver.PlanFullUpdate 或 resolver.PlanScopedUpdate
4. 對 result.Deps 中每個 git-semver 且屬於「本次更新範圍」的 direct dep：
   - 更新範圍定義：PlanFullUpdate → 全部 direct deps；PlanScopedUpdate(pkg) → 只有 pkg 本身（其子樹全部重新走 resolve 但下載沿用一般 install 的「不存在才下載」即可，只有明確點名的 direct dep 才強制清除重下，避免整棵子樹沒必要的全部重新 clone）
   - os.RemoveAll(apm_modules/<key>) 後才進入下載步驟（req-lk-010）
5. 其餘流程重用 install.go 的「Build lockfile」「Deploy primitives」「No-op check」「Write lockfile」邏輯
   —— 抽出成 cmd/apm/install.go 的共用函式（例如 buildLockfile(result, existingLock, ...)、deployAndWriteLockfile(...)），
      install.go 與 update.go 都呼叫，避免複製貼上兩份幾乎相同的邏輯
6. 印出「更新了哪些套件、從哪個版本到哪個版本」摘要（比對 old lock vs new lock 的 ResolvedTag）
```

### A.3 重構 install.go 抽出共用邏輯

`runInstall` 目前把「build lockfile fields → deploy → no-op check → write lockfile」整段寫在函式內。為讓 `runUpdate` 重用同一段邏輯而不複製貼上，抽出：

```go
// buildLockDeps: result.Deps -> []lockfile.LockedDep（含 tree_sha256/resolved_commit 計算）
func buildLockDeps(result *resolver.ResolutionResult, regLoader *registry.Loader, skillSubset []string, packages []string) ([]lockfile.LockedDep, error)

// deployAndFinalize: 跑 deploy.Run + 印摘要 + no-op 比對 + 寫 lockfile + （選擇性）更新 apm.yml
func deployAndFinalize(m *manifest.Manifest, targetFlag string, skillSubset []string, newLock *lockfile.Lockfile, existingLock *lockfile.Lockfile, existingNode *yaml.Node, node *yaml.Node, packages []string) error
```

這是本任務**唯一**需要動到 `install.go` 既有邏輯的地方（抽函式，不改變 `runInstall` 的可觀察行為）——抽出後必須跑一次既有 `cmd/apm/install_test.go`/`cmd/apm/mcp_e2e_test.go` 全數通過，證明抽取沒有引入回歸。

### A.4 實際落地版本（與 A.1-A.3 草案的差異，含 2 輪 codex review 修正）

**抽取後的實際簽章**（比草案更精簡：拆成「建 lockfile 物件」與「deploy+寫檔」兩段，而非單一 `deployAndFinalize` 吃 `newLock` 之外還要吃一堆零散欄位）：

```go
// cmd/apm/install.go
func buildLockfile(result *resolver.ResolutionResult, existingLock *lockfile.Lockfile, regLoader *registry.Loader, skillSubset, packages []string, noProvenance bool) (*lockfile.Lockfile, error)

func deployAndFinalize(m *manifest.Manifest, targetFlag string, skillSubset, packages []string, result *resolver.ResolutionResult, newLock, existingLock *lockfile.Lockfile, existingNode, node *yamllib.Node) error
```

`runInstall` 尾端簡化為：
```go
newLock, err := buildLockfile(result, existingLock, regLoader, skillSubset, packages, noProvenance)
if err != nil { return err }
return deployAndFinalize(m, targetFlag, skillSubset, packages, result, newLock, existingLock, existingNode, node)
```

`deployAndFinalize` 內部重新呼叫一次 `deploy.ResolveTargets(targetFlag, m.Target, ".")`（而非把 `runInstall` 前面已算過的 `targets`/`targetDiags` 當參數傳入）——多一次純檔案系統訊號偵測的成本可忽略，換取兩個函式簽章不需要多帶兩個參數，維持乾淨的抽取邊界。

`runUpdate`（`cmd/apm/update.go`）直接重用同一對函式：`resolver.PlanFullUpdate`/`PlanScopedUpdate` 取代 `resolver.Resolve` 的呼叫點，其餘（build lockfile → deploy → 寫檔）與 `runInstall` 完全共用，無需重複實作。

### A.5 `--frozen`/`--no-frozen` 旗標設計（第 1 輪 codex review 發現原設計有漏洞後修正）

原設計草稿（A.1）只有單一 `--frozen` 旗標，語意「反過來」。實作後 codex review 指出兩個問題：

1. **frozen 拒絕發生在清除 apm_modules 之後**——`runUpdate` 原本是「先清 apm_modules → 再呼叫 PlanScopedUpdate → PlanScopedUpdate 才檢查 frozen 並拒絕」，導致「被拒絕」的 scoped update 仍然刪除了使用者的 apm_modules 內容，違反 req-rs-012「拒絕操作」的字面意義（拒絕不該有副作用）。**修正**：`runUpdate` 最前面（任何檔案 I/O 之前）就做 `if pkg != "" && frozen { return error }` 早退。
2. **CI 環境自動觸發 frozen 時沒有 override 手段**——`internal/resolver/update.go`（既有、非本任務新寫）的錯誤訊息本身就寫「使用 --no-frozen 覆寫」，但 CLI 端從未真的定義過這個旗標。**修正**：新增 `--no-frozen` bool 旗標，與 `--frozen` 用 `cmd.MarkFlagsMutuallyExclusive("frozen", "no-frozen")` 互斥；`runUpdate` 簽章改為 `runUpdate(deps *installDeps, frozen, noFrozen bool, pkg string) error`，`noFrozen` 優先權最高（無論 `--frozen` 或 CI 自動偵測，設了 `--no-frozen` 就強制 `frozen = false`）。

### A.6 `apm_modules` 清除路徑的路徑跳脫防護（2 輪 codex review 共發現 2 層問題）

`directGitSemverUpdateScope` 回傳的 key 組成方式是 `dep.RepoURL + "/" + dep.VirtualPath`（沿用 `depKey` 的邏輯），這兩個欄位在 manifest 解析階段的驗證都**不夠嚴謹**：

- `internal/manifest/depref.go` 的 `repoCharRe`/`segmentRe`（給 `RepoURL` 的 owner/repo 段與 `VirtualPath` 段用）都只做字元集檢查（`^[A-Za-z0-9._-]+$`），字元集本身就包含 `.`，所以 `".."` 這個字串完全合法通過驗證。
- 對照組：`ref.LocalPath`（`path:` 且無 `git:` 鍵，純本地路徑依賴）有 `containsEscape()` 明確擋 `..`；`internal/lockfile/parse.go` 的 `validatePathComponent`（給**lockfile 自己**的 `virtual_path` 欄位用）也明確擋 `..`。**唯獨 manifest 的 `VirtualPath`/`RepoURL` 沒有這層防護**——這是既有、非本任務引入的驗證不一致，但本任務的清除邏輯直接消費這兩個欄位，必須在使用端補防護，而不是動全域的 manifest 驗證規則（範圍與風險都大得多，且不是本任務授權範圍）。

修正分兩層，缺一不可：

1. **`archive.Contained("apm_modules", installDir)`**（第 1 輪 review 修正）——擋住「整個 key 解析後落在 apm_modules 之外」的情況，例如 `path: "../../../evil"` 配 `acme/a`（2 段）淨移動 3 層，逃出 `apm_modules` 本身。沿用 `cmd/apm/install.go` frozen registry 解壓路徑既有的相同防護模式。
2. **`keyHasParentSegment(key)`**（第 2 輪 review 追加修正）——`archive.Contained` 只看「最終落點是否在 root 內」，擋不住「`..` 解析後仍落在 `apm_modules` 內、但换成別的目錄」——例如 `path: ".."` 配 `acme/a`，`filepath.Join` 清理後變成 `apm_modules/acme`（一整個 owner 命名空間），對這個目錄 `os.RemoveAll` 會誤刪同一 owner 下其他不相關套件，而 `Contained` 判斷這仍然「在 apm_modules 內」所以不會擋。**在 `filepath.Join`/`Contained` 檢查之前**，直接拆解 key 的每個 `/` 分段，只要有任一段等於 `".."` 就整個拒絕，不進入路徑清理流程。

兩層防護的順序（先 `keyHasParentSegment`、再 `Contained`）刻意如此：`keyHasParentSegment` 攔截「所有含 `..` 的 key」（不論最終落點在哪），`Contained` 則是對「即使沒有 `..` 也可能異常」的殘餘情況（例如絕對路徑字串）做第二層防護——兩者防禦範圍有重疊但不完全等價，保留兩層符合 fail-safe 精神。

## B. req-lk-007 —— 跳過下載前驗證 checkout 內容（已經 codex exec 重新驗證，確認為真缺陷）

### B.0 重新驗證結論（取代原先「可能不算缺陷」的猜測）

明確委託 codex exec 針對「一般（非 frozen）install 路徑到底有沒有任何 checkout 內容驗證」重新調查，結論：

- **一般 install 路徑完全沒有驗證**：`internal/resolver/resolver.go:143-145` 呼叫 `loader.LoadPackage`，`internal/gitops/clone.go:27-36` 的 `LoadPackage` 只要 `installDir` 存在就直接跳去 `parseSubManifest`，之後全程（含 `cmd/apm/install.go:415` 的 `ComputeTreeSHA256`）都只對「已解析出的 commit」算 tree hash 寫入 lockfile，**不曾重新驗證 apm_modules 裡實際檔案是否真對應該 commit**。這代表一份被竄改/停滯在舊版本的 checkout 會被永久靜默沿用，且 lockfile 仍記錄「正確」的 resolved_commit/tree_sha256（因為那是第一次算的，之後沒人重算），完全符合 req-lk-007 明文禁止的「此優化改變可觀察結果」。
- **frozen 路徑的 `VerifyTreeSHA256` 也有盲點**（次要發現，非本次修復重點但需知悉）：`internal/lockfile/treehash.go` 是對 `git ls-tree <commit>` 算出的**物件樹**內容雜湊，不是對「目前 checkout 出來的 working tree」算雜湊——只要該 commit 物件存在於 `.git` 內，雜湊算出來就會「正確」，即使 working tree 本身處於 detached 到別的 ref、或有 dirty 未提交變更。這不在本次修復範圍（B 只處理「跳過下載」判斷本身），但列為已知限制記錄於此，供未來參考。
- **結論**：這是真缺陷，值得修，且一般 install 路徑（非僅 frozen）風險更高，優先在此修。

### B.1 `resolvedRef` 的真實型態（比原設計猜測複雜）

`internal/resolver/resolver.go:144` 傳入 `LoadPackage` 的 `resolvedRef` 是 `currentPin`：
- git-semver → 贏得 range 的**tag 名**（例如 `v1.5.0`），不是 SHA。
- git-literal → `pinRefs[key]`，可能是 SHA、分支名，或非 semver tag 字面值。
- 其餘 → 使用者在 manifest 寫的原始 `Reference`。

**代表原設計「只在 40-hex 時比對」在最常見的 git-semver 情境下永遠不會觸發**——需要改用「在既有 checkout 內本地解析 `resolvedRef` 對應的 commit」而非要求呼叫端先自行判斷是否為 SHA。

### B.2 修正後的 `LoadPackage`（實際落地版本，已納入 2 輪 codex review + advisor 發現）

```go
// internal/gitops/clone.go
func (r *RealPackageLoader) LoadPackage(ref *manifest.DependencyReference, resolvedRef string) (*manifest.Manifest, error) {
    if ref.IsLocal {
        return r.loadLocalPackage(ref.LocalPath)
    }

    installDir := r.installPath(ref)

    if info, statErr := os.Stat(installDir); statErr == nil && info.IsDir() {
        if checkoutMatchesRef(installDir, resolvedRef) {
            return r.parseSubManifest(installDir) // already correct, skip clone
        }
        // stale/mismatched checkout -- must not silently persist (req-lk-007)
        if err := os.RemoveAll(installDir); err != nil {
            return nil, fmt.Errorf("remove stale checkout %s: %w", installDir, err)
        }
    }

    cloneURL := r.resolveCloneURL(ref)
    if err := r.cloneRepo(cloneURL, installDir, resolvedRef); err != nil {
        return nil, fmt.Errorf("clone %s: %w", cloneURL, err)
    }
    return r.parseSubManifest(installDir)
}

// checkoutMatchesRef reports whether installDir's current HEAD already
// equals resolvedRef AND the working tree is clean, resolved LOCALLY (no
// network) inside the existing checkout. resolvedRef may be a tag, branch,
// or commit SHA -- all resolve the same way via `git rev-parse
// <ref>^{commit}` (the ^{commit} peel handles annotated tags, which
// otherwise resolve to their own tag-object SHA rather than the commit they
// point at). Any failure is treated as a mismatch: fail-safe, not
// fail-open. A dirty/modified working tree is also treated as a mismatch
// even at the right commit, since req-lk-007 requires the skip to never
// change the observable post-install result versus a fresh install.
func checkoutMatchesRef(installDir, resolvedRef string) bool {
    if resolvedRef == "" {
        return false
    }
    head, err := ResolveCommit(installDir)
    if err != nil {
        return false
    }
    resolved, err := resolveRefLocally(installDir, resolvedRef)
    if err != nil {
        return false
    }
    if head != resolved {
        return false
    }
    return worktreeClean(installDir)
}

func resolveRefLocally(repoDir, ref string) (string, error) {
    cmd := exec.Command("git", "rev-parse", ref+"^{commit}")
    cmd.Dir = repoDir
    out, err := cmd.Output()
    if err != nil {
        return "", fmt.Errorf("rev-parse %s in %s: %w", ref, repoDir, err)
    }
    return strings.TrimSpace(string(out)), nil
}

func worktreeClean(repoDir string) bool {
    cmd := exec.Command("git", "status", "--porcelain")
    cmd.Dir = repoDir
    out, err := cmd.Output()
    if err != nil {
        return false
    }
    return len(strings.TrimSpace(string(out))) == 0
}
```

`resolveRefLocally` 改用 `git rev-parse <ref>^{commit}`（非原設計草稿的 `<ref>`）：annotated tag 若不 peel，`rev-parse` 會回傳 tag object 自己的 SHA 而非其指向的 commit，導致恆為 mismatch（safe 但每次都重新 clone，失去優化意義）——這是第一輪 codex review 發現的問題，已修正並補上 `TestCheckoutMatchesRef_TrueForAnnotatedTag` 回歸測試。

`worktreeClean` 是第一輪 codex review 額外發現的缺口：即使 HEAD commit 正確，若 working tree 有 dirty/untracked 變更，沿用該 checkout 產出的最終狀態仍然不等同全新 clone——已補上 `TestCheckoutMatchesRef_FalseWhenWorktreeDirty`／`TestCheckoutMatchesRef_FalseWhenUntrackedFilePresent`。

#### B.2.1 追加缺陷：`cloneRepo` 不接受 raw commit SHA 當 `--branch` 參數（advisor 發現，codex 額度用盡時的替代驗證管道抓到）

frozen 路徑（見 B.3）為了讓 `checkoutMatchesRef` 對到「權威 pin」而非可變動的 `resolved_ref`（如 `main`），改把 `resolvedRef` 優先設成 `dep.ResolvedCommit`（一定是 40-hex SHA）。但 `resolvedRef` 這個值在 `LoadPackage` 內是**雙重用途**：既拿去做本地 `checkoutMatchesRef` 比對（SHA 沒問題），也在需要真正 clone 時原封不動傳給 `cloneRepo` 當 `git clone --depth 1 --branch <ref>` 的 `<ref>`（SHA 會失敗）。

實測驗證（`git clone --depth 1 --branch <40-hex-sha> <url> <dir>`）：

```
fatal: Remote branch <sha> not found in upstream origin
```

exit code 128 —— 標準 shallow clone 的 `--branch` 只接受分支/tag 名稱，不接受任意 commit SHA。

這不是全新引入的缺陷：`internal/resolver/resolver.go` 的 git-literal 分支（`pinRefs[key]` 可能本來就是使用者手寫的 raw SHA）在一般 install 路徑早就可能把 SHA 傳進 `cloneRepo`，只是機率較低、此前未被觸發或測到。B 的 frozen 路徑改動讓「resolvedRef 是 SHA」變成常態（每個 frozen 直接依賴都會是 SHA），大幅提高了觸發機率。

**修正**：`cloneRepo` 依 `isCommitSHA(ref)`（40 字元 hex 判斷）分流：

```go
func (r *RealPackageLoader) cloneRepo(url, dir, ref string) error {
    if isCommitSHA(ref) {
        return r.cloneRepoAtCommit(url, dir, ref)
    }
    args := []string{"clone", "--depth", "1"}
    if ref != "" {
        args = append(args, "--branch", ref)
    }
    args = append(args, url, dir)
    cmd := exec.Command("git", args...)
    out, err := cmd.CombinedOutput()
    if err != nil {
        return fmt.Errorf("%s\n%s", err, string(out))
    }
    return nil
}

// cloneRepoAtCommit clones a repo pinned to an exact commit SHA via a full
// clone (fetches all branch/tag history, so the commit is guaranteed
// present if reachable from any of them) followed by an explicit checkout,
// since shallow --branch cloning rejects raw SHAs.
func (r *RealPackageLoader) cloneRepoAtCommit(url, dir, commit string) error {
    cloneCmd := exec.Command("git", "clone", url, dir)
    if out, err := cloneCmd.CombinedOutput(); err != nil {
        return fmt.Errorf("%s\n%s", err, string(out))
    }
    checkoutCmd := exec.Command("git", "checkout", commit)
    checkoutCmd.Dir = dir
    if out, err := checkoutCmd.CombinedOutput(); err != nil {
        return fmt.Errorf("%s\n%s", err, string(out))
    }
    return nil
}
```

選擇「完整 clone 再 checkout」而非「shallow fetch by SHA」（`git fetch --depth 1 <url> <sha>`）：後者依賴伺服器端 `uploadpack.allowReachableSHA1InWant`（GitHub.com 預設開啟，但非所有自架 git server 都支援），前者對任何標準 git 伺服器皆可行，正確性優先於效能（req-lk-007 的核心精神就是不能為了優化犧牲正確性）。代價是 SHA-pinned 依賴的首次 clone 較慢（無法 shallow），但這只影響「resolvedRef 恰好是 SHA」的情境，一般 tag/branch pin 路徑不受影響。

回歸測試：`internal/gitops/clone_test.go` 的 `TestLoadPackage_ClonesByRawCommitSHA`（透過 `LoadPackage` 端對端驗證 raw SHA 首次 clone 成功）與 `TestIsCommitSHA`（40-hex 判斷的邊界案例）。

**已知限制，記錄但不在本次修復範圍**：frozen 路徑重建 `DependencyReference` 時（`cmd/apm/install.go` 的 `ownerFromRepoURL`/`repoFromRepoURL`），對 local git-path 依賴（`repo_url: ./remote` 這種形式）會被誤判成 `owner="."', repo="remote"`，導致 `resolveCloneURL` 組出錯誤的 `https://github.com/./remote.git`。這是**與本次 SHA 修正無關的既有缺陷**（`git diff HEAD` 確認 `ref` 重建那段程式碼未被本次改動觸及，回溯到更早的 commit 就已存在），只影響「frozen install + local git-path 依賴」這個較窄的組合，不影響一般 owner/repo 形式的 git 依賴。列在此處供未來排查，不在本任務 B 範圍內修復。

### B.5 第 3 輪 codex review（SHA-clone 修正本身）發現的 3 項，逐一處置

codex 額度恢復後針對 B.2.1 的 SHA-clone 修正單獨送審，結論：raw-SHA clone 路由本身正確、完整（codex 明確確認 `internal/gitops/clone.go` 是全專案唯一組 `git clone --branch/--depth` 的地方，`isCommitSHA` 已攔在前面，無遺漏）。另外發現 3 項，逐一判斷是否納入：

1. **`checkoutMatchesRef` 對「可變動」literal ref（分支名，如 `ref: main`）仍可能誤判為 match**——本地 `git rev-parse <ref>^{commit}` 只能看到「上次 fetch 當下」的本地分支指標，若遠端分支之後往前移動，本地解析出的仍是舊 commit，會被誤判 HEAD 相符而跳過 clone，實際上與全新 clone 的結果不同，違反 req-lk-007 的精神。**判定：記錄為已知限制，不在本次修復**——req-lk-007 是 **SHOULD**（非 MUST，見 `acceptance-checklist.md` L134），且此優化的設計前提就是「本地解析、不連網」（B.1/B.2 兩輪已定案），要正確處理「遠端分支移動」本質上需要一次網路往返（`git ls-remote` 或等價操作）才能確認，這會直接抵銷「跳過下載」優化本身的意義——對於這類本質上會變動的 literal ref（相對於 tag／SHA 通常視為不可變），沒有純本地、免網路的方法能保證正確；影響範圍窄（僅限 git-literal 且明確指定分支名而非 tag/SHA 的依賴），列為已知限制。
2. **frozen 路徑重建 `DependencyReference` 遺漏 `VirtualPath`**——`RealPackageLoader.installPath` 與 `lockfile.LockedDep.UniqueKey()` 都會在 `VirtualPath` 非空時附加 `/VirtualPath` 組出路徑，frozen 路徑重建 `ref` 時若漏了這欄，`LoadPackage` 實際 clone/檢查的目錄會跟後續 `VerifyTreeSHA256` 用 `dep.UniqueKey()` 算出的路徑對不上。**判定：修正**——單一欄位遺漏、在本次已經動到的同一段程式碼（`cmd/apm/install.go` 約 276 行），風險低、正確性收益明確。已補 `ref.VirtualPath = dep.VirtualPath`，回歸測試 `TestRunInstall_Frozen_PreservesVirtualPath`。
3. **`worktreeClean` 用預設 `git status --porcelain`，不會列出 ignored 檔案**——全新 clone 不可能含有任何 ignored 檔案（沒有任何流程會在 clone 當下產生它們），因此若既有 checkout 內出現 ignored 檔案，代表這份 checkout 已經跟「全新 clone」的結果不同，屬於 req-lk-007 明文禁止的「改變可觀察結果」。**判定：修正**——單一旗標（`--ignored`）即可涵蓋，風險低、正確性收益明確。已加上該旗標，回歸測試 `TestCheckoutMatchesRef_FalseWhenIgnoredFilePresent`。

### B.3 套用範圍

**下沉到 `LoadPackage` 本身**，一般 install、frozen install、（A 組的）update 三條路徑共用，不在 `cmd/apm/install.go` 個別分支重複判斷邏輯：

- **一般 install 路徑**：`internal/resolver/resolver.go` 呼叫 `loader.LoadPackage` 的地方不用改，直接吃到新行為。
- **frozen 路徑**：`cmd/apm/install.go` 約 265-286 行原本自己的 `os.Stat` 存在性短路（`if _, statErr := os.Stat(installDir); os.IsNotExist(statErr) { deps.loader.LoadPackage(...) }`）之後如果 `installDir` 已存在就完全不呼叫 `LoadPackage`——這代表 frozen 路徑目前**繞過** `LoadPackage`，需要改為**永遠呼叫** `deps.loader.LoadPackage`（讓新邏輯生效），不要在外層自行判斷是否存在。`VerifyTreeSHA256` 複驗邏輯維持不動（多一層防護，非壞事）。

### B.4 觀察行為（修正後）

- checkout 存在且本地可解析 `resolvedRef` 且等於 HEAD → 跳過 clone（優化生效，符合 req-lk-007 SHOULD）。
- checkout 存在但對不上（stale/損毀/pin 換了）→ 整個目錄砍掉重新 clone，確保最終可觀察狀態與全新安裝一致（req-lk-007 明文要求的核心）。
- 任何本地 git 指令失敗（非 git repo、損毀等）一律視為 mismatch，fail-safe 觸發重新 clone，不 fail-open。

## C. Target 自動偵測缺陷

### C.1 antigravity 移出 explicitOnlyTargets

```go
// internal/deploy/adapter.go
var explicitOnlyTargets = map[string]bool{
    "agent-skills": true,
}
```

移除 `"antigravity": true` 那一行，並修正第 79 行註解（目前誤稱這是 req-tg-001 合規，需改為說明只有 agent-skills 才是規範明文 explicit-only）。

### C.2 收斂 copilot 偵測訊號

```go
// internal/manifest/detect.go SignalWhitelist
{".github/copilot-instructions.md", false, "copilot"},
```

刪除 `.github/instructions/`、`.github/agents/`、`.github/prompts/`、`.github/hooks/` 四筆 copilot 訊號。已核對 `conformance/conformance-kit/oracle/targets/expected/copilot.yaml` 無 `detect:` 欄位，不與既有 oracle 衝突。

### C.3 `minimal` fallback

新增 `internal/deploy/minimal.go`：

```go
package deploy

// CompileMinimalAgentsMD writes a single AGENTS.md at the project root from
// local .apm/instructions/*.md content, for the req-tg-001 fallback when no
// target signal, --target, or manifest target: is present. Deliberately
// narrow: this is NOT the general compile_outputs mechanism other targets
// declare (that's unimplemented project-wide and out of this task's scope).
func CompileMinimalAgentsMD(projectDir string) (string, error)
```

`ResolveTargets` 呼叫端（`internal/deploy/adapter.go`）在 `detected` 為空且無 `--target`/manifest `target:` 時，不再回傳 `nil, nil`，而是回傳一個特殊值讓 `deploy.Run` 呼叫 `CompileMinimalAgentsMD`（不透過 `Adapters` map/`TargetAdapter` 介面，因為 `minimal` 不是一個會處理一般 primitive 的完整 adapter，只做這一件事）。具體介接方式（是否新增一個 sentinel target 字串如 `"minimal"` 讓 `deploy.Run` 特判、或在 `ResolveTargets` 外層由 `install.go` 呼叫）留給 implement 階段依現有 `deploy.Run`/`cmd/apm/install.go` 呼叫慣例決定，設計原則是：**不強行讓 `minimal` 實作完整 `TargetAdapter` 介面**（它沒有 `DeployPrimitive`/`SupportedTypes` 等語意，硬套會產生大量空實作）。

## 風險與回退

- A 的 install.go 重構有回歸既有 install 行為的風險：每個抽出的函式都要先跑通既有測試再繼續下一步（TDD 紀律），任何一步測試變紅立即停下排查，不得跳過。
- B 下沉到 `LoadPackage` 影響面最廣（一般 install + frozen + update 共用），需要對三條路徑各補至少一個「stale checkout 被偵測到並修復」的回歸測試。
- C.2 收斂 copilot 訊號有極小機率影響某些使用者現有專案的自動偵測結果（例如只有 `.github/agents/` 沒有 `.github/copilot-instructions.md` 的專案，之前會自動判成 copilot，收斂後不會）——這是規範要求的正確行為，不視為需要相容性保留的破壞性變更。
