# Wave 4 cross-Adapter hardening review

- Reviewer/model: Cursor Agent / cursor-grok-4.5-high
- Adapter: Cursor (`cursor`)
- Execution: read-only `plan` mode with explicit workspace trust
- Initial delivery: status `completed`, reason `terminal_and_final_delivery`, final bytes `18525`
- Final delivery: status `completed`, reason `terminal_and_final_delivery`, final source `result.result`, terminal `true`, exit code `0`, final bytes `18525`
- Delivery recovery: same-session core-authorized follow-up used: `false`
- Prompt: sanitized; private prompt text is intentionally not stored in this artifact

- Scope and worktree basis: 本会话独立只读取证当前 worktree 中 Wave 4 baseline 相关实现与证据（会话起始 `git status`：`internal/adapters/cli/process.go`、`process_test.go`、`replay_test.go`、`stream_benchmark_test.go`、`internal/adapters/artifact/artifact_benchmark_test.go`、三家 Adapter `consumeTurn`/终态分类与 Cursor 对齐单测、`cursor_wave4_review_test.go`、`cursor_real_review_test.go` 的 `normalizeReviewBody`、`tests/orchestration/conformance_test.go`、`fixtures/wave4/replay/*`、`tests/orchestration/evidence/2026-08-10/*`、相关 docs）。对照 `AGENTS.md`、`README.md`、`docs/plans/2026-08-07-multi-agent-orchestration-mvp-waves.md`、`docs/reviews/2026-08-10-wave4-baseline.md`、`W4-baseline.md`、`W4-smoke-matrix.md`、`W4-benchmark-baseline.md`、`tests/orchestration/README.md`。既有 `docs/reviews/2026-08-10-wave4-cursor-review.md` 仅作 prior-run；其 finding/verdict/旧 hash 叙述不自动继承。本会话 Shell 连续被拒，未独立执行 `git diff`/`go test`/未独立重算 artifact SHA；作者侧 `W4-baseline.md` 命令与 hash 记录视为作者证据，不作本会话复验。
- Verdict: PASS WITH CONDITIONS

## Scope and evidence

本切片实际落地（以当前源码与测试文件为准）：

1. 共享 CLI runner 以自定义 `outputCollector` 替代 `StdoutPipe`；`cmd.Wait()` 后 `flush` 再 `finish`/`Finished`
2. Cursor / Claude / Codex CLI 的 `consumeTurn` 在 `Finished` 分支内最终 `drainTurnOutput`，再 `Wait()` / 终态分类
3. 六份 JSONL replay fixtures + `TestWave4StreamReplayMatrix`（`ParseJSONL` / `DeliveryComplete`）
4. 三外部 CLI fixture-process lifecycle conformance（非真实 Provider smoke）
5. Cursor 与 Claude/Codex 对齐：`sendInFlight`、contamination latch、缺 session → `session_identity_missing`、`providerTerminalError`、`cancel_unconfirmed` → `SideEffectState=unknown`
6. review staging：`normalizeReviewBody` + `TestNormalizeReviewBodyRemovesDuplicateHarness`；基线 / smoke matrix / 初始 benchmark 证据与导航；opt-in `cursor_wave4_review_test.go`

不以 fixture conformance 计为真实 Provider smoke，也不将本切片标为 `experimental`。

Real smoke 诊断：`cursor_real_smoke_test.go` / `claude_real_smoke_test.go` / `codex_real_smoke_test.go` 的 `t.Logf` 仅打 status/reason/source/identity/terminal/size 类元数据（Codex delivery 路径含 `stdout_bytes` 长度，非正文）。`W4-smoke-matrix.md` 将 Codex CLI 记为 Provider authorization BLOCKED，未伪装 PASS。

当前 prior review 产物文件顶部仅一套 staging harness（`- Reviewer/model:` / `- Adapter:` 各 1 次，行 3–4）。`W4-baseline.md` 记录本次 prior 产物为 `final_bytes=18232`、artifact `18195` bytes / SHA-256 `797ffac371fd2fabe6427b125ec31fd933e3a7f70d8547fa2293cef34249ce67`；prior 正文内关于 `18552`/`a5fd…` 的叙述已过期，不以该旧数字作证据。

## Coverage ledger

| Area | Status | Evidence |
| --- | --- | --- |
| 1. Shared Windows CLI runner hardening | PASS | `internal/adapters/cli/process.go:21-23` argv-only；`process.go:143-157` collector + Wait 后 flush 再 `finish`；`process.go:220-241` 无换行终行 flush；Cursor/Claude/Codex `consumeTurn` 在 Finished 后最终 drain（`cursor.go:623-626`、`claude.go:736-741`、`codex.go:1005-1010`）。`TestRunnerDrainsAllFastOutputLinesBeforeFinished`（`process_test.go:51-71`）与 `TestRunnerFlushesUnterminatedOutputBeforeFinished`（`process_test.go:74-92`，helper `unterminated` at `110-111`）锁住主修复。进程树/长日志/atomic-replace 恢复仍属后续退出项 |
| 2. JSONL replay / delivery semantics | PASS | 六 fixtures 与 `replay_test.go:10-56`；`stream.go:66-72` 保证 tool-only / provider-error（`is_error`）保持 incomplete；exit 0 与 terminal alone 不能单独晋升 |
| 3. Cross-Adapter lifecycle conformance | PASS | `conformance_test.go:35-87` happy-path preflight→launch→session continuity→observe→cleanup；默认 Cursor Probe 含 `Environment`（`cursor.go:166-167`）；fixture process 非 real smoke，`W4-smoke-matrix.md` 仍权威 |
| 4. Identity / contamination（Cursor Adapter） | PASS | `cursor.go:447-449` Send contaminated 闩；`cursor.go:451-454`/`472-488` `sendInFlight`；`cursor.go:702-721` mismatch 不写本地权威并 latch，返回证据暴露 observed foreign session；`cursor.go:716-717` 缺 session → `session_identity_missing`；`cursor.go:723-724` `providerTerminalError`；`cursor.go:540-542` `cancel_unconfirmed` → `SideEffectState=unknown`；`cursor_test.go:208-335` 对应回归。Claude/Codex 同构实现 + contamination 单测 |
| 5. Evidence and documentation | PASS | README/`agent-capabilities`/baseline/W4-* 均未宣称 experimental 已接受；lifecycle 写 session continuity；`wave4-baseline.md:30` 与 `W4-smoke-matrix.md:18` 指向 Codex BLOCKED；smoke 诊断已脱敏；staging normalize 已落地（见 W4-IR1） |
| 6. Wave 4 exit criteria | DEFERRED | 完整四路径 smoke / Windows 稳定性 / 策略 / 跨路径 contamination·恢复 / 完整 benchmark / Native 同构 / `go test -race` / 人工 experimental 仍 OPEN；属后续退出条件，非本 baseline 切片 FAIL |

## Findings

### W4-R7 — [P1] Cursor contamination 后仍把外来 session 写入本地 resume 权威，且 Send 无 contaminated 闩
- **status:** ALREADY ADDRESSED
- **file/line:** `internal/adapters/cursor/cursor.go:447-449`；`cursor.go:702-721`；`cursor_test.go:303-335`；对照 `claude.go:550-552`、`816-834`；`codex.go:801-803`、`1114-1135`
- **evidence:** 当前 Cursor 仅在 match/初启时更新 `s.providerSessionID`；mismatch 设 `s.contaminated = true` 并返回 `contaminated`/`session_identity_mismatch`；返回的 `ProviderSessionID`/`Identity` 可暴露 observed foreign session，本地权威仍为 `cursor-session-1`；`Send` 拒绝 contaminated 会话。单测断言三者。
- **impact:** 单 Adapter 实现+单测已闭合。不等于 Wave 4「跨 Adapter contamination/恢复」退出项整体闭合。
- **minimal fix:** 无需。
- **verification:** `TestMismatchedResumeSessionIsContaminatedWithoutReplacingResumeAuthority`。

### W4-R1 — [P1] Adapter `consumeTurn` 在 `Finished` 后不做最终 drain
- **status:** ALREADY ADDRESSED
- **file/line:** `cursor.go:623-626`；`claude.go:736-741`；`codex.go:1005-1010`
- **evidence:** 三家均在 `case <-Finished()` 内先 `drainTurnOutput` 再 `Wait()` / 终态处理；与 runner「flush 后关 Finished」合同衔接。
- **impact:** 原间歇性 `terminal_event_missing` / `delivery_missing` 消费路径缺口已闭合。
- **minimal fix:** 无需。
- **verification:** runner burst/unterminated 单测 + conformance。

### W4-R2 — [P2] Cursor 默认 Preflight Probe 不传 `Environment`
- **status:** ALREADY ADDRESSED
- **file/line:** `cursor.go:166-167`；`conformance_test.go:90-109`
- **evidence:** 默认 Probe 已传入 `a.Environment`；conformance 仅设 `adapter.Environment`。
- **impact:** Preflight 与 Launch 环境传播一致。
- **minimal fix:** 无需。
- **verification:** 默认 Probe + fixture Environment 路径已由 conformance 覆盖。

### W4-R3 — [P2] 基线/证据把 lifecycle 写成含「provider identity」，实际只断言 session 连续性
- **status:** ALREADY ADDRESSED
- **file/line:** `docs/reviews/2026-08-10-wave4-baseline.md:20-22`；`W4-baseline.md:25-34`；`conformance_test.go:77-82`
- **evidence:** 当前措辞为 session continuity；断言为 sessionID launch↔observe 一致与终稿 token。
- **impact:** 不再高估 identity 覆盖。
- **minimal fix:** 无需。
- **verification:** 文档与 conformance 断言对齐。

### W4-R6 — [P2] 声称的无换行终行 `flush` 未被回归测试锁住
- **status:** ALREADY ADDRESSED
- **file/line:** `process_test.go:74-92`、`110-111`；对照 `process.go:150-157`、`220-241`
- **evidence:** `TestRunnerFlushesUnterminatedOutputBeforeFinished` 使用无换行 helper 输出 `CLI_UNTERMINATED`，在 `Finished` 后断言可见。
- **impact:** collector 主修复与 flush 防御均有回归锁。
- **minimal fix:** 无需。
- **verification:** 定向 `go test ./internal/adapters/cli`（作者证据；本会话未复跑）。

### W4-IR3 — [P2] Cursor Send 无 `sendInFlight` 闩
- **status:** ALREADY ADDRESSED
- **file/line:** `cursor.go:451-454`、`472-488`、`767-771`；`cursor_test.go:208-238`
- **evidence:** Cursor 已在 Args/Start 窗口设 `sendInFlight`，拒绝并发 follow-up，启动失败经 `rollbackSend` 回滚。
- **impact:** 与 Claude/Codex 生命周期合同已对齐（单 Adapter）。
- **minimal fix:** 无需。
- **verification:** `TestSendRejectsFollowUpWhileAnotherSendIsInFlight`；真实并发深度见 W4-TEST-1。

### W4-IR4 — [P2] Cursor 在 resume 流缺 session 时用 `expectedSession` 回填摘要
- **status:** ALREADY ADDRESSED
- **file/line:** `cursor.go:716-717`；`cursor_test.go:241-259`
- **evidence:** 空 session 走 `session_identity_missing`，保留本地 `ProviderSessionID`；不再用 expected 回填摘要。
- **impact:** 与 Claude/Codex 缺 identity 失败模式对齐（Codex reason 名为 `thread_identity_missing`，属 Provider 命名差异）。
- **minimal fix:** 无需。
- **verification:** `TestResumeWithoutSessionIdentityFails`。

### W4-IR5 — [P3] Cursor Cancel 升格 `cancel_unconfirmed` 时不改 `SideEffectState`
- **status:** ALREADY ADDRESSED
- **file/line:** `cursor.go:540-542`；对照 `claude.go:655-657`、`codex.go:924-926`
- **evidence:** Cursor 在 `!cancel.Confirmed && status==cancelled` 时设 `SideEffectState = "unknown"`。
- **impact:** 未确认取消不再被误读为无副作用。
- **minimal fix:** 无需。
- **verification:** 行为已与 Claude/Codex 对齐。

### W4-IR6 — [P3] Cursor 缺少显式 `providerTerminalError` 分类
- **status:** ALREADY ADDRESSED
- **file/line:** `cursor.go:723-724`、`774-778`；`cursor_test.go:262-274`
- **evidence:** Cursor 对 `error/failed/cancelled/...` 返回 `failed`/`provider_terminal_error`；单测用 provider-error helper 锁住。
- **impact:** 观测/诊断原因码跨 Adapter 对齐。
- **minimal fix:** 无需。
- **verification:** `TestProviderTerminalErrorIsClassified`。

### W4-IR2 — [P3] `wave4-baseline` 矩阵「W3 real smoke」表述偏软
- **status:** ALREADY ADDRESSED
- **file/line:** `docs/reviews/2026-08-10-wave4-baseline.md:30`
- **evidence:** 当前基线列写明 Native 历史 W2、Cursor/Claude 当日 PASS、Codex CLI Provider authorization BLOCKED。
- **impact:** 不再暗示四路径对 Wave 4 全部 real-PASS。
- **minimal fix:** 无需。
- **verification:** 与 `W4-smoke-matrix.md:18` 对齐。

### W4-IR1 — [P2] Wave 4 Cursor review 产物双 harness，且 baseline 证据的 SHA/字节与当前文件不对齐
- **status:** ALREADY ADDRESSED
- **file/line:** `internal/adapters/cursor/cursor_real_review_test.go:173-201`、`203-227`；当前 `docs/reviews/2026-08-10-wave4-cursor-review.md:3-4`；`W4-baseline.md:70-76`
- **evidence:** `normalizeReviewBody` 剥离重复 staging harness，仅保留 Scope/Verdict；`TestNormalizeReviewBodyRemovesDuplicateHarness` 锁住。当前 prior artifact 顶部 `- Reviewer/model:` / `- Adapter:` 各 1 次。`W4-baseline.md` 已记录 `18195` bytes / SHA-256 `797ffac371fd2fabe6427b125ec31fd933e3a7f70d8547fa2293cef34249ce67`（本会话未独立重算该哈希；prior 正文内旧 `18552`/`a5fd…` 叙述作废）。
- **impact:** staging 代码与当前 prior artifact 单 harness 结构上已闭合。下次 staging 覆盖后需同步更新证据哈希。
- **minimal fix:** 无需针对本缺陷再改实现。
- **verification:** normalize 单测；artifact 单 harness 结构检查；作者侧 hash 记录。

### W4-R4 — [P3] `KillConfirmed = killConfirmed || cancelRequested` 稀释杀进程确认语义
- **status:** OUT OF SCOPE
- **file/line:** `internal/adapters/cli/process.go:258-261`
- **evidence:** 置位发生在 `Wait` 完成后的 `finish()`；Adapter 终态看 `CancelResult.Confirmed`，非本切片新引入缺陷。
- **impact:** 证据字段仍可能与 `CancelResult.Confirmed` 分叉，但不构成本切片回归。
- **minimal fix:** 后续收紧置位或分字段（非本 checkpoint 阻断项）。
- **verification:** 取消/孤儿相关单测（后续）。

### W4-CLI-5 — [P3] `flush` 不 Trim `\r`，stderr 无换行无对称测
- **status:** NEW
- **file/line:** `internal/adapters/cli/process.go:220-236` vs `208-209`
- **evidence:** 完整行 `TrimSuffix(\r)`，`flush` 不 trim；stderr 无换行终行无测。JSONL 路径 `AddLine` 有 `TrimSpace`，实际风险低。
- **impact:** 低；不阻断 runner hardening PASS。
- **minimal fix:** flush 对齐 `\r` trim；可选 stderr unterminated 测。
- **verification:** 定向 runner 单测。

### W4-JSONL-3 — [P3] Replay 矩阵不断言中间事件序列
- **status:** NEW
- **file/line:** `internal/adapters/cli/replay_test.go:40-54`
- **evidence:** 矩阵只查 ID/text/terminal/complete，不断言 `Events` 序列/中间事件类型。
- **impact:** 中间 provider 事件回归偏浅；不改变 DeliveryComplete 合同 PASS。
- **minimal fix:** 后续加深事件序列断言（非本 checkpoint 阻断）。
- **verification:** 扩展 replay 断言。

### W4-JSONL-4 — [P3] Replay 未接到 Adapter `terminalResult` 映射
- **status:** DEFERRED
- **file/line:** `replay_test.go` vs Adapter `terminalResult`
- **evidence:** replay 停在 `ParseJSONL`/`DeliveryComplete`；未接到 Adapter reason 映射。
- **impact:** 共享语义 PASS；跨 Adapter 错误分类统一仍属后续。
- **minimal fix:** 后续接到 Adapter 映射断言。
- **verification:** 跨 Adapter provider-error reason 同构测。

### W4-TEST-1 — [P3] Cursor `sendInFlight` 单测为手动置位，非真实并发竞态
- **status:** NEW
- **file/line:** `internal/adapters/cursor/cursor_test.go:208-238`
- **evidence:** 测试直接 `session.sendInFlight = true` 后调用 Send；验证闩语义，不构造两个并发 Send 竞态窗口。
- **impact:** 闩存在性有锁；真实竞态深度仍浅；不阻断本切片 PASS。
- **minimal fix:** 后续补真实并发竞态测（非本 checkpoint 阻断）。
- **verification:** 并发 Send 竞态测试。

### W4-COV-1 — [P3] Claude/Codex 缺与 Cursor 同级的显式单测（`sendInFlight` / missing-session / `provider_terminal_error`）
- **status:** NEW
- **file/line:** Cursor `cursor_test.go:208-274`；Claude/Codex 仅有 contamination 等既有路径
- **evidence:** 三家实现已同构；仅 Cursor 有上述三类显式回归。Claude contamination 测断言本地权威与 Send 拒绝；Codex contamination 测断言 Send 拒绝与 Observe 可见 foreign thread，但未显式断言本地 `providerSessionID` 权威未改（实现已保留权威，见 `codex.go:1114-1116`）。
- **impact:** 覆盖不对称；不构成本切片合同失败。
- **minimal fix:** 后续为 Claude/Codex 补对称单测；Codex 补本地权威断言。
- **verification:** 三家同构 reason/闩/权威断言。

## Wave 4 exit criteria

| Criterion | Status | Evidence / next action |
| --- | --- | --- |
| 共享 JSONL replay 基线 | CLOSED（parser 层） | `TestWave4StreamReplayMatrix` + 六 fixtures；后续可接到 Adapter `terminalResult` 映射断言 |
| 三外部 CLI fixture lifecycle | CLOSED（happy path） | `TestWave4ExternalAdapterLifecycleConformance`；可扩展非 happy path |
| 共享 runner collector + Adapter 最终 drain | CLOSED | 实现与 runner 单测已衔接（含 unterminated flush） |
| Cursor contamination latch / Send 拒绝 / 保留本地权威 | CLOSED（实现+单测） | W4-R7 ALREADY ADDRESSED；**≠** Wave 4「跨 Adapter contamination/恢复」整项闭合 |
| Cursor `sendInFlight` / 缺 identity / `providerTerminalError` / cancel SideEffect 对齐 | CLOSED（实现+Cursor 单测） | IR3–IR6 ALREADY ADDRESSED；Claude/Codex 对称单测见 W4-COV-1 |
| review artifact staging / 单 harness 可复验 | CLOSED（staging 代码+当前 prior artifact 结构） | W4-IR1 ALREADY ADDRESSED；下次覆盖后同步更新 `W4-baseline.md` hash/bytes |
| 四路径完整 smoke matrix + 脱敏 index | OPEN | Native 历史 PASS + Cursor/Claude 当日 PASS；Codex 当日 BLOCKED（authorization）；需授权后重跑并补齐统一 index |
| Windows 进程树 / atomic replace / 恢复 / 长日志 | OPEN | 专项稳定性测试 |
| 策略：未登记 binding、权限/MCP 扩大、Goal/revision | OPEN | 策略矩阵测试 |
| 空交付 / delivery follow-up / contamination / artifact 恢复 | OPEN | 跨 Adapter contamination / delivery / artifact 恢复测试仍缺 |
| benchmark + 独立 review | OPEN | 仅有 parser/artifact 初始数字（`W4-benchmark-baseline.md`）；本审查针对 baseline 切片；experimental 前需再审 |
| `go test -race` 环境 | OPEN | 补 CGO/gcc 后复跑（baseline 已如实记录） |
| 人工确认后写 `experimental` | OPEN | 禁止在未闭合条件项与退出条件前写入 |

## Verdict

**PASS WITH CONDITIONS** — 本 Wave 4 baseline 切片可通过独立 review/checkpoint。

通过条件：

1. 不将本切片标为 `experimental`
2. 不以 fixture conformance 替代 W3/当前 real Provider smoke 的权威性（Codex 当日仍 BLOCKED）
3. 不再把 W4-R7 / IR1 / IR3–IR6 当开放缺陷；宣称 Wave 4 contamination **退出项**闭合前，仍需跨路径/恢复证据（非 Cursor 单测本身）
4. 本会话未独立重跑测试/重算 SHA；若需把作者命令记录升级为独立复验证据，应在可用 Shell/CGO 环境补跑并在证据中更新

完整四路径 smoke、Windows 稳定性、策略矩阵、完整 benchmark、`go test -race` 与人工确认仍属后续退出条件，不构成本切片 FAIL。
