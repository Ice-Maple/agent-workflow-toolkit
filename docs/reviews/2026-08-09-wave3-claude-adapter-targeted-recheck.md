# Wave 3 Claude Code Adapter targeted re-check

- Reviewer/model: Cursor Agent / cursor-grok-4.5-high
- Adapter: Cursor (`cursor`)
- Execution: read-only `plan` mode with explicit workspace trust
- Initial delivery: status `completed`, reason `terminal_and_final_delivery`, final bytes `492`
- Final delivery: status `completed`, reason `terminal_and_final_delivery`, final source `result.result`, terminal `true`, exit code `0`, final bytes `4172`
- Delivery recovery: same-session core-authorized follow-up used: `true`
- Prompt: sanitized; private prompt text is intentionally not stored in this artifact

- Reviewer/model: Cursor Grok 4.5（本会话独立定向复核；只读）
- Scope and worktree basis: 当前工作区 `internal/adapters/claude/`、`internal/adapters/cli/`、`internal/adapters/cursor/cursor_real_review_test.go`、`cursor_claude_targeted_recheck_test.go`，并对照 `AGENTS.md`、`docs/reviews/2026-08-09-wave3-claude-adapter-review.md`、`docs/reviews/2026-08-09-wave3-claude-adapter-progress.md`、`internal/gateway/gateway.go` 相关 identity/session 冻结路径。结论基于当时可读源码与测试；本会话未执行 `git diff` / `go test`。
- Verdict: PASS

## Executive summary

W3CL-F1 已按原 minimal fix 落地：resume 观测到外来 `session_id` 时，结果仍上报观测身份供 Gateway 对比冻结，本地 `providerSessionID` 保留已验证权威，会话标记 `contaminated`，同 handle 后续 `Send` 直接拒绝。配套回归 `TestMismatchedResumeSessionIsContaminatedWithoutReplacingResumeAuthority` 覆盖该生命周期。正常同会话 Send、ActionID 启动失败回滚与 canary/session 绑定仍完整。Claude Code Adapter review/checkpoint 可以关闭。

## Evidence checklist

| Item | Status | Exact evidence |
|------|--------|----------------|
| W3CL-F1 contaminated resume authority | PASS | `internal/adapters/claude/claude.go:805-827`：仅匹配/初启更新 `s.providerSessionID`；mismatch 设 `s.contaminated=true` 并以观测 session 走 `resultLocked`。`856-868`：结果 `ProviderSessionID`/`Identity["session"]` 用 `observedSessionID`。`550-552`：contaminated 后 Send 拒绝。回归：`claude_test.go:201-230` + helper `324-328`（`--resume` 返回 `claude-foreign-session`）。Cleanup 仍删本地权威键：`669-685`；外来 session 未写入 `a.sessions`。并发：`sendInFlight` + `contaminated` 闸门 `550-557`。 |
| Normal Send / ActionID / canary-session binding | PASS | 同会话 Resume：`claude.go:535-599`；回归 `claude_test.go:150-174`。ActionID 启动失败回滚：`581-606`；回归 `177-198`。canary 错配拒绝并回滚 ActionID：`570-574`。Gateway 仍靠结果 session/identity 冻结：`internal/gateway/gateway.go:687-699`。 |
| Review invocation process | PASS | Opt-in harness：`cursor_claude_targeted_recheck_test.go:11-26`。`runRealCursorReview`：`cursor_real_review_test.go:94-119` 使用 `plan` + `read-only` + `TrustAuthorized`；`129-141` 同会话 core-authorized follow-up；`150-161` required-sections 校验后 staging，prompt 不写入 artifact；`173-194`/`197-217` observe 失败/超时先 cancel，cleanup 前非 terminal 先 cancel。 |

## Finding status

### W3CL-F1 — CLOSED

原缺陷：`contaminated` 路径仍把外来 session 写入本地 resume 权威，后续同 handle 的 core-authorized `Send` 会 `--resume` 错误会话。

现行为：`terminalResult` 仅在 `summary.SessionID` 为空期望或与 `expectedSession` 匹配时更新 `s.providerSessionID`（`claude.go:809-811`）；mismatch 设 `contaminated` 并用观测 session 构造结果（`825-827`、`856-868`）；`Send` 在 `s.contaminated` 时立即失败（`550-552`）。回归断言 Observe=`contaminated`/`session_identity_mismatch`、结果 session=`claude-foreign-session`、本地权威仍=`claude-session-1`、后续 Send 被拒（`claude_test.go:201-230`）。不 reopen。

## Invocation process optimization

非 Adapter 合同缺陷，不阻挡 gate：

1. progress 文档仍写 Claude independent review/checkpoint pending；本复检通过后应标 closed 作为关闭证据。
2. 本会话未跑 `go test ./internal/adapters/claude/`；源码与回归已闭合 F1，补跑仅为旁证。
3. 回归未显式断言 error 文案或 contamination 后 `Cleanup`；Send 拒绝与 Cleanup 按原 `providerSessionID` 删除已覆盖合同行为。

## Gate decision

PASS。可以关闭 Claude Code Adapter review/checkpoint。

最小收尾动作（不阻塞 Adapter PASS）：落盘本定向复核报告；将 `docs/reviews/2026-08-09-wave3-claude-adapter-progress.md` 标为 checkpoint closed；可选补跑 Claude/CLI 包测试。之后可开始下一个 Wave 3 Adapter（Codex CLI）。
