# Re-review: Multi-Agent Orchestration MVP Design
- Date: 2026-08-07
- Reviewer: Cursor Agent / Grok 4.5 High (non-Fast)
- Target: `docs/brainstorms/2026-08-07-multi-agent-orchestration-mvp.md`

## Findings

### [P1] Native 控制面未闭合：TaskGateway 无法承载“Go 驱动”的 observe/send/cancel/timeout — `docs/brainstorms/2026-08-07-multi-agent-orchestration-mvp.md:56-66` / `:117-119` / `:375`

**Evidence:** TaskGateway v1 仅有 Skill→Go 的 `prepareAttempt` / `bindProviderHandle` / `appendEvent` / `requestCancel` 等入口，没有 `observe` / `send` / `cleanup`，也未规定 `eventReceipt` / `cancelDecision` / `completionDecision` 可携带强制 `next_host_action`。同文档却写“Go 核心通过 TaskGateway 发出动作请求并接收结果”（L117），且 Native timeout 时“核心先记录 `cancel_requested` 并要求 Skill 调用宿主取消”（L119）；delivery follow-up 也只写“可以…发送一次”（L375），无核心授权路径。

**Impact:** 派发前门禁（ticket）已收紧，但 Native 启动后的 observe/send/cancel/timeout/cleanup 与同 session follow-up 仍可能落回 Skill 私有时序，与 CLI（Go 进程树直控）无法同一矩阵机械验收，原 P1 Native 可执行边界与双权威风险未完全关闭。

**Minimum correction:** 明确 Go→Skill 控制面：例如为 Gateway 响应增加必达 `host_actions[]`（`launch`/`observe`/`send`/`cancel_host`/`cleanup`），或增加 `pollActions(attemptId)`；并规定 `delivery_missing` 的 follow-up 只能由核心签发的 `send` 动作触发，Skill 不得自行续写。

### [P1] 有限自动 retry 条件自相矛盾 — `docs/brainstorms/2026-08-07-multi-agent-orchestration-mvp.md:249-250` / `:259`

**Evidence:** L249–250 要求可重试错误类，并允许“尚未创建 session 且 `execution_started:false`/`side_effect_state:none`”**或者**“只读权限/工具/MCP 已被 Adapter 机械强制”。L253 也暗示已进入可写才必须暂停。但 L259 写明：外部 Provider 不支持幂等键时，“已进入执行的任务默认不得自动 retry”。后一条未排除只读机械强制分支，与前条 OR 条件冲突；且 L249 示例错误类全是 `*_before_session`，使“session 已建立的只读 retry”缺少可枚举错误码。

**Impact:** `limited_auto` 无法写出无歧义的判定测试；实现可择一解释，导致重复远端执行或错误拒绝合法 retry。

**Minimum correction:** 写成互斥优先级：`execution_started:true` 时仅当 `side_effect_state:none` 且 Adapter 证明只读隔离时允许，并枚举对应可重试错误码；否则适用 L259 禁令。删除或改写 L259 的无条件“已进入执行不得 retry”。

### [P2] Goal / HumanDecision 的 `authorityRef` 可核验性未进入核心状态 — `docs/brainstorms/2026-08-07-multi-agent-orchestration-mvp.md:64` / `:76` / `:222-232`

**Evidence:** `recordHumanDecision(..., authorityRef)` 与 L76 要求“可核验的用户输入或显式 Goal authority reference”，但 Gateway 无 `enableGoalMode`（或等价）把用户显式启用写入核心；也未规定核心只接受自身持有的 Goal session 凭证，而非 Skill 自带的不透明引用。语义节点 fail-closed 与性质转换强制暂停（L215–217、L224–232）本身正确。

**Impact:** 在 Skill 已是宿主控制器的前提下，仍可用伪造 `authorityRef` 提交 `approve`，削弱“Goal 显式授权”相对 CompletionDecision 的硬边界（虽性质转换列表可部分挡住设计→实施）。

**Minimum correction:** Goal 启用由核心记录（用户证据写入 Task/Workflow 权威字段）；自动 `HumanDecision` 只校验该字段，拒绝 Skill 单方 `authorityRef`。

## Closed findings

| ID | 结论 | 证据指针 |
|---|---|---|
| P0 Native 绕过核心 / 双真相 | 关闭（派发前） | L50–78、L126、L473：一律 `runPreflight`→`prepareAttempt`→launch ticket；禁止先派发后补记 |
| P1 Native/CLI 同构合同 | 部分关闭 | L104–119、L474：capability 表与 cancel 降级已写；Go 驱动通道见剩余 P1 |
| P1 Skill↔Go TaskGateway | 部分关闭 | L52–66、protocol v1：最小表面已固定；observe/send/host action 未入表面 |
| P1 Checkpoint 修改 vs Task 不可变 | 关闭 | L199–206：`request_revision`→不可变 `TaskRevision` |
| P1 宽松节点削弱门禁 | 关闭 | L215–217：`unspecified` fail closed；宽松仅建议 |
| P1 retry 副作用 / 幂等 | 部分关闭 | L246–260：错误码、`execution_started`、idempotency lineage 已有；L250↔L259 矛盾未解 |
| P1 task canary 缺失 | 关闭 | L348–355、L484：canary + 原生 identity 优先级与 `contaminated`/`incomplete` |
| P2 cancel 状态机 | 关闭（状态定义） | L308–327、L119、L482：`cancel_requested`/`cancelled`/`cancel_unconfirmed` |
| P2 Completion/HumanDecision/Event | 关闭 | L329–337、L76：核心独占 Completion；禁止由 Completion 合成 Human；append-only |
| P2 MCP 分支验收 | 关闭 | L410–421、L488：strict / config-isolated / unsupported |
| P2 Windows 原子写 | 关闭 | L393–399、L489：非 `O_DIRECTORY`、replace、journal/恢复 |
| P2 Reasoning 登记/展示 | 关闭 | L158–164、L181：`unsupported`/`host_fixed`；ticket 渲染派发提示 |
| 用户：workspace trust / Windows sandbox / 禁 force·yolo | 关闭 | L121–136、L405–408、L480 |
| 用户：空输出 / CreatePlan / `delivery_missing` / follow-up | 关闭（语义） | L357–376、L485–486；follow-up 授权通道见剩余 P1 |

## Overall assessment

修订设计已实质关闭原 P0 及多数 P1/P2 与 Cursor 交付问题：launch ticket、preflight/Attempt 分界、检查点/`TaskRevision`、canary/contamination、durable `expected_output`、MCP/Windows 门禁与 AGENTS.md milestone 非目标均对齐，无新的 SQLite/daemon/mailbox 等范围违规。

仍不宜直接进入分 Wave 实施规划。须先闭合上述 2 个 P1（Native Go↔Skill 动作通道；retry 条件去矛盾）；P2 Goal authority 建议同一修订一并写入。关闭后再写实施计划与 smoke matrix。
