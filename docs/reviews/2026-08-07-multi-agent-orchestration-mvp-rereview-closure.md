# Multi-Agent Orchestration MVP 独立复审问题处理记录

- 日期：2026-08-07
- 设计：[多 Agent 协同 Workflow MVP](../brainstorms/2026-08-07-multi-agent-orchestration-mvp.md)
- 复审：[Re-review: Multi-Agent Orchestration MVP Design](./2026-08-07-multi-agent-orchestration-mvp-rereview.md)
- Reviewer：Cursor Agent / Grok 4.5 High（非 Fast）
- 当前状态：3 项 finding 均已产生二次设计修订，待新的独立复审确认；本记录不自行宣称关闭

## 总体处理结论

复审指出的 2 个 P1 和 1 个 P2 均成立。问题不要求改变“Codex Controller Skill + Go 共享核心”的选定架构，但必须继续收紧三个权威边界：Native 宿主动作由谁签发、执行开始后的 retry 如何判定、Goal 自动批准依据由谁持有。

二次修订保持 MVP 范围不变，通过 TaskGateway v1 的兼容扩展和核心权威记录闭合这些边界，没有引入 SQLite、daemon、dashboard、mailbox、tmux 或新的 Adapter。

## Findings 处理表

| Finding | 二次修订 | 可判定验收 | 状态 |
|---|---|---|---|
| P1 Native Go→Skill 控制面不完整 | TaskGateway 增加 `host_actions[]`、`pollHostActions()` 和 `submitHostActionResult()`；`launch`、`observe`、`send`、`cancel_host`、`cleanup` 只能响应核心未决 action；delivery-only follow-up 也必须由核心签发 `send` action | 没有未决 action 时宿主调用数为 0；动作 ID、类型、参数 hash、顺序、lease 和回执与核心记录一致；未知的 launch/send/cancel 结果不得自动重发 | 已修订，待复审 |
| P1 limited retry 条件矛盾 | 将自动 retry 拆成互斥的 pre-start 和 post-start-readonly 分支；分别登记错误码和证据要求；Provider 无远端幂等键时，只有机械证明无副作用的执行后只读分支可 retry | 覆盖分支匹配、错误码错配、证据 unknown、只读隔离未机械强制、可写/副作用未知和次数耗尽；所有不满足项都暂停 | 已修订，待复审 |
| P2 Goal authority 不可由核心核验 | TaskGateway 增加 `enableGoalMode()`/`revokeGoalMode()`；核心创建 `GoalAuthorization` 并绑定 Task/Workflow、TaskRevision、作用域、策略和用户证据；自动 HumanDecision 只接受有效的核心 `goal_authority_id` | 伪造、错 Task/revision、越作用域、已撤销或无法关联明确用户输入的 authority 均被拒绝并进入人工检查点 | 已修订，待复审 |

## Native host action 边界

`prepareAttempt()` 为 Native Attempt 返回 launch ticket 和首个必达 `launch` action。后续动作由 Gateway 响应携带，或由 Controller 使用 `pollHostActions()` 拉取。每个 action 都由核心生成不可变 ID、序号、参数 hash、前置状态、期限和 lease；Skill 只能执行和回传结果，不能自行构造动作。

为了避免 Controller 在“宿主动作已经发生、回执尚未持久化”时重复产生外部执行，`launch`、`send` 和 `cancel_host` 的结果未知时 fail closed 为 `incomplete/action_outcome_unknown`。这不是模型 retry，也不能借 retry 或 fallback 自动绕过。

CLI Adapter 仍由 Go 核心直接调用，不需要经过 Skill host action bridge；两类路径共享 Adapter 语义合同、Attempt 状态和完成判定，但不伪装成相同的物理执行方式。

## Retry 判定顺序

核心按以下顺序判定，命中前一项后不再落入另一分支：

1. capability、workspace trust、模型和平台能力等 preflight 错误：发生在 Attempt 前，不计模型 retry；修正配置或等待人工决定。
2. Attempt 已创建但执行未开始：只接受登记的 pre-start retryable 错误，以及 `execution_started:false`、`side_effect_state:none`。
3. 执行已经开始：只接受登记的 post-start-readonly retryable 错误，以及 `side_effect_state:none` 和 Adapter 机械强制的只读隔离。
4. 其他所有状态，包括任一关键证据 unknown、可写执行、外部副作用可能存在、污染或错误码错配：暂停。

因此“Provider 不支持远端幂等键”不再形成一条与只读分支冲突的绝对规则：它阻止可写或副作用未知的执行后自动 retry，但不否定已经机械证明无副作用的执行后只读分支。

## Goal 授权边界

`authorityRef` 不再接受 Skill 自带的不透明引用。用户显式启用 Goal 后，核心通过 `enableGoalMode()` 创建 `GoalAuthorization`；每次自动 HumanDecision 都必须引用该核心记录，并重新验证当前 Task、revision、作用域、允许的决定、强制暂停条件和撤销状态。

GoalAuthorization 不是永久通行证。授权范围、目标或 revision 实质变化时必须取得新授权；权限/workspace 扩大、未登记绑定、fallback 需要批准、污染、破坏性操作或证据不足等既有强制暂停条件继续有效。

## 回归边界

- 默认模式仍是 Workflow 语义节点级 `checkpointed`，节点完成后等待人工批准；本次修订没有扩大自治默认值。
- 全量显式角色—Adapter—模型—Reasoning 准入、简洁派发提示、fallback 默认逐次批准和有限自动 retry 默认值不变。
- Native 使用 App 原生 subagent；Codex CLI 仍是独立外部 Adapter，二者不互相冒充。
- durable expected output、stream-json/transcript、Cursor workspace trust、Windows 无 sandbox、禁用 force/yolo 和 MCP capability 分支保持原设计。
- 二次修订只定义协议与验收合同，未进入实施 Wave、benchmark 或 smoke-test matrix 编写。

## 新独立复审入口

新的 reviewer 应使用全新 session，直接读取二次修订后的设计、上一轮复审和本处理记录，重点验证：

1. Native 是否还存在没有核心 host action 就能被设计认可的 launch/observe/send/cancel/cleanup 路径；
2. action 重投、lease 过期和结果未知是否可能重复 launch 或 delivery follow-up；
3. retry 的 pre-start 与 post-start-readonly 是否互斥、错误码和证据是否足以写出确定性测试；
4. Skill 是否还能用任意 `authorityRef` 伪造 Goal 自动批准；
5. 本次修订是否与默认人工检查点、CompletionDecision、Attempt 状态和 MVP 非目标冲突。

在新的独立复审返回并经用户确认前，不进入实施 Wave、Go benchmark 或逐 Adapter smoke-test matrix。
