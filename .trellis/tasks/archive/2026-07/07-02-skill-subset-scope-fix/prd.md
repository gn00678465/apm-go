# 修正 --skill 子集部署的全域過濾範圍錯誤

## Goal

`apm install <skill-collection> --skill <skill-name>` 目前的過濾範圍是**整個已解析依賴圖**,而不是**這次呼叫實際指定的那個套件**。修正範圍,使 `--skill` 只影響它鎖定的套件,不波及 local skills 或其他早已宣告的依賴。

## Background(已用重現測試證實)

- `internal/deploy/deploy.go:90-100` 的 `skillFilter` 套用在 `ordered`(local + 所有 direct + transitive deps 的 primitives)上,只比對 `p.Name`,完全沒檢查 `p.Source`/`p.DepKey`。
  - 重現:專案有既有 local skill `alpha`、`beta`,執行 `apm install <other-pkg> --skill zeta`(zeta 與兩者無關)→ `alpha`、`beta` 都未部署(靜默跳過,無警告)。
- `cmd/apm/install.go:396,419-422` 的 `buildLockfile` 對 `result.Deps`(整個已解析依賴圖)逐一寫入同一份 `ld.SkillSubset = skillSubset`,沒有比對 `dep.Key` 是否屬於這次呼叫的 `packages`。
  - 只要這次呼叫同時有 `--skill` 與至少一個 positional package,`apm.lock.yaml` 裡**每一個**依賴(包含完全無關、早就裝好的其他依賴)都會被寫入相同的 `skill_subset`。

兩者根因相同:`skillSubset` 是「這次 CLI 呼叫」範圍的全域參數,卻套用到「這次呼叫解析出的整個依賴圖」,而非「這次呼叫實際指定的套件」。

## Requirements

- `req-pr-001`/`req-tg-003`(acceptance-checklist.md:151,MUST):`apm install <skill-collection> --skill <skill-name>` 只部署被選 skill 到 `.agents/skills/<name>/SKILL.md`;未選 skill 不得落盤;選擇需持久化到 `apm.yml` 的 `skills:` 與 `apm.lock.yaml` 的 `skill_subset`。
- 過濾範圍必須限定在這次 `--skill` 鎖定的套件(依 positional package 解析出的 dep key);local primitives 與其他早已宣告的依賴一律不受影響。
- `apm.lock.yaml` 的 `skill_subset` 欄位只能寫在這次呼叫實際指定的套件的 lock 條目上。

## Non-Goals

- 不處理 `--skill` 搭配多個 positional packages、且每個套件的 skill 名稱有交集/衝突的情境(spec 與既有 oracle fixture 僅涵蓋單一套件情境,超出範圍的組合行為留待未來需求明確後再處理)。
- 不新增 `apm update` 的 `--skill` 支援(現況 `runUpdate` 本來就不傳遞 skillSubset,維持原樣)。

## Acceptance Criteria

- [ ] `apm install <pkgB> --skill x`(x 與既有 local skill 或其他已裝依賴的 skill 無關)不再讓那些無關 skill 被跳過部署。
- [ ] `apm.lock.yaml` 的 `skill_subset` 只出現在這次呼叫實際指定的套件的 lock 條目上,不出現在其他依賴的條目。
- [ ] 同一個被 `--skill` 鎖定的套件內,選中的 skill 仍會部署、未選中的 skill 仍不會落盤(既有正確行為不能回歸)。
- [ ] 新增 regression test 覆蓋兩個修正點(deploy 過濾範圍 + lockfile skill_subset 範圍),取代先前手動重現後刪除的 scratch 測試。
- [ ] `go build ./...`、`go vet ./...`、`gofmt -l .`、`go test ./... -cover` 全過。
