# Wave 4 experimental readiness review

- Reviewer/model: Cursor Agent / cursor-grok-4.5-high
- Adapter: Cursor (`cursor`)
- Execution: read-only `plan` mode with explicit workspace trust
- Initial delivery: status `completed`, reason `terminal_and_final_delivery`, final bytes `351`
- Final delivery: status `completed`, reason `terminal_and_final_delivery`, final source `result.result`, terminal `true`, exit code `0`, final bytes `3281`
- Delivery recovery: same-session core-authorized follow-up used: `true`
- Prompt: sanitized; private prompt text is intentionally not stored in this artifact

## Scope and evidence

对照了 `AGENTS.md`、`docs/plans/2026-08-07-multi-agent-orchestration-mvp-waves.md`、`docs/reviews/2026-08-11-wave4-selected-follow-up.md`、`tests/orchestration/evidence/2026-08-11/W4-selected-follow-up.md`、`W4-benchmark-complete.md`、`tests/orchestration/evidence/2026-08-10/W4-smoke-matrix.md`，以及 cli / artifact / storage / policy / gateway / 三 Adapter / conformance 的实现与测试。

核验结论：Windows 真实父子进程树取消（`taskkill /T /F`）、异常非零退出不误记取消、atomic replace 注入中断后 `recoverPrevious` 恢复、三 Adapter + 共享 delivery recovery、contamination 保留本地 resume 权威并拒绝后续 Send、策略 fail-closed、Goal/revision 越权拒绝、`user_authorization_evidence` 精确脱敏且 `goal_authority_id` 保留。文档未提前写入 `experimental` acceptance。fixture conformance 不计真实 Provider smoke；CLI `--version` 不计 Provider 调用。

## Benchmark audit

| Dimension | Benchmark | Audit |
| --- | --- | --- |
| Fresh-process startup | `BenchmarkRunnerColdStart`、`BenchmarkInstalledCLIColdStart` | helper 冷启动与三 CLI `--version` 分离；后者仅为 startup-only |
| Working/peak set | `BenchmarkRunnerWorkingSet` | 触页后再采样；纯 Go `GetProcessMemoryInfo`；amd64 布局合理 |
| JSON/JSONL throughput | 既有 replay / incremental / large | 仍在 |
| Parallel actor isolation | `BenchmarkRunnerConcurrentActorIsolation` | 分 workspace/环境值；串 actor 会失败 |
| Cancellation response | `BenchmarkRunnerCancellationResponse` | 计时含 Windows `taskkill /T /F` |
| Event append | `BenchmarkStoreEventAppend` | 隔离 append-only JSONL |
| Snapshot replace | `BenchmarkStoreSnapshotReplace` | 走公开 `Store.Save`，与 append 分离 |

未发现误导性 SLA；数字标为本地参考值。`-race` 按当前边界为可选补充，不作 P0/P1。

## Findings

No findings.

先前报告中“共享 contamination conformance 因同一 ActionID 可假 PASS”的 P3 不适用于当前树：污染 inject 与 after-contamination 使用不同 ActionID（`wave4-contamination-inject` / `wave4-after-contamination`）；闩失效时 shared conformance 会失败。本地权威保留由 Cursor/Claude/Codex 单测强断言。无 P0/P1。

## Experimental boundary

四路径仍为只读能力；Native 用历史 W2 真实 evidence；外部三条以 `W4-smoke-matrix.md` 为准。fixture recovery/contamination 是确定性生命周期证据，不替代 Provider smoke。benchmark 为主机参考值；`-race` 非最小阻塞门。`experimental` 尚未接受；本评审也不写入 acceptance。

## Verdict

**PASS**（无未关闭 P0/P1）

自动化 Wave 4 experimental-readiness 切片已具备提交用户明确 `experimental` 批准的条件。下一步仅为人工边界确认与版本化 acceptance 记录；不自动推广用户配置，不自动进入 Wave 5。
