# Thinking Guides

> **Purpose**: Expand your thinking to catch things you might not have considered.

---

## Why Thinking Guides?

**Most bugs and tech debt come from "didn't think of that"**, not from lack of skill:

- Didn't think about what happens at layer boundaries → cross-layer bugs
- Didn't think about code patterns repeating → duplicated code everywhere
- Didn't think about edge cases → runtime errors
- Didn't think about future maintainers → unreadable code

These guides help you **ask the right questions before coding**.

---

## Available Guides

| Guide | Purpose | When to Use |
|-------|---------|-------------|
| [Code Reuse Thinking Guide](./code-reuse-thinking-guide.md) | Identify patterns and reduce duplication | When you notice repeated patterns |
| [Cross-Layer Thinking Guide](./cross-layer-thinking-guide.md) | Think through data flow across layers | Features spanning multiple layers |
| [Oracle Parity Gates](./oracle-parity-gates.md) | Catch same-name-different-behavior CLI defects against the Python `apm` oracle | Adding/changing an apm-go CLI command, subcommand, or flag |
| [Claim-Evidence Guide](./claim-evidence-guide.md) | 把「用形容詞代替 grep」擋在寫出來之前；取代 `AGENTS.md` §5 的詞表式偵測 | **任何**對程式碼下判斷的時候——research、PRD、review、進度回報、完成宣告 |

---

## When Making ANY Claim About Code（最常被跳過的一關）

- [ ] 你寫下了比較句（較佳／優於／更安全）
- [ ] 你寫下了等價句（一致／相同／無差異）
- [ ] 你寫下了化約句（純 X 層／只是／僅為）
- [ ] 你寫下了充分性句（已覆蓋／足夠／不需要）
- [ ] 你寫下了不存在句（無缺口／不受影響／沒問題）
- [ ] 你寫下了量級句（成本大／成本小／一行就好）
- [ ] 你寫下了時序句（延後／另開／不在此範圍）
- [ ] 你寫下了**因果歸因**句（根因是 X／是 X 造成的）
- [ ] 你寫下了**風險接受**句（風險可接受／機率很低／實務上不會發生）
- [ ] 你要宣告某件事「完成」

→ 讀 [Claim-Evidence Guide](./claim-evidence-guide.md)。
**先回答一句**：「如果我錯了，哪一段程式碼會證明我錯？我讀過它了嗎？」
沒讀過就只能寫「未驗證」。

> 這一關是 2026-07-29 從 `07-28-marketplace-plugin-parity` 的四次判斷失效反推出來的。
> 那四句與 `AGENTS.md` §5 的絆線詞表**零重疊**——偵測器看不到它們。

---

## Quick Reference: Thinking Triggers

### When to Think About Cross-Layer Issues

- [ ] Feature touches 3+ layers (API, Service, Component, Database)
- [ ] Data format changes between layers
- [ ] Multiple consumers need the same data
- [ ] You're not sure where to put some logic
- [ ] You are adding an event kind, JSONL record, RPC payload, or config field
- [ ] UI / command code starts casting raw payload fields directly

→ Read [Cross-Layer Thinking Guide](./cross-layer-thinking-guide.md)

### When to Think About Code Reuse

- [ ] You're writing similar code to something that exists
- [ ] You see the same pattern repeated 3+ times
- [ ] You're adding a new field to multiple places
- [ ] **You're modifying any constant or config**
- [ ] **You're creating a new utility/helper function** ← Search first!
- [ ] Two files read the same untyped payload field with local casts
- [ ] Multiple branches update the same derived state from `kind` / `action`

→ Read [Code Reuse Thinking Guide](./code-reuse-thinking-guide.md)

### When Touching apm-go's CLI Surface

- [ ] You're adding or renaming an apm-go command/subcommand
- [ ] The command name matches (case-insensitive) an existing Python `apm <verb>` command
- [ ] Requirement wording is copied from spec text rather than compared against Python CLI directly
- [ ] You're deliberately implementing less than Python's full behavior for a command
- [ ] Output is written to a path whose extension/location implies a structured format (TOML/JSON/YAML/...)
- [ ] Research recorded a `format_id`/transformer key without expanding its logic

→ Read [Oracle Parity Gates](./oracle-parity-gates.md); check
`.trellis/spec/evals/cli-surface-parity-register.md` for the command's existing
classification before writing the PRD.

### When Verifying AI Cross-Review Results

- [ ] Reviewer claims "user input can be malicious" → Check the actual data source (internal manifest? user config? external API?)
- [ ] Reviewer flags "missing validation" → Is the data from a trusted internal source?
- [ ] Reviewer says "behavior change" → Read the code comments — is it intentional design?
- [ ] Reviewer identifies a "bug" in test → Mentally delete the feature being tested — does the test still pass? If yes → tautological test

**Common AI reviewer false-positive patterns**:
1. **Trust boundary confusion**: Treating internal data (bundled JSON manifests) as untrusted external input
2. **Ignoring design comments**: Flagging intentional behavior documented in code comments as bugs
3. **Variable misreading**: Not tracing a variable to its actual definition (e.g., Map keyed by path vs name)

**Verification rule**: Every CRITICAL/WARNING finding must be verified against the actual code before prioritizing. Budget ~35% false-positive rate for AI reviews.

---

## Pre-Modification Rule (CRITICAL)

> **Before changing ANY value, ALWAYS search first!**

```bash
# Search for the value you're about to change
grep -r "value_to_change" .
```

This single habit prevents most "forgot to update X" bugs.

---

## How to Use This Directory

1. **Before coding**: Skim the relevant thinking guide
2. **During coding**: If something feels repetitive or complex, check the guides
3. **After bugs**: Add new insights to the relevant guide (learn from mistakes)

---

## Contributing

Found a new "didn't think of that" moment? Add it to the relevant guide.

---

**Core Principle**: 30 minutes of thinking saves 3 hours of debugging.
