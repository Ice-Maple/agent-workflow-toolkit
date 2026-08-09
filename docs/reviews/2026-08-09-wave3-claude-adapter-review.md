# Wave 3 Claude Code Adapter review

- Reviewer/model: Cursor Agent / cursor-grok-4.5-high
- Adapter: Cursor (`cursor`)
- Execution: read-only `plan` mode with explicit workspace trust
- Initial delivery: status `completed`, reason `terminal_and_final_delivery`, final bytes `598`
- Final delivery: status `completed`, reason `terminal_and_final_delivery`, final source `result.result`, terminal `true`, exit code `0`, final bytes `9605`
- Delivery recovery: same-session core-authorized follow-up used: `true`
- Prompt: sanitized; private prompt text is intentionally not stored in this artifact

- Reviewer/model: Cursor Grok 4.5 High（本会话独立审阅，只读）
- Scope and worktree basis: 当前工作区 `internal/adapters/claude/`、`internal/adapters/cli/`、`internal/adapters/artifact/`，以及相关的 `internal/gateway/`、`internal/orchestration/`；对照 `AGENTS.md`、`docs/plans/2026-08-07-multi-agent-orchestration-mvp-waves.md`（Wave 3）、`docs/reviews/2026-08-09-wave2-closure-wave3-handoff.md`、`docs/reviews/2026-08-09-wave3-claude-adapter-progress.md`、`tests/orchestration/evidence/2026-08-09/W3-claude-adapter-smoke.md` 与同目录测试。本会话未能执行 `git diff`/`go test`；结论基于当时可读源码与测试文件，而非已提交 SHA。
- Verdict: PASS WITH CONDITIONS

## Executive summary

Wave 3 Claude Code 只读 Adapter 切片在 argv-only 启动、Windows hidden-window、显式 workspace trust、`permission-mode plan`、`--strict-mcp-config`、stream-json 终稿门禁、startup-timeout 未确认取消、Send ActionID 失败回滚、transcript 假引用避免，以及 Gateway launch→observe→CompletionDecision 集成与真实 smoke 方面，与里程碑要求大体对齐，并已吸收 Cursor W3C-F1–F5 的修复模式。独立审阅确认 **1 个 P1**：`contaminated` 路径在判定 session mismatch 后仍把会话上已验证的 `providerSessionID` 重写为外来 session，后续同 handle 的 core-authorized `Send` 会 `--resume` 错误会话。在修复 P1 并补回归验证前，**不应关闭 Claude review/checkpoint**；Codex CLI 与 `experimental` 仍属后续范围。

## Coverage ledger

| Review area | Status | Exact evidence |
|-------------|--------|----------------|
| 1. Windows argv-only 启动、hidden-window、stdout/stderr/JSONL、timeout、进程树取消、孤儿进程风险 | PASS WITH CONDITIONS | argv-only：`internal/adapters/claude/claude.go:321-364` + `internal/adapters/cli/process.go:115-129`；hidden window：`internal/adapters/cli/process_windows.go:10-14`；stream：`internal/adapters/cli/stream.go`；startup-timeout 未确认：`claude.go:477-484`（`cancel_unconfirmed` + `Terminal=false`）；确认超时回归：`claude_test.go:135-148`；Cancel：`claude.go:633-658`、`claude_test.go:201-217`。 |
| 2. workspace trust、只读 plan、reasoning/effort、危险 bypass、模型登记限制、写路径 fail-closed | PASS | trust 在 probe 前阻断：`claude.go:148-150,164`、`claude_test.go:62-65`；只读：`claude.go:142-144,331-333,505-507`、`claude_test.go:67-70`；reasoning：`claude.go:207-218,145-147`、`claude_test.go:72-75`；禁危险/扩 scope flag：`claude_test.go:33-38`；ModelProbe degraded 且无 fallback：`claude.go:75,125-128,169-172`。 |
| 3. strict/config-isolated/unsupported MCP、task config 边界、exact allowlist、未请求 MCP 可见性 | PASS | 三分支：`claude.go:220-246`、`claude_test.go:77-97`；`--strict-mcp-config` + 可选 `--mcp-config`：`claude.go:344-359`；exact allowlist + workspace 内绝对路径/symlink：`claude.go:264-304`。Claude 用 CLI 原生 strict MCP 支撑 config-isolated，相对 Cursor 的 unsupported 是合理分化。 |
| 4. provider/session/workspace identity、task canary、contaminated、resume/send、action 复用、cleanup、竞态 | FAIL | ExtractIdentity：`claude.go:976-994`、`claude_test.go:236-250`；Send 需 core 授权 + ActionID：`claude.go:535-537`、`claude_test.go:150-174`；ActionID 启动失败回滚：`claude.go:581-588,598-603`、`claude_test.go:177-198`；Cleanup：`claude.go:665-681`。**P1**：contaminated 后 `resultLocked` 仍写入外来 session（`claude.go:813-815,851-853`），Send 据此 `--resume`（`claude.go:554-578`）。 |
| 5. terminal vs final delivery、assistant/result 归一化、tool-only、exit 0 空终稿、provider terminal error、delivery_missing、同会话恢复 | PASS | 完成条件：`claude.go:810-825`；`DeliveryComplete`：`stream.go:66-72`；tool-only：`claude_test.go:123-132`；`providerTerminalError`：`claude.go:819-821,828-834`；同会话 Send 恢复：`claude_test.go:150-174`；真实 smoke：`tests/orchestration/evidence/2026-08-09/W3-claude-adapter-smoke.md`。 |
| 6. transcript/artifact 边界、canonical workspace、required-section、SHA-256/atomic、Gateway Reason 传播 | PASS | transcript 仅成功写入才入 refs：`claude.go:746-754,848-849`、`claude_test.go:219-233`；artifact：`claude.go:684-686` + `internal/adapters/artifact/artifact.go:17-75,85-128`；HostActionResult Reason：`claude.go:402-415`；Gateway 集成：`claude_gateway_test.go:16-84`；Gateway Reason 字段：`internal/gateway/gateway.go:795-799`。 |
| 7. 测试、真实 smoke、文档是否匹配；共享 Cursor/CLI review 调用风险 | PASS WITH CONDITIONS | 单元/生命周期/Gateway/opt-in real smoke 存在；progress 明确 review candidate、非 experimental。Wave 计划要求的 contaminated resume lifecycle 自动化缺失——与 W3CL-F1 直接相关。共享 review harness：`internal/adapters/cursor/cursor_claude_review_test.go`（只读 plan，非本切片阻断项）。 |

## Findings

### W3CL-F1
- Severity: P1
- Status: NEW
- File/line: `internal/adapters/claude/claude.go:813-815` 与 `851-853`（Send 消费点：`554-578`）
- Evidence: `terminalResult` 在 `turn.expectedSession != "" && summary.SessionID != turn.expectedSession` 时返回 `contaminated`/`session_identity_mismatch`，但同一路径调用的 `resultLocked` 仍执行 `if summary.SessionID != "" { s.providerSessionID = summary.SessionID }`，用外来 session 覆盖已验证身份。随后 `Send` 读取 `s.providerSessionID` 组装 `--resume`。
- Impact / reproduction: Launch 建立 session A 后，core-authorized resume 若观测到 session B，Observe 报 contaminated，但 Adapter 本地权威变为 B；同 handle 再发 Send 会 resume B，违反 resume authority 与 “identity mismatch → contaminated，非普通 retry” 合同。Gateway 可能因结果中的 session 字段冻结 Attempt，但 Adapter 边界仍被破坏。
- Minimal fix: `ExecutionResult`/`HostActionResult` 继续上报观测到的 session（供 Gateway 对比冻结）；会话内 `s.providerSessionID` 仅在匹配已验证身份时更新；mismatch 时保留原值并标记 session contaminated；`Send` 在 contaminated 后直接拒绝。
- Verification: helper/`--resume` 返回不同 `session_id`；断言 Observe=`contaminated`、本地 session 仍为原 ID、后续 Send 被拒绝；现有 Send 成功路径与 Gateway session-mismatch 冻结行为仍通过。

## Already addressed / not new

- Cursor W3C-F1 startup-timeout 在 kill 未确认时误报 `Terminal=true`/`failed`：Claude Launch 未确认分支为 `cancel_unconfirmed` + `Terminal=false`（`claude.go:477-484`），确认路径回归期望 `timed_out`/`process_timeout`（`claude_test.go:135-148`）。
- W3C-F2 Send ActionID 在 start 失败后不可重试：Claude `rollbackSend`（`claude.go:581-588,598-603`）+ `TestSendReleasesActionIDWhenProcessStartFails`。
- W3C-F3 裸 `"name"` 误判为 tool：shared `isToolEvent` 仅认 `tool_name`/`toolName`（`stream.go:148-154`）+ `stream_test.go` 回归。
- W3C-F4 Cursor config-isolated Preflight/Launch 分裂：Claude 以 `--strict-mcp-config` 正当支持 config-isolated，与 Launch 一致（`claude.go:220-223,344-349`、`claude_test.go:91-97`）。
- W3C-F5 transcript 写失败仍发假 ref：Claude 仅 `transcriptWritten` 时追加 ref（`claude.go:848-849`、`claude_test.go:219-233`）。
- argv 不注入 `--dangerously-skip-permissions` / `--allow-dangerously-skip-permissions` / `--fallback-model` / `--add-dir` / `--no-session-persistence`（`claude_test.go:33-38`）。
- Trust 在 version probe 前阻断；只读 plan；tool-only 不完成；需 terminal + 非空 final（`claude.go`/`claude_test.go`/`stream.go`）。
- Send 必须 core-authorized + ActionID；禁止由 exit 0 隐式 follow-up（`claude.go:528-537`）。
- 真实 smoke 记录 terminal + final，明确 exit 0 单独不足（`W3-claude-adapter-smoke.md`）。
- 文档未将本切片标为 `experimental`：与实现成熟度声明一致。

## Deferred / out of scope

- Codex CLI Adapter，以及 Wave 4 完整 smoke matrix / `experimental` 验收。
- `--bare` / `--setting-sources ""` 等用户 settings/hooks 进一步隔离：研究文档有建议，但非本切片关闭条件；当前边界依赖 plan + strict MCP + 可选 `--tools=`。
- Gateway 不识别结果字符串 `status=="contaminated"`，依赖 identity/session/canary 字段冻结：属核心既有行为；本切片以 Adapter 正确区分“观测身份（上报）”与“已验证权威（本地保留）”为前提。
- 自动化缺口（残余风险，非独立于 F1 的阻断项）：`cancel_unconfirmed` 未确认分支专用断言、非 tool 的 empty-final 专用 helper、Send 空 canary 与 Cursor 同型宽松。
- 本会话未跑 `go test -race` / `go vet` / `git diff --check`；修复后 targeted re-check 应补跑 adapters/claude 与相关 gateway 测试。

## Verdict

**PASS WITH CONDITIONS**

本 Claude Code Adapter 只读切片可作为 Wave 3 第二个外部 CLI 实现候选，但 **独立 review/checkpoint 关闭前必须**：

1. 修复 **W3CL-F1（P1）**，并增加 contaminated resume 生命周期回归。
2. 仅对上述 finding 及受影响的 identity/send 轴做 **targeted re-check**。

满足以上条件后，可关闭 Claude checkpoint，再开始下一个 Wave 3 Adapter（Codex CLI）。若 P1 未修，则本切片 **不得** 视为 review/checkpoint 通过。
