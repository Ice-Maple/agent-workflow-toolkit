# Multi-Agent Orchestration MVP 设计评审处理记录

- 日期：2026-08-07
- 设计：[多 Agent 协同 Workflow MVP](../brainstorms/2026-08-07-multi-agent-orchestration-mvp.md)
- 原评审：[Review: Multi-Agent Orchestration MVP Design](./2026-08-07-multi-agent-orchestration-mvp-review.md)
- Reviewer：Cursor Agent / Grok 4.5 High（非 Fast）
- 当前状态：第一轮修订已完成独立复审；原 finding 的关闭情况和 3 项遗留问题见[独立复审报告](./2026-08-07-multi-agent-orchestration-mvp-rereview.md)，二次修订见[复审问题处理记录](./2026-08-07-multi-agent-orchestration-mvp-rereview-closure.md)

## 总体处理结论

评审指出的核心问题成立：初稿虽然选择“Skill 驱动 + Go 共享核心”，但没有机械保证 Codex Native 派发必须先经过核心，也没有闭合 Skill↔Go 权威、Native capability、Task revision、节点 fail-closed、retry 幂等、canary、cancel、持久产物和平台隔离合同。

修订后的设计将 Go 核心从“事后记录者”改为所有执行路径的预派发权威：任何 Native 或 CLI 派发必须先完成 preflight、准入、Attempt 创建并取得 launch ticket；Skill 只执行宿主动作和回传原始事件/授权证据。

## Findings 处理表

| Finding | 处理 | 设计位置 | 状态 |
|---|---|---|---|
| P0 Native 可绕过核心、形成双真相源 | 新增 TaskGateway v1、核心权威字段和 launch ticket；禁止 Skill 先派发后补记 | 设计 §“核心权威与 TaskGateway v1” | 已修订，待复审 |
| P1 Native/CLI 边界不可执行 | 定义同构 Adapter 合同、Native 宿主映射、capability supported/unsupported/degraded 及 cancel 降级 | 设计 §“首批执行路径” | 已修订，待复审 |
| P1 Skill↔Go 合同缺失 | 固定 createTask/proposeNode/preflight/prepareAttempt/bind/event/completion/decision/cancel 表面和 protocol v1 | 设计 §“核心权威与 TaskGateway v1” | 已修订，待复审 |
| P1 Checkpoint 修改与 Task 不可变冲突 | 定义 approve/request_revision/retry_node/abort；修改通过不可变 TaskRevision 表达 | 设计 §“默认检查点模式” | 已修订，待复审 |
| P1 宽松节点判断削弱门禁 | 未定义节点图时创建 unspecified 节点并 fail closed；宽松只用于建议，不得自动放行 | 设计 §“Workflow 语义节点” | 已修订，待复审 |
| P1 retry 副作用与幂等不可测 | 增加 preflight/Attempt 分界、结构化可重试错误、execution_started/side_effect 证据和 idempotency lineage | 设计 §“有限自动 retry” | 已修订，待复审 |
| P1 task canary 缺失 | 恢复 task_id/attempt_id/launch_nonce canary，并定义原生 identity 优先级、缺失与错配状态 | 设计 §“Task canary 与污染检测” | 已修订，待复审 |
| P2 cancel 状态机缺失 | 增加 prepared/launching/running/terminal 以及 cancel_requested/cancelled/cancel_unconfirmed | 设计 §“Attempt 最小状态机” | 已修订，待复审 |
| P2 Completion/HumanDecision/Event 未闭合 | 核心独占 CompletionDecision；HumanDecision 只能来自用户/Goal authority；Event 与历史 append-only | 设计 §“完成与批准的权威关系” | 已修订，待复审 |
| P2 Cursor 非严格 MCP 与门禁不一致 | 按 strict/config-isolated/unsupported capability 分支验收；不支持时要求 MCP 的任务 fail closed | 设计 §“MCP” | 已修订，待复审 |
| P2 Windows 原子写风险缺失 | 增加同目录临时文件、flush/sync/close、Windows replace primitive、journal/恢复及平台测试 | 设计 §“分层存储” | 已修订，待复审 |
| P2 Reasoning 登记与显示不一致 | 规定 explicit/unsupported/host_fixed；不允许不可见 Provider 默认 reasoning 自动准入 | 设计 §“全量显式准入” | 已修订，待复审 |

## 用户追加的 Adapter 问题

### Workspace trust

Cursor workspace trust 被定义为 Attempt 前 preflight。只有任务 workspace 已获用户授权时才可以使用 `--trust`；否则进入人工决定。trust 不代表任务获得更大执行权限。

### Windows sandbox

当前 Cursor CLI 在 Windows 不支持 sandbox。Adapter 不传递不支持的 flag，也不自动修改全局 sandbox 配置。只读任务使用经过验证的 plan/ask mode；可写能力在 allowlist 和 workspace 边界未完成真实 smoke 前 fail closed，禁止使用 force/yolo 补位。

### 退出码 0 但没有正文

调查证实首轮完整报告进入 Cursor `CreatePlan` tool payload，随后 `turn_ended: success`，但没有最终 assistant text，因此 text stdout 为空。修订增加：

- 外部 CLI 优先采集 stream-json 和 transcript；
- durable expected output 必须落盘并验证；
- exit 0、turn success 和 tool payload 不能单独 completed；
- 缺少正文/artifact 为 `delivery_missing`；
- identity 不变时可以同 session delivery-only follow-up，并保留首次失败事件。

详细证据见：[Cursor Agent 评审派发与结果交付调查](../research/2026-08-07-cursor-agent-review-delivery-investigation.md)。

## 复审入口条件

复审应直接读取修订后的设计、原评审、本文以及 Cursor 调查记录，并重点验证：

1. Native 是否仍存在绕过 TaskGateway 的路径；
2. preflight、Attempt 和 retry 的边界是否自洽；
3. required artifact 是否真正成为 completion 的必要证据；
4. Cursor trust/sandbox/MCP capability 是否 fail closed；
5. TaskRevision、HumanDecision 和 Goal authority 是否仍可能静默放行；
6. 12 项 finding 是否逐项关闭且没有引入新的 MVP scope expansion。

在独立复审返回前，不进入实施 Wave 或 smoke-test matrix 编写。
