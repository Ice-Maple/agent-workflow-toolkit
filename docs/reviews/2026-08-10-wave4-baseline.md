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
  session continuity、observe、terminal/final delivery 和 cleanup 均使用同一
  fixture process，结果映射到共同断言。
- 既有每个 Adapter 的 Gateway、失败路径和 opt-in real smoke 证据继续保留，
  不用 fixture process 代替真实 Provider smoke。

## Wave 4 matrix

| 维度 | 当前基线 | 后续证据 |
| --- | --- | --- |
| 四条执行路径 | Native 为历史 W2 evidence；当前 Cursor/Claude real smoke PASS，Codex CLI 当前为 Provider authorization BLOCKED（详见 `W4-smoke-matrix.md`） | 授权刷新后重跑 Codex，并补齐统一四路径 replay/evidence index |
| 正常交付 | 共享 parser + 三个 CLI lifecycle conformance | Native/CLI 同构结果比较 |
| 空正文、tool-only、错误终稿 | deterministic Adapter tests + replay fixtures | 统一 Completion/`delivery_missing` 报告 |
| timeout/cancel/orphan | 各 Adapter deterministic tests | Windows 长运行、异常中断和进程树报告 |
| identity/canary/contamination | 各 Adapter 与 Gateway tests | 跨 Adapter replay 和恢复边界 |
| artifact/transcript | artifact tests 与 Adapter tests | atomic replace 中断/恢复和 hash replay |
| MCP、workspace、permission | 各 Adapter fail-closed tests | 策略矩阵和秘密过滤扫描 |
| 性能/资源 | 已建立 JSONL parser / artifact 初始 benchmark | cold start、内存、并发隔离、取消响应和长日志指标 |

## 当前退出条件

Wave 4 仍需完成：

1. 四路径完整 smoke matrix 和脱敏 evidence index；
2. Windows 进程树、atomic replace、恢复和长日志稳定性测试；
3. 未登记 binding、权限扩大、MCP 扩大、Goal/revision 越权等策略测试；
4. 空交付、一次 delivery-only follow-up、identity contamination 和 artifact
   恢复测试；
5. 完整 benchmark 指标与独立 review；
6. 人工确认后才写入 `experimental` 接受记录。

当前环境的 `go test -race ./...` 还需要可用的 CGO/C 编译器；该环境缺口
不改变本基线测试的语义，但必须在 Wave 4 最终验收前补齐。
