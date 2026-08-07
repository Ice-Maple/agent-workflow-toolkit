# 多 Agent 协同 Workflow MVP 实施 Wave 计划

- 日期：2026-08-07
- 状态：规划中，等待 Wave 0 启动确认
- 设计基线：[多 Agent 协同 Workflow MVP](../brainstorms/2026-08-07-multi-agent-orchestration-mvp.md)
- 独立复审处理：[复审问题处理记录](../reviews/2026-08-07-multi-agent-orchestration-mvp-rereview-closure.md)
- 研究与执行证据：[Cursor Agent 评审派发与结果交付调查](../research/2026-08-07-cursor-agent-review-delivery-investigation.md)

## 计划结论

采用“先跑通 Codex Native，再扩展外部 CLI，最后做跨 Adapter 硬化”的顺序：

```text
Wave 0  合同、探针与实现骨架
   ↓
Wave 1  Go 核心：Task/Attempt/策略/存储/TaskGateway
   ↓
Wave 2  Codex Native 快速闭环（首个可用切片）
   ↓
Wave 3  Claude / Cursor / Codex CLI 外部 Adapter
   ↓
Wave 4  可靠性、交付、性能与 experimental 验收
   ↓
Wave 5  一个真实领域 Workflow 的 pilot-validated 试点
```

Wave 2 是第一个必须达到“用户可以实际使用”的节点。Wave 2 只承诺 Codex App Native 路径，不以尚未完成的外部 CLI、复杂恢复平台或领域 Workflow 阻塞快速验证。

## 共同实施原则

- 每个 Wave 先实现，再由独立 reviewer 检查实际文件、diff、测试和证据；review 结束后暂停等待人工决定。
- Go 共享核心是唯一状态、动作、策略和终态事实源；Skill 不得先执行宿主动作再补记。
- 首版使用本地 CLI/JSON 协议和分层文件存储，不引入 SQLite、daemon、dashboard、tmux、完整 mailbox/dead-letter 或 App Server 控制器。
- 所有可配置值保持开放字符串：角色、Adapter、Provider、模型、Reasoning、权限、workspace 和 MCP 不写成永久闭合枚举。
- 默认值保持安全路径：`checkpointed`、有限自动 retry（最多 1 次）、fallback 逐次批准、Goal 关闭、未登记绑定 fail closed、MCP 按任务 allowlist。
- 运行状态、原始事件和诊断信息写入用户级目录；设计、评审、计划、diff、测试证据等明确交付物写入任务指定 artifact 路径。
- 不把凭据、token、认证头或完整敏感环境变量写入仓库、Prompt、事件或评审 artifact。
- 每个 Wave 的退出条件都必须有可重放的人工可读证据，CLI help、exit code 或模型口头成功不能单独作为完成证据。

## 目标目录与职责边界

实现时建议保持以下边界；具体包名可以在 Wave 0 根据 Go 模块命名调整，但不得把领域 Workflow 规则放入共享核心：

```text
cmd/agent-workflow-core/       Go CLI 入口和 protocol v1 命令
internal/orchestration/        Task、Revision、Node、Attempt、Decision、Event
internal/gateway/              TaskGateway、host action、请求幂等与错误
internal/policy/               准入、checkpoint、retry、fallback、Goal 授权
internal/storage/              用户级状态、append-only event、snapshot、原子写
internal/adapters/             Adapter 合同和共享进程/流/身份能力
internal/adapters/claude/      Claude Code CLI Adapter
internal/adapters/cursor/      Cursor Agent CLI Adapter
internal/adapters/codexcli/   Codex CLI Adapter
skills/multi-agent-orchestration/
                                Codex Controller Skill 和 Native action executor
tests/orchestration/           unit、contract、integration、smoke、evidence
```

Skill 只理解用户目标、具体 Workflow 和人工检查点，不能拼接 Provider 命令；领域仓库只通过 Skill/config/artifact 合同消费本仓库能力。

## Wave 0：合同、探针与实现骨架

### 目标

把最终设计变成不会在实现中漂移的合同，确认本机 Go、Codex App Native 能力和首批 CLI 的实际入口，并建立没有真实 Provider 副作用的测试骨架。

### 工作项

1. 固定 protocol v1 的 envelope、request id、错误结构、版本拒绝规则和 JSON/JSONL 边界。
2. 记录 TaskGateway v1 操作表：Task/Node/Attempt、preflight、launch decision、`host_actions[]`/`pollHostActions`/submit、Completion、HumanDecision、Goal enable/revoke、cancel。
3. 固定配置样例和安全默认值，至少覆盖：显式 role–Adapter–model–Reasoning、workspace、permission、MCP allowlist、`checkpointed`、`limited_auto`、fallback `ask`、Goal disabled。
4. 探针当前 Codex App Native 的 launch/observe/send/cancel/cleanup 可调用边界；不能用 shell-launched Codex CLI 冒充 Native。
5. 记录 Claude、Cursor、Codex CLI 的版本、help、stream/transcript 入口、workspace trust、Windows sandbox 和进程取消能力；不把 help 当成 smoke 完成。
6. 建立 fake Adapter、fake Native host-action executor、事件 fixture 和证据目录；预先定义脱敏规则。

### 不做

- 不实现完整业务调度、真实 Provider 派发或用户级全局配置改写。
- 不把 Provider/模型/角色写成固定 enum。
- 不在探针阶段承诺 Cursor 的可写、sandbox 或 MCP 能力。

### 退出条件

- `protocol_version: 1` 的请求/响应样例和错误码表已落盘。
- 四条执行路径的 preflight 探针结果、版本和已知限制已落盘。
- fake Adapter 能通过一条不产生外部副作用的 prepare→launch→event→completion 流程。
- 真实凭据未进入仓库或证据。
- Wave 0 reviewer 确认合同足够支撑 Wave 1；人工批准后才能开始 Wave 1。

### 回滚

删除/停用 Wave 0 的探针配置和测试 fixture 即可，不修改用户真实配置，不影响业务 workspace。

## Wave 1：Go 核心与 TaskGateway

### 目标

实现所有执行路径共享的确定性核心：状态、策略、存储、准入、动作和完成判定先于任何真实 Provider 调用成立。

### 工作项

1. 实现不可变 `Task`、`TaskRevision`、`WorkflowNode`、`Attempt`、`HostAction`、`GoalAuthorization`、`CompletionDecision`、`HumanDecision`、`Event` 和 `ArtifactRef`。
2. 实现分层文件存储：用户级事件/快照/诊断目录与任务指定 artifact 路径分离；同目录临时文件、flush/sync/close、Windows replace、恢复旧文件和 journal 证据完整。
3. 实现 TaskGateway v1：
   - `createTask`、`proposeNode`、`runPreflight`、`prepareAttempt`；
   - `pollHostActions`、`submitHostActionResult`、带因果 action 的 provider binding/event；
   - `requestCompletion`、`recordHumanDecision`、`requestCancel`；
   - `enableGoalMode`、`revokeGoalMode` 和作用域/TaskRevision 校验。
4. 实现全量显式准入和简洁派发提示：只允许登记组合，提示格式包含角色名、Adapter、模型和 Reasoning；不生成 Prometheus、Unspecified High 等隐藏显示名。
5. 实现默认 Workflow checkpoint：语义节点完成后 `awaiting_human_decision`；`unspecified`、性质转换和缺少边界均 fail closed；`request_revision` 只创建不可变 revision。
6. 实现 Attempt 状态、task canary/identity/authority/workspace 污染检测、Completion 与 HumanDecision 分离。
7. 实现 retry/fallback 判定骨架：pre-start 与 post-start-readonly 互斥错误类、`execution_started`/`side_effect_state` 证据、幂等 lineage 和 fallback `ask/auto/disabled`。

### 测试与证据

- Domain unit：状态转移、不可变 revision、checkpoint、Goal scope、污染冻结、Completion/HumanDecision 不可互相推导。
- Gateway contract：重复 request、重复 action result、lease 过期、版本不兼容、未知 binding、未登记 Reasoning。
- Storage integration：append-only、快照恢复、原子替换中断、旧文件保留、canonical path 和秘密过滤。
- Fake E2E：四种 Adapter 能力都尚未真实实现时，核心仍能确定地返回 preflight/unsupported/degraded/fail-closed 结果。

### 退出条件

- 不经过 `runPreflight`/`prepareAttempt` 无法得到可执行 launch ticket。
- Native 没有核心未决 host action 时，核心不会接受 launch/observe/send/cancel/cleanup 结果。
- 未登记绑定、未授权权限、workspace 不可信和 MCP 不可隔离均不创建可执行 Attempt。
- `completed` 必须具备 terminal event、identity/authority、canary/原生 identity 和 required artifact 证据。
- 所有核心测试稳定通过，事件和快照可重放。
- Wave 1 reviewer 和人工检查点通过后，才能调用真实 Codex Native。

### 回滚

核心只使用独立用户级运行目录和测试 workspace；若状态模型不一致，停用 CLI 入口并保留事件/fixture，不删除用户 artifact，不以手工改 snapshot 修复历史。

## Wave 2：Codex Native 快速闭环

### 目标

让 Codex App 主控在默认人工检查点下真实完成一个通用 Workflow 节点闭环，验证“Skill + Go 核心 + Native host action”不是两套事实源。

### 工作项

1. 实现 `skills/multi-agent-orchestration/` 的 Controller Skill：读取用户目标和 Workflow Skill 提案，调用 Go Gateway，渲染核心返回的派发提示，并在节点完成后停在人工检查点。
2. 实现 Native action executor：只执行核心签发的 `launch`、`observe`、`send`、`cancel_host`、`cleanup`；action ID、lease、参数 hash 和回执完整回传。
3. 绑定 Codex App 原生 Subagent task handle、原生 identity、workspace 和 task canary；不得用 shell 启动 Codex CLI 代替 Native。
4. 覆盖角色和模型显示：例如 `reviewer（Cursor Agent / Grok 4.5 High）`，不显示内部 Provider 代号或自行创造英文角色名。
5. 验证默认 `checkpointed`：设计/评审节点完成后必须等待人工 approve；`request_revision`、`retry_node`、`abort` 保留历史。
6. 验证 durable expected output：要求 artifact 路径时，Skill/核心必须落盘并校验；空 stdout、只有 `turn_ended: success` 或只有 tool payload 都进入 `delivery_missing`，follow-up 只能由核心签发一次 `send` action。

### 首个真实闭环

首个闭环建议使用只读评审 Workflow：

```text
用户任务
  → explorer/reviewer 节点（Codex Native 或登记的外部 review binding）
  → 保存 Markdown artifact
  → awaiting_human_decision
  → 用户 approve 后才允许下一节点
```

初始闭环不写业务仓库，不扩大权限，不启用 Goal；它只验证协议、人工检查点、artifact 和 Native 生命周期。

### 测试与证据

- Native contract：无 action 零宿主调用；动作顺序/lease/回执一致；未知 launch/send/cancel 结果不自动重发。
- App smoke：真实 Codex Native launch、observe、completion、人工 approve、request_revision、cancel/timeout。
- Delivery smoke：非空 artifact、空正文、CreatePlan/tool payload、同 session delivery-only follow-up。
- Evidence：`tests/orchestration/evidence/<date>/W2-*`，保存脱敏后的 Gateway 请求、核心事件、Native handle 摘要、artifact hash 和人工检查点结果。

### 退出条件

- 在一个真实任务中，Skill 无法绕过核心直接调用 Native。
- 用户能看到准确的 role/Adapter/model/Reasoning 派发提示。
- 节点完成后不会未经批准进入下游节点。
- required artifact、空输出、cancel/timeout、identity mismatch 都能得到核心可判定结果。
- Wave 2 reviewer 和人工检查点通过；此时标记“Native MVP 可用”，但尚未标记 `experimental`。

### 回滚

停用 Controller Skill 的安装/调用入口，保留核心事件和 Native evidence；不回退 Codex App 全局配置，不删除任务 artifact。若宿主能力不足，记录 `unsupported/degraded` 并停止该路径，不用 force/yolo 或 CLI 冒充补位。

## Wave 3：外部 CLI Adapter

### 目标

在不改变核心状态机和 Skill 合同的前提下，逐个接入 Claude Code、Cursor Agent 和 Codex CLI，并用真实 smoke 证明 Windows 进程、流、权限、MCP 和交付边界。

### 实施顺序

按“证据最充分、风险最可见、再扩展”的顺序逐个完成：

1. Cursor Agent：先处理 workspace trust、Windows 无 sandbox、Plan/只读 review、stream-json/transcript、空正文和 CreatePlan delivery。
2. Claude Code：复用 Go 进程 runner 和事件归一化，单独验证 argv、权限、MCP allowlist、session identity 和 cancel。
3. Codex CLI：验证独立 CLI 进程和模型/Reasoning 登记，不与 Codex App Native 混用。

每个 Adapter 完成后单独停在 review/checkpoint，再开始下一个；任一路径失败不改变其他 Adapter 的准入结果。

### 共享实现

- Go 进程树启动、stdin/stdout/stderr、stream-json/transcript capture、超时、取消、孤儿进程清理和 Windows 无额外交互窗口。
- `capabilities()` 明确 `supported`、`unsupported`、`degraded`；不支持 sandbox、strict MCP、follow-up 或 cancel 时 fail closed/降级，不伪造能力。
- `preflight()` 在 Attempt 前完成版本、workspace trust、权限、模型、MCP 和 artifact 能力检查。
- `extractIdentity()` 校验 provider/session/workspace/launch canary；错配为 `contaminated`，未知不走普通 retry。
- CLI Adapter 的 `send` follow-up 只由核心状态允许，不能根据 exit 0 自行补发。

### 测试与证据

每个 Adapter 至少覆盖：

- 正常 launch/observe/terminal/completion；
- 未登记 role/Adapter/model/Reasoning；
- workspace trust 未满足；
- Windows sandbox 或权限 capability 不支持；
- strict/config-isolated/unsupported MCP 三分支；
- pre-start retry、post-start-readonly retry、可写/未知副作用暂停；
- timeout、cancel、cancel_unconfirmed、孤儿进程；
- exit 0 空正文、transcript recovery、artifact 校验和 delivery_missing；
- provider/session/workspace identity mismatch。

证据放在 `tests/orchestration/evidence/<date>/W3-<adapter>-*`，不得把认证信息、完整环境变量或用户私有 prompt 原文写入证据。

### 退出条件

- 三个 CLI Adapter 各自完成真实 smoke，且所有路径共享同一 Task/Attempt/Completion 语义。
- 每个 Adapter 的失败、unsupported 和 degraded 分支均可判定，不用 fallback 静默替换原绑定。
- Windows 进程、超时、取消、原子 artifact 和 stream/transcript 证据满足 design contract。
- 三个 Adapter 各自 reviewer 和人工 checkpoint 通过。

### 回滚

每个 Adapter 由独立配置开关控制；失败时撤销该 Adapter 的登记或只读能力声明，不影响核心、Native 或其他 Adapter。禁止删除已有 Attempt/Event/artifact。

## Wave 4：跨 Adapter 硬化与 experimental 验收

### 目标

把 Wave 2/3 的真实调用证据汇总成可重复的可靠性、资源和安全边界，达到设计定义的 `experimental`，但不把它误报成领域生产能力。

### 工作项

1. 完成完整 smoke matrix 和可重放 harness：请求、事件、快照、进程摘要、artifact hash、人工决定和错误分类统一收集。
2. 做 Go runtime benchmark：单次冷启动时间、常驻/峰值内存、JSON/JSONL 吞吐、并行不同 actor 的隔离、取消响应时间、事件追加和 snapshot replace 成本。
3. 做 Windows 稳定性测试：异常中断、atomic replace、恢复旧文件、进程树清理、无额外交互窗口、长日志和并发边界。
4. 做策略安全测试：未登记派发、Reasoning 不一致、角色/Adapter 混淆、Goal authority 伪造/越权/撤销、revision 变化、性质转换、MCP 权限扩大。
5. 做交付和恢复测试：空正文、只存在 tool payload、stream-json 最终正文、一次 delivery-only follow-up、artifact 不合规、identity contamination。
6. 汇总指标：准入拒绝正确率、人工检查点阻断率、重复派发率、污染检测率、required artifact 完成率、delivery_missing 恢复率、孤儿进程率、retry 误放行率、资源开销。

### `experimental` 退出条件

设计文档中的 `experimental` 条件全部有证据：四条执行路径真实调用；Native action/CLI Adapter 同构合同；默认 checkpoint 阻止未批准下游；有限 retry/fallback 可判定；canary/identity/authority/workspace 污染可冻结；required output durable；MCP、Windows 和秘密过滤通过；reviewer 检查实际文件/diff/测试。

如果任一 P0/P1 失败、出现重复 launch、串 task、静默 fallback、Goal 越权或无限 retry/continuation，Wave 4 立即停止并回到对应最小复现，不通过放宽策略或提示词掩盖。

### 回滚

experimental 标记只写入版本化文档和证据索引，不自动推广用户配置。失败时保留证据，关闭对应 capability 或 Adapter registration，回到 Wave 2 的 Native MVP。

## Wave 5：pilot-validated 领域试点

### 目标

在一个真实领域仓库中验证通用核心与 Workflow Skill 的边界，而不是把领域规则写回本仓库。

### 试点约束

- 选择一个有明确“探索/设计或评审/人工批准/实施”节点边界的领域 Workflow。
- 领域仓库提供 Workflow Skill、角色策略、artifact 路径和验收规则；本仓库只提供核心、Adapter 和通用检查点。
- 首轮继续使用默认 `checkpointed`，Goal 只在另行授权的受控样本中比较，不以自治模式作为试点默认。
- 试点报告同时记录收益和干预成本：错误派发、人工暂停次数、复审发现、重复执行、artifact 完成、资源和耗时。

### 退出条件

- 真实领域任务完成至少一轮完整闭环，所有节点和人工决定可追溯。
- 领域规则没有污染共享核心；复用边界通过安装/调用接口验证。
- 试点用户确认角色、Adapter、模型和 Reasoning 显示准确，未出现隐藏英文角色名或错误绑定。
- 试点 review、用户确认和回滚演练完成后，才将成熟度升级为 `pilot-validated`。

## 依赖与检查点

```text
W0 合同/探针
  └─ 人工批准
      ↓
W1 核心/TaskGateway
  └─ 核心 reviewer + 人工批准
      ↓
W2 Codex Native MVP
  └─ Native smoke + 人工批准（首个可用切片）
      ↓
W3 外部 CLI（Cursor → Claude → Codex CLI）
  └─ 每 Adapter 独立 reviewer + 人工批准
      ↓
W4 experimental 验收
  └─ benchmark/reliability review + 人工批准
      ↓
W5 领域试点
```

Wave 3 的 Adapter 可以在 Wave 2 通过后按顺序推进；不并行修改核心协议。若 Wave 2 发现 Native 宿主能力不足，先修正 capability/preflight 合同并重新复审，不用外部 CLI 冒充 Native 来取得“成功”。

## 首轮实施任务拆分

Wave 0 通过后，第一轮实际实现建议按以下原子任务建立提交边界：

1. `protocol-v1-contract`：envelope、错误、操作表和 fixture。
2. `core-domain-storage`：实体、事件、snapshot、atomic replace 和恢复。
3. `gateway-policy`：preflight、准入、checkpoint、Attempt、Goal、retry/fallback。
4. `fake-adapter-contract`：fake Adapter、host action lease、幂等和 contract tests。
5. `native-controller-slice`：Codex Controller Skill、Native action executor、真实 W2 smoke。

每个任务只提交所属文件，保留用户已有修改；完成后报告实际文件、测试命令、证据路径和未解决边界。

## 暂不进入的内容

- SQLite、daemon、dashboard、tmux UI、完整 mailbox/dead-letter。
- OpenCode/Kilo/Antigravity 等未登记 Adapter。
- Provider HOME/cache 的全局生命周期管理。
- 通用 Workflow 节点分类器、自动推断用户进入 Goal、默认自动批准。
- 业务领域 Prompt、产品验收和领域 artifact 规则。
- App Server 精确 token 调度；除非 Wave 4 指标证明现有控制不足，再单独立项。

## 当前决策点

当前建议直接启动 Wave 0。Wave 0 不是“实现前的空转”：它会产出 protocol v1、能力探针、fake contract 和证据格式，为 Wave 1/2 的快速闭环减少返工。

Wave 0 完成后应再次停在人工检查点，确认实际宿主能力和合同，再进入 Wave 1；不自动连续执行后续 Wave。
