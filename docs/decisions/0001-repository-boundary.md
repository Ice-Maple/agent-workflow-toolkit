# 0001：通用 Agent 能力独立建仓

- 状态：Accepted
- 日期：2026-08-07

## 背景

Agent 能力建设同时包含跨领域的工作方法、Skills、上下文管理、调用编排和工程基础设施，也包含具体业务 Workflow。若全部放在 `doc-ai-toolkit`，通用能力会与 DITA、JIRA、发布说明等业务语义形成错误耦合。

最初建仓讨论以多 Agent 协同为切入点，但后续明确还需要容纳 brainstorming Skill 改进、上下文自动压缩以及未来尚未预见的其他通用 Agent 能力。因此仓库不能以当前 multi-agent MVP 作为永久范围定义。

## 决策

建立独立仓库 `agent-workflow-toolkit`，作为可持续演进的通用 Agent 能力仓库。

仓库可承载但不限于：

- 通用 Skills 和工作方法；
- 上下文压缩、handoff、恢复、memory/context 管理；
- 单 Agent 或多 Agent 的协议、运行时和编排；
- 宿主、Provider、CLI、工具和 MCP Adapter；
- 通用测试、评测、诊断、安装和分发能力；
- 其他与具体业务解耦、具有复用价值的 Agent 能力。

上述分类不是白名单，也不是固定目录结构。新能力按自身需求选择 Skill、模块、脚本、研究或工具等合适形态。

## 边界原则

具体领域仓库继续拥有领域任务、Prompt、产物、数据模型和验收规则。通用仓库拥有与领域解耦的能力和方法。

```text
领域仓库 → agent-workflow-toolkit
```

`agent-workflow-toolkit` 不得导入或依赖具体领域仓库。领域仓库可以安装、调用或固定版本使用本仓库的能力。

## 当前首个 milestone

首个 milestone 是多 Agent 协同 MVP，覆盖 Codex App 原生 Subagent、Claude Code、Cursor Agent 和 Codex CLI，以及相应的 Task/Attempt/Completion、隔离、取消和 smoke test。

SQLite、看板、daemon、tmux、完整 mailbox 等只是不属于当前 milestone，不是仓库级永久禁止项。

## 影响

- brainstorming、上下文压缩等通用能力可以与 multi-agent 能力在同一仓库发展。
- 仓库需要允许不同形态、不同成熟度的能力共存，而不能假设所有内容都是同一种运行时 package。
- milestone 或模块的局部约束必须写在局部文档中，不能无意升级为全仓库规则。
- 具体领域 Workflow 仍由领域仓库维护，避免通用仓库变成业务集合。
- 暂不因未来可能增长而提前拆分多个仓库；真实发布、依赖或维护冲突出现后再决策。
