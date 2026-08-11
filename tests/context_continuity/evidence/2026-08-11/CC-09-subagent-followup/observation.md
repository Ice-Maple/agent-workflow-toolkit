# Observation

## 真实宿主复现

当前 App 对同一已完成 agent 的 follow-up turn 没有再次发送 `SubagentStart`，但会发送新的 `SubagentStop`。旧实现把该 stop 当作 orphan，加入 `unknown` 并阻塞后续 seal。

## 修复与自动化

修复增加 64 条有界 completion fingerprint ledger。定向 14 项、全部 context-continuity 70 项、`compileall` 和 `git diff --check` 通过；独立 reviewer verdict 为 PASS。

## 修复后真实 App smoke

同一 agent 初次完成后再次通过 `followup_task` 返回 `W3_SUBAGENT_FOLLOWUP_OK`。App 仍只发送新的 `SubagentStop`；终态 `active_subagent_count=0`、`lifecycle_status=completed`，没有形成 `unknown`。

## 保守边界

被 interrupt 且宿主没有发送 stop 的 agent 仍保留为 running，并继续阻止 seal；没有可靠完成证据时不自动清理。App payload 也没有 lifecycle epoch，因此同一 ID 的非历史 stop 只能信任宿主 identity，这一不可判定边界不降级为假 safe。
