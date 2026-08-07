# Claude Codex Bridge（CCB）多 Agent 机制研究

**研究日期：** 2026-08-06
**研究对象：** `E:\projects\others\claude_codex_bridge-main`（CCB，README 标示 v8.5.6）
**目的：** 为自建、轻量的多 Agent Skill/协议提炼可移植机制，而非复制 CCB 的常驻 TUI/daemon 产品。

## 1. 方法、证据边界与限制

先在该仓库根的 `.codegraph/` 索引上使用 `codegraph explore` 定位 provider registry、runtime launcher、dispatcher、session/pane health 与 mailbox；随后仅用文件读取和 `rg` 核对源码、测试和 README/设计文档。没有修改研究仓库，也没有读取或输出凭据。

本文的标记含义如下：

- **[源码事实]**：当前 Python 实现可直接核对。
- **[测试事实]**：测试用例中存在的断言；不等同于真实 Provider 已在本机实测。
- **[文档声明/设计]**：README 或 `docs/` 的产品声明、合同或未来设计，不能自动当作当前实现事实。
- **[研究判断]**：针对轻量协议的取舍。

执行了 `pytest -q test/test_v2_provider_core_registry.py test/test_cancel_flags.py test/test_session_binding_validation.py`：6 项通过；2 项 `test_cancel_flags.py` 在 Windows 失败，原因是 `lib/storage/atomic.py` 要求 POSIX `O_DIRECTORY` 目录 fsync，抛出 `NotImplementedError`，并非取消状态机断言失败。本机不能据此宣称 CCB 的持久化取消链路已在 Windows 通过。

`rg -i TASK_CANARY` 在源码、测试、README 和 docs 中没有命中。因此 CCB **没有可验证的、名为 `TASK_CANARY` 的协议实现**；本报告不把其会话/pane 证据机制误称为 canary。此次研究未发现 task/session/inbox/worktree contamination 或异常 canary echo。

## 2. 架构与关键调用链

### 2.1 分层

```text
项目配置 AgentSpec（agent -> provider/model/startup_args/workspace）
  -> ProviderBackendRegistry
       -> ProviderRuntimeLauncher（启动、私有状态、session payload）
       -> ProviderSessionBinding（恢复、live identity）
       -> ProviderExecutionAdapter（提交、轮询、取消）
  -> ccbd JobDispatcher
       -> MessageBureau / MailboxKernel（message -> attempt -> inbound/reply）
       -> CompletionTracker / provider-native evidence
       -> JSON/JSONL 状态与 tmux pane health
```

- **[源码事实]** `ProviderBackend` 把 manifest、执行 adapter、session binding 和 runtime launcher 组成一个 provider 单元；registry 从这些单元导出三类接口（`lib/provider_core/contracts.py:23-71`、`lib/provider_core/registry.py:16-97`）。这比在调度器中写 provider `if/elif` 更可扩展。
- **[源码事实]** 核心 Provider 是 Codex、Claude、Gemini；OpenCode、Agy 等为 optional。构造函数按此清单显式装配 backend（`lib/provider_core/registry_runtime/builtin_backends.py:5-74`）。运行时/客户端 spec 又统一规定 provider key、状态文件、锁名、环境变量和 session 文件名（`lib/provider_core/runtime_specs.py:7-24, 40-50, 64-109, 176-224`）。
- **[源码事实]** 调度入口 `JobDispatcher.submit()` 用 transition lock 串行化，`tick()` 依次维护文本 artifact、callback、reply delivery 和 job tick，`complete/cancel/poll` 是独立操作（`lib/ccbd/services/dispatcher.py:119-170`）。
- **[源码事实]** 单播按 agent 名称路由，广播只选择存活、未禁用且非 sender 的 agent；还检查 drain 和 `REJECT_WHEN_BUSY`（`lib/ccbd/services/dispatcher_runtime/routing.py:14-74`）。一次请求会按每个 target 的 `AgentSpec.provider` 形成 job draft（`lib/ccbd/services/dispatcher_runtime/submission_service.py:23-41, 98-111`）。

### 2.2 Provider 启动、模型/MCP 的传递方式

- **[源码事实]** 统一 launcher 合同接收 `build_start_cmd`、`build_session_payload`、可选 runtime/context preparation 与工作目录解析；launch mode 只有 `simple_tmux` 或 `codex_tmux`（`lib/provider_core/contracts.py:38-59`）。它是可抽象的 adapter 边界。
- **[源码事实]** Codex 启动先准备受管 home，再加入 `spec.startup_args`；恢复时先比对 provider/memory fingerprint，匹配才加 `resume <session>`，否则可走受支持的 `fork`（`lib/provider_backends/codex/launcher_runtime/command_runtime/service.py:22-96, 120-174`）。
- **[源码事实]** Claude 构建私有环境与 settings overlay，若 CLI 支持则传递 `--setting-sources`、`--settings`；自动权限模式使用 `--permission-mode bypassPermissions`，恢复可用 `--continue` 或 `--resume ... --fork-session`（`lib/provider_backends/claude/launcher_runtime/service.py:56-154`）。
- **[源码事实]** OpenCode 同样由 `spec.startup_args` 透传任意 CLI 参数，使用 agent-local `opencode.json` 和私有 `XDG_DATA_HOME`；自动恢复添加 `--continue`（`lib/provider_backends/opencode/launcher.py:60-137`）。
- **[源码事实]** Gemini launcher 是对共享 runtime launcher 的包装，并将 provider authority fingerprint 写入 session payload（`lib/provider_backends/gemini/launcher.py:28-119`）；Agy launcher列出并隔离其配置/MCP 文件路径（包括 `.gemini/config/mcp_config.json`），且为 Windows executable 准备 NTFS 私有 HOME（`lib/provider_backends/agy/launcher.py:36-55, 143-159`）。
- **[研究判断]** CCB 不抽象出“跨 Provider 的 model/MCP 参数对象”：模型、MCP、权限等主要是 `AgentSpec.startup_args`、provider profile、受管 home/config 和 provider-specific code 的组合。因此轻量 Skill 应保留 `model`、`mcp_config_ref`、`extra_args` 的显式字段，再由每个 Adapter 负责映射；不能把 CCB 的 shell command 拼装当成通用协议。

### 2.3 Message、inbox、完成与重试

- **[源码事实]** durable message 记录逻辑请求/来源/targets/retry policy，attempt 绑定 message、agent、provider、job 与 retry index，reply 再独立保存（`lib/message_bureau/models.py:20-92`）。这使“发出”“执行一次”“收到回复”不是同一状态。
- **[源码事实]** retry 禁止对活动或已完成 attempt 操作，只允许最新 attempt；重试会创建新 job、保留 source job id，ask 可以用 `continue` 语义（`lib/ccbd/services/dispatcher_runtime/lifecycle.py:77-177`）。
- **[源码事实]** inbox 读取的是 pending inbound event；取详情时按顺序返回，遇到失效的队首事件会 abandon；task reply 会被补充 reply/attempt/heartbeat 元数据（`lib/message_bureau/control_queue_runtime/views_runtime/inbox.py:19-90`、`lib/message_bureau/control_queue_runtime/events.py:34-81, 111-148`）。
- **[文档设计]** mailbox 文档明确主张“每 agent 入站串行”、reply 也回到 caller inbox，provider 只上报启动/原生进展/health/completion，bureau 才决定 retry、dead-letter 与下一个 event（`docs/agent-mailbox-kernel-design.md:108-112, 125-131, 183-220, 525-546`）。这与当前 message/inbox 类型相符，但该文档本身仍是设计依据，不能替代端到端证据。
- **[源码事实]** 取消先写 job cancel request 与每 agent 的协作 flag，然后调用 execution service cancel；在 lock 内终结 completion、清理 queue/active state，并记录 message-bureau cancellation（`lib/ccbd/services/dispatcher_runtime/cancellation.py:17-56, 69-149`）。相关测试断言取消 flag 和 cancelled 状态（`test/test_cancel_flags.py:46-70`），但受上述 Windows fsync 限制，本机未跑通。

### 2.4 session、pane、worktree 与 authority

- **[源码事实]** Codex authority fingerprint 覆盖 profile、继承/显式 API 环境、管理或源 auth 文件以及 config authority，使用 runtime-local HMAC key 计算；恢复要求已存 fingerprint 匹配（`lib/provider_backends/codex/session_authority.py:32-84, 116-175`）。敏感原文不需要进入调度协议。
- **[源码事实]** authority 改变时连续性 generation 加一，旧 provider binding 进入 history；状态区分 `resumed_same_authority`、`continued_on_new_authority`、`new_conversation`（`lib/provider_backends/session_authority.py:360-405`）。
- **[源码事实]** tmux binding 会两次检查 pane liveness，随后核对 pane 的 CCB user options；不匹配为 `foreign`（`lib/provider_core/session_binding_evidence_runtime/validation.py:9-49`、`lib/provider_core/tmux_ownership_runtime/inspection.py:22-45, 53-81`）。health 层把 missing/foreign/dead 映射成不同状态，健康时 rebind，其他情况降级（`lib/ccbd/services/health_assessment/provider_pane.py:12-86`、`lib/ccbd/services/health_monitor_runtime/provider.py:4-31`）。
- **[文档声明]** README 的示例允许 `worker1:codex(worktree)` 和 `worker2:claude(worktree)`（`README.md:162-172`），并承诺一个项目的 daemon 在前台 UI 关闭后继续保存状态（`README.md:47-50`）。这描述的是 CCB 的多 pane/TUI 产品模式，不是轻量 headless CLI 的必须前提。
- **[文档设计]** Claude isolation contract 要求两个 configured Claude agent 不共用 effective `claude_home`（`docs/claude-session-isolation-contract.md:48-57, 135-137`）；storage plan 也要求 provider duplicate home hard fail，并称已覆盖 same-provider dual-agent/cross-project isolation（`docs/ccb-provider-state-storage-boundary-plan.md:556-558, 1206-1210`）。它说明 CCB 目标是将“同 Provider + worktree”隔离，而非共享 session/pane。

## 3. 可直接借鉴（轻量协议的最小闭环）

1. **Adapter 注册表。** 使用 `ProviderAdapter`：`capabilities`、`build_invocation()`、`submit()`、`observe()`、`cancel()`、`extract_identity()`；provider 名称不得成为 task route。借鉴 CCB 将 execution/session/launcher 放入同一 backend 的做法。
2. **稳定 Task 信封。** 至少保存 `task_id`、`attempt_id`、`from`、`to`、`provider`、`workspace`、`model`、`mcp_config_ref`、`deadline`、`reply_policy`、`parent_task_id`。将 task 与 attempt 分离，重试新建 attempt 而非覆写旧结果。
3. **完成判定必须多证据且 fail closed。** Adapter 返回统一 `CompletionDecision {terminal,status,reason,confidence,reply_ref,evidence}`；缺少精确 session/turn/输出证据应为 incomplete，而不是伪造 completed。CCB 将健康与 completion 分层的方向正确。
4. **显式身份绑定。** 每次 dispatch 保存 `provider_session_id`、runtime/pane ref（如有）、workspace、authority fingerprint 和启动 generation；恢复或 follow-up 前逐项比对。fingerprint 只存摘要/版本，绝不存 token 或配置正文。
5. **单接收者串行。** 一个 agent 同一时刻只消费一个 inbound task/reply/control event；广播可以并行，但每个 target 独立 attempt。这是避免 prompt/回复串线最有价值的 mailbox 规则。
6. **取消为状态转换。** `cancel_requested -> cancelled` 需可审计，并区分“已取得部分结果”“无结果”“实际 Provider 无法中断”。不要只向终端写 `Ctrl-C` 后宣布成功。

## 4. 简化后借鉴

| CCB 机制 | 轻量版本 |
|---|---|
| 私有 HOME、provider asset projection、session fork/import | 仅建立每 task 的工作目录和可选的 provider profile 文件；只有目标 CLI 明确支持时才 resume。 |
| daemon + tmux pane + socket health | 主控进程直接持有 child process handle；`poll()` 读结构化输出/退出码，超时后 cancel，再有限等待。 |
| message/attempt/reply/inbound 多 JSONL store、lease、dead-letter | 一个 append-only `tasks.jsonl` 加每 task 目录内 `request.json`、`result.json`、`events.jsonl`；需恢复时再引入 lock/lease。 |
| authority fingerprint 全覆盖 auth/config 文件 | `authority_version` + 非秘密配置摘要 + provider session id；发生变化则拒绝 resume 并创建新 attempt。 |
| 复杂 pane ownership options/namespace cleanup | process PID + launch nonce + workspace canonical path；没有长驻终端即不需要 pane 管理。 |
| 全 provider 17 家族的 CLI probe | 先支持 Codex 与一个第二 provider，Capability 表声明 `supports_resume/cancel/structured_output/mcp_override`。 |

## 5. 不建议复制

- **不要复制 daemon/tmux/多窗口控制面。** CCB 的卖点是“可见、可接管的原生终端”与 UI 关闭后 daemon 保活（README: `README.md:47-50`），因此必须维护 socket、pane、重连、reflow、后台 health、清理和大量 provider-private 状态。普通 headless CLI task 的交付面是结构化结果，不是终端接管；复制会把大部分研发成本从 task 协议转到生命周期兼容。
- **不要复制 shell 字符串作为协议。** Codex/Claude/OpenCode 的启动分别依赖不同 home、flag 和环境投影（上述 2.2）；用命令字符串跨 provider 传递 model/MCP/权限不可验证且不利于 Windows/Unix 兼容。协议存声明式字段，adapter 在本地构建 argv/env。
- **不要先实现全量 mailbox。** CCB 的 fan-in、callback、reply delivery、恢复、dead-letter 适合长期运行服务；轻量 Skill 先做单层 parent-child、显式 wait、确定的 terminal status。只有发生真实的并发回调/重启恢复需求时才演进为 mailbox。
- **不要把“同 provider(worktree)共享 session/pane”当作可接受默认。** CCB 的隔离合同反而要求 agent-local state/home 和 pane ownership。该“共享碰撞”是本项目 `AGENTS.md` 所述的外部宿主/Provider 约束，**不是 CCB 内建事实**；当前 Codex Desktop/CCB provider 的具体共享模型不能由 CCB 源码推出。若运行环境已知会复用 session/pane，必须以其运行时事实为准，默认禁止该拓扑或串行化该 provider。

## 6. TASK_CANARY 与污染检测的建议

CCB 的可借鉴点是“绑定证据必须与目标一致”，但它没有 TASK_CANARY。因此建议自建协议新增最小 canary，不依赖 provider UI：

```text
dispatch: task_id, attempt_id, launch_nonce, expected_provider, expected_workspace_hash
prompt prefix: "TASK_CANARY=<task_id>/<attempt_id>/<launch_nonce>; echo it first"
accept result only if: echoed nonce + observed provider/session/workspace identity all match
otherwise: status=contaminated, preserve evidence, stop automatic retry on same session
```

这比只检查“任务完成文字”强，也比 CCB 的 tmux 专用 user options 可移植。canary 失败时应记录为 protocol contamination，隔离该 session/pane/process，通知主控；不能把它当作可重试的普通模型失败。

## 7. 为未来“非 Codex 主控调用 Codex”预留的接口

主控不应是 Codex 特例。建议公共接口只引用 **actor/provider capability**，例如：

```text
TaskGateway.submit(TaskRequest) -> TaskHandle
TaskGateway.observe(TaskHandle, cursor) -> TaskEvent[]
TaskGateway.cancel(TaskHandle) -> CancelReceipt
ProviderAdapter.resume(handle, identity) -> ResumeResult  # optional capability
```

- `TaskRequest` 不包含“由 Codex 发起”的假设；`from_actor` 可以是 Claude、人工 CLI、服务或 scheduler。
- Codex adapter 负责将结构化 model、MCP、sandbox、cwd 与 resume 映射到 Codex CLI/API；未来 Claude/OpenCode adapter 实现同一接口。
- `TaskHandle` 保存 opaque `provider_ref`，而非把 Codex thread id 作为全局 id；parent-child lineage 使用 protocol 的 task/attempt id。
- 只有 provider 宣称并实际验证 `supports_resume` 时，主控才请求 resume；否则明确创建新上下文并带入受控摘要。CCB 的 authority generation/fingerprint 是此处可取的防误续接机制。

## 8. 已验证风险/坑

1. **Windows 可移植性。** 当前选测中 2/8 失败，因为 durable atomic writes 依赖 `O_DIRECTORY`；这会阻断取消链路的测试搭建。任何借鉴持久化状态机的 Windows 方案需先采用 Windows 支持的 fsync/替代耐久策略。[测试事实]
2. **文档与实现成熟度不等。** `agent-mailbox-kernel-design.md` 和多个 session/isolation contract 提供了高质量目标模型，但包含“应当/计划”语言；实施前必须逐条追到源码与回归测试。[研究判断]
3. **Provider isolation 代价高。** 私有 HOME 能防 session/auth 串线，却容易与 CLI 自更新、缓存和平台路径耦合；CCB 文档已经记录 Claude 无专用 HOME flag，只能换 HOME（`docs/claude-session-isolation-contract.md:48-57`）。[文档设计]
4. **completion 不能只靠进程退出。** CCB 的取消实现要先捕获 provider evidence、写 terminal decision、更新 reply/attempt 再释放 queue；这揭示 provider 的“进程在/不在”不足以判定 task 成功。[源码事实]
5. **同 Provider(worktree) 碰撞是外部环境风险。** 这是本项目 `AGENTS.md` 的 CCB Provider Isolation Policy，不是本研究仓库的实现声明。CCB 的目标是隔离而非共享，且本次没有运行真实 Codex/Claude/OpenCode 多工作树实验；不能以 CCB 文档代替目标运行时的实测。对已知共享 session/pane 的宿主，必须以 `provider+worktree` 加独占锁/串行调度，或改用不同 provider/非 worktree 通道。[研究判断]

## 9. 结论

CCB 最值得借鉴的不是 tmux 或 daemon，而是三条边界：**agent 名称路由而非 provider 路由、task/attempt/reply 分离、以及基于可核验身份的完成/恢复**。对于自建轻量多 Agent Skill，先实现“声明式 Adapter + durable task envelope + per-agent serial queue + evidence-based terminal decision + canary”即可形成可靠闭环。

CCB 的多窗口、可接管终端、常驻 daemon、受管 HOME、pane recovery 和全 provider 兼容层应被视为后续产品化能力，而不是协议 MVP。尤其是同 provider(worktree) 可能共享 session/pane 的宿主，应由外层调度策略显式隔离；CCB 本身不能证明该风险不存在。
