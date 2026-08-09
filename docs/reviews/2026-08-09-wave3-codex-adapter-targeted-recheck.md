# Wave 3 Codex CLI Adapter targeted re-check

- Reviewer/model: Cursor Agent / cursor-grok-4.5-high
- Adapter: Cursor (`cursor`)
- Execution: read-only `plan` mode with explicit workspace trust
- Initial delivery: status `completed`, reason `terminal_and_final_delivery`, final bytes `361`
- Final delivery: status `completed`, reason `terminal_and_final_delivery`, final source `result.result`, terminal `true`, exit code `0`, final bytes `6001`
- Delivery recovery: same-session core-authorized follow-up used: `true`
- Prompt: sanitized; private prompt text is intentionally not stored in this artifact

- Reviewer/model: Cursor Grok 4.5（本会话独立定向复检；只读）
- Scope and worktree basis: 当前工作区 `internal/adapters/codexcli/`、`internal/adapters/cli/`、`internal/adapters/cursor/cursor_codex_review_test.go`、`cursor_codex_targeted_recheck_test.go`、`cursor_real_review_test.go`，对照 `AGENTS.md`、`docs/reviews/2026-08-09-wave3-codex-adapter-review.md`、`docs/reviews/2026-08-09-wave3-codex-adapter-progress.md`、`tests/orchestration/evidence/2026-08-09/W3-codex-adapter-smoke.md` 与 Gateway 集成测试。结论基于当时可读源码、测试与证据文件；本会话未执行 `git diff` / `go test`。
- Verdict: PASS

## Executive summary

W3CX-F1 与 W3CX-F2 均已按原 minimal fix 落地：空 MCP allowlist 路径在 Invocation/Launch/Send 固定传入 `--ignore-user-config`；自定义 provider 路由保持 argv-only 且拒绝 URL 内嵌凭据；非空 allowlist/`config-isolated` 仍在 Preflight/Launch 一致 fail-closed。真实 smoke 在保留 ephemeral durable 专项之外，补齐了非 ephemeral Adapter Preflight→Launch→thread→Observe→durable 终稿与 cleanup 证据。既有 contaminated、ActionID 回滚、terminal/final、transcript refs 与 Gateway 映射仍完整。Codex CLI Adapter review/checkpoint 可以关闭。

## Evidence checklist

| Item | Status | Exact evidence |
|------|--------|----------------|
| W3CX-F1 user-config / MCP isolation | PASS | `internal/adapters/codexcli/codex.go:258-261`：`Args` 在 `exec`/`exec resume` 后固定 `--ignore-user-config`。Launch 经同一 `Args`：`666-679`；Send resume：`843-856`。Capabilities `MCP=unsupported`：`73`。`validateMCPRequest`：`557-571`（非空 allowlist → `mcp_unsupported`；`config-isolated` → `mcp_config_isolation_unsupported`）。Preflight：`488-490`；Launch：`759-761`。argv 回归：`codex_test.go:15-42`、`109-134`、`137-174`（含 `--ignore-user-config` 与嵌入凭据拒绝）。MCP allowlist Preflight 拒绝：`177-196`。证据：`W3-codex-adapter-smoke.md` 声明 user config ignored、无 task MCP allowlist。 |
| W3CX-F2 Adapter lifecycle real smoke | PASS | `codex_real_smoke_test.go:53-120`：`TestRealCodexAdapterLifecycleSmoke`（非 ephemeral）Preflight→Launch→Observe→`terminal_and_final_delivery`/`ADAPTER_SMOKE_OK`；`110-114` defer Cleanup；`122-148` observe 失败/超时先 Cancel。保留 ephemeral durable：`15-50`。证据：`W3-codex-adapter-smoke.md` Real Adapter lifecycle 段（`launch_thread_present`、`durable_last_message`、`transcript`）。 |
| Previous regression safeguards | PASS | contaminated 不覆盖本地权威：`codex.go:1110-1134`、`807-809`；回归 `codex_test.go:292-320`。ActionID 启动失败回滚：`877-881`、`838-866`；回归 `267-289`。tool-only/空终稿：`241-252`、`52-64`；transcript 仅成功写入入 refs：`1171-1172`。Gateway Launch→Observe→Completion：`codex_gateway_test.go:16-84`。stream `thread_id`/`isToolEvent`：`stream.go:85`、`148-154`。 |
| Review / smoke invocation process | PASS | Review harness：`cursor_real_review_test.go:94-119` `plan`+`read-only`+`TrustAuthorized`；`129-141` 同会话 core-authorized follow-up；`150-161` required-sections 后 staging；`173-217` observe 失败/超时先 cancel，cleanup 前非 terminal 先 cancel。Codex recheck harness：`cursor_codex_targeted_recheck_test.go:11-26`。Smoke 可用非 secret provider route（`codex_real_smoke_test.go:150-156`）；证据不落凭据/端点。 |

## Finding status

### W3CX-F1 — CLOSED

原缺陷：空 allowlist 可 Preflight/Launch，但 argv 未施加用户配置/MCP 隔离，全局 MCP/hooks/skills 仍可能可见。

现行为：`Invocation.Args` 始终追加 `--ignore-user-config`（`codex.go:258-261`），Launch 与 Send 共用该路径。自定义 provider 以 `--config` argv 注入 name/base_url/wire_api/auth 标志（`243-252`）；`validateProviderConfig` 要求绝对 URL 且 `parsed.User == nil`（`191-216`）。非空 allowlist 与 `config-isolated` 仍由共享 `validateMCPRequest` 在 Preflight/Launch fail-closed（`488-490`、`759-761`、`565-570`）。argv/provider/MCP allowlist 回归已锁（`codex_test.go:15-42`、`137-196`）。不 reopen。

### W3CX-F2 — CLOSED

原缺陷：真实 smoke 仅 ephemeral `Invocation.Run`，未覆盖 Adapter Launch/Observe/thread 身份路径。

现行为：新增 opt-in `TestRealCodexAdapterLifecycleSmoke`（`codex_real_smoke_test.go:53-120`）覆盖非 ephemeral Preflight、Launch、thread 身份、Observe、durable 终稿；observe 失败/超时先 Cancel（`129-144`）；成功路径 defer Cleanup（`110-114`）。保留 `TestRealCodexDeliverySmoke` ephemeral durable 专项（`15-50`）。证据文件已记录 lifecycle 结果（`W3-codex-adapter-smoke.md`）。不要求真实 Send follow-up。不 reopen。

## Invocation process optimization

非 Adapter 合同缺陷，不阻挡 gate：

1. Cursor review 调用路径已具备 plan/read-only/trust、sanitized prompt/report、required-sections、同会话 delivery follow-up、observe 失败先 cancel；无新的安全阻断项。
2. Codex 真实 smoke 通过显式非 secret provider route 规避全局 config 依赖，与 `--ignore-user-config` 一致；证据未暴露凭据。残余优化：补一条专用 `config-isolated` Preflight/Launch 单测（逻辑已在共享校验中 fail-closed）、本机补跑 deterministic `go test ./internal/adapters/codexcli/` 作为旁证；progress 文档仍写 independent review pending，关闭后应标 closed。

## Gate decision

PASS。可以关闭 Codex CLI Adapter review/checkpoint。

最小收尾动作（不阻塞 Adapter PASS）：将本复检报告落盘为关闭证据；更新 progress 将 independent review/targeted closure 标为 closed；可选补跑 `./internal/adapters/codexcli/` 与相关 cli/gateway 测试。之后可进入跨 Adapter 硬化 / Wave 4；`experimental` 仍属后续范围。
