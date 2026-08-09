# Wave 3 Cursor Adapter review

- Reviewer/model: Cursor Agent / cursor-grok-4.5-high
- Adapter: Cursor (`cursor`)
- Execution: read-only `plan` mode with explicit workspace trust
- Delivery: `completed`, reason `terminal_and_final_delivery`, final source `result.result`, terminal `true`, exit code `0`
- Delivery recovery: same-session core-authorized follow-up used: `true`
- Prompt: sanitized; private prompt text is intentionally not stored in this artifact

# Wave 3 Cursor Adapter review

- Reviewer/model: Cursor Grok 4.5 High（本会话独立审阅）
- Scope and worktree basis: 当前工作区 `internal/adapters/cursor/`、`internal/adapters/cli/`、`internal/adapters/artifact/`，以及相关的 `internal/gateway/`、`internal/orchestration/`；对照 `AGENTS.md`、`docs/plans/2026-08-07-multi-agent-orchestration-mvp-waves.md`（Wave 3）、`docs/reviews/2026-08-09-wave2-closure-wave3-handoff.md`、`docs/reviews/2026-08-09-wave3-cursor-adapter-progress.md`、`tests/orchestration/evidence/2026-08-09/W3-cursor-adapter-smoke.md` 与同目录测试。本会话未能执行 `git diff`/`go test`；结论基于当时可读源码与测试文件，而非已提交 SHA。
- Verdict: PASS WITH CONDITIONS

## Executive summary

Wave 3 Cursor 只读 Adapter 切片在 argv-only 进程启动、Windows 无额外控制台窗口、workspace trust / plan·ask 只读 fail-closed、tool-only/`CreatePlan` 不作为最终交付、`delivery_missing` 分类、core-authorized 同会话 Send，以及 `HostActionResult.Reason` 进入 CompletionDecision 等关键合同上，与里程碑要求大体对齐，并有真实 smoke 证据。独立审阅确认 **1 个 P1**（startup-timeout 在 kill 未确认时仍返回 `Terminal=true`/`failed`，可导致孤儿进程与错误 Attempt 终态）和 **4 个 P2**。在修复 P1 并补回归验证前，**不应关闭 Cursor review/checkpoint**；Claude Code / Codex CLI 与 `experimental` 仍属后续范围。

## Coverage ledger

| Review area | Status | Exact evidence |
|-------------|--------|----------------|
| 1. Windows argv-only 启动、hidden-window、stdout/stderr/JSONL、timeout、进程树取消、孤儿进程风险 | FAIL | argv-only：`internal/adapters/cli/process.go:22-30,115-129`；hidden window：`internal/adapters/cli/process_windows.go:10-14`；stream：`internal/adapters/cli/stream.go`；cancel：`internal/adapters/cursor/cursor.go:508-532` 与 `cursor_test.go:140-161`。**P1**：startup-timeout 在 kill 未确认时仍 `Terminal=true`（`cursor.go:374-381`），与同文件 `372` 的未确认语义不一致。 |
| 2. workspace trust、只读 plan/ask、sandbox capability、MCP allowlist/isolation、不支持写路径 fail-closed | PASS WITH CONDITIONS | trust 在 probe 前阻断：`cursor.go:139-141,169`；仅 plan/ask + read-only：`cursor.go:133-138,397-402`；sandbox enabled 拒绝：`cursor.go:142-144`、`cursor_test.go:59-62`；MCP 三分支：`cursor.go:145-156`、`cursor_test.go:64-72`；Invocation 禁危险 flag：`cursor_test.go:29-33`。**P2**：config-isolated=`degraded` 与 Launch 硬拒 allowlist 分裂（`cursor.go:149-150` vs `403-405`）。 |
| 3. session/provider/workspace identity、task canary、contaminated、resume/send 授权、action 复用、cleanup、竞态 | PASS WITH CONDITIONS | `ExtractIdentity`：`cursor.go:822-841`、`cursor_test.go:187-201`；Send 需 core 授权：`cursor.go:423-432,450-455`、`cursor_test.go:150-152,164-184`；contaminated 路径：`cursor.go:681-682`；Cleanup：`cursor.go:535-551`。**缺口**：contaminated resume 无自动化测。**P2**：Send 在 `startTurn` 成功前消耗 ActionID（`cursor.go:456-471`）。 |
| 4. terminal vs final delivery、tool-only/CreatePlan、exit 0 空正文、`delivery_missing`、同会话 delivery-only 恢复 | PASS | 完成条件：`cursor.go:684-690`；`DeliveryComplete`：`stream.go:66-72`；tool-only 拒绝：`stream_test.go:5-16`、`cursor_test.go:124-137`；正常终稿：`stream_test.go:18-27`、`cursor_test.go:109-118`；同会话 Send 恢复：`cursor_test.go:164-184`；真实 smoke：`tests/orchestration/evidence/2026-08-09/W3-cursor-adapter-smoke.md`。 |
| 5. transcript/artifact 边界、canonical workspace、required-section、SHA-256/atomic、Gateway Reason 传播 | PASS WITH CONDITIONS | artifact：`artifact.go:17-75,85-128`、`artifact_test.go`；Reason 字段：`orchestration/model.go:172-174`；HostActionResult 透传：`cursor.go:304-317`；Gateway 写入/回读：`gateway.go:795-799,1388-1398`、`gateway_test.go:315-345`。**P2**：transcript 写失败被忽略但仍写入 RawEventRefs（`cursor.go:621-622,704-705`）。 |
| 6. 测试与真实 smoke 是否足以支撑本切片；文档声明是否匹配实现 | PASS WITH CONDITIONS | 单元/生命周期/Gateway 集成与 opt-in 真实 smoke 存在；进度文档明确 review candidate、非 experimental，与实现一致。Wave 计划要求的 cancel_unconfirmed、orphan、空终稿（非 tool）等矩阵未完全自动化——属残余风险，但不足以单独改判 FAIL。 |

## Findings

### W3C-F1
- Severity: P1
- Status: NEW
- File/line: `internal/adapters/cursor/cursor.go:374-381`
- Evidence: `StartupTimeout` 触发后调用 `Cancel()`，随后无论 `<-turn.done` 还是 `time.After(2 * time.Second)`，均 `s.result("failed", "session_identity_missing", true)`。同文件 launch context 取消未确认路径（`372`）使用 `cancel_unconfirmed` 且 `Terminal=false`。
- Impact / reproduction: 若 2s 内进程未被确认杀死，Launch 仍返回 `failed`+`Terminal=true`。Gateway 对 Launch 的 `failed` 映射为 `AttemptFailed`（`internal/gateway/gateway.go:757-758`），宿主停止 observe，但 Cursor 进程/会话可能仍存活，形成孤儿执行。
- Minimal fix: 仅在 `<-turn.done` 后返回 `Terminal=true`；2s 未完成则返回 `cancel_unconfirmed`（或等价未确认状态）且 `Terminal=false`，`SideEffectState=unknown`。
- Verification: helper 永不输出 `session_id` + 极短 `StartupTimeout`；断言未确认分支的 status/terminal；确认仍可 Cancel/Cleanup。

### W3C-F2
- Severity: P2
- Status: NEW
- File/line: `internal/adapters/cursor/cursor.go:456-471`
- Evidence: `sendActions[request.ActionID]` 在 `Args`/`startTurn` 之前写入；canary 失败会 `delete`（`458`），但 `Args`/`startTurn` 失败不会回滚。
- Impact / reproduction: 进程启动失败后，同一 core `ActionID` 无法重试，delivery-only 恢复被误伤。
- Minimal fix: 在 `startTurn` 成功后再写入 `sendActions`；或失败路径 `delete(s.sendActions, request.ActionID)`。
- Verification: 人为使 `Start` 失败后，同一 `ActionID` 可再次 `Send`。

### W3C-F3
- Severity: P2
- Status: NEW
- File/line: `internal/adapters/cli/stream.go:148-154`
- Evidence: `isToolEvent` 在 `firstString(object, "tool_name", "toolName", "name")` 非空时即判为 tool；`AddLine`（`55-57`）仅在 `event.Text != "" && !event.Tool` 时采纳 `FinalText`。
- Impact / reproduction: 顶层带 `name` 的非 tool `assistant`/`result` 等会被标成 tool，合法终稿被丢弃，误报 `delivery_missing`。
- Minimal fix: 去掉裸 `"name"`；仅认 `tool_name`/`toolName`，或要求 type/content 已表明 tool/function。
- Verification: 增加带 `name` 的非 tool fixture，断言 `DeliveryComplete()` 仍为 true。

### W3C-F4
- Severity: P2
- Status: NEW
- File/line: `internal/adapters/cursor/cursor.go:149-150` 与 `403-405`
- Evidence: Preflight 对 `config-isolated` + allowlist 返回 `degraded`；`normalizeLaunchRequest` 对任何非空 `MCPAllowlist` 直接错误。测试只覆盖 Preflight degraded（`cursor_test.go:69-71`），未对齐 Launch。
- Impact / reproduction: Preflight 暗示可降级继续，Launch 必然失败；与进度文档“config-isolated 显式 degraded”及 fail-closed 合同不一致。
- Minimal fix: 推荐将 Cursor 该分支改为与 Launch 一致的 `unsupported`（CLI 无法验证 isolation）；避免假 degraded 准入。
- Verification: 同一输入下 Preflight 与 Launch 判定一致的测试。

### W3C-F5
- Severity: P2
- Status: NEW
- File/line: `internal/adapters/cursor/cursor.go:621-622` 与 `704-705`
- Evidence: `consumeTurn` 中 `_ = writeTranscript(...)`；`resultLocked` 只要 `TranscriptPath != ""` 就追加 `transcript:<path>`。
- Impact / reproduction: 写盘失败时 RawEventRefs 仍声称 transcript 存在，审计/排障会误判证据完整。
- Minimal fix: 检查 `writeTranscript` 错误；失败时不加入 transcript ref（或标记 `transcript_write_failed`）。
- Verification: 不可写路径下断言 refs 不含虚假 transcript 引用。

## Already addressed / not new

- argv-only 且不注入 `--force` / `--yolo` / `--approve-mcps` / `--sandbox` / `--add-dir`：`cursor_test.go:15-40`。
- Trust 在 version/model probe 前阻断；Windows sandbox enabled unsupported；MCP strict / config-isolated / unsupported 分支：`cursor.go:121-183`、`cursor_test.go:42-98`。
- tool-only `CreatePlan` + `turn_ended:success` 不完成；需 terminal + 非空 final：`stream.go:66-72`、`cursor.go:687-690`、对应测试。
- Send 必须 core-authorized + ActionID；禁止由 exit 0 隐式 follow-up：`cursor.go:423-432`。
- `HostActionResult.Reason`（如 `delivery_missing`）可进入 CompletionDecision：`gateway_test.go:315-345`。
- `KillConfirmed = killConfirmed || cancelRequested`（`process.go:232`）不单独列为 Cursor 缺陷：Cursor `Cancel` 分类使用 `CancelResult.Confirmed`（`cursor.go:522-528`）。
- 真实 smoke 记录 terminal + final，明确 exit 0 单独不足：`W3-cursor-adapter-smoke.md`。
- 文档未将本切片标为 `experimental`：与实现成熟度声明一致。

## Deferred / out of scope

- Claude Code Adapter、Codex CLI Adapter，以及 Wave 4 完整 smoke matrix / `experimental` 验收。
- Provider `modelUsage` 主模型 vs 辅助模型区分：当前 Cursor 探针仅为 `models` 列表，本切片未声称已实现。
- Gateway 自动调用 Adapter.Preflight：Wave 3 保持核心状态机不变；属 Controller 接线，非本切片关闭条件（当前 Gateway 集成测主要走 policy preflight）。
- 非 Windows 平台 `killProcessTree` 不可用：有 `Process.Kill` 回退；本切片主证据面为 Windows。
- 自动化缺口（残余风险，非独立缺陷）：contaminated resume mismatch、`cancel_unconfirmed`、非 tool 的 empty-final、startup-timeout orphan 专用断言。

## Verdict

**PASS WITH CONDITIONS**

本 Cursor Adapter 只读切片可作为 Wave 3 首个外部 CLI 实现候选，但 **独立 review/checkpoint 关闭前必须**：

1. 修复 **W3C-F1（P1）**，并增加回归测试。
2. 处理 **W3C-F2–F5（P2）**（建议同轮至少完成 F2 与 F4；F3/F5 紧随）。
3. 本机补跑 adapters/gateway 相关测试与静态检查后，仅对上述 finding 做 **targeted re-check**。

满足以上条件后，可关闭 Cursor checkpoint，再开始下一个 Wave 3 Adapter（Claude Code）。若 P1 未修，则本切片 **不得** 视为 review/checkpoint 通过。
