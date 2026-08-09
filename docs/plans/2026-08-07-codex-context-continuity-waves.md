# Codex 上下文连续性实现 Wave 计划

> 2026-08-08 更正：Wave 1/2 的 `Stop` continuation 仅保留为历史/严格实验路径。真实 App 验证发现用户级 `Stop` 会在每个正常轮次触发，并把 `decision: "block"` 的 reason 显示为用户提示。Wave 3 默认 Fixture 和全局安装器因此改为非阻塞设计：不安装 `Stop`，不覆盖全局 `compact_prompt`，只在真实写入后的压缩或异常恢复中要求核对。安装范围不能替代行为正确性。

- 日期：2026-08-07
- 状态：执行中（Wave 0 探针、Wave 1 连续性 MVP、Wave 2 非阻止恢复骨架与 Wave 3 机械硬化已实现；宿主 App smoke 待执行）
- 设计基线：`docs/brainstorms/2026-08-07-codex-context-continuity.md`
- 评审处理：`docs/reviews/2026-08-07-codex-context-continuity-review-closure.md`

## 计划结论

在线建议的“Phase 1 只做 SessionStart + Stop + checkpoint + seal”适合作为最小连续性 MVP，但它主要解决 turn 边界恢复，不能覆盖当前最初问题中的 mid-turn 自动压缩。

因此采用以下节奏：

```text
Wave 0  宿主探针与基线
   ↓
Wave 1  Turn 边界连续性 MVP
   ↓
Wave 2  最小中途保护与降级恢复   ← 达到原始问题的实用 MVP
   ↓
Wave 3  机械硬化：dirty tracking、预算、工具/Subagent
   ↓
Wave 4  评测扩展与可选精确调度
```

Wave 1 和 Wave 2 应保持相邻；如果 Wave 1 结束后发现 mid-turn 压缩仍是主要痛点，不应先做 Wave 3 的基础设施，而应直接进入 Wave 2。

## 分层原则

- 每个 Wave 都能独立运行和验证，不依赖尚未实现的完整状态平台。
- 先实现可观察的连续性收益，再增加机械状态和预算约束。
- 不在第一 Wave 引入完整 state machine、工具矩阵、Subagent 生命周期或自动评测平台。
- `PreCompact` 第一版只允许压缩并保存机械 manifest，不使用 `continue:false` 阻止 context-limit 压缩。
- 用户级 `config.toml` 和 Hooks 只通过隔离配置验证；在取得明确确认前不写入真实用户配置。
- 每个 Wave 都保留人工可读的证据，不把“App 没报错”当作完成标准。

## Wave 0：宿主探针与基线

### 目标

在实现语义功能前，确认当前 Codex App 的 Hook 事件、配置入口和压缩路径，消除评审指出的宿主语义假设。

### 最小范围

- 只读探针 Hook：记录事件名、trigger/source、session/turn、是否有 transcript 路径。
- 隔离的 `config.toml`/Hooks 配置副本。
- 手动或脚本化触发 manual compact、低阈值 auto compact、Stop continuation。
- 记录 `PreCompact` 返回 `continue:false` 的实际行为；探针不在正式路径中阻止压缩。

### 不做

- 不创建正式 checkpoint 状态机。
- 不修改用户级配置。
- 不实现 token 估算、工具分类或 Subagent 状态。

### 退出条件

- 证据记录当前 App 的 `PreCompact`、`PostCompact`、`SessionStart(source=compact)`、`Stop` 顺序。
- 明确 manual/auto 两类事件的实际字段。
- 明确 `continue:false` 是否造成 `TurnAborted`，并将差异同步回设计文档。
- 证据保存到 `tests/context-continuity/evidence/<date>/W0-*`。

## Wave 1：Turn 边界连续性 MVP

### 目标

用最小组件解决最常见的任务重启问题：每个 turn 结束时保存一个精简 checkpoint，压缩或新任务开始后自动把它带回来。

### 组件

1. `SessionStart` Hook

   - 在 startup/resume/compact 时创建或定位用户私有 session 目录。
   - 读取一个简短的 `checkpoint.md`。
   - compact 后向模型注入恢复提示；startup/resume 只注入路径和必要摘要。

2. `Stop` Hook

   - 检查本 turn 是否存在成功 seal。
   - 缺失时最多发起一次 continuation，要求模型只整理 checkpoint，不重新执行业务工作。
   - continuation 后仍未 seal 时显示告警并结束，不循环重试。

3. 一个精简 checkpoint 文件

   - 固定字段：目标、用户纠正、当前进度、首个未完成步骤、关键证据路径、阻塞项。
   - 只保存真正影响续接的事实，不保存日志、源码和过程复述。
   - 使用简单字符/字节上限和固定模板防止明显膨胀；暂不引入 tokenizer 或 hot/warm/cold 三层预算。

4. 一个 seal 脚本

   - 原子写入 checkpoint 和 session 元数据。
   - 校验 session/workspace 身份和最小必填字段。
   - 幂等返回成功或可修复错误。
   - Seal 失败时保持未确认状态，不能伪装成已完成。

5. 配置模板

   - `compact_prompt` 仅加入“保留用户纠正、未完成步骤、证据路径和下一步”的短指令。
   - 暂不替用户选定最终自动阈值；使用隔离配置和手动 compact 验证。

### 明确缺口

Wave 1 没有 `PreToolUse` dirty tracking，也没有 `PreCompact` emergency manifest。因此，如果一个 turn 在 checkpoint 更新前就触发 mid-turn 自动压缩，Wave 1 只能依靠 Codex 原生摘要和上一次 checkpoint，不能保证保留当前工具结果。这是已知边界，不应在 Wave 1 报告为已解决。

### 退出条件

- 一个跨两个 turn 的任务在手动 compact 后能从首个未完成步骤继续。
- 下一条 prompt 触发的 auto compact 能读取上一次安全 checkpoint。
- Stop continuation 最多一次，不出现无限循环。
- 不重复已完成的研究、编辑或测试。
- checkpoint 文件保持精简，状态文件不进入业务仓库。
- 证据保存到 `tests/context-continuity/evidence/<date>/W1-*`。

## Wave 2：最小中途保护与降级恢复

### 目标

补上 Wave 1 对原始痛点的主要缺口：自动压缩在 mid-turn 发生时，不阻止压缩，但留下足够小的机械线索，让压缩后能核对并恢复。

### 组件

1. `PreCompact` Hook（非阻止）

   - manual/auto 均允许继续。
   - 若当前 turn 尚未 seal，写入不超过约 250 estimated tokens 的 emergency manifest。
   - manifest 只记录最后安全 checkpoint、当前 turn、最近工具、transcript 路径、workspace、Git/artifact 摘要和 trigger。
   - 不在此时调用模型生成语义摘要。

2. `PostCompact` 与 compact 后 `SessionStart`

   - 记录压缩成功事件。
   - safe origin 直接恢复 hot checkpoint。
   - dirty origin 注入最后安全 checkpoint + emergency manifest，标为 `recovery_required`。

3. 失败可见性

   - 如果进入 `compacting` 后没有观察到成功恢复入口，下一次 `SessionStart`/`UserPromptSubmit` 报告 `compact_failed`。
   - 保留最后安全 checkpoint，不假定压缩已经成功。

### 不做

- 不使用 `PreCompact continue:false`。
- 不做 PreToolUse 全工具分类。
- 不做完整 token tokenizer 或 Subagent 账本。

### 退出条件

- dirty + auto compact 会写 manifest、放行压缩并进入恢复核对。
- dirty + manual compact 同样放行，但向用户显示警告。
- dirty + 近阈值 + Stop continuation 不因 Hook 阻止而中止。
- 压缩后模型先核对工作区、artifact 和 transcript 指针，再继续首个未完成步骤。
- 任何失败路径都不产生无限循环或静默丢失。
- 证据覆盖 CC-02、CC-03、CC-04、CC-05，并保存到 `tests/context-continuity/evidence/<date>/W2-*`。

### Wave 1/2 合并判断

如果目标是尽快改善截图中的问题，交付单元应是 Wave 1 + Wave 2，而不是仅交付 Wave 1。Wave 1 提供连续性骨架，Wave 2 才提供 mid-turn 压缩后的最低限度可恢复性。

## Wave 3：机械硬化

### 目标

把依赖模型遵守的软规则逐步转成可观察、可校验的机械规则，但不引入长期运行平台。

### 组件

- `PreToolUse` dirty tracking。
- 工具分类表：写入/外部副作用、只读证据、状态维护、未知工具默认 dirty。
- Seal CLI 正式退出码、内容摘要、原子替换和幂等行为。
- hot/warm/cold 状态分层。
- 确定性保守 token 估算、600/1,500 estimated-token 校验和 `additionalContextLimit` 配置。
- 未知工具事件记录和实际 MCP tool name 覆盖测试。
- SubagentStart/SubagentStop 的根 session 归属；Subagent 不能直接 seal 根状态。

### 不做

- 不实现完整 mailbox、daemon、数据库或 dashboard。
- 不把只读工具盲目从 dirty 分类中删除；豁免必须由评测证据支持。

### 退出条件

- CC-07 预算边界和超限行为可判定。
- CC-09 覆盖 Bash、apply_patch、MCP、只读工具和 Subagent。
- unknown tool 不会产生假 safe。
- hot 注入不因恢复 capsule 过大而加速再次压缩。
- 证据保存到 `tests/context-continuity/evidence/<date>/W3-*`。

## Wave 4：评测扩展与可选精确调度

### 默认范围

- 将 CC-01 至 CC-09 整理成可重复的 smoke harness。
- 统一收集 events、状态快照、App 观察和 artifact 证据。
- 计算安全边界率、降级恢复率、重复执行率、必须信息召回率、预算合规率和循环违规数。
- 增加不同模型、阈值和长 turn 的对比样本。

### 只有满足条件才做的扩展

如果 Wave 2/3 之后仍满足以下任一情况，才评估 App Server 伴随控制器：

- 安全边界率持续偏低，且 mid-turn compact 是主要失败来源；
- dirty 降级恢复仍频繁重复已完成工作；
- 用户明确需要“只在 turn 完成后自动 compact”的强保证。

控制器若进入范围，应监听 `thread/tokenUsage/updated`，在 turn completed 且状态安全时调用 `thread/compact/start`，并单独进行权限、生命周期和失败恢复设计。它不是前四个 Wave 的隐含依赖。

## 建议的实施顺序

### 最小可用路径

```text
W0 探针
  → W1 SessionStart/Stop/checkpoint/seal
  → W2 非阻止 PreCompact + 恢复
  → 实际使用与小样本评测
```

### 暂不做的内容

- Wave 1 不做完整 state machine、token budget、tool matrix、Subagent state 或 smoke framework。
- Wave 2 不做 `continue:false` 阻止，不做精确 token 调度。
- Wave 3 之前不修改用户级配置，不推广到所有项目。
- Wave 4 的 App Server controller 只在指标证明必要时启动。

## 风险与回滚

- 每个 Wave 使用独立隔离配置和用户私有状态目录；失败时删除/停用对应 Hook 配置即可回滚，不触碰业务仓库文件。
- Wave 1/2 的状态文件只增加恢复信息，不覆盖工作区产物；恢复逻辑发现证据冲突时必须停在告警状态。
- 若 Hook 事件顺序或 `TurnAborted` 行为与设计不符，先更新设计和 closure，再继续实现，不通过提示词掩盖宿主差异。
- 任一 Wave 连续出现重复执行、状态串任务或无限 continuation，应暂停下一 Wave并回到最小复现。

## 当前决策点

Wave 0 → Wave 1 → Wave 2 已构成可初步使用的连续性路径，Wave 3 的机械硬化也已在脚本级实现并通过本地回归与 CLI smoke。当前仍不把 Wave 3 说成真实 Codex App 宿主已验证：需要在隔离配置中观察真实的 `PreToolUse`、`Subagent*`、`PreCompact`、`PostCompact` 和 `SessionStart(source=compact)` 顺序。

Wave 4（统计评测和可选精确调度）继续延期，只有真实使用数据表明中途压缩仍频繁造成重复执行时再启动。
