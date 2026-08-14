# Role-first real-combination closure review

- Reviewer/model: Cursor Agent / cursor-grok-4.6-high
- Adapter: Cursor (`cursor`)
- Execution: read-only `plan` mode with explicit workspace trust
- Initial delivery: status `completed`, reason `terminal_and_final_delivery`, final bytes `436`
- Final delivery: status `completed`, reason `terminal_and_final_delivery`, final source `result.result`, terminal `true`, exit code `0`, final bytes `5976`
- Delivery recovery: same-session core-authorized follow-up used: `true`
- Prompt: sanitized; private prompt text is intentionally not stored in this artifact

## Scope and evidence

只读审查当前磁盘上的 Role-first real-combination 改动，未修改、暂存、提交或另启 Agent。本轮 `git status` / `git diff` 调用被拒绝，改动范围以会话开始时的 worktree 快照为准（`main` 相对 `origin/main` ahead 21；所列 Go/PowerShell/文档/证据文件均为 dirty 或 untracked），并直接阅读这些文件的当前内容。

已核对象：`AGENTS.md`、`README.md`、`cmd/agent-workflow-core/main.go` 及其测试、`internal/bridge/owner_process.go`、`internal/bridge/owner.go`、`internal/bridge/executor.go`、`internal/bridge/e2e_test.go`、`internal/bridge/cursor_real_e2e_test.go`、`internal/adapters/cursor/cursor.go`、`cursor_test.go`、`cmd/agent-workflow-collaboration/main.go` 及其测试、`skills/multi-agent-orchestration/SKILL.md`、`invoke_project_role.ps1`、`tools/orchestration/install_project_role_package.ps1`、`smoke_project_role_entry.ps1`、adoption/architecture 指南、`tests/orchestration/evidence/2026-08-14/role-first-real-combination-closure.md`、`go.mod`。未把 brainstorm 文档中的 `doc-ai-toolkit` 示例当作本轮实现导入。

## Findings

### [P1] 把 owner 标为 ready 后未允许 follow-up launch，IPC serve 仍要等 Provider session 结束才开始 — `internal/bridge/owner_process.go:174-198`、`internal/bridge/executor.go:100-112` 与 `179-180`

`runOwnerProcess` 在 listener 建立且 `RegisterCollaborationOwner` 成功后把 receipt 写成 `ready` 并返回给 `startOwnerProcess`；随后才同步 `execute(launch)`，最后才 `serve()`。`Executor` 对已存在 receipt 的再次 `launch` 会走 `validateOwnerForRequest`，该函数只允许 `starting` 和 `running`，`ready` 被当成 `stale_owner`。

影响：public `bridge --execute --action-kind launch` 现在在 session 启动前返回，core 上的 launch HostAction 仍是 pending。SKILL 要求的 poll/execute 循环、或丢失 `after-action-sequence` 的 Controller restart，会在这个新窗口（真实 Cursor 可达数十秒）再次提交同一 launch，得到误导性的 `stale_owner`，也无法通过 IPC 挂上仍在启动的 owner。当前 E2E 用 `GetAttempt` 等到 `running` 再 `observe`，所以测不到这条公共循环。旧行为是 launch CLI 等到离开 `starting`（即 session 已提交结果）才返回，因此不存在这个窗口。

最小修复：若 action 是 `launch` 且 receipt 为 `starting`/`ready`，直接返回现有 owner view，不要再走 IPC；并把 `ready` 列入 `validateOwnerForRequest`。若还需要在 session 启动期间 cancel，则必须在 `execute(launch)` 之前并发 `serve()`，且不能让 `executionMu` 把 `cancel_host` 堵到 launch 结束。

### [P2] disable 的 AttemptID-only 回退仍可能向 nil `entry.Attempts` 赋值 — `cmd/agent-workflow-core/main.go:272-283`

`disableAndConverge` 在 `entry.Attempts` 为空但顶层 `AttemptID` 有值时，把迭代用的 `attempts` 换成新 map，随后却写入 `entry.Attempts[attemptID]`。JSON `attempts,omitempty` 反序列化后该字段为 nil；`RegisterRunUnderLock` 的单 Attempt 形状会触发 panic。已注册的 external owner 会同时写入 `Attempts` map，因此 `TestDisableCancelsActiveExternalOwnerWithoutPanic` 覆盖不到这条回退。`latest.Attempts` 在 terminal cancel 后不再重建，那条历史 panic 已闭合。

最小修复：回退时把 `entry.Attempts` 设为同一个新 map，或只在 `entry.Attempts != nil` 时写回。

## Regression matrix

- Risk 1 active-owner disable：对已 `running` 且 `Attempts` 已填充的 owner，不再向已删除的 `latest.Attempts` 写入，E2E 要求 `cancelled` 且 owner `terminal`。残留是上面的 AttemptID-only P2；`ready` 窗口中 disable 走 IPC 超时后返回 `outcome_unknown`（不确定状态如实），但 session 仍会继续启动（并入 P1）。
- Risk 2 owner ready 独立于 session：`ready` 写在 registration 之后、`execute(launch)` 之前；slow fixture 要求 1s 内返回且 `ProviderSessionID` 为空。发布时机正确，但 `ready` 未接入 follow-up 状态机（P1）。
- Risk 3 Cursor multiline Prompt：`cursor.go:267-277` 在 `--` 后按非空行传递；`cursor_test.go:44-63` 覆盖 `--force is forbidden prompt text`。真实 E2E 以精确 canary 闭合首行截断。
- Risk 4 project Role Package：单一 installer/receipt/registry/`invoke_project_role.ps1`；Router `--role/--task` 只生成 `count=1` step；Role Package 无 Adapter/model；不要求下游 development Skill。
- Risk 5 边界：zero-install 为 `ordinary_delegation`；direct/single/framework/disable 由 smoke 断言；invoke 在 route 失败时禁止 direct fallback；Prompt tamper fail-closed。
- Risk 6 证据分层：fixture 测试与 opt-in `TestRealCursorFrameworkBridgeE2E` 分开；后者走 public launch → observe → completion → technical release；README/指南/2026-08-14 证据均声明 fixture 不计真实 Provider，且 `d68a143` 已撤销。
- Risk 7 仓库边界：`go.mod` 只有 `gopkg.in/yaml.v3`；managed entry 使用 receipt 绝对路径；未发现凭据或 Provider raw stdout 入库。

## Complexity check

最小路径仍是 `role/task → collaboration 一-step plan → core run/attempt → bridge launch/observe → completion → technical release`。新增的安全/耐久层是 schema-v2 receipt、project-role registry 与 business-skill 入口并列、owner `ready` 与 session `running` 分离、disable 收敛跳过已终结 Attempt、Cursor `--` 分行 argv。这些都贴在现有 CLI/owner/adapter 上，没有多余包或间接层。问题是 `ready` 状态只发布、未接到 Executor/IPC，属于未完成的状态机，而不是过度抽象。`install_project_role_package.ps1` 与 `invoke_project_role.ps1` 复制了 business-skill 安装器的 hash/receipt 样板，作为第二条入口可以接受。

## Verdict

BLOCKED
