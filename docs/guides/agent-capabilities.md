# Agent Workflow Toolkit 功能入口清单

这份清单回答一个问题：**我想使用某项能力，应该从哪里开始？**

仓库里的目录不都代表同一种东西：`SKILL.md` 是给模型加载的工作方法，
`hooks.json` 是宿主事件配置，`main.go` 是 CLI 入口，`tests/` 和 `docs/` 是验证与说明材料。
不要把这些目录当成可以互相替代的功能入口。

## 先按需求选择

| 你想做什么 | 功能类型 | 主要入口 | 当前状态 |
| --- | --- | --- | --- |
| 把模糊想法梳理成需求、比较方案并生成 brainstorm 文档 | Skill | [`skills/brainstorming/SKILL.md`](../../skills/brainstorming/SKILL.md) | 可用的 Skill |
| 降低 Codex 自动压缩后的遗忘和重复执行 | Hook 运行包 | [`tests/context_continuity/fixtures/wave3/hooks.example.json`](../../tests/context_continuity/fixtures/wave3/hooks.example.json) | Wave 3 已对当前支持的 Windows ChatGPT/Codex App 宿主关闭 |
| 验证上下文连续性 Hook 的本地与宿主行为 | 测试/App smoke | [`tests/context_continuity/README.md`](../../tests/context_continuity/README.md) | 70 项回归、逐 case 宿主证据和独立复核均已通过 |
| 校验多 Agent 协同请求/响应协议 | Go CLI + Protocol | [`cmd/agent-workflow-core/main.go`](../../cmd/agent-workflow-core/main.go) | Wave 0 协议骨架 |
| 接入 Cursor Agent 外部 Adapter | Go Adapter | [`internal/adapters/cursor/cursor.go`](../../internal/adapters/cursor/cursor.go) | 受控只读 Windows 路径已随 Wave 4 验收为 `experimental` |
| 接入 Claude Code 外部 Adapter | Go Adapter | [`internal/adapters/claude/claude.go`](../../internal/adapters/claude/claude.go) | 受控只读路径已随 Wave 4 验收为 `experimental` |
| 接入 Codex CLI 外部 Adapter | Go Adapter | [`internal/adapters/codexcli/codex.go`](../../internal/adapters/codexcli/codex.go) | 受控只读路径已随 Wave 4 验收为 `experimental` |
| 了解多 Agent 协同框架的实际架构 | Architecture | [`docs/architecture/multi-agent-orchestration-architecture.md`](../architecture/multi-agent-orchestration-architecture.md) | Wave 0–4 当前实现与边界的主说明 |
| 在具体项目中接入多 Agent 协同框架 | Guide | [`docs/guides/multi-agent-orchestration-project-adoption.md`](multi-agent-orchestration-project-adoption.md) | 当前 `experimental` 只读路径的最小落地指南 |
| 了解多 Agent MVP 的整体设计和 Wave | 设计/计划 | [`docs/plans/2026-08-07-multi-agent-orchestration-mvp-waves.md`](../plans/2026-08-07-multi-agent-orchestration-mvp-waves.md) | 计划与实现并行演进，不是直接运行入口 |

## 各功能的实际入口

### 1. Brainstorming

这是当前仓库中最典型的 Skill。

- Skill 文件：[`skills/brainstorming/SKILL.md`](../../skills/brainstorming/SKILL.md)
- 可选视觉辅助说明：[`skills/brainstorming/visual-companion.md`](../../skills/brainstorming/visual-companion.md)
- 产物目录：`docs/brainstorms/`
- 测试与案例：`tests/brainstorming/`
- 使用方式：让 Agent 进行需求头脑风暴，或在支持 Skill 调用的宿主中调用 `brainstorming`。

它主要改变 Agent 的提问、比较和落盘方式，不负责监听 Codex 的压缩或工具生命周期事件。

### 2. Codex 上下文连续性

这是一个 Hook 运行包，不是当前可直接调用的 Skill。

- 配置入口：[`tests/context_continuity/fixtures/wave3/hooks.example.json`](../../tests/context_continuity/fixtures/wave3/hooks.example.json)
- 运行脚本：[`tools/context_continuity/`](../../tools/context_continuity/)
- 用户指南：[`docs/guides/codex-context-continuity.md`](codex-context-continuity.md)
- 跨环境安装与还原：[`docs/guides/codex-context-continuity-global-setup.md`](codex-context-continuity-global-setup.md)
- 测试说明：[`tests/context_continuity/README.md`](../../tests/context_continuity/README.md)
- 本地 smoke 证据：[`tests/context_continuity/evidence/2026-08-07/W3-local-cli-smoke.md`](../../tests/context_continuity/evidence/2026-08-07/W3-local-cli-smoke.md)
- 当前 Windows App 宿主证据：[`tests/context_continuity/evidence/2026-08-11/W3-app-host-smoke.md`](../../tests/context_continuity/evidence/2026-08-11/W3-app-host-smoke.md)

Wave 3 已对 Windows ChatGPT/Codex App `26.803.5235.0` Store x64 关闭，
Wave 4 可以从评测开始；其他机器或宿主版本仍需单独加入兼容性矩阵。

启用时，宿主通过已信任的用户级 `~/.codex/hooks.json` 或项目级
`.codex/hooks.json` 自动调用这些脚本：

```text
SessionStart    → session_start.py
PreCompact      → pre_compact.py
PostCompact     → post_compact.py
PreToolUse      → pre_tool_use.py
SubagentStart   → subagent_start.py
SubagentStop    → subagent_stop.py
```

Wave 3 默认配置不注册 `Stop`；[`stop.py`](../../tools/context_continuity/stop.py) 只保留给
历史严格 Fixture 回归。其中 [`seal.py`](../../tools/context_continuity/seal.py) 是恢复核对或
显式保存 checkpoint 后运行的手动确认入口；
[`state.py`](../../tools/context_continuity/state.py) 和 [`tool_policy.py`](../../tools/context_continuity/tool_policy.py)
是内部状态与分类实现，不是给用户单独调用的功能入口。

状态默认写到 `CODEX_CONTEXT_CONTINUITY_HOME` 指定的用户私有目录，而不是仓库。

当前没有 `skills/context-continuity/SKILL.md`。因此，checkpoint 应该写什么、恢复后如何核对，
目前主要由 Hook 提示、用户指南和设计文档共同说明；后续可以增加一个模型行为 Skill，
但它不能替代这些 Hook。

### 3. 多 Agent 协同 MVP

这是 Go CLI、协议和 Adapter 组成的运行时能力，不是当前已交付的单一 Skill。

- CLI 入口：[`cmd/agent-workflow-core/main.go`](../../cmd/agent-workflow-core/main.go)
- 请求/响应合同：[`protocol/v1/request.schema.json`](../../protocol/v1/request.schema.json) 和 [`protocol/v1/response.schema.json`](../../protocol/v1/response.schema.json)
- 共享协议实现：`internal/protocol/v1/`
- Adapter 实现：`internal/adapters/`
- 测试与运行说明：[`tests/orchestration/README.md`](../../tests/orchestration/README.md)
- 当前架构主文档：[`docs/architecture/multi-agent-orchestration-architecture.md`](../architecture/multi-agent-orchestration-architecture.md)
- 项目接入指南：[`docs/guides/multi-agent-orchestration-project-adoption.md`](multi-agent-orchestration-project-adoption.md)
- 设计与计划：[`docs/brainstorms/2026-08-07-multi-agent-orchestration-mvp.md`](../brainstorms/2026-08-07-multi-agent-orchestration-mvp.md) 和 [`docs/plans/2026-08-07-multi-agent-orchestration-mvp-waves.md`](../plans/2026-08-07-multi-agent-orchestration-mvp-waves.md)

`skills/multi-agent-orchestration/` Controller Skill 已完成真实 Codex App Native smoke、artifact 校验和人工批准，
详见 `tests/orchestration/evidence/2026-08-09/W2-native-app-smoke.md`；Wave 2 Native MVP 已验收。Wave 3 的 Cursor Agent、
Claude Code 和 Codex CLI 只读 Adapter 均已完成实现、真实 smoke、独立 review 和 targeted closure，记录见
[`Wave 3 当前状态`](../../docs/reviews/2026-08-09-wave3-current-status.md)。Wave 4 已补齐跨路径硬化、完整 benchmark 和最终独立 review；四条受控只读路径现已验收为 `experimental`，记录见 [`W4-experimental-acceptance.md`](../../tests/orchestration/evidence/2026-08-11/W4-experimental-acceptance.md)。后续 project-adoption hardening 又补齐 Skill receipt、public CLI E2E、跨进程锁/request journal/state schema，并通过真实 Native smoke 与 Cursor targeted closure，见 [`AWT-adoption-hardening.md`](../../tests/orchestration/evidence/2026-08-11/AWT-adoption-hardening.md)。

## 按目录判断“这是什么”

| 目录/文件形态 | 含义 | 是否通常是用户入口 |
| --- | --- | --- |
| `skills/<name>/SKILL.md` | 模型可加载的 Skill 和行为方法 | 是 |
| `tests/*/fixtures/*.json` | 配置或 Hook 示例模板 | 是配置入口，不是实现本身 |
| `tools/<name>/*.py` | Hook、脚本或运行时辅助实现 | 由配置或其他入口间接调用 |
| `cmd/<name>/main.go` | 可执行 CLI 的主入口 | 是 |
| `internal/` | Go 内部实现、Adapter、状态和协议逻辑 | 否，通常不直接调用 |
| `protocol/` | 跨进程 JSON 合同/schema | 否，供 CLI 和 Adapter 使用 |
| `tests/` | 回归测试、fixture、smoke 和脱敏证据 | 否 |
| `docs/brainstorms/`、`docs/plans/`、`docs/reviews/` | 设计、计划和评审记录 | 否 |
| `docs/guides/` | 面向用户的使用说明 | 是阅读入口 |

## 当前仍存在的整理缺口

1. 上下文连续性只有 Hook 入口，没有对应的模型行为 Skill。
2. 多 Agent Wave 0–4 已完成，四条受控只读执行路径已验收为 `experimental`；尚未开始 Wave 5 领域试点、写入能力或生产化验证。
3. 以前各功能按各自 Wave 逐步加入，暂时没有统一的 capability manifest 或根级入口索引。

因此，今后新增功能至少应同时登记：

- 功能类型：Skill、Hook、CLI、Protocol 或组合；
- 唯一主要入口；
- 实现目录；
- 用户文档；
- 测试/证据位置；
- 当前成熟度和未验证边界。

本文件就是当前仓库的统一导航页；它不是新的运行时依赖，也不会改变现有 Skill、Hook 或 CLI 路径。
## Wave 4 status update (2026-08-10)

Wave 3 的三个外部 Adapter 已关闭实现和 review checkpoint，但尚未声明
`experimental`。Wave 4 当前从共享 JSONL replay、跨 Adapter lifecycle
conformance 和证据矩阵基线开始，详细范围见
[`Wave 4 baseline`](../../docs/reviews/2026-08-10-wave4-baseline.md)。

## Wave 4 status update (2026-08-11)

Windows 真实进程树与 atomic replace 故障注入、跨 Adapter delivery
recovery/contamination、策略与 Goal/revision/secret 边界以及原计划七个维度的
完整 benchmark 已补齐；最终独立 Cursor review 已 PASS 且无 findings。用户已按当前证据和只读边界接受 Wave 4 为 `experimental`；Windows `go test -race` 需要额外 CGO/C 编译器工具链，但仓库本身不依赖 CGO，因此仅作为可选补充验证。该验收不自动进入 Wave 5。
