# oh-my-openagent 多 Agent 机制调研

## 研究对象、日期、方法与限制

- **研究对象**：`E:\projects\others\oh-my-openagent-dev` 当前工作树（分支标识为 `dev`）。该目录没有 `.git`；因此本次没有以提交历史验证行为。
- **研究日期**：2026-08-06。
- **方法**：全程只读。先确认该目录存在 `.codegraph/` 且 `codegraph explore` 可用，随后用两次 CodeGraph 探索定位后台 agent、团队协作、模型路由和 Codex Light 边界；再阅读目标源码及其相邻测试。根 `AGENTS.md` 和进入的嵌套 `AGENTS.md` 也已阅读。
- **限制**：根 `AGENTS.md` 明确声明该项目正在进行大规模多 harness 重构，结构并非稳定契约。下文的“源码事实”仅描述当前工作树；“仓库说明”来自该仓库的 `AGENTS.md`；“判断”是为自建轻量 workflow 作的研究推论，不是最终方案。

## harness/core/adapter 架构

### 仓库说明

`packages/AGENTS.md` 将 42 个 package 分为 adapter、harness-neutral core、MCP 等角色：`omo-opencode` 是 OpenCode Ultimate adapter，`omo-codex` 是 Codex CLI Light adapter；`delegate-core`、`team-core`、`model-core`、`mcp-client-core`、`omo-config-core` 等被定位为 core。

`omo-config-core` 的仓库说明将其定义为 harness-neutral 的严格 Zod schema、walk-up loader、harness/profile view、原子 writer 和 lock+journal migration；并明确不得依赖 OpenCode、Codex、Senpi 或 Pi adapter：`packages/omo-config-core/AGENTS.md`。

### 源码事实

OpenCode adapter 的启动入口是 `createPluginModule()`。它通过依赖注入组装配置、manager、tools、hooks 和 plugin interface：`packages/omo-opencode/src/testing/create-plugin-module.ts:56-122,159-220`。启动还处理 legacy workspace/config migration、重复 plugin 检测、server auth 注入和启动诊断：同文件 `163-218`。

### 判断

值得保留的是“core 定义协议与纯逻辑、adapter 翻译 harness API”的边界。第一版只需要少量接口，不需要把每个功能都拆成独立 package。

## 关键调用链

### delegation、category 与 model routing

1. `resolveCategoryExecution()` 验证 category、查询可用模型、解析 category 配置，并产出 agent、model、prompt append、fallback chain 等执行信息：`packages/omo-opencode/src/tools/delegate-task/category-resolver.ts:65-205`。
2. 对特定模型要求的 category，它调用 `delegate-core` 的 `resolveModelForDelegateTask()`；其顺序为 user override、冷缓存 skip sentinel、category default、user fallback、hardcoded fallback chain、system default：`packages/delegate-core/src/model-selection.ts:77-260`。
3. background 执行最终把 category/model/skills/permission 一并交给 `BackgroundManager.launch()`：`packages/omo-opencode/src/tools/delegate-task/background-task.ts:102-137`。启动后返回 background task ID，并明确要求等待系统通知而不是立刻轮询：同文件 `216-223`。

**判断**：模型选择应输出来源/provenance 与 fallback 信息，避免子 agent 的实际模型不可解释；但第一版不应复制 OMO 的 fuzzy matching、跨 provider shaping、`-high` 特判与两套 fallback 体系。

### background agent、队列、session 生命周期与 parent wake

1. `BackgroundManager.launch()` 先检查/保留子代深度名额，立即创建 `pending` task，按 model/provider key 入 FIFO 队列，然后异步触发 `processKey()`：`packages/omo-opencode/src/features/background-agent/manager.ts:559-655`。
2. `processKey()` 防止同 key 重入，逐项获得 `ConcurrencyManager.acquire()` 的槽位，启动 session；启动失败时标记 error、释放槽位、abort 已创建 session 并通知父 session：同文件 `662-736`。
3. `ConcurrencyManager` 的默认上限为 5；model limit 覆盖 provider limit，provider limit 覆盖 default；满额时 FIFO wait，release 优先移交给 waiter，取消可精确移除等待者：`packages/omo-opencode/src/features/background-agent/concurrency.ts:16-160`。其并发/归一化 key 与 manager 测试位于同目录的 `concurrency*.test.ts`、`manager*.test.ts`。
4. `cancelTask()` 同时从业务队列删除 pending task、取消 concurrency waiter，running task 可 abort child session，随后释放槽位并通知父会话：`manager.ts:2363-2457`。
5. `tryCompleteTask()` 在将 child 标记为完成前 reserve parent notification preparation；该 reservation 避免 parent poller 在“child 已终态、wake 尚未入队”的窗口过早结束。随后释放并发、清理 child session、触发 parent notification，并在 finally 中释放 reservation：`manager.ts:2515-2600`。
6. `ParentWakeNotifier` 维护 pending/dispatched 两种 wake 状态，支持 session activity 记录、延迟 flush、prompt 失败重入队、空 assistant turn 重试，以及在已有 assistant/tool 输出后抑制晚到重试：`packages/omo-opencode/src/features/background-agent/parent-wake-notifier.ts:18-185`。相关竞态被一组 `parent-wake-*.test.ts` 覆盖。

**判断**：第一版可直接采用 task 状态、稳定 ID、每 routing key 的 FIFO 限流、精确取消与单父会话 completion/wake 合并器。不要复制其 archive/toast/marker/live-server-route/process sweep 等 OpenCode 特有周边。

### team：mailbox、tasklist、worktree 与污染防护

`team-core` 将 team 运行状态、mailbox、tasklist、worktree 定义为 harness-neutral 域能力；其仓库说明列出了各存储位置和原子锁：`packages/team-core/AGENTS.md`。

- 消息 schema 有 version、UUID、from/to、kind、body、references、timestamp 和 correlation ID：`packages/team-core/src/types.ts:91-103`。
- `sendMessage()` 限制 body 32KB、recipient 未读量、lead-only broadcast、active/reserved recipient；以 inbox lock 和 atomic write 写入：`team-mailbox/send.ts:135-183`。
- `pollAndBuildInjection()` 为每个 turn 标记已注入消息、保留 pending ack，避免相同消息重复注入：`team-mailbox/poll.ts:44-100`。
- delivery reservation 用 `.delivering-*` 文件与 rename，完成后放入 processed；可回收 stale reservation：`team-mailbox/reservation.ts:30-104`。
- `claimTask()` 在 lock 内重新读取 task 与依赖状态，再把任务原子改为 `claimed`：`team-tasklist/claim.ts:45-96`。
- state lock 使用 `wx`、pid/timestamp、超时与死进程检测；atomic write 是 temp + fsync + rename：`team-state-store/locks.ts:107-209`。
- worktree 使用 `git worktree add --detach`：`team-worktree/manager.ts:42-63`。
- agent eligibility 明确拒绝无写权限的 read-only agent 成为 team member，因为它们无法写 mailbox：`types.ts:190-237`。

**判断**：若未来真有多个长驻、跨进程 agent，可简化借鉴 versioned message、correlation ID、ack/reservation 和 atomic task claim。第一版应优先使用 harness 的消息/线程能力或一个 task event store，不应急于实现每 member 的文件 mailbox、tmux grid 和完整 team runtime。

### MCP、权限和状态恢复

OpenCode 的 built-in MCP 由 `createBuiltinMcps()` 根据 disabled list、CodeGraph enabled 开关和 runtime executable 组装：`packages/omo-opencode/src/mcp/index.ts:36-70`。Codex Light 则以一个 aggregate plugin manifest 暴露 skills、hooks 和 MCP：`packages/omo-codex/plugin/.codex-plugin/plugin.json:21-60`；其 `codegraph` MCP 是 `required:false`，LSP 有 10 秒启动 timeout：`packages/omo-codex/plugin/.mcp.json:1-26`。

共享 stdio MCP core 支持 idle timeout 与可选 parent-pid watchdog；watchdog 关闭 input 让服务经统一 finally 路径释放 timer：`packages/mcp-stdio-core/src/server.ts:13-38,58-96,172-220`。

权限方面，team schema 按 agent 能否写 mailbox 执行资格限制；OpenCode adapter 的 delegated background task 也会传入 session/user permission：`background-task.ts:119-137`。

**判断**：第一版应预留 MCP 的 namespace、scope（global/workflow/task/session）、启动/idle timeout 和 shutdown contract；不建议先实现 OMO 的三层 MCP、OAuth、Claude compatibility loader。

### Codex Light 与 OpenCode 主体的边界

Codex Light 并非 Ultimate team-core 的完整移植。它把多个 component 以 aggregate plugin 形式挂到 Codex hooks/skills/MCP；其 team hook 主要处理创建 thread 或 pending worktree 后的命名与就绪提醒：`packages/omo-codex/plugin/components/teammode/src/codex-hook.ts:22-86`。

Codex side 的 spawn guard 兼容多个 tool token（v1、flattened v2、dotted v2），按 session 做 fanout backstop，并在 final gate reviewer spawn 前检查 code-review 和 manual-QA artifact：`packages/omo-codex/plugin/components/ulw-loop/src/spawn-guard.ts:10-100`。相关兼容性由 `components/teammode/test/v2-spawn-schema.test.ts`、`components/ulw-loop/test/spawn-guard.test.ts` 固定。

**判断**：这说明 Codex 适配层应薄，利用 Codex 的原生 thread/collaboration 能力；不要把 OpenCode 的 session、队列和文件 mailbox 语义假定为 Codex 的统一语义。

## 可直接借鉴

1. 任务控制面状态：`pending/running/completed/error/cancelled`、parent/root lineage、session ID、时间戳与稳定 task ID。
2. per-parent completion/wake 去重与延迟策略，防止 child 完成和用户输入冲突。
3. 以 routing key 限流的 FIFO 队列、精确取消等待者、资源 `finally` 释放。
4. worker 与 reviewer 的权限/职责隔离；reviewer 独立检查可验证产物。
5. 在流程 gate 中检查已存在的 review/QA evidence，而非仅接受 agent 文本声明。
6. 版本化 config/task/message 协议，adapter 只负责映射 harness 特有字段。

## 应简化后借鉴

1. **模型路由**：保留 override、availability、fallback、provenance；放弃高级匹配和多套 fallback。
2. **mailbox**：仅当有跨进程持久协作需求时采用 reservation/ack；否则采用单 task event store。
3. **tasklist**：先有 atomic claim 与 dependency，后续再考虑 team spec、tmux 和 runtime state machine。
4. **迁移**：从 schema version、诊断、原子写/备份开始；出现真实历史配置后再引入 journal/lease。
5. **worktree**：先确定 task ownership、dirty tree 和写路径边界；只有并行编辑会重叠时再启用。

## 不建议复制

1. OpenCode Ultimate 的约 53--60 hooks、11 agents、42 package、多 harness/Claude/OpenClaw/tmux 组合。它是多前端产品兼容成本，不是轻量 workflow 的起点。
2. `BackgroundManager` 的完整周边：archive、toast、marker、attempt history、live route、process sweep 等，见其广泛依赖面：`manager.ts:1-123,245-285`。
3. 完整 team file runtime、tmux pane/grid 和 orphan sweep。
4. ULW 的强制 notepad/goal/超细 todo/全量 RED→GREEN+手动 QA 流程。它适合高风险实现，不应套用于所有研究、文档和小任务；其强制指令可见 `components/ultrawork/directive.md:98-206`。
5. 用 prompt 文本或工具名称字符串辨识角色/协议的兼容层；它解决的是历史 harness schema 漂移，而非通用设计。

## 为未来“非 Codex 主控调用 Codex”预留的接口

以下是研究得出的接口边界，不是最终方案：

1. `HarnessAdapter`：`spawn`、`cancel`、`inspect`、`send`、`isSessionActive`。core 不直接调用 Codex/OpenCode SDK。
2. `TaskEnvelope v1`：task/parent/root ID、role/category、message/objective、model intent、permission、workspace ref、timeout/retry、idempotency key；输出 accepted/started/progress/completed/failed/cancelled 事件。
3. `ModelResolver`：输入 intent、availability、policy；输出 model、provenance、fallback chain。参照 `model-core/src/model-resolution-types.ts:15-43` 与 `delegate-core/src/model-selection.ts:77-260`。
4. `WorkspaceLease`：表达 shared/read-only/isolated-worktree、owner 和允许路径，不把路径仅作为 prompt 文本。
5. `WakePolicy`：父会话可接受结果的条件、dedupe key、defer/requeue 与 delivery receipt。
6. `Tool/McpScope`：global/workflow/task/session、namespace、启动/idle timeout、关闭回收。

## 已验证风险与坑

1. **parent wake 竞态**：child 终态与 wake 入队之间会令 parent 过早收敛；对应 reservation 逻辑见 `manager.ts:2522-2529`，且有大量 `parent-wake-*.test.ts`。
2. **取消不完整**：只删除业务 task queue 或只取消 concurrency waiter 都会留下 zombie wait 或 slot leak；OMO 同时操作两者，见 `manager.ts:2376-2391` 与 `concurrency.ts:111-160`。
3. **session completion 非稳定边界**：sync child 完成后仍需 abort，防止 continuation hook 重唤醒已交回 child：`delegate-task/sync-task.ts:155-172`。
4. **文件 mailbox 不是 exactly-once**：pending ack、delivery reservation、stale reclaim 本身证明必须设计 crash recovery：`team-mailbox/poll.ts:60-94`、`reservation.ts:30-104`。
5. **worktree path 校验可移植性风险**：`validateWorktreeSpec()` 以 `/` 分隔并限定相对/绝对形式：`team-worktree/manager.ts:15-18,36-56`。这是已见代码事实；在 Windows 等路径语义下仍需额外 canonical-path containment 校验（判断，未做攻击验证）。
6. **Codex collaboration schema 漂移**：同一 spawn 能力已出现多个 token，因此外部控制器不可依赖 tool-name string 作为自身协议：`spawn-guard.ts:10-13`。
7. **仓库持续重构**：根 `AGENTS.md` 的重构警告意味着上述路径和行为可能变化；消费时应以目标版本的源码和测试再次核验。

## 结论

OMO 最值得借鉴的不是其庞大框架，而是少数可靠性边界：任务状态与 lineage、按 key 的并发和取消、父会话结果交付去重、角色权限隔离、可验证 reviewer gate，以及 core/adapter 协议分层。文件 mailbox、完整 team runtime、多 MCP 层、跨 harness 兼容和密集 hooks 都应在真实需求出现后逐步引入。本文只提供研究证据与取舍依据，不构成自建 workflow 的最终设计。
