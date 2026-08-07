# 多 Agent 协同 Workflow MVP

- 日期：2026-08-07
- 状态：已二次修订，待独立复审
- 研究基线：[多 Agent 协同 Workflow：本机探索、方案对齐与初步设计](../research/2026-08-06-multi-agent-workflow-research-and-initial-design.md)

## 背景与目标

本设计在 CCG、oh-my-openagent（OMO）、Claude Codex Bridge（CCB）研究和本机执行路径实测的基础上，收敛多 Agent 协同 MVP 的正式方向。

目标是构建一个轻量、可配置、可审计的多 Agent Workflow：

- 第一版由 Codex App 主控，优先快速跑通；
- 通用协议、状态、策略校验和外部 CLI 执行能力从第一版起进入共享核心；
- 后续其他主控可以复用同一核心，不重写状态、策略和 Adapter；
- 避免隐藏角色/模型绑定、错误派发、擅自 fallback，以及设计完成后未经人工决定直接进入实施；
- 保持通用能力与具体领域 Workflow 的边界，领域仓库可以消费本仓库，本仓库不反向依赖领域语义。

## 核心需求

### 渐进式共享核心

第一版采用“Skill 驱动、共享核心承载确定性机制”的渐进路线，而不是实现两套独立系统：

```text
Codex Controller Skill
  ├─ 理解用户目标和具体 Workflow
  ├─ 向 Go 核心申请 task / attempt / launch action
  ├─ 只按 Go 核心签发的 host action 调用 Codex Native Subagent
  ├─ 展示派发信息和人工检查点
  └─ 将 host action 结果、宿主原始事件和用户决定回传共享核心

Go 共享核心
  ├─ Task / Workflow Node / Attempt / Decision / Event
  ├─ 角色—Adapter—模型准入策略
  ├─ 状态、证据和 artifact 引用
  ├─ retry / fallback / timeout / cancel
  ├─ completion / contamination 判断
  └─ Claude、Cursor、Codex CLI Adapter

Workflow Skill
  ├─ 定义任务或领域流程
  ├─ 定义语义节点和角色需求
  ├─ 细化人工检查点
  └─ 不直接拼接 Provider 命令
```

MVP 中，Skill 负责理解任务、提出语义节点并执行 Go 核心签发的宿主原生动作；Go 核心从第一次派发起就是任务、Attempt、动作、策略、状态和终态的唯一事实源。Skill 不能自行发起宿主动作后再补记，也不能独立生成 ID、裁定终态或批准下游节点。随着真实用例增加，更多调度判断可以逐步下沉到核心，Codex Skill 最终保留为 Codex App 的 ControllerAdapter 和 NativeAgentAdapter，而不是被废弃。

### 核心权威与 TaskGateway v1

Skill 与 Go 核心通过版本化的本地 CLI/JSON 协议交互。所有请求都包含 `protocol_version: 1`；不兼容的 major version 必须拒绝执行，新增可选字段只能保持向后兼容。

最小 TaskGateway 表面为：

```text
createTask(taskRequest) -> taskId
proposeNode(taskId, nodeSpec) -> nodeId
runPreflight(nodeId, requestedBinding, policies) -> preflightDecision
prepareAttempt(nodeId, approvedBinding, preflightEvidence) -> launchDecision
pollHostActions(attemptId, afterActionSequence, controllerInstanceId) -> hostActionBatch
submitHostActionResult(attemptId, actionId, actionLease, result, rawEventRefs) -> actionReceipt
bindProviderHandle(attemptId, launchActionId, providerHandle, identityEvidence) -> bindingDecision
appendEvent(attemptId, causalActionId, sequence, rawEventRef) -> eventReceipt
requestCompletion(attemptId) -> completionDecision
enableGoalMode(taskId, scope, userAuthorizationEvidence) -> goalAuthority
revokeGoalMode(taskId, goalAuthorityId, reason) -> authorityDecision
recordHumanDecision(nodeId, decision, authorityRef) -> transitionDecision
requestCancel(attemptId, reason) -> cancelDecision
```

权威规则：

- Go 核心生成 `task_id`、`node_id`、`attempt_id`、`launch_nonce` 和 idempotency key；
- Skill 只能提交任务建议、节点建议、host action 结果、原始宿主事件和用户授权证据；
- 所有派发在启动前必须通过 `runPreflight()` 和 `prepareAttempt()`；`launchDecision` 为 Native Attempt 同时返回不可复用的 launch ticket 和必达的 `launch` host action；
- launch ticket 固定职能角色、Adapter、模型、Reasoning、权限、workspace、MCP 和 expected output；Skill 不得修改后再启动；
- Native 的 `launch`、`observe`、`send`、`cancel_host`、`cleanup` 以及 delivery-only follow-up 只能响应核心签发的未决 host action 执行；Skill 不得自行决定动作类型、参数或时序；
- 每个 host action 都包含核心生成的 `action_id`、单调序号、Attempt ID、动作类型、参数 hash、前置状态、期限和 lease；Gateway 只接受与未决 action/lease 完全匹配的结果；
- Gateway 响应可以携带 `host_actions[]`，`pollHostActions()` 用于无待处理响应或恢复场景；同一 action 的重复投递返回同一 ID，已确认结果不得重复执行；
- `launch`、`send` 或 `cancel_host` 的 lease 过期且结果未知时，不得自动重发动作，Attempt 进入 `incomplete/action_outcome_unknown` 并等待人工决定；只有 capability 明确声明幂等且核心验证前置状态未变化的 `observe`/`cleanup` 才能安全重发；
- Go 核心是状态和终态的唯一写入者；Skill 的本地笔记、Prompt 文本或 Agent 自述都不是状态事实；
- 冲突时以核心 append-only 事件和权威记录为准，冲突本身进入诊断事件，不静默覆盖；
- 自动 HumanDecision 只能引用核心创建且仍有效的 GoalAuthorization；Skill 提交的任意字符串或外部 authority reference 不构成授权，CompletionDecision 也不能推导 HumanDecision。

Codex Native 虽由 Skill 调用宿主工具，也必须先取得 Go 核心 launch ticket 和对应 `launch` host action，再把宿主 task handle 作为该 action 的结果绑定到对应 Attempt。Native 的后续 observe/send/cancel/cleanup 同样必须逐项取得 action。这样第一版不存在“Skill 私有协议”和“Go 协议”两套真相，后续只需下沉决策逻辑，不迁移历史状态。

### 实现语言与分发

共享核心使用 Go：

- 单一 Go module 和单二进制；
- 不依赖 CGO，优先保证 Windows 可分发性；
- 使用 goroutine、context 和结构化进程控制管理并行流、超时和取消；
- Workflow、角色、模型准入和运行默认值保留为 YAML/JSON 配置，不因使用 Go 而固化；
- Python 可以继续用于其他 Skill 辅助脚本，但不承担多 Agent 运行核心；
- TypeScript 只在未来需要直接嵌入 Node/OpenCode Plugin 生态时作为薄 Adapter 评估，不作为 MVP 核心。

选择 Go 的主要标准是最终运行效率、基础资源占用、冷启动、并发流处理和 Windows 进程生命周期稳定性，而不是迭代期依赖成本。正式实施仍需通过统一 benchmark 验证这些预期。

### 首批执行路径

MVP 支持：

- Codex App Native Subagent；
- Claude Code CLI；
- Cursor Agent CLI；
- Codex CLI。

Codex Native 必须通过 App 原生能力调用，不能由 Go 核心 shell 启动 Codex CLI 来冒充。Claude、Cursor 和 Codex CLI 的 argv、环境、流解析、进程、超时、取消和清理由各自 Go Adapter 管理。

所有 Adapter 对核心暴露同一语义合同：

```text
capabilities()
preflight(task, binding, environment)
launch(launchTicket)
observe(providerHandle, cursor)
send(providerHandle, message)
cancel(providerHandle)
extractIdentity(rawEvents)
cleanup(providerHandle)
```

CLI Adapter 由 Go 直接实现这些动作。Codex Native Adapter 由 Skill 将核心 `host_actions[]` 映射到 App 原生 spawn、wait/observe、follow-up、interrupt/cancel 和 task handle，并通过 `submitHostActionResult()` 回传结果；Skill 中不得存在绕过 action executor 的第二条宿主调用路径。每项 capability 必须明确为 `supported`、`unsupported` 或 `degraded`，不能因为接口同构就虚构宿主能力。Native smoke test 使用可记录调用的宿主边界验证：没有未决 action 时零调用，动作种类、参数 hash、顺序和回执均与核心记录一致。

Native timeout 到达时，核心先记录 `cancel_requested` 并签发 `cancel_host` action；Skill 只能按该 action 调用宿主取消能力。只有宿主确认不再运行时才能记为 `cancelled`；宿主没有取消能力或无法确认时记为 `cancel_unconfirmed`/`incomplete`，保留 task handle 和部分事件，不能伪造成功取消。该映射必须进入与 CLI Adapter 同一 smoke matrix。

### Adapter preflight

capability、版本、workspace trust、sandbox/permission、模型可用性、MCP 隔离和输出交付能力必须在创建 Attempt 之前完成 preflight：

- preflight 失败记录在 Task/WorkflowNode 事件中，但不创建 provider session，也不消耗模型 retry 次数；
- preflight 通过后才由核心创建 Attempt 和 launch ticket；
- 任何需要扩大权限、信任新 workspace 或改变已准入绑定的操作都进入 `awaiting_human_decision`；
- capability probe 结果带 Adapter 版本、平台和时间，版本变化后失效并重新探测。

Cursor Agent 的 MVP 规则：

- workspace 未被 Cursor 信任时，Adapter 报告 `workspace_trust_required`；只有任务 workspace 已由用户明确授权时，Controller 才能使用 `--trust`，并记录该授权，不能把 trust 当成通用权限绕过；
- Windows 当前不支持 Cursor sandbox。Adapter 不得传入已知不支持的 `--sandbox enabled`，也不得自动改写用户全局 sandbox 配置；
- Cursor 的只读探索/评审使用可验证的 `--mode plan` 或 `--mode ask`；
- Windows 上的可写 Cursor 任务只有在 allowlist 和 workspace 边界经过真实 smoke test 后才声明支持，否则 preflight fail closed；禁止用 `--force`/`--yolo` 伪造支持；
- 外部 CLI 优先使用 stream-json，而不是只读取 text stdout，以保存 session、tool、final 和 turn terminal 事件。

Antigravity 当前不作为必达路径。OpenCode、Kilo 等 Provider 在真实需求证明必要时再新增 Adapter。

## 角色、模型和派发策略

### 全量显式准入

自动派发只允许使用策略中明确登记的“职能角色—Adapter—模型—Reasoning”组合：

- 未登记组合不得自动派发；
- 未登记时主控暂停，说明缺少的配置并引导用户补充；
- 主控不能临时创造不透明角色，也不能在派发时暗中替换 Adapter、模型或 Reasoning；
- 模型升级、新模型 ID 或别名不自动继承旧的适用性结论；
- 策略记录适用角色、禁止角色、原因、验证案例和复核日期；
- Provider、Adapter、角色、模型和 Reasoning 使用开放字符串，不在 schema 中形成永久闭合枚举。

首批已确认的适用性限制包括：

- Grok：只允许探索和评审；禁止需求分析、事实裁定、产品文档写作和内容优化；
- Gemini Flash：默认只允许评审。

Reasoning 不能依赖不可见的 Provider 默认值：

- Adapter 支持 reasoning override 时，准入策略必须登记实际值，launch ticket、派发提示和 Attempt 三者一致；
- Adapter 不支持 override 时显式登记 `reasoning: unsupported`；
- 宿主固定但不可覆盖时登记 `reasoning: host_fixed:<value>`，只有宿主能报告实际值时才允许自动派发；
- 无法确定实际 reasoning 时不得把该组合注册为自动准入；
- 用户提示仅在实际存在可辨识 reasoning 值时显示，`unsupported` 不显示为伪参数。

### 用户可见派发信息

派发前只展示用户判断是否符合预期所需的信息：

> 我将发送当前方案给 reviewer（Cursor Agent / Grok 4.5 High）进行评审。

展示内容包括：

- 直观的职能角色；
- Adapter；
- 模型；
- Reasoning（实际指定且该路径支持时）。

正常对话不展示 Provider profile、workspace、策略 ID 等诊断字段，但这些事实仍写入 Attempt 记录。不得只显示 `Prometheus`、`Unspecified High` 等无法判断职责或模型的名称；内部 ID 即使存在，也不能代替面向用户的职能名。

派发提示必须由核心返回的 launch ticket 渲染，不能由 Skill 根据计划值自行拼接。这样提示展示的是已通过准入且即将实际执行的绑定，而不是意图值。

## Workflow 节点与人工决策

### 默认检查点模式

默认运行模式为 `checkpointed`：

```text
节点执行完成
→ 汇总节点结果
→ awaiting_human_decision
→ 用户选择 approve、request_revision、retry_node 或 abort
→ 获得批准后才能进入下一语义节点
```

执行完成和获准继续是两个不同事实。Agent 声称“设计完成”或某个 Attempt 成功，都不能自动批准下游实施。

`HumanDecision` 的语义为：

- `approve`：批准当前节点结果，核心才允许创建已声明的下游节点；
- `request_revision`：创建不可变 `TaskRevision`，引用上一 revision 并记录用户的新指令；后续 Attempt 引用新 revision，原 Task、revision、Attempt 和决定均保留；
- `retry_node`：目标和 revision 不变，按 retry/fallback 策略创建新的 Attempt；
- `abort`：停止当前 Workflow，不删除已有历史和 artifact。

初始 Task 意图不可变。任何改变目标、范围、约束或验收标准的“修改”都必须产生新的 TaskRevision，禁止原地改写 Task。只要求修复节点产物且不改变任务意图时，也要以新的 revision instruction 或 child corrective task 记录，不能覆盖原结果。

### Workflow 语义节点

人工检查点以 Workflow 语义节点为粒度，不按每个 Agent 子任务暂停：

- 节点内部可以并行探索、评审或执行有限 retry，汇总后暂停一次；
- 通用核心只实现检查点机制，不固化所有 Workflow 的节点名称；
- 具体 Workflow Skill 根据自身流程定义和细化节点；
- Skill 尚未定义节点图时，核心创建单一 `unspecified` 节点并 fail closed：节点完成后必须等待人工决定，未获批准不能派发任何性质不同的后续任务；
- 从设计进入实施、从分析进入写作、从修改进入发布等明显改变工作性质的转换必须暂停；
- 主控可以宽松建议节点名称或边界，但“宽松判断”不能自动放行；缺少显式节点标签、边界不清或工作性质发生变化时一律进入 `awaiting_human_decision`；
- 第一版先运行并记录检查点过多、过少或位置不合适的案例，再逐步改进 Skill，不预先建设复杂的通用节点分类系统。

### Goal 自治模式

`goal_driven` 只有在用户显式启用 Codex Goal 或等价自治模式后才能使用。主控不能依据“用户似乎想快速完成”自行推断进入自治模式。

Goal 启用是核心权威状态转换，而不是 Skill 的运行时标志：

- `enableGoalMode()` 只接受当前用户交互产生的授权证据，并由核心创建不可变 `GoalAuthorization`，记录 `goal_authority_id`、Task/Workflow、TaskRevision、授权节点/操作范围、策略版本、创建时间和证据引用；
- `recordHumanDecision()` 的 `authorityRef` 是可判别联合：人工决定引用当前用户输入证据，自动决定引用核心 `goal_authority_id`；两类来源不得互相冒充；
- Skill 后续请求自动 HumanDecision 时只能引用该核心签发的 `goal_authority_id`；核心校验 Task、revision、作用域、允许的决定、强制暂停条件和撤销状态，不接受 Skill 自带的不透明 authority 值；
- 授权范围、目标或 revision 实质变化时，旧授权不能覆盖新增范围，必须取得新的用户授权；
- `revokeGoalMode()` 立即阻止新的自动决定，但保留既有 GoalAuthorization、HumanDecision 和事件历史；
- 没有有效核心授权、证据无法关联到明确用户输入或校验不通过时 fail closed，进入 `awaiting_human_decision`。

Goal 模式可以在已授权范围内自动通过普通节点判断，但以下情况仍强制暂停：

- 需要使用未登记角色、Adapter、模型或 Reasoning；
- fallback 没有已登记候选，或当前配置要求逐次批准；
- 权限或 workspace 范围需要扩大；
- identity、authority、workspace 或 session 不一致，进入 `contaminated`；
- 目标、需求或实施范围发生实质变化；
- 涉及未授权的破坏性操作或外部副作用；
- 缺少继续推进所要求的证据。

## Retry 与 fallback

### 有限自动 retry

有限自动 retry 是默认值，并可通过配置调整：

```yaml
retry:
  mode: limited_auto
  max_auto_retries: 1
```

自动 retry 先按执行边界进入且只能进入以下一个分支；两类错误码不得交叉使用：

1. **执行前分支**：Attempt 已创建，但 Provider session 尚未创建，且 Adapter 以结构化证据报告 `execution_started: false`、`side_effect_state: none`。错误码必须属于 `launch_not_started`、`transport_before_session`、`rate_limited_before_session` 等已登记的 pre-start retryable 集合。
2. **执行后只读分支**：`execution_started: true` 时，只有 Adapter 同时证明 `side_effect_state: none`、任务权限/工具/MCP 的只读隔离已被机械强制，并返回 `transport_after_start_readonly`、`provider_transient_after_start_readonly` 或 `host_disconnect_after_start_readonly` 等已登记的 post-start-readonly retryable 错误码，才允许创建一次新 Attempt。

两个分支还必须同时满足：职能角色、Adapter、模型和 Reasoning 完全不变；未出现 identity、authority、workspace 或 session 污染；未超出 `max_auto_retries`。`execution_started`、`side_effect_state` 或当前分支要求的其他证据为 `unknown`，错误码未登记，或者错误类与分支不匹配时，一律暂停。

workspace trust、sandbox 不支持、模型不存在、权限不满足等 capability/preflight 错误发生在 Attempt 之前，不属于模型 retry；它们应通过修正配置或人工决定重新 preflight。执行状态不明、已进入可写执行、无法证明只读隔离或可能有外部副作用时必须暂停。自动 retry 再次失败后暂停，不擅自转入 fallback。

核心为每次逻辑执行生成 idempotency key，并在 launch 前原子登记：

- Controller 重复提交同一 launch action 结果时返回已有 action receipt/Attempt，不再次启动 Provider；
- retry 创建新的 Attempt ID，记录 `retry_of`，但保留同一逻辑操作的 idempotency lineage；
- 外部 Provider 不支持幂等键时，核心键只能防止本地重复派发，不能证明远端无副作用；因此已进入执行的任务只有命中上述“执行后只读分支”才可自动 retry，任何可写或副作用未知的执行均暂停；
- 所有自动 retry 的错误码、证据和次数进入 Event，便于 smoke test 判定。

可配置模式包括：

- `limited_auto`：上述默认行为；
- `ask`：每次 retry 都等待批准；
- `disabled`：不 retry。

### fallback 默认逐次批准

fallback 默认配置为：

```yaml
fallback_mode: ask
```

可配置模式包括：

- `ask`：展示原绑定失败原因和备用绑定，等待用户批准；
- `auto_registered`：只允许策略中明确登记的 fallback 自动执行；
- `disabled`：禁用 fallback。

Goal 模式不会自动把 `ask` 改成 `auto_registered`，必须由 Goal、Workflow 或任务配置显式授权。

fallback 必须：

- 保持原 Workflow 语义节点；
- 保持原职能角色、目标、输出要求和后续检查点；
- 保存原 Attempt 的错误和部分证据；
- 创建新的 Attempt 记录实际采用的 Adapter、模型和 Reasoning；
- 不因备用绑定改变后续 Workflow 或绕过人工判断、评审和复审。

fallback 候选在 preflight 通过并获得所需 HumanDecision 后才创建新 Attempt；原 Attempt 的 exit、session、部分输出和 artifact 不得被备用 Attempt 覆盖。

## 状态、身份和完成判定

共享核心至少区分：

- `Task`：不可变任务意图；
- `TaskRevision`：对目标、范围、约束或验收标准的不可变修订，引用前一 revision；
- `WorkflowNode`：语义阶段、节点结果和检查点状态；
- `Attempt`：实际 Adapter、模型、Reasoning、provider session、错误和证据；
- `HostAction`：核心签发给 Native 宿主桥接层的动作、lease、结果和因果关系；
- `GoalAuthorization`：核心持有的 Goal 启用证据、作用域、状态和策略版本；
- `CompletionDecision`：本次执行是否真正终止及其依据；
- `HumanDecision`：节点结果是否获准继续；
- `Event`：append-only 状态变化和原始事件引用；
- `ArtifactRef`：交付物和证据位置。

### Attempt 最小状态机

```text
prepared → launching → running
                     ├→ completed
                     ├→ failed
                     ├→ incomplete
                     ├→ timed_out
                     └→ contaminated

running → cancel_requested
          ├→ cancelled
          └→ cancel_unconfirmed → incomplete
```

- preflight 位于 `prepared` 之前；preflight blocked 不创建 Attempt；
- `completed`、`failed`、`incomplete`、`timed_out`、`contaminated` 和 `cancelled` 是互斥终态；
- timeout 触发取消请求，但只有 Provider/宿主终止证据充分时才能记为 `timed_out`；无法确认进程或 Native task 已停止时为 `incomplete`，reason 为 `timeout_cancel_unconfirmed`；
- `cancelled` 只表示执行已确认停止，不表示副作用已回滚；
- cancel、timeout、failure、contamination 都保留最终消息、部分输出、diff、日志和 artifact 引用；
- `contaminated` 冻结当前 WorkflowNode，不允许 retry、fallback、completion 或下游派发，必须由新的人工决定隔离后续处理。

### 完成与批准的权威关系

进程退出、Provider `turn_ended`、Agent 文本声称完成、CompletionDecision 成功、artifact 已交付和 WorkflowNode 获准继续是不同事实：

- CompletionDecision 只能由 Go 核心依据 Adapter 归一化事件生成；
- HumanDecision 只能由可核验的用户输入或有效的核心 `GoalAuthorization` 生成；CompletionDecision 永远不能隐式创建 HumanDecision；
- Agent 自述、exit code 0、`turn_ended: success`、runtime 存活或单个 tool result 均不足以单独产生 `completed`；
- Event、TaskRevision、Attempt、CompletionDecision 和 HumanDecision 一经追加不得覆写；纠正使用新记录和前驱引用；
- retry 和 fallback 必须创建新 Attempt，并分别记录 `retry_of` 或 `fallback_of`。

对需要持久产物的任务，`completed` 的最小证据集包括：

1. launch ticket 与实际 Adapter、模型、Reasoning、权限和 workspace 一致；
2. provider/session identity 已绑定，authority 未变化；
3. task canary 通过，或 capability 明确允许更强的原生结构化 identity 作为主证据；
4. 收到可判定 terminal 事件，且无未解决的 cancel/timeout/contamination；
5. expected output artifact 已存在、非空、路径合规并通过格式/结构校验；
6. CompletionDecision 保存上述证据引用和 fail-closed reason。

### Task canary 与污染检测

每个 Attempt 的任务前缀包含不可复用的 `task_id`、`attempt_id` 和 `launch_nonce`，要求 Agent 在首个协议响应中回显。核心同时核对宿主 task handle、provider session ID、workspace canonical path 和可获得的 provider identity：

- canary 值不匹配时立即标记 `contaminated`，隔离 session/process；
- 需要 canary 的路径没有回显时为 `identity_unverified`/`incomplete`，不得 completed；
- Codex Native 等能提供结构化 task identity 的宿主以原生 identity 为主证据，canary 为纵深探针，不能替代权限和 workspace 校验；
- 任何 authority、workspace 或 session binding 不一致都进入 `contaminated`，不能作为普通失败自动 retry。

### Durable expected output

评审报告、设计、计划、测试证据等必须通过 `expected_output` 声明持久交付合同：

```yaml
expected_output:
  format: markdown
  artifact_path: task-assigned-path
  required: true
  min_bytes: 1
  required_sections: optional
```

- Prompt 明确要求将产物写入分配路径；若 Adapter 的只读模式不允许 Agent 写文件，Go 核心必须从 stream-json 的最终正文原子落盘，而不是只把正文留在控制台；
- 可写 Adapter 只能写入获授权的 artifact staging/path，核心随后验证 canonical path、大小、格式和 hash；
- 外部 CLI 支持时必须采集 stream-json 和 provider transcript，不能只依赖 text stdout；
- 如果 exit code 0/`turn_ended: success` 但没有最终正文和合规 artifact，CompletionDecision 为 `incomplete`，reason 为 `delivery_missing`；
- `CreatePlan`、tool payload 或 transcript 中的中间内容可以保存为 recovery evidence，但除非符合 expected output 并被核心落盘验证，否则不是最终交付物；
- identity/authority 校验通过时，核心可以为同一 session 签发一次 `send` 类型的 delivery-only host action（CLI 路径由核心直接执行）；Skill 不得自行续写。follow-up 产生新事件，不掩盖首次 `delivery_missing`；
- required artifact 未持久化时，WorkflowNode 不能进入人工批准状态，更不能继续下游节点。

## 存储、权限和 MCP

### 分层存储

默认采用分层存储：

- 用户级运行目录保存内部状态、原始事件和诊断日志；
- Workflow 明确要求保留的设计、评审、diff、测试证据和交付物写入任务指定位置；
- 内部记录通过 ArtifactRef 指向交付物；
- 默认不在业务项目中创建运行状态目录；
- 路径和保留周期可配置；
- token、认证头和秘密配置在写入前过滤，秘密不能进入仓库或任务 artifact。

Windows 默认使用平台用户数据目录；其他系统使用相应平台目录。确切路径在实施计划中确定，不能依赖硬编码的用户 HOME。

状态和 artifact 写入必须采用平台安全的原子策略：

- append-only Event 先写完整记录、flush 并同步文件，再对外发布 receipt；
- snapshot/artifact 在目标同目录写临时文件，flush、sync、close 后再使用平台支持的 atomic replace/rename；
- Windows 实现不得依赖 POSIX `O_DIRECTORY` 或目录 fsync；使用经过测试的 Windows replace primitive，并保留失败时可恢复的旧文件/临时文件；
- 启动时检测残留临时文件和未完成 replace，按 journal/校验和恢复或隔离，不能静默选择最新时间戳；
- experimental 必须包含 Windows 原子替换失败、进程中断和恢复测试。

### 权限

- 探索、方案讨论和评审默认只读；
- worker 只有在实施节点获批后才能取得任务声明的写权限；
- 权限和 workspace 是任务字段，Adapter 不能擅自扩大；
- headless CLI 遇到超出声明范围的权限需求时暂停并说明原因；
- 不默认添加 bypass、yolo 或 dangerous flags；
- 只有 Workflow、Goal 或任务显式预授权的范围才能自动继续。

### MCP

- 任务未声明 MCP 时使用空或最小集合；
- 声明 MCP 时只加载 allowlist；
- session 结束后回收本次启动的 MCP；
- 不覆盖用户全局配置；
- Adapter 分别声明 `supports_strict_mcp`、`supports_config_isolation`、`supports_mcp_cleanup`，不能用一个笼统布尔值代替；
- `supports_strict_mcp=true` 的路径必须验证只加载声明服务器；
- 不支持 strict flag 但支持配置隔离的路径，必须用临时配置/home 证明只看见任务 allowlist；
- 两者都不支持时，要求 MCP 的任务在 preflight fail closed；该 Adapter 只能声明 `mcp: unsupported`，不能宣称已隔离；
- experimental 按 capability 分支验收：支持者验证隔离，不支持者验证缺口上报和拒绝派发；
- Cursor 是否能通过任务专属配置目录实现等价隔离必须实测，不能因为没有 strict flag 就默认成功或默认全局 MCP 可用。

## 配置原则

只要不同选择可以共享同一状态机且实现与测试成本可控，就提供配置并讨论安全默认值；只有会导致两套核心架构、两套恢复语义或明显扩大测试面的分歧，才选定单一路线。

默认配置优先级为：

```text
任务显式配置
> Workflow Skill 配置
> 用户级配置
> 核心安全默认值
```

配置不能绕过硬安全规则。每次实际采用的值及来源写入 Attempt 证据。

并发数量、日志保留时间等数值默认值在真实 smoke test 和资源 benchmark 后确定，不要求在设计阶段凭空冻结。已确认的并发边界保持不变：同一逻辑 actor 默认串行；只有不同 actor 且 Adapter、provider profile/session namespace 和 workspace 隔离键明确不同的任务才允许有限并行。

## 方案与决策

### 决策标准

- 优先快速得到 Codex App 可用的 MVP；
- 避免第一版 Skill 在未来共享核心成熟后被推倒重写；
- 角色、Adapter、模型和 Reasoning 必须可见、可配置、可审计；
- 默认人工控制 Workflow 语义节点推进；
- 默认不自动改变派发绑定或扩大权限；
- 运行核心以最终效率、资源和 Windows 稳定性为主要标准；
- 领域 Workflow 能够复用通用能力，但通用仓库不理解领域语义。

### 选定方向

采用“Codex Skill 驱动 + Go 渐进式共享核心 + 配置化 Workflow/派发策略”。Skill 先承担宿主理解和语义节点驱动，Go 核心从第一版起承担协议、状态、策略、外部 CLI Adapter 和机械 gate；后续逐步下沉调度决策，不另建第二套系统。

### 选择理由

该方向在短期可利用 Codex App 原生 Subagent 快速跑通，同时让 Task/Attempt、显式准入、retry/fallback、完成判定、证据和 CLI Adapter 从第一版就可以被其他主控复用。Go 为频繁冷启动、并行 JSONL 流、进程超时/取消和 Windows 进程树提供较低资源、单二进制和清晰的生命周期边界。

### 主要备选

- 纯 Skill 编排：更快，但状态、策略和完成判断容易停留在 Prompt 软约束中，未来主控需要重复实现。
- 从第一版起让核心完全拥有调度：一致性最强，但必须先解决宿主回调、幂等 next-action 和原生 Subagent bridge，扩大 MVP。
- Python 或 TypeScript 核心：开发便利，但在用户明确以最终运行效率和资源为首要语言标准后不再作为首选；未来仍可作为辅助脚本或宿主薄 Adapter。

## 成熟度与验证

### `experimental`

达到以下条件后标记：

- 四条执行路径完成真实调用并保存可复验证据；
- 所有路径在派发前经 TaskGateway preflight、准入、Attempt 创建和 launch ticket；Codex Native 还必须取得核心签发的未决 `launch` host action，没有 action 时 Skill 对宿主零调用；
- Codex Native 的 launch/observe/send/cancel/identity/cleanup 映射、action lease/回执/未知结果和 capability 降级有真实 smoke test；
- 指定 Adapter、模型和 Reasoning 的实际 Attempt 与派发提示一致；
- 未登记组合被拒绝并能引导配置；
- Workflow 语义节点在默认模式下阻止未经人工批准的下游派发；
- 未定义节点图、缺少节点标签或工作性质改变时 fail closed；TaskRevision、HumanDecision 和历史保留经过测试；
- Goal 模式只依据核心创建、限定作用域且未撤销的 GoalAuthorization 自动推进；伪造、错 Task/revision、越权或撤销后的 authorityRef 被拒绝；
- preflight blocked 不创建 Attempt、不消耗 retry；workspace trust 和平台不支持 sandbox 的分支可判定；
- limited retry 的 pre-start 与 post-start-readonly 互斥错误码、execution_started/side_effect/只读隔离证据、idempotency 去重，以及 fallback ask/auto/disabled 有可判定测试；
- timeout、cancel_requested、cancelled、cancel_unconfirmed、incomplete 和 contaminated 状态转换有可判定测试；
- task/attempt 分离，retry 和 fallback 不覆盖历史；
- task canary、原生 identity、authority 和 workspace 的优先级经过测试；值错配进入 `contaminated`，缺失必要 canary 进入 `incomplete`；
- 进程退出、空回复、`turn_ended: success`、仅 CreatePlan/tool payload 或 Agent 自称完成不会被误判为 completed 或节点获批；
- required expected output 必须直接或由核心从 stream-json 原子落盘并通过格式校验；delivery_missing 只能由核心签发 delivery-only send action，在同一已核验 session follow-up 且保留首次失败证据；
- worker 和 reviewer 使用独立任务上下文，reviewer 检查实际文件、diff 和测试；
- MCP 按 strict/config-isolated/unsupported capability 分支验收；权限扩大暂停、秘密过滤和分层存储经过验证；
- Windows 下无额外交互窗口和孤儿进程，原子 replace/中断恢复不依赖 `O_DIRECTORY`；
- 保存中立 explorer/worker/reviewer E2E 证据。

CLI help、模型口头返回“成功”或单次人工观察不能替代真实 smoke test。

### `pilot-validated`

在 `experimental` 基础上：

- 由 `doc-ai-toolkit` 或其他领域仓库消费本仓库能力；
- 完成至少一个真实领域 Workflow；
- 领域角色、Prompt、产物和业务 gate 留在领域仓库；
- 领域仓库依赖本仓库，本仓库不反向依赖领域代码；
- 真实试点发现的通用问题回到本仓库修复并完成回归。

## 约束与非目标

MVP 不包含：

- SQLite 和看板；
- 常驻 daemon；
- tmux 多窗格 UI；
- 完整 mailbox、dead-letter 和长期恢复平台；
- CCB 式完整私有 HOME、asset projection 和 provider cache 生命周期；
- 自动发现并启动所有 MCP；
- OpenCode、Kilo 等新 Provider 的正式支持；
- OMO 式多层自动 fallback；
- 将当前 Provider、模型、角色或 Workflow 固化为仓库永久枚举。

这些是当前 milestone 的非目标，不是仓库级永久禁止项。

## 开放问题

以下问题不阻塞本设计确认，可在实施计划和 benchmark 阶段根据证据确定：

- Skill 和 Go 二进制的正式名称；
- 用户级运行目录的确切平台路径；
- 原始日志和事件的默认保留期限；
- 全局和单 Adapter 的默认并发数；
- 配置文件的最终拆分和版本迁移方式；
- 第一个 `pilot-validated` 领域 Workflow 的具体选择。

## 下一步

1. 由全新 session 的独立 reviewer 对二次修订后的设计、复审 finding 和处理记录进行复审。
2. 关闭新复审中的阻断问题，并保留处理记录；用户确认后才进入下一节点。
3. 编写分 Wave 实施计划、Go benchmark 和逐 Adapter smoke-test matrix。
4. 达到 `experimental` 后再开展真实领域试点。
5. 领域试点通过后升级为 `pilot-validated`。
