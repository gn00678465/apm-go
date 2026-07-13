# Gate 6b 修正 round 2 驗證 — A6/B2/C2

> **codex round 2 被自身安全過濾器阻擋**：codex exec 在重現 B2 的 NTFS junction 逃逸時
> 兩次回 `ERROR: This content was flagged for possible cybersecurity risk`（OpenAI 對
> junction/link 逃逸的安全內容過濾），未能寫出判定檔。故本 round 2 由**主 session 親自**
> 以更強的 mutation 測試替代驗證（claim 不採信；每項附辨識力證據）。

## A6 CONFIRMED — metadata 過濾 case-insensitive
主 session 親跑（bin/apm-go.exe）：造 bundle（plugin.json + agents/foo.agent.md + 大寫
`APM.LOCK.YAML`，無 pack: 段走 fallback walk），`apm-go install --target claude` 後
部署樹只有 `.claude/agents/foo.agent.md`，`.claude/APM.LOCK.YAML` **未被部署**（✓ 排除）。
自動化：`TestIntegrateLocalBundle_NilMeta_CaseInsensitiveMetadataExcluded` PASS。
修正：`bundleDeployFileRels` 三 metadata 檔一律 `strings.ToLower` 比對排除。

## B2 CONFIRMED — NTFS junction 逃逸被擋（mutation 證明辨識力）
自動化測試 `TestVerifyBundleIntegrity_JunctionWithListedTargetFile_StillRejected`
（真實 `mklink /J` junction 指向 bundle 外、bundle_files hash **對應外部檔**，即唯一防線
是 junction 偵測）主 session 親跑 **PASS**（回報 `Symlink rejected ... skills/linked`）。
**Mutation 辨識力**：把 `isSymlinkOrReparsePoint` 改回只認 `os.ModeSymlink`（修正前行為）
→ junction 測試 **FAIL**（junction 逃逸重現，正是 codex 首輪 B2 發現）；還原 → PASS。
修正：`isSymlinkOrReparsePoint = info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0`
（Windows junction 為 `IO_REPARSE_TAG_MOUNT_POINT`，surface 成 ModeIrregular），
套用於 verify.go 三 sweep + SkipDir，integrate.go `deployBundleFile` 另有
`hasReparsePointAncestor` 逐路徑段檢查（防 leaf 是 junction 且無 prior verify 的情況）。

## C2 CONFIRMED — 巢狀-skill regression 測試存在
主 session 親跑 `go test ./cmd/apm/... -run NestedSkill -count=1`：
`TestRunInstall_LocalBundle_NestedSkill_DeploysVerbatim`、
`TestRunInstall_LocalBundle_NestedSkill_CopilotUsesAgentsRoot` 皆 PASS；
fixture 為兩層巢狀 `skills/engineering/ask-matt/SKILL.md`，斷言部署到
`.claude/skills/engineering/ask-matt/SKILL.md`（copilot → `.agents/skills/...`）。
`go test -list Nested` 找得到。

## 整體
全 repo `go build/vet/test ./... -count=1` 綠（23 套件）；gofmt 觸碰檔乾淨、LF；
localbundle 82.0%、cmd/apm 84.5%。

OVERALL: PASS (A6+B2+C2，主 session mutation 驗證替代 codex 過濾阻擋)
