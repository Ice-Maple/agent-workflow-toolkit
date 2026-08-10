# Wave 4 跨 Adapter 硬化基线

- 日期：2026-08-10
- 仓库：`E:\projects\agent-workflow-toolkit`
- 范围：Codex App Native、Cursor Agent、Claude Code、Codex CLI
- 状态：进行中；不代表 `experimental` 验收完成

## 目标

把 Wave 2/3 已完成的单路径证据收敛为可重放、可比较的共同运行基线，
再进入 Windows 稳定性、策略安全、交付恢复和性能验收。Wave 4 不新增
Provider，不改变 Task/Attempt/Completion 合同，也不把只读 smoke 扩大为
写入或自治能力声明。

## 已建立的基线

- 共享 `internal/adapters/cli.StreamSummary` 的 JSONL replay fixtures：
  正常终稿、三种 Provider 事件形态、tool-only、malformed diagnostic 和
  provider-error delivery。
- 三个外部 CLI Adapter 的统一 lifecycle conformance：preflight、launch、
  session continuity、observe、terminal/final delivery、同 session follow-up
  和 cleanup 均使用同一 fixture process，结果映射到共同断言。
- 既有每个 Adapter 的 Gateway、失败路径和 opt-in real smoke 证据继续保留，
  不用 fixture process 代替真实 Provider smoke。

## Wave 4 matrix

| 维度 | 当前基线 | 后续证据 |
| --- | --- | --- |
| 四条执行路径 | Native 为历史 W2 evidence；当前 Cursor/Claude/Codex CLI real smoke 均 PASS（详见 `W4-smoke-matrix.md`） | 进入 Windows 稳定性、策略、恢复和性能退出项 |
| 正常交付 | 共享 parser + 三个 CLI lifecycle conformance | Native/CLI 同构结果比较 |
| 空正文、tool-only、错误终稿 | deterministic Adapter tests + replay fixtures | 统一 Completion/`delivery_missing` 报告 |
| timeout/cancel/orphan | runner 的长日志、CR/无换行、注入式 tree-kill、重复取消测试；各 Adapter deterministic tests | Windows 长运行、异常中断和真实进程树故障注入报告 |
| identity/canary/contamination | 各 Adapter 与 Gateway tests | 跨 Adapter replay 和恢复边界 |
| artifact/transcript | artifact tests 与 Adapter tests；`.previous` interrupted-replace recovery 已有回归 | atomic replace 故障注入和 hash replay |
| MCP、workspace、permission | 各 Adapter fail-closed tests；`TestWave4PolicyMatrixFailsClosed` | 策略矩阵扩展、Goal/revision gateway 边界和秘密过滤扫描 |
| 性能/资源 | JSONL parser / artifact baseline 已扩展到 256 KiB delivery 与大 artifact | cold start、内存、并发隔离、取消响应和长日志指标 |

Codex CLI 的初次 Wave 4 smoke 曾返回脱敏 `401 Unauthorized`。原因是探索阶段
继承了用户 `model_provider=custom`，而隔离 Adapter 固定使用
`--ignore-user-config`；原 smoke 没有显式传入 custom Provider 的非敏感路由配置，
因而回退到默认 OpenAI endpoint。补齐 Provider name、base route、wire API 和 auth
requirement 后，delivery 与 lifecycle 两条真实 smoke 均通过；凭据未进入仓库或证据。

## 当前退出条件

四路径 smoke matrix 与脱敏 evidence index 已完成当前基线；自动化 follow-up
已覆盖 runner/artifact/policy/同 session delivery 的基础路径；Wave 4 仍需完成：

1. Windows 真实进程树/异常中断故障注入和完整 atomic replace 验证；
2. 策略矩阵的秘密过滤、Goal/revision gateway 边界及更多越权组合；
3. 跨 Adapter identity contamination、delivery recovery 和 artifact 故障注入；
4. 完整 benchmark 指标与独立 review；
5. race-enabled verification；
6. 人工确认后才写入 `experimental` 接受记录。

当前环境的 `go test -race ./...` 还需要可用的 CGO/C 编译器；该环境缺口
不改变本基线测试的语义，但必须在 Wave 4 最终验收前补齐。
