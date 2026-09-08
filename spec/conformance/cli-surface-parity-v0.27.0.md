# CLI 表面 parity 缺口實測（vs upstream v0.27.0）

**測定日期**：2026-08-04
**上游基準**：`microsoft/apm` v0.27.0（`git -C D:/Projects/apm-dev/apm describe --tags` → `v0.27.0-2-g703dd9e7`；HEAD 的 2 個 commit 未觸及 `src/`）
**apm-go**：`feat/marketplace-plugin-parity` 分支，含未提交改動

> **⛔ 範圍護欄（2026-08-04 使用者第三次重申）**
>
> 本文涵蓋的**整體 CLI parity 已由使用者明示排入其他任務**。
> `07-28-marketplace-plugin-parity` 及其五個 child **專注於 plugin / marketplace**，
> 本文所列的缺席指令與旗標**一律不屬於該 parent 的驗收範圍**。
>
> 具體禁止事項（寫給未來的自己，因為口頭記住已被證明失敗三次）：
> - 不得把本文的數字（33 指令 / 75 旗標）放進 plugin/marketplace 任務的
>   完成度、缺口清單或交付報告
> - 不得在回答「plugin/marketplace 是否完整」時引用本文作為未完成的理由
> - 該五個 child 的驗收邊界是它們各自 `prd.md` 的 AC，不是本文
>
> 本文的唯一用途：作為**另一個任務**的輸入資料。

## 測法（可重跑）

不用原始碼掃描，用**兩邊實際 `--help` 輸出**比對——原始碼掃描已被證明會漏
（多行 `@click.option(\n "--only",` 抓不到）也會誤報（`-v, --verbose` 的前綴形式）。

```bash
# 指令清單
uv --project D:/Projects/apm-dev/apm run apm --help | sed -n '/^Commands:/,$p' | grep -E '^  [a-z]' | awk '{print $1}' | sort -u
apm-go.exe --help | sed -n '/Available Commands:/,/^Flags:/p' | grep -E '^  [a-z]' | awk '{print $1}' | sort -u

# 單一指令旗標
uv --project D:/Projects/apm-dev/apm run apm <cmd> --help | grep -oE '\-\-[a-z][a-z-]+' | sort -u
apm-go.exe <cmd> --help                                   | grep -oE '\-\-[a-z][a-z-]+' | sort -u
```

## 1. 指令層級：apm-go 實作 10 / 33

**共有（10）**
`audit` `compile` `experimental` `init` `install` `marketplace` `pack` `plugin` `uninstall` `update`

**上游有、apm-go 沒有（23）**
`approve` `cache` `config` `deny` `deps` `doctor` `find` `lifecycle` `list` `lock`
`mcp` `outdated` `policy` `preview` `prune` `publish` `run` `runtime` `search`
`self-update` `targets` `unpack` `view`

**apm-go 獨有（4）**
`completion` `help`（皆為 cobra 內建）、`normalize`、`validate`

## 2. 旗標層級：10 個共有指令共缺 75 個

| 指令 | 上游 | apm-go | 缺 | 缺少的旗標 |
|---|---:|---:|---:|---|
| `install` | 39 | 18 | **24** | `--all` `--allow-insecure-host` `--allow-protocol-fallback` `--as` `--audit` `--ci` `--dry-run` `--exclude` `--global` `--https` `--legacy-skill-paths` `--no-audit` `--no-policy` `--only` `--parallel-downloads` `--prefix` `--refresh` `--root` `--runtime` `--ssh` `--trust-transitive-mcp` `--update` `--yes` |
| `compile` | 22 | 2 | **20** | `--all` `--chatmode` `--clean` `--dry-run` `--force-instructions` `--global` `--legacy-skill-paths` `--local-only` `--no-constitution` `--no-dedup` `--no-force-instructions` `--no-links` `--output` `--root` `--single-agents` `--validate` `--verbose` `--watch` `--with-constitution` |
| `audit` | 18 | 8 | **11** | `--dry-run` `--external-args` `--external-llm` `--external-sarif` `--file` `--no-cache` `--no-drift` `--no-external-llm` `--no-fail-fast` `--no-policy` `--output` |
| `pack` | 18 | 8 | **10** | `--archive` `--archive-format` `--check-clean` `--check-versions` `--format` `--json` `--legacy-skill-paths` `--output` `--target` |
| `update` | 8 | 4 | **6** | `--force` `--global` `--parallel-downloads` `--target` `--verbose` `--yes` |
| `init` | 6 | 4 | **3** | `--marketplace` `--plugin` `--verbose` |
| `experimental` | 2 | 1 | **1** | `--verbose` |
| `marketplace` | 1 | 1 | 0 | — |
| `plugin` | 1 | 1 | 0 | — |
| `uninstall` | 4 | 4 | 0 | — |

`marketplace` / `plugin` / `uninstall` 三者零缺口，對應
`07-28-marketplace-plugin-parity` 及其 child 已做過的範圍。

## 3. 版本歸屬：這**不是** v0.27.0 造成的

```bash
# install 旗標在 v0.26.0 與 v0.27.0 的差集
git show v0.26.0:src/apm_cli/commands/install.py | grep -oE '"--[a-z-]+"' | tr -d '"' | sort -u > /tmp/py26.txt
git show v0.27.0:src/apm_cli/commands/install.py | grep -oE '"--[a-z-]+"' | tr -d '"' | sort -u > /tmp/py27.txt
comm -13 /tmp/py26.txt /tmp/py27.txt      # → 空
```

⇒ install 的缺口旗標**在 v0.26.0 就全部存在**，v0.26→v0.27 新增 0 個。
本表記錄的是 **apm-go 整體 parity 程度**，不是版本位移造成的新缺口。
（v0.26→v0.27 真正新增且已處理的是 `source.tag_pattern`；未處理的見
`.trellis/tasks/07-28-marketplace-plugin-parity/upstream-v0.27.0-delta.md`。）

## 4. 已知量測誤差（記錄以免下次重犯）

1. **原始碼掃描不可靠**：`grep '@click\.option\("--[a-z-]+'` 漏掉多行寫法，
   導致我一度回報 install 缺 19 個（實測 24 個）。
2. **help 輸出的前綴形式**：`-v, --verbose` 會讓錨定行首的 pattern 漏抓，
   一度誤報 apm-go 缺 `--verbose`（實際有）。
3. **本表數字來自 `--help` 實測**，不是原始碼推導。
4. 表中 `--dry-` / `--mcp-` / `--archive-` 這類截斷片段已從原始輸出剔除
   （grep 對 `--dry-run` 之類含連字號旗標會產生部分匹配）。

## 5. 未量測的部分（明確聲明）

- **行為等價性未驗**：本表只比對旗標**存在與否**，同名旗標的語意是否一致
  **完全未測**。
- 上游 v0.26→v0.27 的 102 檔變更中，`install/`（30 檔）、`integration/`（10）、
  `adapters/client/`（6）、`deps/`（12）、`core/auth.py`、`security/`、`policy/`、
  `commands/uninstall/` 的**行為落差未評估**。
- 23 個缺席指令的**實作成本未估**。
