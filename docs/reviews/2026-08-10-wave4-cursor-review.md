# Wave 4 cross-Adapter hardening review

- Reviewer/model: Cursor Agent / cursor-grok-4.5-high
- Adapter: Cursor (`cursor`)
- Execution: read-only `plan` mode with explicit workspace trust
- Initial delivery: status `completed`, reason `terminal_and_final_delivery`, final bytes `621`
- Final delivery: status `completed`, reason `terminal_and_final_delivery`, final source `result.result`, terminal `true`, exit code `0`, final bytes `17042`
- Delivery recovery: same-session core-authorized follow-up used: `true`
- Prompt: sanitized; private prompt text is intentionally not stored in this artifact

- Scope and worktree basis: 当前 worktree 中 Wave 4 baseline + automated follow-up 相关实现与证据（会话起始 `git status`：`internal/adapters/cli/process.go`、`process_test.go`、`stream_benchmark_test.go`、`internal/adapters/artifact/*`、Claude/Codex `*_wave4_regression_test.go`、`codex_test.go`、`internal/policy/policy_test.go`、`tests/orchestration/conformance_test.go`、`docs/reviews/2026-08-10-wave4-baseline.md`、`tests/orchestration/evidence/2026-08-10/*`、`tests/orchestration/README.md`、`.gitignore`）。对照 `AGENTS.md`、`README.md`、`docs/plans/2026-08-07-multi-agent-orchestration-mvp-waves.md`、W4 baseline/smoke/benchmark 证据。既有 `docs/reviews/2026-08-10-wave4-cursor-review.md` 仅作 prior-run；finding/verdict/旧 hash 不自动继承。本会话未独立重跑 `go test`/`go test -race`、未独立重算 artifact SHA；作者侧 `W4-baseline.md` 命令与 hash 记录视为作者证据，不作本会话复验。
- Verdict: PASS WITH CONDITIONS

## Scope and evidence

本切片实际落地（以当前源码与测试文件为准）：

1. 共享 CLI runner：自定义 `outputCollector` 替代 `StdoutPipe`；`cmd.Wait()` 后 `flush` 再 `finish`/`Finished`；无换行终行 flush 对齐 `\r` trim；长输出与重复 cancel 回归
2. Cursor / Claude / Codex CLI 的 `consumeTurn` 在 `Finished` 分支内最终 `drainTurnOutput`，再 `Wait()` / 终态分类
3. 六份 JSONL replay fixtures + `TestWave4StreamReplayMatrix`（`ParseJSONL` / `DeliveryComplete`）
4. 三外部 CLI fixture-process lifecycle conformance（含同 session follow-up；非真实 Provider smoke）
5. Cursor / Claude / Codex 对齐：`sendInFlight`、contamination latch、缺 session → identity missing、`providerTerminalError`、`cancel_unconfirmed` → `SideEffectState=unknown`；Claude/Codex 已有显式 Wave 4 对称回归；Codex 另断言本地 resume 权威不被外来 thread 覆盖
6. Artifact：`.previous` interrupted-replace recovery 与成功后清理 stale backup
7. Policy：`TestWave4PolicyMatrixFailsClosed`（未登记 binding、权限扩大、MCP 无 isolation、Goal scope、fallback scope）
8. 证据：四路径 smoke matrix（Cursor/Claude/Codex 当日 PASS；Native 历史 W2）；parser/artifact 初始 benchmark；opt-in Cursor Wave 4 review staging / `normalizeReviewBody`

不以 fixture conformance 计为真实 Provider smoke，也不将本切片标为 `experimental`。

Real smoke 诊断：各 Adapter real smoke 的 `t.Logf` 仅打 status/reason/source/identity/terminal/size 类元数据；`W4-smoke-matrix.md` 将 Codex CLI 记为显式 custom Provider route 后的 PASS，未伪装未授权成功。

## Coverage ledger

| Area | Status | Evidence |
| --- | --- | --- |
| 1. Shared Windows CLI runner hardening | PASS | `internal/adapters/cli/process.go` argv-only；collector + Wait 后 flush 再 `finish`；`flush` 含 `TrimSuffix('\r')`（约 `220-231`）；`TestRunnerDrainsAllFastOutputLinesBeforeFinished`、`TestRunnerFlushesUnterminatedOutputBeforeFinished`、`TestRunnerFlushesUnterminatedStdoutAndStderrWithCR`、`TestRunnerCapturesLongOutputWithoutTruncation`、`TestRunnerRepeatedCancelIsStable`（`process_test.go`）。Cursor/Claude/Codex `consumeTurn` 在 Finished 后最终 drain。完整真实进程树/异常中断故障注入仍属后续退出项，不构成本切片 FAIL |
| 2. JSONL replay / delivery semantics | PASS | 六 fixtures + `TestWave4StreamReplayMatrix`；tool-only / provider-error / malformed 保持 incomplete；exit 0 与 terminal alone 不能单独晋升。事件序列深度与 Adapter `terminalResult` 映射仍可后续加深，不改变 DeliveryComplete 合同 PASS |
| 3. Cross-Adapter lifecycle conformance | PASS | `TestWave4ExternalAdapterLifecycleConformance`：preflight→launch→session continuity→observe→terminal/final→same-session follow-up→cleanup；fixture process 非 real smoke，权威仍在 `W4-smoke-matrix.md` |
| 4. Identity / contamination / Adapter 对称 | PASS | Cursor：Send contaminated 闩、mismatch 保留本地权威并暴露 foreign session、缺 session → `session_identity_missing`、`providerTerminalError`、`cancel_unconfirmed` → `SideEffectState=unknown`。Claude/Codex：`*_wave4_regression_test.go` 覆盖 concurrent follow-up / missing-identity / provider-terminal-error；Codex `TestMismatchedResumeSessionIsContaminatedWithoutReplacingAuthority` 显式断言本地权威。`sendInFlight` 单测仍为手动置位，非真实并发竞态（残余 P3，不阻断） |
| 5. Evidence and documentation | PASS | README/`agent-capabilities`/baseline/W4-* 均未宣称 experimental 已接受；lifecycle 写 session continuity；smoke matrix 四路径边界清晰；Codex 401 根因与 custom route 补齐叙述与脱敏诊断一致；`normalizeReviewBody` + 去重 harness 单测存在；作者侧 review artifact bytes/SHA 已记录（本会话未独立重算） |
| 6. Wave 4 exit criteria | DEFERRED | Windows 完整稳定性故障注入、策略/秘密过滤与 Goal/revision gateway 扩展、跨路径 contamination·恢复、完整 benchmark、`go test -race`、人工 experimental 仍 OPEN；属后续退出条件，非本 hardening 切片 FAIL |

## Findings

### W4-R7 — [P1] Cursor contamination 后仍把外来 session 写入本地 resume 权威，且 Send 无 contaminated 闩
- **status:** ALREADY ADDRESSED
- **file/line:** `internal/adapters/cursor/cursor.go`（Send contaminated 闩；mismatch 保留本地权威）；`cursor_test.go` 对应三行为回归；对照 Claude/Codex 同构路径与 Codex authority 断言
- **evidence:** mismatch 设 contaminated 并返回证据暴露 observed foreign session；本地权威不变；Send 拒绝 contaminated 会话
- **impact:** 单 Adapter 实现+单测已闭合。不等于 Wave 4「跨 Adapter contamination/恢复」退出项整体闭合
- **minimal fix:** 无需
- **verification:** Cursor contamination 回归；Codex authority 回归

### W4-R1 — [P1] Adapter `consumeTurn` 在 `Finished` 后不做最终 drain
- **status:** ALREADY ADDRESSED
- **file/line:** Cursor/Claude/Codex `consumeTurn` Finished 分支内先 `drainTurnOutput`
- **evidence:** 与 runner「flush 后关 Finished」合同衔接
- **impact:** 原间歇性 `terminal_event_missing` / `delivery_missing` 消费路径缺口已闭合
- **minimal fix:** 无需
- **verification:** runner burst/unterminated 单测 + conformance

### W4-R2 — [P2] Cursor 默认 Preflight Probe 不传 `Environment`
- **status:** ALREADY ADDRESSED
- **file/line:** `internal/adapters/cursor/cursor.go` 默认 Probe 传入 `a.Environment`；conformance 覆盖 fixture Environment
- **evidence:** Preflight 与 Launch 环境传播一致
- **impact:** 已闭合
- **minimal fix:** 无需
- **verification:** conformance Environment 路径

### W4-R3 — [P2] 基线/证据把 lifecycle 写成含「provider identity」，实际只断言 session 连续性
- **status:** ALREADY ADDRESSED
- **file/line:** `docs/reviews/2026-08-10-wave4-baseline.md`；`W4-baseline.md`；`conformance_test.go`
- **evidence:** 当前措辞为 session continuity；断言为 sessionID launch↔observe 一致与终稿 token
- **impact:** 不再高估 identity 覆盖
- **minimal fix:** 无需
- **verification:** 文档与 conformance 断言对齐

### W4-R6 — [P2] 声称的无换行终行 `flush` 未被回归测试锁住
- **status:** ALREADY ADDRESSED
- **file/line:** `process_test.go` `TestRunnerFlushesUnterminatedOutputBeforeFinished`；对照 `process.go` flush
- **evidence:** 无换行 helper 输出在 Finished 后可见
- **impact:** collector 主修复与 flush 防御均有回归锁
- **minimal fix:** 无需
- **verification:** 定向 runner 单测（作者证据）

### W4-CLI-5 — [P3] `flush` 不 Trim `\r`，stderr 无换行无对称测
- **status:** ALREADY ADDRESSED
- **file/line:** `internal/adapters/cli/process.go` `flush` 含 `TrimSuffix('\r')`；`process_test.go` `TestRunnerFlushesUnterminatedStdoutAndStderrWithCR`
- **evidence:** stdout/stderr 无换行 + CR 路径均有回归
- **impact:** prior P3 已闭合
- **minimal fix:** 无需
- **verification:** `TestRunnerFlushesUnterminatedStdoutAndStderrWithCR`

### W4-IR3 — [P2] Cursor Send 无 `sendInFlight` 闩
- **status:** ALREADY ADDRESSED
- **file/line:** Cursor Send Args/Start 窗口 `sendInFlight`；失败回滚；Claude/Codex 同构 + `*_wave4_regression_test.go`
- **evidence:** 三家均拒绝 in-flight follow-up
- **impact:** 生命周期合同对齐；真实并发深度见 W4-TEST-1
- **minimal fix:** 无需
- **verification:** 三家 concurrent follow-up 回归

### W4-IR4 — [P2] Cursor 在 resume 流缺 session 时用 `expectedSession` 回填摘要
- **status:** ALREADY ADDRESSED
- **file/line:** Cursor `session_identity_missing`；Claude/Codex Wave 4 terminal classification 回归（Codex reason `thread_identity_missing`）
- **evidence:** 空 session 不再用 expected 回填摘要；保留本地 ProviderSessionID
- **impact:** 跨 Adapter 缺 identity 失败模式对齐（Provider 命名差异保留）
- **minimal fix:** 无需
- **verification:** Cursor/Claude/Codex missing-identity 回归

### W4-IR5 — [P3] Cursor Cancel 升格 `cancel_unconfirmed` 时不改 `SideEffectState`
- **status:** ALREADY ADDRESSED
- **file/line:** Cursor/Claude/Codex Cancel 路径在未确认时设 `SideEffectState=unknown`
- **evidence:** 三家行为对齐
- **impact:** 未确认取消不再被误读为无副作用
- **minimal fix:** 无需
- **verification:** 实现对照

### W4-IR6 — [P3] Cursor 缺少显式 `providerTerminalError` 分类
- **status:** ALREADY ADDRESSED
- **file/line:** Cursor `provider_terminal_error`；Claude/Codex `TestWave4TerminalClassificationRequiresIdentityAndRejectsProviderError`
- **evidence:** provider-error terminal → `failed`/`provider_terminal_error`，无终稿
- **impact:** 观测/诊断原因码跨 Adapter 对齐
- **minimal fix:** 无需
- **verification:** 三家 provider-terminal-error 回归

### W4-IR2 — [P3] `wave4-baseline` 矩阵「W3 real smoke」表述偏软
- **status:** ALREADY ADDRESSED
- **file/line:** `docs/reviews/2026-08-10-wave4-baseline.md`；`W4-smoke-matrix.md`
- **evidence:** 当前矩阵写明 Native 历史 W2 PASS；Cursor/Claude/Codex 当日 real PASS（Codex 需显式 custom Provider route）
- **impact:** 四路径当前证据边界清晰，未伪装 experimental
- **minimal fix:** 无需
- **verification:** 与 `W4-smoke-matrix.md` 对齐

### W4-IR1 — [P2] Wave 4 Cursor review 产物双 harness，且 baseline 证据的 SHA/字节与当前文件不对齐
- **status:** ALREADY ADDRESSED
- **file/line:** `normalizeReviewBody` + `TestNormalizeReviewBodyRemovesDuplicateHarness`；当前 prior artifact 顶部单套 harness；`W4-baseline.md` 记录作者侧 bytes/SHA
- **evidence:** staging 去重逻辑已落地；作者证据记录当前 artifact hash/bytes（本会话未独立重算）
- **impact:** staging 结构闭合；下次覆盖后需同步更新证据哈希
- **minimal fix:** 无需针对本缺陷再改实现
- **verification:** normalize 单测；artifact 单 harness 结构；作者侧 hash 记录

### W4-R4 — [P3] `KillConfirmed = killConfirmed || cancelRequested` 稀释杀进程确认语义
- **status:** OUT OF SCOPE
- **file/line:** `internal/adapters/cli/process.go` `finish()` 内置位
- **evidence:** Adapter 终态看 `CancelResult.Confirmed`，非本切片新引入缺陷
- **impact:** 证据字段仍可能与 `CancelResult.Confirmed` 分叉，但不构成本切片回归
- **minimal fix:** 后续收紧置位或分字段（非本 checkpoint 阻断项）
- **verification:** 取消/孤儿相关单测（后续）

### W4-JSONL-3 — [P3] Replay 矩阵不断言中间事件序列
- **status:** NEW
- **file/line:** `internal/adapters/cli/replay_test.go` 矩阵断言
- **evidence:** 矩阵只查 ID/text/terminal/complete，不断言 `Events` 序列/中间事件类型
- **impact:** 中间 provider 事件回归偏浅；不改变 DeliveryComplete 合同 PASS
- **minimal fix:** 后续加深事件序列断言（非本 checkpoint 阻断）
- **verification:** 扩展 replay 断言

### W4-JSONL-4 — [P3] Replay 未接到 Adapter `terminalResult` 映射
- **status:** DEFERRED
- **file/line:** `replay_test.go` vs Adapter `terminalResult`
- **evidence:** replay 停在 `ParseJSONL`/`DeliveryComplete`；未接到 Adapter reason 映射
- **impact:** 共享语义 PASS；跨 Adapter 错误分类统一仍属后续
- **minimal fix:** 后续接到 Adapter 映射断言
- **verification:** 跨 Adapter provider-error reason 同构测

### W4-TEST-1 — [P3] `sendInFlight` 单测为手动置位，非真实并发竞态
- **status:** NEW
- **file/line:** Cursor `cursor_test.go`；Claude/Codex `*_wave4_regression_test.go` 均直接置位 `sendInFlight=true`
- **evidence:** 验证闩语义，不构造两个并发 Send 竞态窗口
- **impact:** 闩存在性有锁；真实竞态深度仍浅；不阻断本切片 PASS
- **minimal fix:** 后续补真实并发竞态测（非本 checkpoint 阻断）
- **verification:** 并发 Send 竞态测试

### W4-COV-1 — [P3] Claude/Codex 缺与 Cursor 同级的显式单测
- **status:** ALREADY ADDRESSED
- **file/line:** `claude_wave4_regression_test.go`；`codex_wave4_regression_test.go`；`codex_test.go` authority 断言
- **evidence:** concurrent follow-up、missing-identity、provider-terminal-error 与 Codex 本地权威均已有显式回归
- **impact:** prior 覆盖不对称已闭合；深度限制见 W4-TEST-1
- **minimal fix:** 无需
- **verification:** 上述回归测试

## Wave 4 exit criteria

| Criterion | Status | Evidence / next action |
| --- | --- | --- |
| 共享 JSONL replay 基线 | CLOSED（parser 层） | `TestWave4StreamReplayMatrix` + 六 fixtures；后续可接到 Adapter `terminalResult` 映射断言 |
| 三外部 CLI fixture lifecycle | CLOSED（happy path） | `TestWave4ExternalAdapterLifecycleConformance`；可扩展非 happy path |
| 共享 runner collector + Adapter 最终 drain | CLOSED | 实现与 runner 单测已衔接（含 unterminated/`\r`/stderr/长日志/重复 cancel） |
| Cursor/Claude/Codex contamination latch / Send 拒绝 / 保留本地权威 | CLOSED（实现+单测） | W4-R7 / W4-COV-1 ALREADY ADDRESSED；**≠** Wave 4「跨 Adapter contamination/恢复」整项闭合 |
| `sendInFlight` / 缺 identity / `providerTerminalError` / cancel SideEffect 对齐 | CLOSED（实现+三家回归） | IR3–IR6 ALREADY ADDRESSED；真实并发深度见 W4-TEST-1 |
| Artifact `.previous` interrupted-replace recovery | CLOSED（基础路径） | `recoverPrevious` + `TestStageRecoversInterruptedPreviousArtifactBeforeReplacement` / stale cleanup；完整故障注入仍 OPEN |
| Policy fail-closed 基础矩阵 | CLOSED（基础路径） | `TestWave4PolicyMatrixFailsClosed`；秘密过滤、Goal/revision gateway 边界扩展仍 OPEN |
| review artifact staging / 单 harness 可复验 | CLOSED（staging 代码+作者 hash 记录） | W4-IR1 ALREADY ADDRESSED；下次覆盖后同步更新 `W4-baseline.md` hash/bytes |
| 四路径完整 smoke matrix + 脱敏 index | CLOSED（当前证据边界） | Native 历史 PASS + Cursor/Claude/Codex 当日 PASS（Codex 需显式 custom Provider route）；非 experimental 验收 |
| Windows 进程树 / atomic replace 完整故障注入 / 长日志真实稳定性 | OPEN | 专项稳定性测试 |
| 策略：秘密过滤、Goal/revision gateway、更多越权组合 | OPEN | 策略矩阵扩展 |
| 跨 Adapter contamination / delivery recovery / artifact 故障注入 | OPEN | 跨路径恢复与故障注入仍缺 |
| 完整 benchmark（冷启动/内存/并发隔离/取消响应等） | OPEN | 仅有 parser/artifact 参考数字（`W4-benchmark-baseline.md`） |
| `go test -race` 环境 | OPEN | 补 CGO/gcc 后复跑（baseline 已如实记录） |
| 人工确认后写 `experimental` | OPEN | 禁止在未闭合条件项与退出条件前写入 |

## Verdict

**PASS WITH CONDITIONS** — 本 Wave 4 cross-Adapter hardening 切片（baseline + automated follow-up）可通过独立 review/checkpoint。

通过条件：

1. 不将本切片标为 `experimental`
2. 不以 fixture conformance 替代 real Provider smoke 的权威性；当前四路径 smoke 以 `W4-smoke-matrix.md` 为界，且 Codex 依赖显式非敏感 custom Provider route
3. 不再把 W4-R7 / IR1–IR6 / W4-CLI-5 / W4-COV-1 当开放缺陷；宣称 Wave 4 contamination/recovery **退出项**或完整 benchmark/`-race`/experimental 闭合前，仍需对应证据
4. 本会话未独立重跑测试/重算 SHA；若需把作者命令记录升级为独立复验证据，应在可用 Shell/CGO 环境补跑并在证据中更新

完整 Windows 稳定性故障注入、策略扩展、跨路径恢复、完整 benchmark、`go test -race` 与人工确认仍属后续退出条件，不构成本切片 FAIL。
