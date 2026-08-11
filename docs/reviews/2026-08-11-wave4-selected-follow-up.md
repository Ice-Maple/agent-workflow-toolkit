# Wave 4 selected follow-up

- 日期：2026-08-11
- 范围：Windows 稳定性/故障注入、交付恢复与跨 Adapter contamination、策略安全扩展，以及后续恢复推进的完整 benchmark。
- 状态：自动化范围、完整 benchmark 与最终独立 Cursor review 均通过；用户已按当前证据和只读边界接受 Wave 4 为 `experimental`。

## 已完成

1. Windows 进程与 artifact 稳定性

   - `TestRunnerKillsRealProcessTree` 在 Windows 上启动真实父/子进程，使用实际 `taskkill /T /F`，验证取消后子进程退出。
   - `TestRunnerReportsUnexpectedProcessExit` 验证异常非零退出不会被误记为取消或超时。
   - `TestStageRecoversAfterInjectedAtomicReplaceInterruption` 在备份旧 artifact 后注入中断，确认下一次 `Stage` 恢复 `.previous` 并完成替换。

2. 跨 Adapter recovery 与 contamination

   - Cursor、Claude Code、Codex CLI 各自补充 `delivery_missing` 后的同 session/thread delivery-only follow-up 回归。
   - `TestWave4ExternalAdapterDeliveryRecoveryConformance` 统一验证三家 fixture Adapter 的 delivery recovery。
   - `TestWave4ExternalAdapterContaminationConformance` 统一注入外来 session/thread，验证 `contaminated`、本地 authority 不静默替换以及后续 Send 拒绝。
   - fixture conformance 不替代既有真实 Provider smoke；真实 smoke 仍以 `W4-smoke-matrix.md` 为权威。

3. 策略与 Gateway 边界

   - 扩展 role、Adapter、Reasoning、permission、workspace 混淆组合的 fail-closed 矩阵。
   - 验证 Goal authorization evidence 的持久化过滤。
   - `TestGoalAuthorityCannotCrossRevisionBoundary` 验证旧 revision 的 Goal authority 不能批准新 revision 节点。

4. 完整 benchmark

   - 补齐本地 helper 冷启动、三套已安装 CLI 的无 Provider `--version` 启动探针、Windows working/peak set、并行不同 actor 隔离和取消响应。
   - 补齐 append-only JSONL event 与真实 `Store.Save` snapshot replace 成本；既有 JSON/JSONL 和 artifact replace benchmark 一并按统一参数重跑。
   - 三次十迭代运行全部 PASS；完整数字与测量边界见 [`W4-benchmark-complete.md`](../../tests/orchestration/evidence/2026-08-11/W4-benchmark-complete.md)。
   - 内存采样使用纯 Go Win32 `syscall`，没有引入 CGO 或新依赖。

5. 最终独立 review

   - 真实 Cursor Agent 通过当前 Cursor Adapter、只读 `plan` 和显式 workspace trust 检查实际文件、diff 边界、七维 benchmark 与已有 smoke/evidence。
   - 首轮 review 发现 Goal evidence 脱敏测试被含 `secret` 的 map key 假阳性掩盖；已改为精确脱敏 `user_authorization_evidence`，并验证非敏感 authority ID 保留。
   - 后续 P3 建议指出共享 contamination conformance 的 ActionID 复用可能掩盖闩回归；污染注入与污染后拒收现使用不同 ActionID，定向测试通过。
   - 最终复审为 **PASS / No findings / 无未关闭 P0/P1**，详见 [`2026-08-11-wave4-experimental-readiness-review.md`](2026-08-11-wave4-experimental-readiness-review.md)。

## 验证

以下定向测试通过：

```text
go test -count=1 ./internal/adapters/cli ./internal/adapters/artifact ./internal/adapters/cursor ./internal/adapters/claude ./internal/adapters/codexcli ./internal/policy ./internal/storage ./internal/gateway ./tests/orchestration  PASS
go test -count=1 -v -run 'TestRunnerKillsRealProcessTree|TestRunnerReportsUnexpectedProcessExit|TestStageRecoversAfterInjectedAtomicReplaceInterruption|TestDeliveryMissingRecoversWithSameSessionDeliveryFollowUp|TestDeliveryMissingRecoversWithSameThreadDeliveryFollowUp|TestWave4ExternalAdapterDeliveryRecoveryConformance|TestWave4ExternalAdapterContaminationConformance|TestWave4PolicyMatrix|TestGoalAuthorityCannotCrossRevisionBoundary|TestStorageFiltersSecretsFromEventsAndReceipts' ...  PASS
go vet ./...                                                               PASS
go build ./...                                                             PASS
git diff --check  PASS
go test -run '^$' -bench . -benchmem -benchtime=10x -count=3 ./internal/adapters/cli ./internal/adapters/artifact ./internal/storage  PASS
CURSOR_ADAPTER_WAVE4_FINAL_REVIEW=1 go test -run '^TestRealCursorWave4ExperimentalReadinessReview$' -v ./internal/adapters/cursor  PASS
```

`go test -race ./...` 没有运行：Windows Go race detector 需要 CGO 与受支持的 C 编译器，而仓库本身没有 CGO 依赖。它保留为可选补充验证，不作为原始 Wave 4 工作项或最小 `experimental` 路径的阻塞项。

## `experimental` 验收

用户已明确接受当前边界：四路径均为只读能力；Native 使用历史真实 evidence；fixture 不冒充 Provider；benchmark 是当前主机参考值；`-race` 暂不设为阻塞门；`experimental` 不等于生产可用。

版本化决定见 [`W4-experimental-acceptance.md`](../../tests/orchestration/evidence/2026-08-11/W4-experimental-acceptance.md)。该决定不推广用户配置，也不自动进入 Wave 5。
