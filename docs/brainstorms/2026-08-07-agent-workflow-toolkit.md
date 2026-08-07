# Agent Workflow Toolkit 仓库定位与首个 milestone

- 日期：2026-08-07
- 状态：已确认

## 背景与目标

现有多 Agent 研究最初落在 `doc-ai-toolkit`，但其中包含可被其他领域复用的调用、编排和可靠性能力，因此决定建立独立仓库 `E:\projects\agent-workflow-toolkit`。

进一步讨论确认：新仓库不能被永久定义成“多 Agent 调度器”。后续还可能承载 brainstorming Skill 改进、上下文自动压缩、handoff、memory/context 管理以及当前尚未预见的其他通用 Agent 能力。多 Agent 协同只是第一个明确的 milestone，而不是仓库的功能上限。

仓库目标是提供一个可持续演进的位置，用于沉淀跨领域复用的 Agent 工作方法、Skills、运行能力和工程实践，同时避免与具体业务 Workflow 混在一起。

## 顶层范围原则

本仓库采用开放而非枚举式的范围定义。以下内容适合进入本仓库：

- 面向 Agent 本身的能力、工作方法或工程基础设施；
- 具有合理的跨项目、跨领域或跨 Workflow 复用价值；
- 能与具体产品、文档或业务语义保持清晰边界；
- 可以形成独立 Skill、模块、脚本、接口、测试、评测或研究资产。

当前分类只是示例，不是白名单。未来能力即使不属于 Skills、context、多 Agent 或 Adapter，只要符合上述原则，也可以在本仓库发展。对于尚未证明复用价值的想法，可以先以聚焦的实验、研究或模块开始，不要求预先抽象成完整平台。

## 当前已识别方向

### 通用 Skills

- brainstorming Skill 的问题设计、交互、确认和产物质量改进；
- 规划、研究、执行、评审等可跨领域使用的工作方法；
- 后续发现的其他通用 Skills。

### 上下文生命周期

- 上下文自动压缩；
- 摘要完整性和事实保真；
- handoff、恢复、memory 和 context 管理；
- 长任务和跨会话连续性。

### Agent 调用与编排

- 单 Agent 或多 Agent 的任务协议、调度和结果交付；
- Codex App、Claude Code、Cursor、Codex CLI 等宿主/执行端 Adapter；
- session identity、authority、canary、权限和 workspace 隔离；
- explorer、worker、reviewer、fix/re-review 等通用编排原语。

### 工程支撑

- 通用测试、评测、诊断、安装、分发和维护能力；
- MCP、工具和配置管理；
- 后续出现的其他 Agent 工程能力。

以上方向不构成固定路线图或完整分类。

## 与领域仓库的边界

### agent-workflow-toolkit

- 通用 Agent 能力、Skills、工作方法和工程基础设施；
- 可跨领域复用的上下文、调用、编排、测试与诊断能力；
- 与具体领域解耦的研究和设计资产。

### doc-ai-toolkit 等领域仓库

- 领域任务拆分与路由；
- DITA、需求分析、发布说明等具体角色和 Prompt；
- 领域 MCP 选择、产物位置和业务 gate；
- 安装或调用通用仓库提供的能力。

依赖方向为：

```text
领域仓库 → agent-workflow-toolkit
```

通用仓库不反向依赖具体领域仓库。

## 选定方案

采用一个可扩展的通用 Agent 能力仓库，同时容纳通用 Skills、上下文能力、多 Agent 编排、Adapter 和其他未来能力。暂不拆分成 `agent-skills`、`agent-context` 等多个仓库，也不预设最终目录分类；当真实规模、发布周期或依赖冲突证明需要拆分时再重新决策。

## 当前首个 milestone：多 Agent 协同 MVP

当前优先交付：

- Codex App 原生 Subagent；
- Claude Code `deepseek-v4-pro`；
- Cursor Agent `cursor-grok-4.5-high`，非 Fast；
- Codex CLI `gpt-5.6-luna` + `max`；
- Task/Attempt/Completion schema；
- 最小并发、超时、取消和结果收集；
- task canary、identity/authority 核验和 `contaminated` 状态；
- explorer/worker/reviewer 最小闭环；
- 所有主要执行路径的真实 smoke test。

以下仅是这个 milestone 的非目标：SQLite、看板、常驻 daemon、tmux、完整 mailbox、长期恢复平台以及 OpenCode/Kilo 正式支持。它们不是整个仓库的永久禁区，未来有独立需求时可以重新评估。

## 仓库级约束

- 不依赖具体领域仓库或把领域业务语义写入通用核心。
- 不把当前分类、Provider、模型、Skill 或实现方式固化为永久封闭集合。
- 不默认扩大权限，不把凭据和秘密写入仓库或运行产物。
- 每项能力独立说明自己的范围、成熟度、验证方式和适用边界。
- 避免为了假想未来一次性设计完整平台，以真实用例推动抽象和拆分。

## 成功标准

### 仓库定位

- 新的通用 Agent 能力可以在不重写仓库定位的情况下加入。
- milestone 的技术限制不会被误认为整个仓库的永久规则。
- 领域仓库能够复用能力，而通用仓库无需理解领域业务。

### 当前 multi-agent milestone

- 四条主要执行路径均能在本机运行并保存可复验证据。
- 模型覆盖、MCP allowlist、workspace、超时、取消和结构化输出都有测试。
- retry 新建 Attempt，不覆盖历史证据。
- authority 或 identity 变化时拒绝误 resume。
- worker 和 reviewer 使用独立任务上下文并核对真实证据。

## 开放问题

- [ ] 顶层目录如何支持不同形态的 Skill、模块、研究和工具，而不提前固化技术栈。
- [ ] 各能力如何声明状态、兼容宿主、依赖、安装方式和验证证据。
- [ ] multi-agent milestone 的实现语言、包结构和协议版本。
- [ ] brainstorming Skill 改进的具体问题与成功标准。
- [ ] 上下文自动压缩的触发、保真、恢复和评测方法。
- [ ] 后续何时需要拆仓或独立发布某项能力。

## 下一步

1. 新会话先确定可扩展但不过度设计的仓库骨架。
2. 为当前 multi-agent milestone 编写实施计划和 smoke test matrix。
3. 分阶段跑通多 Agent MVP。
4. brainstorming、上下文压缩等能力分别开展独立讨论和设计，不被当前 milestone 阻塞。
5. multi-agent MVP 稳定后回到 `doc-ai-toolkit` 实现首个领域集成。
