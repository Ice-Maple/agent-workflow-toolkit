# Agent Workflow Toolkit

面向 Agent 工作方式、能力扩展和工程实践的通用工具仓库。多 Agent 协同 MVP 的 Wave 0–4 已验收为受控只读 `experimental`；Role-first U0–U3 已实现业务 Skill 与项目 Role 两类 managed entry，但项目接入必须固定到包含真实 Cursor framework 组合回归的新 acceptance commit，不能继续固定已被组合缺陷否决的 `d68a143`。Wave 5 尚未启动。

Role-first installer 会用 schema-v2 receipt 固定同一 clean revision 的 core、collaboration、bridge 三个 binary，并安装统一 managed entry。正常用户只调用业务 Skill或在项目规则中点名已注册 Role；`zero-install/direct/single/framework` 路由由 Controller 一处承载，不要求业务仓库复制底层编排。

## 仓库定位

本仓库不是只解决某一种 Workflow，也不以当前列出的功能作为永久边界。它用于持续沉淀跨领域可复用的 Agent 能力，例如：

- 通用 Skills 及其改进，如 brainstorming、规划、评审和研究方法；
- 上下文生命周期能力，如自动压缩、handoff、恢复、memory 与 context 管理；
- 单 Agent 或多 Agent 的编排、任务协议和可靠性机制；
- Codex App、Claude Code、Cursor、Codex CLI 等宿主或执行端 Adapter；
- 工具/MCP 使用、权限、workspace、session 和配置隔离能力；
- 通用的评测、测试、诊断、安装、分发和维护工具；
- 后续出现的其他通用 Agent 能力。

以上是当前已知方向，不是功能白名单。新能力不需要属于现有分类，只要它具有合理的跨领域复用价值，并能与具体业务语义保持清晰边界，就可以在本仓库中发展。

## 与领域仓库的边界

通常符合以下特征的内容适合放在本仓库：

- 面向 Agent 本身的能力、工作方法或基础设施；
- 可以被多个项目、领域或 Workflow 使用；
- 不依赖 DITA、JIRA、发布说明等具体产品或业务语义；
- 能形成独立的说明、接口、Skill、脚本、测试或评测资产。

具体领域的任务规则、Prompt、产物和验收标准留在领域仓库。领域仓库可以依赖本仓库，本仓库不反向依赖具体领域仓库。

```text
领域仓库 ──调用或安装──> agent-workflow-toolkit
```

## 当前 milestone：多 Agent 协同 MVP

首个 milestone 聚焦多 Agent 调用和轻量编排：

- Codex App 原生 Subagent；
- Claude Code：`deepseek-v4-pro`；
- Cursor Agent：`cursor-grok-4.5-high`，不启用 Fast；
- Codex CLI：`gpt-5.6-luna` + `max`；
- Task/Attempt/Completion 协议；
- 最小并发、超时、取消和结果收集；
- task canary、身份核验和 `contaminated` 状态；
- explorer/worker/reviewer 最小闭环；
- 每条执行路径的真实 smoke test。

SQLite、看板、daemon、tmux、完整 mailbox、OpenCode/Kilo 和复杂恢复平台仅不属于这个 milestone，并非整个仓库永远禁止的方向。未来能力若有明确需求和收益，可以重新评估。

## 已识别的后续方向

- brainstorming Skill 的方法、交互和产物改进；
- 上下文自动压缩、摘要质量、handoff 与恢复；
- 其他跨宿主、跨领域的通用 Skills 和 Agent 工程能力。

这些方向不是固定路线图或范围上限，优先级和实现方式在各自开始前单独讨论。

## 使用入口

- [功能入口清单](docs/guides/agent-capabilities.md)
- [多 Agent 协同框架架构说明](docs/architecture/multi-agent-orchestration-architecture.md)
- [多 Agent 协同框架项目接入与使用指南](docs/guides/multi-agent-orchestration-project-adoption.md)
- [Role-first 可选协作入口设计](docs/brainstorms/2026-08-12-role-first-collaboration-entrypoint.md)

## 设计资料

- [仓库定位、需求与首个 milestone](docs/brainstorms/2026-08-07-agent-workflow-toolkit.md)
- [通用 Agent 能力独立建仓决策](docs/decisions/0001-repository-boundary.md)
- [多 Agent Workflow 总研究与初步设计](docs/research/2026-08-06-multi-agent-workflow-research-and-initial-design.md)
- [多 Agent 协同框架架构说明](docs/architecture/multi-agent-orchestration-architecture.md)
- [CCG Workflow 研究](docs/research/2026-08-06-ccg-workflow-multi-agent-study.md)
- [oh-my-openagent 研究](docs/research/2026-08-06-oh-my-openagent-multi-agent-study.md)
- [Claude Codex Bridge 研究](docs/research/2026-08-06-claude-codex-bridge-multi-agent-study.md)

当前多 Agent 运行时已覆盖 Codex App Native、Cursor Agent、Claude Code 和 Codex CLI 的受控只读路径，并已完成 role-first U0–U3、跨 Adapter replay/conformance、Windows 稳定性、安全策略、完整 smoke/benchmark 与人工 `experimental` 验收。业务侧通常只定义 Role Prompt 和 `role/task/count/after`，本机 route 决定 Adapter/model；后续只有在另行授权后才进入 Wave 5 领域试点。
