# Wave 1 当前状态与交接记录

- 日期：2026-08-08
- 状态：Wave 1 实现与本地验证完成，已交接 Wave 2；未启动修复后新一轮外部复审
- 复审模型：Cursor Agent / `cursor-grok-4.5-high`（非 Fast）

## 已完成范围

Wave 1 已落地 provider-independent Go 核心，包括：

- Task、TaskRevision、WorkflowNode、Attempt、HostAction、CompletionDecision、HumanDecision、GoalAuthorization、Event 和 ArtifactRef；
- 文件 snapshot/event 持久化、原子替换恢复、snapshot/JSONL reconciliation 和秘密过滤；
- binding、workspace、permission、MCP allowlist、Goal scope 和 authority 的 fail-closed preflight；
- host-action lease、request/action 幂等、provider/session/workspace/canary 污染冻结；
- checkpoint、completion/artifact 证据、有限 retry、fallback lineage、取消和 fake Adapter E2E；
- CLI `--gateway` smoke 与 Wave 1 证据文件。

## 复审与修复状态

最近一次独立 Cursor / Grok 4.5 High 复审结论为 `PASS WITH CONDITIONS`。复审发现的最后一个 P1 是：确认取消后的 Attempt 在人工 `retry_node` 后不能作为同节点手动重试 predecessor。

该问题已修复：

- `PrepareAttempt` 允许 `AttemptCancelled` 作为显式人工 retry 的 predecessor；
- 仍要求节点处于人工决策后的可执行状态，并保留 `RetryOf` lineage；
- 已增加 `cancelled → RequestCompletion → retry_node → PrepareAttempt` 回归测试。

修复后本地验证全部通过：

```text
go test -race ./...
go vet ./...
go build ./...
git diff --check
```

此前已修复的 fallback attempt-bound approval、重复消费保护、retry lineage 计数、unknown side-effect、MCP preflight、重复 in-flight、污染持久化和事件恢复问题均保留回归覆盖。

## 诚实边界

最后一个 P1 修复后没有再次调用外部 reviewer；因此本记录证明的是当前代码与本地验证状态，不冒充修复后的独立外部最终验收。Wave 2 仍按计划只推进 Codex App Native 路径，不把外部 CLI 当作 Native 替代。

## Wave 2 交接

下一阶段从 `skills/multi-agent-orchestration/` Controller Skill、Native host-action executor 合同、fake Native executor 和真实 Codex App smoke 入口开始。Wave 2 不修改 Wave 1 的核心事实源，不引入 SQLite、daemon、dashboard、完整 mailbox/dead-letter 或新的外部 Adapter。
