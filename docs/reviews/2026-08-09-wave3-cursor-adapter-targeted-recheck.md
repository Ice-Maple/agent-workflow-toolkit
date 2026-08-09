# Wave 3 Cursor Adapter targeted re-check

- Reviewer/model: Cursor Agent / cursor-grok-4.5-high
- Adapter: Cursor (`cursor`)
- Execution: read-only `plan` mode with explicit workspace trust
- Initial delivery: status `completed`, reason `terminal_and_final_delivery`, final bytes `525`
- Final delivery: status `completed`, reason `terminal_and_final_delivery`, final source `result.result`, terminal `true`, exit code `0`, final bytes `6052`
- Delivery recovery: same-session core-authorized follow-up used: `true`
- Prompt: sanitized; private prompt text is intentionally not stored in this artifact

- Reviewer/model: Cursor Grok 4.5（本会话独立复检；只读）
- Adapter: Cursor (`cursor`)
- Execution: read-only `plan` mode with explicit workspace trust
- Scope and worktree basis: 当前工作区 `internal/adapters/cursor/`、`internal/adapters/cli/`，并核对 `internal/gateway/gateway.go` 对非 terminal / `cancel_unconfirmed` Launch 结果的映射；对照 `docs/reviews/2026-08-09-wave3-cursor-adapter-review.md`、`docs/reviews/2026-08-09-wave3-cursor-adapter-progress.md`、`AGENTS.md`、既有 `docs/reviews/2026-08-09-wave3-cursor-adapter-targeted-recheck.md`。本会话未执行 `git diff`/`go test`；结论基于当时可读源码与测试文件。
- Prompt: sanitized; private prompt text is intentionally not stored in this artifact
- Verdict: PASS

## Executive summary

W3C-F1 至 W3C-F5 的代码修复与对应回归测试均已落地，原 P1（startup-timeout 在 kill 未确认时仍返回 `Terminal=true`/`failed`）已消除。五项 finding 全部 CLOSED。进度文档已将 Cursor checkpoint 标为 closed，且 `config-isolated` 表述已与实现一致（`unsupported`）。Cursor Adapter review/checkpoint 可以保持关闭；下一步可开始 Claude Code Adapter。

## Evidence checklist

| Item | Status | Exact evidence |
|------|--------|----------------|
| W3C-F1 startup-timeout / orphan | PASS | `internal/adapters/cursor/cursor.go:376-383`：`CancelWithCause(DeadlineExceeded)`；`<-turn.done` → `terminalResult`；2s 未完成 → `cancel_unconfirmed`/`startup_timeout_cancel_unconfirmed` + `Terminal=false`。确认路径回归：`cursor_test.go:144-158`（`TestLaunchStartupTimeoutReportsOnlyAfterProcessTermination`）期望 `timed_out`/`process_timeout`/`Terminal=true`。`TimedOut` 证据：`internal/adapters/cli/process.go:233-235`。Gateway 不会将该路径标为 `AttemptFailed`（`internal/gateway/gateway.go:752-766`）；无 session identity 时可能经 `evidenceIncomplete` 落成 `AttemptIncomplete`（`739-744`），仍非误标 failed。 |
| W3C-F2 Send ActionID | PASS | `cursor.go:458-479`：`Args`/`startTurn` 失败均 `delete(sendActions)`。回归：`cursor_test.go:208-231`（`TestSendReleasesActionIDWhenProcessStartFails`）。 |
| W3C-F3 generic `name` | PASS | `internal/adapters/cli/stream.go:148-154`：仅认 `tool_name`/`toolName`，无裸 `"name"`。回归：`stream_test.go:36-40`（`TestStreamSummaryDoesNotTreatGenericNameAsTool`）。 |
| W3C-F4 config-isolated | PASS | Preflight：`cursor.go:150-151` → `unsupported`/`mcp_config_isolation_unsupported`。Launch：`cursor.go:405-407` 非空 allowlist 硬拒。测试：`cursor_test.go:70-76`。进度文档已同步为 `unsupported`。 |
| W3C-F5 transcript refs | PASS | `cursor.go:630-638`、`721-722`：仅 `transcriptWritten` 时追加 `transcript:` ref。回归：`cursor_test.go:234-253`（`TestTranscriptWriteFailureDoesNotEmitTranscriptReference`）。 |
| Review invocation process | PASS | `cursor_real_review_test.go:94-119`：`plan` + `read-only` + `TrustAuthorized`；`150-161`：required-sections 校验后 staging；`129-141`：同会话 core-authorized follow-up；prompt 不写入 artifact；`173-194`/`197-217`：observe 失败/超时先 `cancelCursorReview`，cleanup 前先 probe running state。 |

## Finding status

### W3C-F1 — CLOSED
原缺陷为 startup-timeout 无论 kill 是否确认都返回 `failed`/`Terminal=true`。现行为：确认终止走 `terminalResult`（timeout → `timed_out`/`process_timeout`）；未确认走 `cancel_unconfirmed` + `Terminal=false` + `SideEffectState=unknown`（`cursor.go:376-383`、`710-713`）。回归覆盖确认终止路径（`cursor_test.go:144-158`）。残余：未确认分支无专用自动化测；无 session identity 时 Gateway 可能落成 `AttemptIncomplete` 而非继续 observe / `AttemptCancelUnconfirmed`——语义偏保守，不构成 reopen。

### W3C-F2 — CLOSED
`Send` 在 `Args`/`startTurn` 失败时回滚 `ActionID`（`cursor.go:458-479`）。`TestSendReleasesActionIDWhenProcessStartFails` 验证同一 core `ActionID` 在进程启动失败后可重试（`cursor_test.go:208-231`）。会话 identity 绑定由既有 `TestCoreAuthorizedSendResumesTheVerifiedSession`（`185-205`）覆盖。

### W3C-F3 — CLOSED
`isToolEvent` 已去掉裸 `"name"`，仅认 `tool_name`/`toolName`（`stream.go:148-154`）。带顶层 `name` 的 assistant 终稿仍可 `DeliveryComplete()`（`stream_test.go:36-40`）；tool-only 路径仍拒绝交付（既有 tool-only 测试）。

### W3C-F4 — CLOSED
Preflight 对 `config-isolated` + allowlist 返回 `unsupported`/`mcp_config_isolation_unsupported`（`cursor.go:150-151`）；Launch 对任意非空 `MCPAllowlist` 硬拒（`cursor.go:405-407`）。同一输入下 Preflight 与 Launch 判定一致（`cursor_test.go:70-76`）。进度文档已与实现对齐。

### W3C-F5 — CLOSED
`consumeTurn` 记录 `transcriptWritten`（`cursor.go:630-638`）；`resultLocked` 仅在写盘成功时追加 `transcript:` RawEventRef（`721-722`）。`TestTranscriptWriteFailureDoesNotEmitTranscriptReference` 断言写失败时 `TranscriptWritten=false` 且 refs 无虚假 transcript 引用（`cursor_test.go:234-253`）。

## Invocation process optimization

非 Adapter 合同缺陷，不阻挡 gate：

1. `cleanupCursorReview` 现先 `Observe` probe，非 terminal 时先 `Cancel` 再 `Cleanup`（`cursor_real_review_test.go:206-217`）；observe 失败/超时路径显式调用 `cancelCursorReview`（`181-182`、`189-191`）。
2. 进度文档 Independent review checkpoint 已标 closed，且 `config-isolated` 已写为 `unsupported`；本复检为确认性关闭证据，无需再改合同声明。

## Gate decision

PASS。可以关闭 Cursor Adapter review/checkpoint。

最小收尾动作（不阻塞 Adapter PASS）：可选落盘本确认性报告；本机补跑 `./internal/adapters/cursor/` 与 `./internal/adapters/cli/` 测试作为关闭证据。之后可开始下一个 Wave 3 Adapter（Claude Code）。
