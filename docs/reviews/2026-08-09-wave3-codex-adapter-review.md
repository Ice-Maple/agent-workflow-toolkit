# Wave 3 Codex CLI Adapter review

- Reviewer/model: Cursor Agent / cursor-grok-4.5-high
- Adapter: Cursor (`cursor`)
- Execution: read-only `plan` mode with explicit workspace trust
- Initial delivery: status `completed`, reason `terminal_and_final_delivery`, final bytes `638`
- Final delivery: status `completed`, reason `terminal_and_final_delivery`, final source `result.result`, terminal `true`, exit code `0`, final bytes `12873`
- Delivery recovery: same-session core-authorized follow-up used: `true`
- Prompt: sanitized; private prompt text is intentionally not stored in this artifact

- Reviewer/model: Cursor Grok 4.5（本会话独立审阅，只读）
- Scope and worktree basis: 当前工作区 `internal/adapters/codexcli/`、`internal/adapters/cli/`、`internal/adapters/artifact/`，以及相关的 `internal/gateway/`、`internal/orchestration/`；对照 `AGENTS.md`、`docs/plans/2026-08-07-multi-agent-orchestration-mvp-waves.md`（Wave 3）、`docs/reviews/2026-08-09-wave2-closure-wave3-handoff.md`、`docs/reviews/2026-08-09-wave3-codex-adapter-progress.md`、`tests/orchestration/evidence/2026-08-09/W3-codex-adapter-smoke.md` 与同目录测试。本会话未能执行 `git diff`/`go test`；结论基于当时可读源码与测试文件，而非已提交 SHA。
- Verdict: PASS WITH CONDITIONS

## Executive summary

Wave 3 Codex CLI 只读 Adapter 切片在 argv-only 启动、Windows hidden-window、显式 workspace trust、`--sandbox read-only`、`--ask-for-approval untrusted`、durable `--output-last-message`、tool-only/exit 0 空终稿不完成、contaminated 后保留本地 resume 权威（已吸收 W3CL-F1）、Send ActionID 失败回滚、transcript 假引用避免，以及 Gateway launch→observe→CompletionDecision 集成方面，与里程碑要求大体对齐，并已吸收 Cursor W3C-F1–F5 的修复模式。独立审阅确认 **1 个 P1**：空 MCP allowlist 仍可 Preflight/Launch，但 argv 未施加用户配置/MCP 隔离（研究已标明的 `--ignore-user-config` 未使用），未声明 MCP 的任务仍可能看到用户全局 MCP/hooks/skills。另有 **1 个 P2**：真实 smoke 仅覆盖 ephemeral `Invocation.Run`，未覆盖 Adapter Launch/Observe/thread 身份路径。在修复 P1（并处理 P2 证据缺口）前，**不应关闭 Codex review/checkpoint**；`experimental` 仍属后续范围。

## Coverage ledger

| Review area | Status | Exact evidence |
|-------------|--------|----------------|
| 1. Windows argv-only 启动、hidden-window、stdout/stderr/JSONL、`thread.started`、`exec resume`、timeout、进程树取消、`cancel_unconfirmed`、孤儿进程风险 | PASS WITH CONDITIONS | argv-only：`internal/adapters/cli/process.go:22-30,115-129` + Codex `Invocation.Args`：`internal/adapters/codexcli/codex.go:194-215`；hidden window：`internal/adapters/cli/process_windows.go:10-14`；`thread_id`/Codex JSONL：`internal/adapters/cli/stream.go:82-89,114-122,181-182` + `stream_test.go:50-55`；resume argv：`codex.go:204-213` + `codex_test.go:109-134`；startup-timeout 确认路径：`codex.go:658-660,1116-1127` + `codex_test.go:215-224`（`timed_out`/`process_timeout`/`Terminal=true`）；未确认分支：`codex.go:1125-1126`（`cancel_unconfirmed` + `Terminal=false`）。Cancel：`codex.go:847-872`。残余：未确认 cancel 无专用自动化。 |
| 2. workspace 授权、只读 permission、`--sandbox read-only`、`--ask-for-approval untrusted`、model/reasoning、危险 bypass、ephemeral vs resumable、写/非 untrusted 路径 fail-closed | PASS | trust：`codex.go:428-430,695-696`；只读/sandbox/approval Preflight：`codex.go:419-426` + Launch：`683-691`；reasoning：`162-176,200-202` + `codex_test.go:109-134`；禁危险 bypass：`codex_test.go:39-42`；Adapter 拒绝 ephemeral：`codex.go:692-694`；直接 Invocation 允许 ephemeral：`208-210`（smoke 专用）。ModelProbe degraded、无静默 fallback：`codex.go:73,401-402,446-448`。 |
| 3. MCP strict/config-isolated/unsupported、task allowlist、Preflight/Launch 一致性、未请求 MCP 可见性 | FAIL | 三分支与 allowlist 校验：`codex.go:500-514` + `codex_test.go:137-156`；Capabilities `MCP=unsupported`：`72`。**P1**：空 allowlist 可 passed/Launch，但 `Args()`（`194-215`）不传 `--ignore-user-config` 或等价空 MCP 边界，用户全局 MCP/配置可能仍可见。 |
| 4. provider/thread/workspace identity、task canary、contaminated、本地 resume 权威、core-authorized send、ActionID 回滚、并发 follow-up、cleanup、竞态 | PASS | ExtractIdentity：`codex.go:1306-1325`；Send 需 core 授权 + ActionID：`731-733`；contaminated 闸门：`746-748`；权威保留：`1045-1069`（仅匹配/初启更新 `providerSessionID`；mismatch 设 `contaminated` 并以观测 session 上报）；回归：`codex_test.go:252-280`；ActionID 回滚：`778-801,812-816` + `codex_test.go:227-249`；`sendInFlight`：`750-752,771,806`；Cleanup：`879-895`。 |
| 5. terminal vs final、durable `--output-last-message`、结构化 JSONL fallback、tool-only、exit 0 空终稿、provider terminal error、`delivery_missing`、同线程恢复 | PASS | 完成条件：`codex.go:1064-1080`；durable 优先：`963-966`；`DeliveryComplete`：`stream.go:66-72`；tool-only：`codex_test.go:201-213`；Normalize 空终稿/durable/structured：`codex_test.go:45-73`；`providerTerminalError`：`codex.go:1074-1076,1083-1089`；同线程 Send 恢复：`codex_test.go:182-192`；真实 durable smoke 证据：`tests/orchestration/evidence/2026-08-09/W3-codex-adapter-smoke.md`。 |
| 6. durable last-message 路径 freshness/边界、transcript/artifact、canonical workspace、required-section、SHA-256/atomic、Gateway Reason 传播 | PASS WITH CONDITIONS | freshness：`codex.go:312-321,1193-1205`；workspace 内路径：`705-711,1239-1244`；transcript 仅成功写入才入 refs：`982-985,1106-1107`；artifact：`898-900` + `internal/adapters/artifact/artifact.go`；HostActionResult Reason：`577-590`；Gateway 集成：`codex_gateway_test.go:16-84`。残余：transcript 写失败无专用回归（逻辑已正确）；transcript 路径未做 `ensureInside`（与 Cursor/Claude 同型共享缺口）。 |
| 7. 单元/生命周期/Gateway 测试、opt-in 真实 smoke、文档是否匹配 | PASS WITH CONDITIONS | 单元/生命周期/Gateway/contaminated/ActionID 测试存在；progress 明确 review candidate、非 experimental。**P2**：真实 smoke 仅 `Invocation{Ephemeral:true}.Run`（`codex_real_smoke_test.go:30-36`），未覆盖 Adapter Launch/Observe；与 Claude/Cursor Adapter 路径真实 smoke 不对等。 |
| 8. Cursor review 调用路径本身的安全/交付/清理评估 | PASS | `internal/adapters/cursor/cursor_codex_review_test.go`：只读 review harness、required sections、delivery follow-up；与 Adapter findings 分离，不构成本切片阻断项。本审阅在 plan/只读约束下完成，未改仓库。 |

## Findings

### W3CX-F1
- Severity: P1
- Status: NEW
- File/line: `internal/adapters/codexcli/codex.go:194-215`（`Args`）、`500-514`（`validateMCPRequest`）、`428-434` / `698-700`（Preflight/Launch 准入）
- Evidence: Capabilities 声明 `MCP=unsupported`（`72`）。非空 task MCP allowlist 与 `config-isolated` 在 Preflight/Launch 一致 fail-closed（`508-513`，测试 `codex_test.go:149-151`）。但 **空 allowlist + 默认/strict isolation 可通过并 Launch**。`Invocation.Args` 组装 `--ask-for-approval` / `--sandbox` / `--cd` / `--model` / `exec` / `--json` / `--output-last-message`（及可选 resume/ephemeral/reasoning），**从不传 `--ignore-user-config`**。研究文档已将 `--ignore-user-config` 列为 Codex CLI 隔离控件；设计要求未声明 MCP 时使用空/最小集合，且不得默认全量用户 MCP。
- Impact / reproduction: Wave 3 只读任务在“未请求 MCP”前提下仍可能加载用户全局 MCP、hooks 或 skills；read-only sandbox 不能抵消 MCP 侧信道与上下文污染，违反 `AGENTS.md` 的 task/session MCP allowlist 合同。进度文档“allowlist fail-closed”只覆盖显式 allowlist，未覆盖“空集合必须真正为空”。
- Minimal fix: 在 Adapter `Preflight`/`Launch`（及同合同的 Wave 3 `Invocation` 路径）传入已验证的用户配置隔离 flag（首选 `--ignore-user-config`），并用 argv 断言锁定。若该 flag 无法证明禁用 MCP，则空 allowlist 也必须 fail closed，直到存在可证隔离边界。
- Verification: argv 含隔离 flag（或空 allowlist 被拒绝）；现有 MCP allowlist/config-isolated 拒绝测试仍通过；Launch/生命周期与 Gateway 集成仍绿。

### W3CX-F2
- Severity: P2
- Status: NEW
- File/line: `internal/adapters/codexcli/codex_real_smoke_test.go:30-36`；证据：`tests/orchestration/evidence/2026-08-09/W3-codex-adapter-smoke.md`；对比 Adapter 拒绝 ephemeral：`codex.go:692-694`
- Evidence: opt-in 真实 smoke 使用 `Invocation{Ephemeral: true}.Run`，验证 durable last-message 交付。Adapter `Launch` 明确拒绝 ephemeral，要求可 resume 会话。确定性 helper 覆盖 launch/observe/send/contaminated/Gateway，但 **真实 Codex CLI 上的 `thread.started`、非 ephemeral Launch/Observe 合同缺少对等证据**。Claude/Cursor 真实 smoke 均走 `Adapter.Launch`→`Observe`。
- Impact / reproduction: durable 专项风险已实测，但 checkpoint 对“真实 launch/observe/terminal/completion”的 Adapter 路径证据弱于同 Wave 兄弟切片；resume/thread 身份在真实 CLI 上未被 smoke 钉死。
- Minimal fix: 增加 opt-in Adapter Launch/Observe 真实 smoke（非 ephemeral、read-only/untrusted、断言 thread 身份与终稿），并更新 evidence；可保留现有 ephemeral Invocation 作为 durable 专项。
- Verification: evidence 记录 session/thread 身份、terminal、final source；现有确定性生命周期与 Gateway 测试仍通过。

## Already addressed / not new

- Cursor W3C-F1 startup-timeout 在 kill 未确认时误报 `Terminal=true`/`failed`：Codex `cancelledLaunchResult` 未确认分支为 `cancel_unconfirmed` + `Terminal=false`（`codex.go:1125-1126`）；确认路径回归期望 `timed_out`/`process_timeout`（`codex_test.go:215-224`）。
- W3C-F2 Send ActionID 在 start 失败后不可重试：Codex `rollbackSend`（`778-801,812-816`）+ `TestSendReleasesActionIDWhenProcessStartFails`。
- W3C-F3 裸 `"name"` 误判为 tool：共享 `isToolEvent` 仅认 `tool_name`/`toolName`（`stream.go:148-154`）+ `stream_test.go` 回归；Codex `item.completed`/`thread_id` 识别已加（`stream.go:114-122`、`stream_test.go:50-55`）。
- W3C-F4 Preflight/Launch MCP 分裂：Codex 对 allowlist/config-isolated 两侧一致 unsupported（`500-514`、`698-700`）。
- W3C-F5 transcript 写失败仍发假 ref：仅 `transcriptWritten` 时追加 ref（`1106-1107`）。
- W3CL-F1 contaminated 覆盖本地 resume 权威：仅匹配/初启更新 `providerSessionID`（`1048-1050`）；mismatch 设 `contaminated` 并以观测 session 构造结果（`1067-1069,1109-1113`）；Send 拒绝（`746-748`）；回归 `TestMismatchedResumeSessionIsContaminatedWithoutReplacingAuthority`（`codex_test.go:252-280`）。
- argv 不注入 `--force` / `--dangerously-bypass`（`codex_test.go:39-42`）。
- Trust、只读 sandbox、untrusted approval 在 probe/Launch 前阻断；tool-only 与空终稿不完成；需 thread + terminal + 非空 final（`codex.go`/`codex_test.go`/`stream.go`）。
- Send 必须 core-authorized + ActionID；禁止由 exit 0 隐式 follow-up（`731-733`）。
- 真实 smoke 记录 durable last-message 完成，明确 exit 0 单独不足（`W3-codex-adapter-smoke.md`）。
- 文档未将本切片标为 `experimental`：与实现成熟度声明一致。

## Deferred / out of scope

- Cursor/Claude Adapter 本体、Wave 4 完整 smoke matrix / `experimental` 验收。
- Gateway 不识别结果字符串 `status=="contaminated"`，依赖 identity/session/canary 字段冻结：属核心既有行为；本切片以 Adapter 正确区分“观测身份（上报）”与“已验证权威（本地保留）”为前提。
- 自动化缺口（残余风险，非独立于 F1/F2 的阻断项）：`cancel_unconfirmed` 未确认分支专用断言、transcript 写失败专用回归、`empty-delivery` helper 未挂 Adapter 测试、Send 空 canary 与兄弟 Adapter 同型宽松、Cancel API 专用测试、transcript 路径 `ensureInside` 共享缺口。
- 本会话未跑 `go test -race` / `go vet` / `git diff --check`；修复后 targeted re-check 应补跑 `internal/adapters/codexcli` 与相关 cli/gateway 测试。

## Verdict

**PASS WITH CONDITIONS**

本 Codex CLI Adapter 只读切片可作为 Wave 3 第三个外部 CLI 实现候选，但 **独立 review/checkpoint 关闭前必须**：

1. 修复 **W3CX-F1（P1）**：为未声明 MCP 的 Launch/Preflight 路径施加已验证的用户配置/MCP 隔离（或在无法证明隔离时 fail closed），并增加回归。
2. 处理 **W3CX-F2（P2）**：补齐 Adapter Launch/Observe 真实 smoke 与 evidence（可保留现有 ephemeral durable 专项）。
3. 仅对上述 finding 及受影响的 MCP/argv 与 smoke 证据轴做 **targeted re-check**。

满足以上条件后，可关闭 Codex checkpoint，再进入跨 Adapter 硬化 / Wave 4。若 P1 未修，则本切片 **不得** 视为 review/checkpoint 通过。
