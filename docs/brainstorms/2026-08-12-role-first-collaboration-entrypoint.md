# Role-first 可选协作入口优化设计

- 日期：2026-08-12
- 状态：已确认方向并实施；U0–U3 与最小发布收口证据见 `tests/orchestration/evidence/2026-08-13/role-first-implementation-smoke.md`
- 面向范围：`agent-workflow-toolkit` 的通用协作入口、Router、Controller、Role Prompt、调度、policy、CLI 与分发合同
- 兼容基线：现有 TaskGateway、Codex App Native 宿主路径，以及 Claude Code、Cursor Agent、Codex CLI 三个 external CLI Adapter
- 不包含：具体业务 Prompt 内容、业务 artifact 内容、业务审批规则以及任何具体业务仓库的 Workflow 改造实现

## 1. 设计结论

业务 Workflow 或项目定义 Agent 角色及其完整 Prompt；业务 Skill 只声明“让什么角色做什么、需要几个实例、步骤之间有什么依赖”。上游框架负责加载 Role Prompt、选择执行模式、将 Role 映射到用户机器上可用的 binding、调度步骤并通过现有 Gateway 和 Adapter 执行。

```text
业务 Skill
  role + task + count + after
          ↓
Collaboration Router
  framework / direct / single
          ↓
Role Registry
  加载 Workflow 或项目提供的唯一 Role Prompt
          ↓
machine-local Role Route
  Role ID → 已注册 binding
          ↓
通用 Scheduler / Controller
          ↓
  现有 TaskGateway、Native Host / external CLI Adapter
```

关键边界如下：

- Workflow 定义角色语义和 Prompt；
- Skill 定义本次业务任务、步骤关系、业务输入、业务产物和业务 gate；
- 使用者的 machine-local 配置选择 Adapter、model、reasoning、permission 和 MCP；
- 上游框架实现通用路由、调度、状态、安全关闭和恢复；
- 现有 Codex App Native 宿主路径与三个 external CLI Adapter 全部复用，不重新开发其底层执行能力；
- framework 缺失、关闭或未启用时，原业务 Skill 仍能继续执行；
- 正常业务使用不要求用户手工调用或理解框架 Controller Skill。

## 2. 本设计是原始目标的纠偏，不是新增 Adapter milestone

### 2.1 原始设计已经提出 Role-first

最初的[多 Agent 研究与初始设计](../research/2026-08-06-multi-agent-workflow-research-and-initial-design.md)已经明确：

- 逻辑 Agent/role 是任务路由目标；
- Adapter、Provider 和 model 只是一次 Attempt 的执行选择；
- 同一 Role 可以根据成本、可用性和风险路由到不同执行后端；
- 不应通过 Provider 名称路由业务任务。

[MVP 设计](./2026-08-07-multi-agent-orchestration-mvp.md)进一步要求：

- Workflow Skill 定义语义节点和角色需求；
- Workflow Skill 不直接拼接 Provider 命令；
- 共享核心维护“角色—Adapter—模型”准入策略；
- 角色、Adapter、模型和 reasoning 可配置、可见、可审计。

这些方向吸收了 OMO 的任务状态、角色隔离和调度可靠性经验，以及 CCB 的 agent 名称路由、task/attempt 分离和 Provider backend 边界；本设计不复制 OMO 的复杂 category/persona 系统，也不复制 CCB 的 daemon、tmux 或完整 mailbox。

### 2.2 遗漏发生在哪里

[Wave 实施计划](../plans/2026-08-07-multi-agent-orchestration-mvp-waves.md)将“binding 必须显式登记”落实成了 exact preflight，但没有定义以下公开能力：

- 只输入 Role ID 的 Role Resolver；
- Role ID 到完整 binding 的 machine-local route；
- 业务 Skill 到统一 Controller 的自动入口；
- `count/after` 的通用调度与消费侧验收。

Wave 2–4 因而完成并验证了 Gateway 状态、安全、恢复、Codex App Native 宿主路径和三个 external CLI Adapter，却只实现了 role 的校验与审计，没有实现 role-first 的选择入口。随后[项目接入指南](../guides/multi-agent-orchestration-project-adoption.md)把完整 binding 和 Gateway loop 暴露为普通消费路径。

先前接入评审验证的是“指南与当前实现一致”，没有反向验证“当前实现覆盖原始 role-first 产品目标”。因此此前的“功能完成”应修正为：

> 当时的判断是：Adapter 与 TaskGateway 底层执行、安全和恢复合同已经完成；原始设计中的 role-first 普通消费入口尚未完成。该缺口现已由本文 U0–U3 的实现闭合，当前状态见文末。

本设计用于补回该缺口。U0–U3 是同一上层 capability 的完整实现范围，不按 Native 或 external Adapter 重新切分。

## 3. 目标与非目标

### 3.1 目标

1. 业务 Skill 只表达 `role`、`task`、`count` 和 `after`，不复制框架机制。
2. Role 的完整 Prompt 可以随可复用 Workflow 分发，也可以由普通项目为自身开发定义。
3. 单独安装一个 Skill 时，自动安装并固定其依赖的 Role Prompt。
4. 每个使用者可以按本机实际环境，把同一 Role 映射到不同的现有 Adapter/model。
5. 同一通用 Scheduler 支持串行、多实例、依赖和结果汇总，不内置 review/design/write 等业务 Workflow。
6. 提供 `framework`、`direct`、`single` 三种执行模式以及安全的一键关闭能力。
7. collaboration 内部技术步骤按已声明计划自动推进，业务审批继续由原 Workflow 持有。
8. 保留现有 Gateway 的 admission、identity、completion、checkpoint、cancel、recovery 和审计合同。

### 3.2 非目标

- 不重新实现 Codex App Native 宿主能力，也不复制 Claude Code、Cursor Agent、Codex CLI Adapter 的底层执行能力。
- 不构造复杂的 Role capability、permission 或 effect DSL。
- 不支持 Prompt 继承、字段级 merge 或同 Role ID 的项目覆盖。
- 不为评审、设计、写作或实现分别创建编排引擎。
- 不把业务 Prompt、业务 artifact、领域状态或业务审批移入上游仓库。
- 不把 Adapter、model 或 reasoning 固定在 Workflow/Skill 中。
- 不扩大当前 framework 路径的权限和成熟度。
- 不引入 daemon、dashboard、SQLite、tmux 或完整 mailbox。

## 4. 核心概念与职责

| 概念 | 负责 | 不负责 |
| --- | --- | --- |
| 业务 Workflow | 分发 Role Prompt、业务 Skills 和业务规则 | 用户本机 Adapter/model 选择 |
| 业务 Skill | 业务正文、task、步骤关系、输入、产物和业务 gate | Gateway loop、Provider 参数、恢复状态机 |
| Role Prompt | 定义一个角色如何完成职责 | 本机执行后端和具体步骤依赖 |
| Collaboration Router | 识别 collaboration 声明、计算 effective mode、安全降级 | 修改业务目标或批准业务结果 |
| Role Registry | 定位唯一 Role Prompt、校验 package/version/hash | 合并多个 Prompt 或猜测角色 |
| Role Resolver | 将 Role ID 确定性解析为一个已注册 binding | 根据 task 文本临时猜 Provider/model |
| Scheduler | 展开 instance、判断 ready step、传递上游结果 | 理解“评审”“设计”等业务语义 |
| Controller | 驱动 Gateway 和宿主 HostAction loop | 维护第二套 execution 真相 |
| TaskGateway | admission、Attempt、action、completion、checkpoint 和审计 | 决定业务 Skill 应使用哪种模式 |
| Native Host / external CLI Adapter | Provider execution、observe、cancel、identity 和 completion evidence | 选择业务 Role 或定义 Workflow |
| machine-local 配置 | framework gate、workspace mode、Role route 和完整 binding | 业务 Prompt 和业务审批 |

## 5. Role Prompt 模型

### 5.1 Role Contract 的最小形态

本设计仍使用“Role Contract”表示稳定角色约定，但 v1 不建立复杂结构化 DSL。一个 Role Contract 只由以下内容组成：

- 带 namespace 的稳定 Role ID；
- package/version；
- 一份完整 Markdown Prompt；
- 用于安装和运行时校验的内容 hash。

角色职责、禁止事项、工作方法和输出习惯直接写入 Prompt，不拆成 `responsibility/prohibitions/capabilities/effects/delivery` 等字段。

### 5.2 Role Package

可复用 Workflow 可以集中维护公共 Role：

```text
doc-ai-toolkit/
├─ roles/
│  ├─ roles.yaml
│  ├─ technical-writer.md
│  └─ independent-reviewer.md
└─ skills/
   ├─ dita-writing/
   │  ├─ SKILL.md
   │  └─ collaboration.yaml
   └─ requirement-analysis/
      ├─ SKILL.md
      └─ collaboration.yaml
```

`roles.yaml` 只承担定位和版本化：

```yaml
schema_version: collaboration-roles/v1
package: doc-ai-toolkit
version: 1.0.0

roles:
  technical-writer:
    prompt: technical-writer.md
  independent-reviewer:
    prompt: independent-reviewer.md
```

完整 Role ID 分别为：

```text
doc-ai-toolkit/technical-writer
doc-ai-toolkit/independent-reviewer
```

### 5.3 Role Prompt 示例

```markdown
# 独立文档评审者

你负责独立评审当前文档修改。

- 必须检查实际文件和 diff；
- 不依赖写作者总结作结论；
- 不修改被评审文件；
- 按当前 Skill 提供的业务验收规则输出 findings 和 verdict。

## 本次任务

{{ task }}

## 输入材料

{{ inputs }}

## 输出要求

{{ output_requirements }}
```

占位符集合及转义规则由实现阶段版本化；Role Prompt 不包含 Adapter、model、reasoning、本机路径或 Gateway 操作。

### 5.4 Workflow Role 与项目 Role

Role Prompt 有两种合法来源：

| 场景 | Role Prompt 所有者 |
| --- | --- |
| `doc-ai-toolkit` 等可移植 Workflow | Workflow 自带 Role Package |
| 普通代码仓库自身开发 | 项目级 Role Package |
| 开发 `doc-ai-toolkit` 自身的 Skill/MCP | `doc-ai-toolkit` 的项目开发 Role Package |
| 将来的代码开发或 Workflow 开发套件 | 对应 Workflow 自带 Role Package |

同一个仓库可以同时包含可分发 Workflow Role 和仅用于仓库自身开发的项目 Role，但必须使用不同 namespace。例如：

```text
doc-ai-toolkit/technical-writer
doc-ai-toolkit-development/skill-reviewer
```

### 5.5 不做 Prompt 继承或 merge

每次 Role 引用只解析到一份完整 Prompt：

- Workflow 或项目提供完整 Role Prompt；
- Skill 只追加本次 task、输入和业务输出要求；
- 项目不能用同名文件覆盖 Workflow Role；
- 想改变角色行为时必须定义新的 Role ID；
- 相同 Role ID 出现不同 package/version/hash 时 fail closed；
- 框架不做自然语言 merge，也不根据文件发现顺序选择 Prompt。

## 6. 业务 Skill 的最小协作声明

### 6.1 Sidecar 格式

业务 Skill 使用可选的 `collaboration.yaml`：

```text
skills/dita-writing/
├─ SKILL.md
└─ collaboration.yaml
```

```yaml
schema_version: collaboration/v1

role_dependencies:
  - doc-ai-toolkit/technical-writer
  - doc-ai-toolkit/independent-reviewer

steps:
  - id: write
    role: doc-ai-toolkit/technical-writer
    task: 按已批准方案修改目标文档

  - id: review
    role: doc-ai-toolkit/independent-reviewer
    task: 按当前 Skill 的业务规则检查实际修改
    after: [write]
```

v1 只解释四个业务调度字段：

- `role`：使用哪一个 Role Prompt；
- `task`：本次交给该 Role 的具体任务；
- `count`：需要几个独立实例，默认 `1`；
- `after`：依赖哪些 step。

`id` 是步骤稳定标识，不属于角色语义。

### 6.2 Skill 继续持有的业务合同

以下内容继续由 `SKILL.md` 或其业务资源持有：

- 完整业务流程和原始任务正文；
- 输入材料选择与业务上下文；
- 真实业务 artifact；
- 领域检查表和验收标准；
- 人工审批和业务状态转换；
- 在 framework 完全不存在时如何由主 Agent 完成原任务。

### 6.3 Skill 不再复制的框架内容

- Adapter、model、reasoning、permission 和 MCP allowlist；
- Gateway request、state-dir、receipt、lock 和 request journal；
- launch、observe、completion、retry、cancel 和 contamination 处理；
- framework/direct/single 的选择与 route fallback；
- 内部临时 artifact 路径和 Agent instance ID。

## 7. Prompt 组合与执行身份

发送给执行 Agent 的最终输入由三个固定部分组成：

```text
1. Framework envelope
   task/run/step/instance identity、canary、交付协议

2. Role Prompt
   Workflow 或项目提供的唯一完整角色模板

3. Step payload
   本次 task、依赖步骤输出、Skill 指定的业务上下文和输出要求
```

三部分按版本化协议组合，不进行 Prompt 继承或自然语言 merge。Framework envelope 的 identity、canary 和交付合同不能由 Role Prompt 覆盖。

`count: 2` 必须创建两个不同的 Actor Instance；是否并行只是调度优化。两个实例可以使用同一 Adapter/model，但不能复用同一执行身份或把主 Agent 的两次自审伪装成两个独立 Agent。

Role Prompt 中定义“独立 reviewer 不依赖作者总结”等业务行为；框架只机械保证创建不同 instance，并记录实际执行事实。若 direct/single 无法满足业务所需独立性，业务 gate 仍由 Skill 根据返回事实保持未通过。

## 8. Role 到现有 Adapter 的本机路由

### 8.1 Role 与 binding 分离

Workflow 和 Skill 不指定 Adapter。使用者按自己的机器环境配置 Role route：

```yaml
bindings:
  local-codex-reviewer:
    role: doc-ai-toolkit/independent-reviewer
    adapter: codex-native
    model: configured-model
    reasoning: high
    permission: read-only
    workspace: E:\projects\target-repository
    mcp_allowlist: []

  local-cursor-reviewer:
    role: doc-ai-toolkit/independent-reviewer
    adapter: cursor
    model: configured-model
    reasoning: high
    permission: read-only
    workspace: E:\projects\target-repository
    mcp_allowlist: []

role_routes:
  doc-ai-toolkit/independent-reviewer:
    primary: local-codex-reviewer
    fallbacks:
      - local-cursor-reviewer
```

另一位使用者可以将同一个 Role 路由到 Claude Code 或 Codex CLI，而不修改 Workflow、Skill 或 Role Prompt。

### 8.2 Resolver 规则

```text
Role ID
→ 定位并校验唯一 Role Prompt
→ 查找 machine-local role route
→ 取得现有完整 binding
→ 调用现有 exact preflight
→ 冻结本次 Attempt binding
→ 使用已有 Adapter
```

规则如下：

- Role ID 和 binding 中的 role 必须完全一致；
- primary/fallback 由本机配置显式排序，不依赖 JSON/YAML 文件顺序猜测；
- 无 route、引用不存在、Prompt 冲突或 exact preflight 拒绝时 fail closed；
- resolver 不从 `task` 文本猜 permission、MCP、Provider 或 model；
- framework 内 binding fallback 继续使用现有 Gateway/policy 合同；
- admission 拒绝不能通过 route fallback 到 direct 来绕过；
- 已冻结 binding 在 Attempt 内不得静默改变。

### 8.3 Adapter 范围

当前执行路径应准确区分为：

- **Codex App Native 宿主路径**：由 Controller Skill 调用 App 原生 subagent capability；[`internal/adapters/native`](../../internal/adapters/native/native.go) 只承载 Native host contract 和测试用 fake executor，不是与 CLI 包对等的第四个 Adapter；
- **三个 external CLI Adapter**：Claude Code、Cursor Agent 和 Codex CLI 的底层 Adapter 已在 `internal/adapters/claude`、`cursor`、`codexcli` 实现。

本设计不增加新的 Provider Adapter abstraction、不重写 Adapter lifecycle，也不按执行后端拆分 U0–U3。设计时“内部 Adapter 已实现”并不等于消费仓库已有公开调用入口，因此 U1 要求增加下面的 toolkit-side bridge，U3 要求分别验证 Native 宿主路径与三个 external CLI Adapter 的真实 handoff；这些要求现已实现并验收。

### 8.4 External CLI Host-Action Bridge

toolkit 提供一个公开、Provider-neutral 的 **External CLI Host-Action Bridge**。它是 Controller/Gateway 与三个已有 internal Adapter 之间的薄执行边界，不是第四个 Provider Adapter，也不进入业务仓库：

```text
core-issued HostAction
+ Attempt 中冻结的 Binding
+ 本次运行冻结的 machine-local Adapter profile
        ↓
External CLI Host-Action Bridge
        ↓
claude / cursor / codexcli internal Adapter
        ↓
HostActionResult → submitHostActionResult
```

公开 bridge 至少支持两类入口：

1. **Adapter preflight**：在创建 Attempt 前，将候选 Binding 与 machine-local profile 转成对应 Adapter 的 `PreflightRequest`，返回 capability/version/model/trust/MCP 结果；core exact preflight 与 Adapter preflight 必须都通过。
2. **HostAction execute**：映射 Gateway v1 的 `launch`、`observe`、`cancel_host`，并由 bridge 将真实结果直接提交回 Gateway；业务侧不能接收任意 shell 输出后自行构造 `HostActionResult`。

Bridge 以 toolkit 发布的 versioned command/facade 对 Controller 开放，消费仓库无需也不能 import `internal/adapters/*`。Role Prompt、framework envelope 与 step payload 由 core/Router 先组合并冻结；`launch` action 必须携带 exact dispatch prompt，或携带可由 core 校验的 immutable prompt ref + hash。Bridge 只做结构化字段映射，不能再次拼接、改写或从业务文件猜 Prompt。

调用方只提供 state-dir、Attempt/action 标识、Controller identity 与 profile 引用。Bridge 必须从 core 状态取得而不是信任调用方重建 HostAction，并在任何 Provider 调用前核验：

- action ID、Attempt ID、kind、sequence、lease 与 parameters hash；
- Controller identity、canonical workspace、task canary 与 action expiry；
- action 中的冻结 Binding 与 profile 声明的 Adapter 类型一致；
- profile version/hash 与本次 preflight 后冻结的值一致；
- `observe`/`cancel_host` 使用同一 provider handle、session 和 bridge owner。

machine-local Adapter profile 只保存机器相关执行信息，例如 executable、允许的 environment 引用、trust/sandbox/approval 映射、MCP config 路径和 Codex provider 配置；凭据仍来自用户环境或系统凭据存储，不能进入业务仓库、Binding、receipt 或 action parameters。Bridge 按以下来源组装 Adapter request：

| 来源 | 内容 |
| --- | --- |
| frozen Binding | role、Adapter、model、reasoning、permission、workspace、MCP allowlist |
| core-issued action | prompt、task canary、expected output、provider handle/session 和 action 因果数据 |
| frozen Adapter profile | executable、environment ref、trust/sandbox/approval、MCP config 和 Provider 本机参数 |

三个 internal Adapter 当前在进程内持有 session/process handle。为支持 one-shot Gateway/Controller 在后续调用中执行 `observe` 或 `cancel_host`，bridge 必须为每个 active Attempt 维持一个临时 owner，并在 machine-local bridge state 中原子写入 receipt，至少包含 owner PID/start marker、local endpoint、Attempt/action、Adapter/profile hash、workspace、provider handle/session 和最后 heartbeat。具体 IPC 形式由实施计划选择，但必须满足：

- owner 只服务一个冻结 Attempt lineage，并用随机 capability 校验本机请求；
- receipt 可在 Controller 进程重启后重新定位原 owner，但不能据此重启或伪造原 Provider session；
- owner 不可联系或身份不匹配时返回 `stale_owner`/`outcome_unknown`，保留 checkpoint，不静默重新 launch；
- terminal 或确认取消后在 `finally` 中调用对应 Adapter cleanup，并幂等清理 endpoint；owner 准备退出而 execution 仍 active 时必须先 cancel/observe 到可确认边界，取消未确认则保留 receipt 并记录 outcome unknown，不能假装 cleanup 成功；
- 该 owner 是 Attempt 生命周期内的临时 helper，不是常驻 daemon 或通用 mailbox。

Codex App Native 不经过该 CLI bridge，仍由框架 Controller Skill把 core-issued action 映射到 App 原生能力并回传结果。两类宿主路径共享 Gateway、Binding、completion、technical release 和审计合同。

## 9. Framework Skill 与业务 Skill

### 9.1 当前框架 Skill 的职责

[`multi-agent-orchestration` Skill](../../skills/multi-agent-orchestration/SKILL.md) 是基础设施 Controller Skill，不是 reviewer、writer 或 implementer Role。当前它承担 Codex App 与 Go core 之间的宿主桥接：

- 校验 toolkit 安装 receipt；
- 调用 protocol v1 和 Gateway；
- 只执行 core 签发的 HostAction；
- 调用 Codex App Native subagent 或对应 host bridge；
- 回传 action result 和宿主事件；
- 遵守 checkpoint、identity、completion 和 cancel 合同。

Go core 无法直接调用 Codex App 原生 `spawn_agent`，因此该 Skill 仍有必要。

### 9.2 与业务 Skill 的区别

| 对比项 | 框架 Skill | 业务 Skill |
| --- | --- | --- |
| 所有者 | `agent-workflow-toolkit` | Workflow 或具体项目 |
| 关注点 | 如何安全调度和执行 | 业务上做什么 |
| 业务知识 | 不包含 | 包含完整业务规则 |
| Role Prompt | 加载并发送，不定义 | 引用 Workflow/项目定义的 Role |
| Adapter | 执行 resolver/core 已确定的 binding | 不选择 |
| Gateway lifecycle | 持有 | 不复制 |
| 用户是否手工调用 | 正常业务流程不需要 | 是业务入口 |

### 9.3 Role-first 后的使用时机

```text
用户调用业务 Skill
→ Router 发现 collaboration.yaml
→ 计算 effective mode
→ framework 模式下自动进入框架 Controller
→ 加载 Role Prompt、解析 binding、执行计划
→ 将 CollaborationResult 交回业务 Skill
```

模式边界：

- `framework`：Router 使用框架 Controller Skill；
- `direct`：不进入完整 Gateway/Controller loop；
- `single`：不使用 Controller，也不创建子 Agent；
- Router 未安装：业务 Skill 按原正文执行；
- Skill 没有 collaboration 声明：框架 Skill 不参与。

Router 与 Controller 在概念上分层；首版可以继续实现在同一个框架 Skill/组件中，不强制拆成两个 Skill，但文档、入口和测试必须区分职责。

## 10. 三种执行模式

| 模式 | 行为 | Role Prompt | Gateway | subagent |
| --- | --- | --- | --- | --- |
| `framework` | Router/Controller 解析 Role、调度计划，通过 TaskGateway 执行 | 发送给相应 Agent | 使用 | 按 binding |
| `direct` | 主 Agent 按宿主现有能力组织执行 | Router 存在时可发送给原生子 Agent | 不使用 | 允许但不保证 |
| `single` | 主 Agent 顺序完成全部任务 | 可作为任务指导 | 不使用 | 禁止 |

所有模式必须返回实际执行模式、实际 instance 数、输出引用和降级原因。`direct` 不得声称保证 requested count 或独立性；`single` 明确只有主 Agent 一个 actor。

### 10.1 零安装兼容

Router 完全不存在时，不能虚构中央路由决策。`collaboration.yaml` 是可忽略的增强声明，主 Agent 按 `SKILL.md` 的完整正文执行原任务。

因此业务 Skill 不得将业务任务只写在 sidecar 中；安装 framework 不是业务 Skill 能否理解和执行的前提。

## 11. 模式配置与一键关闭

### 11.1 双层控制

1. machine-local `framework_enabled`：默认 `false`，对所有项目拥有 veto；
2. canonical workspace 对应的 mode：`framework/direct/single`，默认 `direct`。

| machine gate | workspace mode | effective mode |
| --- | --- | --- |
| `false` | 未配置 / `direct` / `framework` | `direct` |
| `false` | `single` | `single` |
| `true` | 未配置 / `direct` | `direct` |
| `true` | `single` | `single` |
| `true` | `framework` 且能力检查通过 | `framework` |
| `true` | `framework` 但派发前能力不可用 | 中央 route fallback，默认 `direct` |

安装 core、Controller Skill 或 Role Prompt 不自动启用 framework；global enable 也不自动改变项目 mode。

### 11.2 一键关闭

公开、幂等的 global disable 使用 machine-local active-run index，而不是扫描任意业务目录。每个 framework `CollaborationRun` 创建、恢复和终结时都更新该索引；单条 active entry 至少包含：

- run ID、canonical workspace、state-dir 与 gate epoch；
- Controller 或 external CLI bridge owner identity；
- 当前 active instance、Attempt、HostAction 和最后 action sequence；
- owner endpoint/receipt 引用、heartbeat/update time 与当前 shutdown 状态。

索引只是查找入口，不是运行真相；最终状态仍必须重读对应 state-dir。stale index entry 可以回收，但不能用“索引未找到”证明 Provider 没有运行。

所有同时获取多个锁的路径遵守唯一顺序：

```text
machine gate OS lock
→ active-run index OS lock
→ 按 canonical state-dir path 升序获取 state-dir OS lock
→ 逆序释放
```

只访问一个 state-dir 的现有 Gateway 操作仍可只取该锁；但只要还要读写 gate 或 active-run index，就不得先持有 state-dir 后反向获取上层锁。任何 Provider IPC、进程等待或取消都在释放这些锁后执行。

当前 `agent-workflow-core --gateway` 会在进入 Gateway 前先取得 state-dir 锁，因此 collaboration 路径不能用“先单独读取 gate，再调用旧 one-shot `prepareAttempt`”拼出安全保证。U0 必须由同一个 toolkit coordination facade/command 按上述顺序持有 gate/index，再取得 state-dir 并在同一临界区调用 Gateway admission；普通 protocol v1 one-shot 操作仍可保持只获取 state-dir 的兼容路径。

Attempt admission 是硬安全边界：创建 `CollaborationRun` 时冻结当前 gate epoch；每次创建 Attempt 时，Gateway 必须在持有 machine gate、active-run index 和目标 state-dir 锁的临界区内，重新读取 `framework_enabled` 与 epoch，并核对 run ID、workspace 和 state-dir。gate 已关闭或 epoch 不同均拒绝创建；旧 run 不能在重新 enable 后自动继承新 epoch。

global disable 分两阶段执行：

1. 在上述锁顺序下原子关闭 gate、递增 epoch、重读 active-run index 和对应 state-dir，并把可识别的 active run 标为 shutdown/cancel requested；从此旧 epoch 不能创建 Attempt。
2. 释放锁后联系 Controller/bridge owner，对真实 active HostAction 请求取消；随后按相同锁顺序回写每个 run 的取消结果和审计事件。

Controller 必须在每个新 instance 前重读 gate。Controller/bridge 在每次 `pollHostActions`、提交 non-terminal launch/observe 结果以及请求 Gateway 签发后续 action前也重读 gate。关闭后只允许对已存在的 Attempt 做收敛：提交已执行 action 的真实结果、执行已经签发的 observe、签发/执行 `cancel_host`、记录 terminal 或 outcome-unknown；禁止创建新 Attempt、启动新 instance、签发推进执行的新 `launch`/`observe`、执行 `send`/新 turn、retry 或 fallback。若关闭与 non-terminal launch result 相遇，Gateway 必须保存真实 result 并转入 cancel 收敛，不能再自动签发普通 observe。Gateway 仍在 action 签发和 Attempt admission 处执行最终检查，避免 Controller 检查与 core 写入之间的 TOCTOU。

disable 返回结构化逐 run 汇总：

| 状态 | 含义 |
| --- | --- |
| `cancelled` | 已取得宿主证据，确认 active execution 已停止 |
| `cancel_pending` | 已送达取消请求，但尚无终止确认 |
| `outcome_unknown` | action/进程可能已产生副作用，当前无法判定终态 |
| `not_contacted` | 本轮没有成功联系 owner，尚不能判断它是否仍运行 |
| `stale_owner` | receipt 指向的 PID/start marker、endpoint 或 capability 已失配，不能把新进程当成原 owner |

没有 daemon 时，disable 的同步保证仅是“返回后旧 epoch 不再创建新 Attempt”，不能声称返回即所有 Provider 进程已经停止。调用方根据汇总继续 observe/cancel 或保留 checkpoint；state、receipt、artifact 和审计证据全部保留。

## 12. 安全降级

route fallback 与现有 binding fallback 是两件事：

- route fallback：`framework → direct/single`；
- binding fallback：framework 内在已注册 binding 之间选择新的 Attempt binding。

| 失败类别 | 示例 | 默认处理 |
| --- | --- | --- |
| 正常关闭或框架缺失 | global gate off、项目非 framework、Router 未安装 | 原 Skill 继续 direct；无 Router 时不虚构路由 |
| 派发前能力不可用 | runtime 暂不可用、宿主不支持 requested count | 能保持业务 gate且不扩大权限时降级并报告 degraded |
| 配置或 admission 拒绝 | Role/Prompt 冲突、route 缺失、exact preflight 拒绝 | fail closed，不用 direct 绕过 |
| 已派发或结果不确定 | 已产生副作用、identity contamination、outcome unknown、取消未确认 | 保留 checkpoint，不重复执行 |

降级不能：

- 扩大原 Skill 或宿主权限；
- 自动通过业务审批；
- 把主 Agent 自审伪装成独立 reviewer；
- 把 requested instance 数伪报为实际 instance 数；
- 在未知副作用后重新执行同一步。

## 13. 通用调度语义

### 13.1 `count` 与 `after`

- `count` 默认 `1`；
- `count: 2` 创建两个不同 instance，不要求并行；
- `after` 表示等待指定 step 的全部 instance 完成；
- `after` 省略时，默认依赖前一个声明的 step；
- 第一个 step 可省略 `after`；
- 多个 ready instance 可以并行，也可以因宿主限制串行；
- 下游自动获得依赖 step 的全部输出；
- DAG 必须拒绝环、重复 ID、缺失依赖和 `count < 1`。

不增加 `fan-out`、`fan-in`、`parallel`、`review`、`design` 等专用节点字段。

### 13.2 示例

```yaml
steps:
  - id: draft
    role: workflow/designer
    task: 形成方案

  - id: review
    role: workflow/reviewer
    count: 2
    task: 独立评审方案
    after: [draft]

  - id: risk-check
    role: workflow/risk-reviewer
    task: 检查风险
    after: [draft]

  - id: integrate
    role: workflow/lead
    task: 汇总评审结论
    after: [review, risk-check]
```

## 14. Collaboration 内部自动推进

用户启动一段 collaboration plan，即授权 Scheduler 执行该段已经声明并冻结的 DAG。内部技术步骤完成后可以自动推进，但不能复用当前 `HumanDecision.approve` 实现。现有 Gateway 在 `requestCompletion` 后把 node 置为 `awaiting_human_decision`，现有 Controller Skill 也要求在该状态停止；若不增加独立的 collaboration-scoped 技术放行，自动推进只是文档声明，无法通过当前状态机。

### 14.1 实体映射

framework 模式首版在同一个 core 内建立确定映射，不让 Scheduler 创建第二套 execution 真相；direct/single 只返回实际模式与 actor/output 投影，不虚构 Task/Node/Attempt：

| Collaboration 投影 | 现有 Gateway 实体 | 映射规则 |
| --- | --- | --- |
| `CollaborationRun` | `Task` | 一个 run 对应一个受冻结 plan 约束的 Task；保存 task ID、plan hash 和 scheduler authority ID |
| `StepRun` | 一组 `WorkflowNode` | 一个业务 step；`count` 决定其包含的 instance 数，本身不直接执行 |
| `InstanceRun` | `WorkflowNode` | 每个独立 Actor instance 对应一个 node；`count: 2` 必须是两个 node，不共享执行身份 |
| 一次实际执行 | `Attempt` | 一个 instance 的每次 launch/retry/fallback 都形成不可变 Attempt lineage；不得覆写旧 Attempt |

`HostAction`、`CompletionDecision`、identity、artifact 和 provider evidence 继续挂在 Attempt 上。Step/Instance 状态只能由这些现有记录投影得出，不能单独声称 Provider 已完成。

### 14.2 Run-scoped Scheduler Authority

core 在 `CollaborationRun` 创建时校验并冻结完整 DAG、Role Prompt hash、workspace、effective mode 和 gate epoch，并在该 run 内生成 scheduler authority ID 与作用域。首版不额外建立 Role permission DSL 或通用 authorization subsystem；专用 technical-release operation 只接受 run/instance 标识，并从 core state 解析 authority，调用方不能传入任意 authority string。该 authority：

- 仅绑定一个 run ID、Task ID、revision ID、plan hash、明确的 step/instance 集合和 gate epoch；
- 由 core 保存并在专用技术放行操作内解析，调用方不能传入任意字符串冒充；
- 只授权释放冻结 DAG 中依赖已经满足的下游 instance；
- global disable、run cancel、plan/revision/hash 变化或 epoch 变化后立即失效；
- 不是 `GoalAuthorization`、`HumanDecision` 或业务审批 authority。

Router 通过 typed `createCollaborationRun` 请求创建冻结 run；core 必须先验证 machine/workspace gate、plan schema、Role/route admission 和调用方 Controller identity，再在 state 内生成 authority。请求可保存业务 Skill invocation/host event 作为来源审计，但 Scheduler 不得据此制造 `user-input:*`，也不得把 scheduler authority 写成 `goal:*`。该 authority 表示“core 接纳了这段冻结技术计划”，不是用户身份，也不表示用户预先批准其业务结果。

### 14.3 CollaborationTechnicalRelease

新增专用 core operation（工作名 `recordCollaborationTechnicalRelease`）和不可变记录 `CollaborationTechnicalRelease`，与 `recordHumanDecision`/`HumanDecision` 完全分离。最小记录包含 run、step、instance、Task、Node、Attempt、CompletionDecision、plan hash、scheduler authority、release reason/evidence 和时间。

`requestCompletion` 保持现有行为：先形成不可变 `CompletionDecision` 和 checkpoint。只有同时满足以下条件，Scheduler 才能记录 technical release：

1. node 与 instance 明确属于该冻结 run，Attempt 是当前 checkpoint Attempt，revision/plan hash/epoch 全部匹配；
2. CompletionDecision 为 validated completed，required delivery、identity、canary、terminal event 和 artifact evidence 均成立；
3. 对应 step 的上游依赖满足，run 未 cancel/disable；一个 instance 可独立 technical release，但下游 step 必须等待其所有上游 step 的全部 instance 都已放行；
4. 没有 `delivery_missing`、admission failure、contamination、outcome unknown、取消未确认或其他人工 release 条件。

技术放行只做三件事：把该 instance 标为 **technically delivered**、清除它的技术 checkpoint、允许 Scheduler 计算 frozen DAG 中新的 ready instance。实现需增加与 `NodeApproved` 明确区分的 node 状态/事件；它不能追加 `HumanDecision`、不能产生普通 `approve`、不能修改业务 Workflow 状态，也不能授权 DAG 外的新节点。Task 的技术状态只在不存在其他未释放 checkpoint 时恢复为 active。

若任一条件不满足，保留当前 checkpoint 并返回明确 reason。`delivery_missing` 仍只能由真实用户选择 revision/abort；contamination release 仍必须是直接 `user-input`；未知副作用、取消未确认、失败后的 fallback/retry 继续遵守现有人工与 policy 合同，technical release 不得绕过。

### 14.4 与现有人工 checkpoint 的兼容关系

现有低层 protocol v1 调用方不携带 collaboration run/instance 关联，因而不具备 scheduler authority，行为完全不变：completion 后必须等待 `HumanDecision`。框架 Controller Skill 的规则调整为：

```text
requestCompletion → awaiting_human_decision
  ├─ 普通 protocol v1 node
  │    → 停止并请求真实 HumanDecision
  └─ collaboration-scoped instance
       → 请求 core 校验 technical release
            ├─ eligible → technically delivered → 计算下一批 ready instance
            └─ rejected → 保留 checkpoint → 请求相应真实人工决定
```

因此 Controller Skill 当前“每个 checkpoint 都停下”的规则不是删除，而是收窄为“普通 node 或 technical release 不满足时停下”。专用 operation 必须拒绝缺少 collaboration linkage 的 node，也必须拒绝调用方提供的 `user-input:*`/`goal:*` 作为技术放行替代品。

该授权只允许：

- 创建计划中声明的 step 和 instance；
- 在依赖满足后推进；
- 使用已解析并通过 preflight 的 binding；
- 对满足上述严格条件的成功 completion 记录 technical release；
- 收集结果并交回原业务 Workflow。

它不允许：

- 增加 Role、扩大 count 或修改 task；
- 修改运行中的 Role Prompt；
- 生成或冒充 `HumanDecision`、`user-input:*` 或 `goal:*`；
- 自动批准业务结果或改变业务状态；
- 在 admission、污染、未知副作用或取消未确认后继续。

需要人工业务决策时，由业务 Workflow 将流程拆成两段：

```text
collaboration A
→ 原业务人工审批
→ collaboration B
```

因此 collaboration 完成只表示已交付协作结果，不表示业务审批已经通过。

## 15. 最小持久状态与结果

core 增加三个上层投影及一种 collaboration-scoped release 记录：

- `CollaborationRun`：冻结计划、Role Prompt hash、effective mode、gate epoch、scheduler authority scope 和整体状态；
- `StepRun`：step、依赖、requested count 和完成状态；
- `InstanceRun`：Role、instance identity、binding、现有 node/attempt 引用和输出引用；
- `CollaborationTechnicalRelease`：validated completion 对 frozen DAG 下游的技术放行证据。

这些投影和记录不能再实现一套 Provider execution 状态机，也不能承载业务 approval。framework instance 的实际执行仍以现有 `WorkflowNode/Attempt/HostAction/CompletionDecision` 为唯一事实源，人工业务决定仍只来自 `HumanDecision`。

最小返回结果示例：

```yaml
run_id: run-...
effective_mode: framework
degraded: false
steps:
  - id: review
    role: doc-ai-toolkit/independent-reviewer
    requested_instances: 2
    actual_instances: 2
    outputs:
      - instance_id: review-1
        output_ref: artifact-...
      - instance_id: review-2
        output_ref: artifact-...
```

框架只返回调度和执行事实，不解释“文档是否合格”等业务结论。业务 Skill 读取 Agent 输出并执行自己的 gate。

## 16. Skill 与 Role 的分发

单独安装一个 Skill 时：

```text
读取 role_dependencies
→ 解析 Role Package 依赖闭包
→ 安装 Skill
→ 安装所需 Role Prompt
→ 核验 package/version/hash
→ 写入 Skill、Role、framework revision receipt
```

规则：

- 多个 Skills 使用同一 Role 时复用同一已安装模板；
- 相同 Role ID、package/version、hash 相同则幂等；
- 相同 Role ID 内容不同则停止安装，不静默覆盖；
- 缺失 Role 依赖时，framework 模式 fail closed；
- receipt 必须能证明 Skill 和 Role Prompt 来源；
- 安装或升级后，仍需按宿主发现机制新建任务确认实际加载；
- direct 零安装兼容依赖 `SKILL.md` 完整正文，不依赖伪造 Role Prompt。

## 17. 实施范围 U0–U3

### U0：可选入口与分发基础

1. 定义 `collaboration/v1` 和 `collaboration-roles/v1` 最小格式。
2. 实现 Skill → Role 依赖闭包安装、冲突检查和 receipt。
3. 实现 machine gate、workspace mode 以及公开 `status/enable/disable`。
4. 实现 gate OS lock/epoch、active-run index 与固定锁顺序，并在 Gateway Attempt admission 中原子校验。
5. 定义 Adapter profile 和 bridge receipt 的 machine-local schema、hash 与凭据边界。
6. 提供 Router 入口，使未安装或未启用 framework 时原 Skill 正常执行。

完成条件：clean consumer 未启用 framework 时原 Skill 可执行；单 Skill 安装能带齐 Role 依赖；global disable 返回后旧 epoch 不能创建新 Attempt。

### U1：Role-first 单步骤闭环

1. 实现 Role Registry 和 machine-local `role_routes`。
2. Role ID 确定性解析为现有完整 binding。
3. 继续调用现有 exact preflight，并将 frozen Binding + frozen Adapter profile 映射到 external Adapter preflight，不修改低层 binding 合同。
4. 交付公开 External CLI Host-Action Bridge，把 `launch/observe/cancel_host` 映射到 Claude Code、Cursor Agent、Codex CLI Adapter，并持久化临时 owner receipt 供 observe/cancel 恢复定位。
5. 实现单 step 的 `framework/direct/single` 以及 CollaborationRun/InstanceRun → Task/Node/Attempt 映射。
6. 在 `CollaborationRun` 内实现 run-scoped scheduler authority，并记录 `CollaborationTechnicalRelease`；成功技术 completion 可返回业务 Skill，但不生成 HumanDecision 或业务 approval。
7. 调整框架 Skill，使业务入口自动进入 Router/Controller，并区分普通人工 checkpoint 与 collaboration technical release。

完成条件：只含 `role + task` 的业务 Skill step 可通过用户本机配置使用 Codex App Native 宿主路径或任一 external CLI Adapter；业务文件没有 Provider/model/Gateway lifecycle；普通 protocol v1 node 仍逐节点等待真实 HumanDecision。

### U2：通用多步骤调度

1. 实现 `count`、`after`、默认顺序和 DAG 校验。
2. 扩展 durable run/step/instance 投影和 active-run index，保持与 Task/Node/Attempt 的一一映射不变量。
3. collaboration 内仅通过 technical release 按冻结计划自动推进，并在业务 Workflow 边界返回结果。
4. 支持取消、恢复、运行中 global disable 和 outcome-unknown checkpoint。
5. 统一返回 effective mode、requested/actual instance 数、输出引用和降级原因。

完成条件：同一个 `designer ×1 → reviewer ×2 → lead ×1` intent 可运行于三种模式；framework 使用对应 Native 宿主或 external CLI Adapter 创建真实 instance，direct/single 如实报告实际能力，不伪造独立性或实例数。

### U3：发布与真实接入验收

1. 更新接入指南，将 role-first 作为普通路径，将完整 binding/Gateway loop 保留为高级低层参考。
2. 更新 architecture、框架 Skill、CLI help、policy/schema、installer 和 capability matrix。
3. 用可移植 Workflow 将单个 Skill 及其 Role 依赖安装到 clean consumer。
4. 证明只修改 machine-local route/profile 即可在支持的执行路径之间切换，不修改业务 Skill/Role Prompt。
5. 对 Codex App Native 宿主路径做真实 role-first smoke；它继续通过 Controller Skill 使用 App 原生 capability。
6. 对 public bridge + Claude Code、Cursor Agent、Codex CLI 分别做真实 role-first handoff/smoke，覆盖 launch → observe → completion、cancel_host 和 cleanup；不以 internal Adapter 单测或 shell shim 替代。
7. 验证 technical release、旧 protocol 人工 checkpoint、disable 并发 interleaving、stale owner 和 outcome-unknown fault injection。
8. 使用独立 reviewer 检查指南、实际实现、原始目标和消费体验。

完成条件：形成新的 acceptance commit；文档、CLI、Role 分发、Router/Controller、Gateway 和真实宿主行为一致。

## 18. 验收矩阵

| ID | 场景 | 预期结果 |
| --- | --- | --- |
| COLLAB-001 | 无 framework/Router 运行现有业务 Skill | 按完整 Skill 正文执行，不虚构 Router/Gateway state |
| COLLAB-002 | 已安装但 machine gate 默认关闭 | effective mode 为 direct；安装不等于启用 |
| COLLAB-003 | global on、workspace 未选择 framework | 仍为 direct |
| COLLAB-004 | Workflow Role + Skill 单独安装 | Role 依赖闭包、version/hash/receipt 全部可核验 |
| COLLAB-005 | 同 Role ID 内容冲突 | fail closed，不覆盖、不按发现顺序选择 |
| COLLAB-006 | Role route 唯一且 binding/profile 已注册 | 通过 core exact preflight 与对应宿主/Adapter preflight |
| COLLAB-007 | 用户切换 Role route/profile | 不修改 Skill/Role Prompt 即可切换支持的执行路径 |
| COLLAB-008 | Role/route 缺失或 preflight 拒绝 | fail closed，不降级 direct 绕过 admission |
| COLLAB-009 | `count=2` | 创建两个不同 instance；并行不是必要条件 |
| COLLAB-010 | `after` DAG | 只有全部依赖完成后才执行下游，并收到全部上游结果 |
| COLLAB-011 | `single` | 不调用任何 spawn/subagent/Gateway 入口 |
| COLLAB-012 | direct 能力不足 | 如实报告 actual instance 数和 degraded，不伪造独立性 |
| COLLAB-013 | collaboration 内 validated completion | 记录专用 technical release 后按冻结 DAG 推进；无 HumanDecision、`user-input:*` 或业务 approval |
| COLLAB-014 | 运行中 global disable | 旧 epoch 不再创建 Attempt；按 active-run index 收敛并逐 run 返回五类取消状态 |
| COLLAB-015 | outcome unknown/contamination | 不切到 direct/single 重做同一步 |
| COLLAB-016 | 业务 Skill 静态检查 | 只有 role/task/count/after 与业务合同，无 Adapter/Gateway 复制 |
| COLLAB-017 | 框架 Skill 使用体验 | 用户只调用业务 Skill，framework 模式自动进入 Controller |
| COLLAB-018 | Codex App Native 宿主路径 + 三个 external CLI Adapter | Native 不经过 CLI bridge；public bridge 分别映射三个 CLI 且真实 smoke 通过，无底层 Adapter 重实现 |
| COLLAB-019 | protocol v1 旧调用方 | 无 collaboration authority 时 completion 后仍等待真实 HumanDecision，既有 conformance 保持兼容 |
| COLLAB-020 | technical release 非法输入 | 普通 node、伪造 scheduler authority、`user-input:*`/`goal:*` 替代和失败 completion 全部 fail closed |
| COLLAB-021 | external CLI owner 恢复 | observe/cancel 只能定位同一 PID/start marker/capability/profile hash；stale owner 不重新 launch |
| COLLAB-022 | Bridge action 验证 | action/attempt/lease/sequence/hash/workspace/binding/canary 任一不匹配时 Provider 调用数为零 |
| COLLAB-023 | disable 与 admission/poll race | 固定锁顺序无死锁；disable 线性化后无新 Attempt 或推进执行的新 launch/observe，只允许结果记录与 cancel 收敛 |
| COLLAB-024 | 独立目标复审 | 同时验证指南与实现一致、实现与原始 role-first 目标一致 |

最低验证包括：

- `go test -count=1 ./...`；
- `go vet ./...`；
- `go build ./...`；
- Linux CGO-disabled build；
- `git diff --check` 和 Markdown 本地链接检查；
- clean consumer Skill + Role dependency smoke；
- 三态路由与 global disable lock/epoch/active-index interleaving fault tests；
- DAG、实体映射、technical release、旧人工 checkpoint、恢复和 outcome-unknown tests；
- Codex App Native 真实 smoke；
- public bridge + Claude Code、Cursor Agent、Codex CLI 的真实 role-first launch/observe/cancel handoff smoke；
- bridge mismatch 零 Provider 调用、stale owner 和 cleanup tests；
- 独立 broad review 与 targeted closure。

## 19. 下游接入后的目标体验

上游实现并发布可固定版本后，`doc-ai-toolkit` 等 Workflow 仓库只需要：

1. 集中定义并版本化 Workflow Role Prompt；
2. 在需要协作的 Skill 中增加简短 `collaboration.yaml`；
3. 保留 Skill 的完整业务正文和零安装执行能力；
4. 安装时携带 Role 依赖闭包和 receipt；
5. 不在业务仓库维护 Adapter/model/Gateway loop；
6. 不要求最终使用者显式调用框架 Skill。

最终使用者只在 machine-local 配置中：

1. 安装并显式启用 framework；
2. 为目标 workspace 选择 mode；
3. 将 Workflow Role 映射到自己可用的 binding/Adapter；
4. 用 `status` 查看配置来源、effective mode 和降级原因；
5. 必要时一键 global disable。

普通代码仓库可以使用项目 Role Package；可复用代码开发或 Workflow 开发套件则可以像 `doc-ai-toolkit` 一样随套件分发 Role Prompt。

## 20. 开放实现项

以下事项由实施计划决定，不改变本设计：

- CLI 子命令的最终命名；
- machine-local 配置在 Windows、macOS、Linux 的具体路径；
- Role Prompt 占位符、转义与 context 大小限制；
- Role Package 和 Skill sidecar 的 installer 命令形式；
- Router 与 Controller 是否在首版共用一个 Skill/组件；
- collaboration 投影扩展现有 snapshot，还是由同一 core 中的上层模块保存；
- external CLI bridge 临时 owner 的本机 IPC 形式与 receipt 具体路径；
- direct 模式可调用哪些宿主原生协作能力及如何报告 capability。

这些实现项不得改变以下不变量：

- 业务 Skill role-first；
- Role 行为由 Workflow/项目的一份完整 Prompt 定义；
- Adapter 由使用者本机灵活绑定；
- 通用机制只实现一次；
- framework 缺失或关闭时原业务流程可继续；
- admission 和未知副作用不能被降级绕过；
- collaboration technical release 不伪造人工 authority，也不替代业务审批；
- external CLI 必须经过 toolkit public bridge，业务仓库不拼 Provider shell 或伪造 result；
- framework Skill 是自动进入的基础设施运行时，不是第二个用户业务入口。

## 21. 下一步

1. 基于本设计编写 U0–U3 实施计划、文件 ownership、迁移顺序和验证矩阵。
2. 先在上游实现通用入口，不在 `doc-ai-toolkit` 提前复制临时 Router 或 Gateway shim。
3. 实现完成后由独立 reviewer 同时核对原始 role-first 目标、设计、代码、指南和真实消费体验。
4. 上游形成新的 acceptance commit 后，再选择 `doc-ai-toolkit` 中一个可单独安装的 Skill 做真实接入。

截至 2026-08-13，U0–U3 的核心合同、Role Registry、三态 route/run 创建入口、technical release、external CLI public bridge、依赖闭包安装器、clean-consumer smoke、三 CLI public bridge handoff和 Codex App Native role-first smoke 已实现并形成证据；最终独立 closure、全量回归和 acceptance commit `f3851c0` 均已完成。本文保留为设计基线，当前能力与使用方式以架构说明、接入指南和实现证据为准。
