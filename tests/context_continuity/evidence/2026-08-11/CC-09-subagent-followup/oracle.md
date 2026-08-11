# Oracle

- 同一已完成 agent 被 `followup_task` 复用、且 App 只发新的 `SubagentStop` 时，必须保持可归属的 `completed`，不得创建 orphan `unknown`。
- 完全相同的历史 completion fingerprint 重放必须幂等，不得污染 `safe` 或 pending turn。
- 已知历史 stop 不得清除同一 ID 的新 running lifecycle。
- 完全陌生或已被有界 ledger 淘汰的 stop 仍必须 fail-dirty 为 `unknown`。
- ledger 只保存 agent、turn 和 message 的摘要，最多 64 条。
