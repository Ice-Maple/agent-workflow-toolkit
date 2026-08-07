# Review: Multi-Agent Orchestration MVP Design
- Date: 2026-08-07
- Reviewer: Cursor Agent / Grok 4.5 High (non-Fast)
- Target: `docs/brainstorms/2026-08-07-multi-agent-orchestration-mvp.md`

## Findings

### [P0] Native 派发可绕过共享核心，形成双真相源 — `docs/brainstorms/2026-08-07-multi-agent-orchestration-mvp.md:26-47`
架构把 Codex Native 放在 Skill 侧直接调用，再“回填共享核心”，同时宣称 Go 从第一版起拥有 Task/Attempt、准入、retry/fallback、completion/contamination 与机械 gate。设计未要求所有执行路径（含 Native）在派发前经同一共享核心完成 Attempt 创建、准入校验与身份绑定；回填是事后记账，不能阻止 Skill 已发生的误派发。对照研究基线要求宿主能力经 `ControllerAdapter`/`TaskGateway` 进入编排核心（`docs/research/2026-08-06-multi-agent-workflow-research-and-initial-design.md` §5.2–5.3）。影响：MVP 易长出 Skill 私有协议与 Go 协议两套真相，后续“下沉调度”变成迁移/重写。最低纠正：Skill 仅作 Controller/Native 薄适配；任何派发必须先由共享核心分配 `task_id`/`attempt_id`、执行准入并返回允许启动的 handle；事件写入以核心为权威。

### [P1] Codex Native 与 CLI Adapter 边界不可执行 — `docs/brainstorms/2026-08-07-multi-agent-orchestration-mvp.md:66-71`
要求 Native 用 App 原生能力、CLI 由 Go Adapter 管 argv/流/超时/取消，但未给出 Native 路径上可测试的 `observe`/`cancel`/`timeout`/`cleanup`/`extractIdentity` 合同，也未说明 Native 不在 Go 进程树内时如何满足同文档 `experimental` 的 timeout/cancel 判定（约 L303）。研究基线将 `codex-native` 列为与 CLI 同形的 `AgentAdapter`（研究 §5.3）。影响：Native 与 CLI 无法按同一矩阵验收，边界停留在概念层。最低纠正：为 Native 定义与 CLI 同构的 Adapter capability 表与宿主 API 映射（含无法原生取消时的明确降级与 Attempt 状态），并写入 smoke matrix。

### [P1] Skill↔Go 调用与状态写入合同缺失 — `docs/brainstorms/2026-08-07-multi-agent-orchestration-mvp.md:21-47`
“渐进共享核心”依赖 Skill 驱动与 Go 机械校验并存，但未定义 IPC/CLI API、谁生成稳定 ID、谁可写哪些状态字段、冲突时谁胜出、协议版本如何演进。开放问题只列名称/路径/保留期等（L342–349），未把跨层权威合同列为必须关闭项。影响：实施时必然即兴发明接口，与“避免推倒重写”冲突。最低纠正：固定最小 `TaskGateway`（或等价）表面与“核心权威字段 / Skill 只读+回传原始事件”规则，并声明 v1 schema 版本策略。

### [P1] Checkpoint“修改”与 Task 不可变冲突 — `docs/brainstorms/2026-08-07-multi-agent-orchestration-mvp.md:118` / `:204`
默认检查点允许用户“修改”，同时又规定 `Task` 为不可变意图。未说明 modify 是新建 Task、修订 WorkflowNode 目标、还是派生 child Task，以及原 Attempt/HumanDecision 如何保留。影响：实现会在原地改写意图与保留审计历史之间分裂，破坏 Task/Attempt 分离。最低纠正：定义 `HumanDecision` 取值语义；modify 必须产生新的不可变 Task（或显式 `TaskRevision`）并链接前驱，禁止覆写原 Task。

### [P1] “宽松节点判断”削弱强制语义检查点 — `docs/brainstorms/2026-08-07-multi-agent-orchestration-mvp.md:131-133`
允许 Skill 未定义节点时“按任务阶段作宽松判断”，同时又要求设计→实施等性质转换必须暂停。未给出可判定的阶段边界、证据字段或 fail-closed 默认。影响：主控可把下游实施解释为同阶段延续，静默越过人工门禁；与“完成≠获准继续”（L122）及 `experimental` 防未批准下游派发（L301）冲突。最低纠正：未声明节点图时 fail-closed（凡改变工作性质或缺少显式节点标签即 `awaiting_human_decision`）；“宽松”仅限记录/建议，不得自动放行。

### [P1] 自动 retry 的副作用证明不可测，且缺少幂等合同 — `docs/brainstorms/2026-08-07-multi-agent-orchestration-mvp.md:161-168`
自动 retry 要求“证明尚未开始实际执行，或只读且确认无副作用”，但未定义机械证据。研究基线明确警告自动 fallback/retry 需结合 idempotency（研究 §8.1）。设计未引入 idempotency key 或等价去重。影响：`limited_auto` 无法客观验收，写路径可能重复副作用却仍被当成安全 retry。最低纠正：将可自动 retry 的失败类枚举为 Adapter 可报告的启动前错误码；凡已进入执行或权限为可写，默认禁止自动 retry；对允许的自动路径要求 idempotency key 与 Attempt 去重规则。

### [P1] Milestone 要求的 task canary 从设计合同中消失 — `docs/brainstorms/2026-08-07-multi-agent-orchestration-mvp.md:200-214`
MVP 状态模型列出 launch nonce 与若干身份字段，但未纳入 `README.md` / `docs/brainstorms/2026-08-07-agent-workflow-toolkit.md` 与研究 §5.8 已对齐的 task canary 协议，也未规定 canary 失败→`contaminated` 的验收步骤。`experimental` 身份条款（L305）只写 identity/authority/workspace 错配。影响：串线防护相对仓库 milestone 成功标准回退。最低纠正：恢复 canary 为 Attempt 级协议（回显 `task_id/attempt_id/launch_nonce`），并写明与原生 identity 的优先级及失败隔离行为。

### [P2] 取消与 Attempt 状态机未定义，但 maturity gate 要求可测 cancel — `docs/brainstorms/2026-08-07-multi-agent-orchestration-mvp.md:200-214` / `:303`
Go 核心叙事含 timeout/cancel（L36），`experimental` 要求 timeout/cancel 可判定测试（L303），但状态段未定义 `cancel_requested`→`cancelled`、部分结果保留、Provider 无法中断时的终态，也未区分 Attempt 终态与 WorkflowNode/HumanDecision。研究 §5.5 与 CCB 研究均将取消视为可审计状态转换。影响：cancel 测试无法对照合同编写。最低纠正：补 Attempt 最小状态机（含 cancel/contaminate/timeout）及与 CompletionDecision 的映射。

### [P2] CompletionDecision / HumanDecision / Event 关系与权威字段未闭合 — `docs/brainstorms/2026-08-07-multi-agent-orchestration-mvp.md:200-212`
区分了记录类型，但未规定：谁可创建 HumanDecision；CompletionDecision 成功是否允许隐含批准（文意否定但缺硬规则）；Event 是否 append-only；`contaminated` 如何冻结 WorkflowNode。研究 §5.4 要求 completed 必须多证据 fail-closed。影响：实现可把 Agent 自证写入 CompletionDecision 并被 Skill 当成可继续。最低纠正：禁止由 CompletionDecision 合成 HumanDecision；列出 completed 的最小证据集；Event/Attempt 历史不可覆写。

### [P2] Cursor 等非严格 MCP 路径与“MCP allowlist 已验证”门禁不对齐 — `docs/brainstorms/2026-08-07-multi-agent-orchestration-mvp.md:240-247` / `:309`
设计正确要求能力缺口必须报告（对照研究 §3.3：Cursor 无等价 `--strict-mcp-config`）。但 `experimental` 将“MCP allowlist…经过验证”写成统一通过条件，未区分“严格隔离通过”与“缺口已声明且 fail-closed”。影响：Cursor 路径可能因无法真隔离而永远达不到 experimental，或测试被降级为口头声明。最低纠正：按 Adapter capability 拆分验收——`supports_strict_mcp=true` 测隔离；`false` 测缺口上报且不得宣称已隔离。

### [P2] Windows 耐久写/原子写风险未进入设计或 experimental — `docs/brainstorms/2026-08-07-multi-agent-orchestration-mvp.md:218-229` / `:311`
分层存储与“无孤儿进程”有写，但未吸收 CCB 研究已证实的 Windows `O_DIRECTORY`/目录 fsync 不可用导致的状态耐久失败（`docs/research/2026-08-06-claude-codex-bridge-multi-agent-study.md` §1/§8；研究验证建议 #15）。影响：取消/状态落盘在 Windows 上可能静默不可靠，却仍通过“无孤儿进程”类 smoke。最低纠正：规定 Windows 安全的原子写策略，并纳入 experimental 平台测试。

### [P2] 准入元组中 Reasoning 展示规则与强制登记不一致 — `docs/brainstorms/2026-08-07-multi-agent-orchestration-mvp.md:79-86` / `:99-104`
自动派发要求“角色—Adapter—模型—Reasoning”全量显式登记；展示又写 Reasoning 仅在“实际指定且路径支持时”显示。未定义路径不支持 Reasoning 时登记键如何写、是否允许宿主默认 reasoning、默认是否对用户可见。影响：可出现登记组合与真实 Attempt reasoning 不一致的静默误路由。最低纠正：不支持 Reasoning 的 Adapter 登记显式 `reasoning: unsupported|null`；支持则必须登记实际值；Attempt 记录值必须与派发提示一致。

## Overall assessment
设计需先关闭上述阻断性问题后再进入实施规划。方向（Codex Skill 驱动 + Go 共享核心、显式准入、检查点、retry/fallback ask、非目标列表、领域边界）与 `AGENTS.md` / 研究非目标大体对齐，但当前文本在跨层权威、Native 可执行边界、检查点/重试硬合同、canary 上不足以防止双协议与不可测门禁。建议优先修复 P0–P1，将 P2 纳入同一设计修订后再写分 Wave 计划与 smoke matrix。
