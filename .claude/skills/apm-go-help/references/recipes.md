# 值得照抄的提示

把路徑、issue 編號和完成條件換成實際的。

## 理解

- `讀 #<N> 和它的留言，用你自己的話重述使用者碰到的問題，先不要改程式。`
- `用 gitnexus 找出 <函式> 的呼叫者和會受影響的流程，說明風險等級。`
- `PRODUCT.md 對 <指令或行為> 怎麼寫？引用條目原文，再用 verify-apm-go 跑一次確認現在的執行檔符合它。`

## 修改

- Bug：`<症狀>。先用 verify-apm-go 重現，再修，修完附上前後兩次的 stdout、stderr、exit code 與檔案樹。`
- 跟 Oracle 對齊：`把 <行為> 改成跟 Oracle 0.33.0 一樣。先在兩個新目錄各跑一次記下差異，引用 Oracle 的 file:line，測試的 expected 值用量到的 Oracle 輸出。`
- 一批小項目：`修 <項目清單>，每項都要有測試，跑過 verify.sh check，全部做完再一起開一個 PR。`
- 改到 lock 或清理：`改完後用前一版 apm-go 和 Oracle 0.33.0 各寫一份 lock，跑 install、update、install --frozen、uninstall，貼出結果。`

## 審查與交付

- `開 PR，說明逐條交代 AGENTS.md 的強制規則和適用的 lesson；升級後使用者看得到的改動加到下一版的 release notes issue。`
- `在 herdr 開一個 codex pane，用 $ponytail-review 審查 PR #<N>，只回報可以動手修的發現。`
- `board card start <N>`，之後用 `board status` 看進度。
- `PR #<N> 檢查都過了就照 AGENTS.md 的指令合併，刪掉本機 branch 和 worktree。`

## 收尾

- `跑 Lessons review：範圍從 LESSONS.md 第一行的 cursor 到 origin/main，照 AGENTS.md 的步驟開 lessons: PR。`
- `這次做完了嗎？用 os-done-or-not 寫報告。`
