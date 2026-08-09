# Wave 2 Codex App Native read-only smoke

- Date: 2026-08-09
- Core attempt: `attempt-455a9bcba35af74e85428670`
- Scope: real Codex App Native reviewer/explorer call through the core-issued `launch` and `observe` action path.
- Workspace: `E:\projects\agent-workflow-toolkit`
- Delivery: Controller-delivered Markdown artifact; the Native Subagent did not create, modify, or delete repository files.

## Findings

当前 Controller/Native 边界清楚；无重要问题。

- Controller 只能执行 Core 返回的 pending host action，并必须使用 Codex App 原生能力。
- Native 结果需携带 provider/session、workspace 与 task canary；不匹配时不得静默重试。
- `native.go` 将真实宿主能力留在 Go 核心之外，`FakeExecutor` 仅是无副作用契约 fixture。

## Evidence

实际只读读取：

- [SKILL.md](E:/projects/agent-workflow-toolkit/skills/multi-agent-orchestration/SKILL.md:12)
- [native-action-contract.md](E:/projects/agent-workflow-toolkit/skills/multi-agent-orchestration/references/native-action-contract.md:27)
- [native.go](E:/projects/agent-workflow-toolkit/internal/adapters/native/native.go:17)

可核验事实：`SKILL.md:15` 明确禁止 shell 启动 CLI；契约要求返回 workspace/task canary。本次在 Codex App 原生 Subagent 宿主、`E:\projects\agent-workflow-toolkit` 工作区完成，Core attempt 为 `attempt-455a9bcba35af74e85428670`，Native launch receipt 已由 Core 校验并发出 observe action，未使用外部 Agent CLI。
