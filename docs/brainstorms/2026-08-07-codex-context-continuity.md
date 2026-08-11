# Codex 上下文连续性与安全压缩

- 日期：2026-08-07
- 状态：已实施，Wave 3 已对当前支持的 Windows ChatGPT/Codex App 宿主关闭
- 评审：`docs/reviews/2026-08-07-codex-context-continuity-review.md`

## 背景与目标

Codex App 的自动上下文压缩可能发生在一个 turn 的中途。压缩不会撤销已经写入磁盘的文件或已经执行的外部操作，但有损摘要可能遗漏当前进度、已验证证据、用户纠正和首个未完成步骤，导致压缩后的 Agent 重复探索、重复执行，甚至从头开始处理任务。

本能力先通过 Skill、Codex 配置和生命周期 Hooks 改善这一问题：尽量把压缩安排在任务或 turn 的安全边界；无法避开中途压缩时，保存精简的恢复依据，放行压缩，并在压缩后先核对真实证据再继续。

第一版追求“边界优先 + dirty 时降级恢复 + 可审计失败”，不承诺仅在任务边界压缩。若真实评测证明中途压缩仍然频繁或恢复质量不足，再考虑通过 App Server token usage 事件和主动 compact API 实现精确调度。

## 已确认与待验证的宿主能力

### 公开合同

- Codex 支持 `model_auto_compact_token_limit`、`model_auto_compact_token_limit_scope` 和 `compact_prompt`。
- `PreCompact`、`PostCompact` 和压缩后的 `SessionStart` Hooks 可分别处理压缩前、压缩后和立即恢复。
- Hook 可取得 session、workspace、turn 和 transcript 路径等机械信息。
- Hooks 不提供精确 token usage，也不能直接触发当前 App 线程的 compact；这些能力由 App Server 提供。

### 宿主证据与兼容性边界

Grok 评审引用当前 Codex 本地与 remote compact 路径指出：`PreCompact` 返回 `continue: false` 会使 compact 返回 `TurnAborted`，中止当前 turn；压缩没有完成，因此不会触发压缩后的 `SessionStart(source=compact)`。

第一版据此规定：

- 不把 `PreCompact continue: false` 用作 dirty 自动压缩的正常保护路径。
- manual 和 auto 压缩均默认放行；dirty 时先写机械 emergency manifest，再在压缩后恢复。
- 当前 Windows ChatGPT/Codex App `26.803.5235.0` Store x64 已观察到 dirty auto、clean/safe/dirty manual compact、普通 turn UI、Subagent follow-up/replay 和真实 `PreToolUse` fail-open；版本化证据、自检和独立复核均已通过，Wave 3 已对该支持宿主关闭。其他机器或宿主版本进入兼容性矩阵。

官方参考：

- [Configuration Reference](https://learn.chatgpt.com/docs/config-file/config-reference.md)
- [Hooks](https://learn.chatgpt.com/docs/hooks.md)
- [Codex App Server](https://learn.chatgpt.com/docs/app-server.md)

## 保证边界

| 层级 | 第一版承诺 |
|---|---|
| Hooks 硬保证 | 对已覆盖工具执行机械状态转换；校验 session/workspace；原子写状态；记录事件；压缩后按预算注入；默认路径不产生 continuation；不复制完整 transcript；不写凭据。 |
| Skill 尽力而为 | 提炼真正重要的语义；在阶段和 turn 边界维护检查点；识别新任务；选择需要保留的决策、证据和下一步。模型未遵循时只能告警和降级，不能宣称机械保证。 |
| Codex 宿主 | 判断自动压缩阈值、执行压缩并产生生命周期事件。第一版不控制精确压缩时机。 |

“每个 turn 结束前必须形成安全检查点”是目标，不是无条件硬保证。当前默认 no-`Stop` 路径允许 turn 直接结束，并在状态仍为 dirty 时保留可审计的后续恢复义务；历史严格实验路径的 Stop Hook 最多提供一次补写机会。

## 核心设计

### 1. 边界优先压缩

- Skill 应在正常 turn 结束前形成精简检查点并 seal。
- 新用户 prompt 到达时保留上一 turn 的安全标记，由 Codex 原生阈值决定是否在下一次模型请求前压缩。
- 安全状态下触发的压缩直接放行，使压缩尽量落在两个 turn 之间。
- 业务工具开始执行前，状态转为 dirty；工具批次结果进入检查点并 seal 后才能恢复为 safe。
- 新任务开始时，Skill 封存上一任务的最终检查点并创建新任务标识；只继承仍适用的用户约束和 workspace 事实。

### 2. 状态机

状态包括：

- `safe`：最新重要状态已经落盘，可以正常压缩。
- `dirty`：存在尚未进入语义检查点的工具结果。
- `compacting`：`PreCompact` 已运行，等待压缩结果。
- `recovery_required`：dirty 状态已经压缩，必须先核对证据。
- `compact_failed`：压缩开始后未观察到成功恢复入口，不能假定上下文已更新。

正常边界路径：

```text
safe -> PreCompact -> compacting -> SessionStart(compact) -> safe
```

dirty 降级恢复路径：

```text
dirty -> PreCompact + emergency manifest -> compacting
      -> SessionStart(compact) -> recovery_required
      -> reconcile -> seal -> safe
```

压缩失败或 turn 中止路径：

```text
safe|dirty -> PreCompact -> compacting
           -> no successful compact continuation -> compact_failed
           -> next SessionStart/UserPromptSubmit warns and inspects
           -> preserve last safe checkpoint; do not pretend compact succeeded
```

`PostCompact` 负责记录成功事件；压缩后的 `SessionStart` 才负责模型可见的恢复注入。若后续事件发现状态长期停留在 `compacting`，下一次可用入口将其转为 `compact_failed` 并显示警告。

### 3. Manual 与 Auto 策略

| 触发方式 | safe | dirty |
|---|---|---|
| `auto` | 放行压缩，压缩后注入热状态。 | 写 emergency manifest 后放行，压缩后进入 `recovery_required`。 |
| `manual` | 放行用户显式压缩。 | 写 emergency manifest并显示警告，但仍放行；不得把用户主动 compact 变成 `TurnAborted`。 |

第一版的 `PreCompact` Hook 不返回 `continue: false`。未来若考虑阻止，只能作为独立实验，并必须明确 `TurnAborted` 后果、排除 context-limit 路径且先通过真实 smoke。

### 4. 精简的三级状态

#### 热状态

压缩后自动注入，硬预算约 600 estimated tokens，只包含：

- 当前目标；
- 不可违反的用户约束和最新纠正；
- 当前阶段及一句话进度；
- 首个未完成步骤；
- 当前阻塞或待用户决定项；
- 关键 artifact 和温状态路径；
- 禁止无理由重复已完成工作的简短提示。

超过预算时必须由 Skill 重写，不能机械截断。目标、用户纠正、阻塞和下一步不得被裁掉。

#### 温状态

按需读取，硬预算约 1,500 estimated tokens，保存在 `checkpoint.md`，包含：

- 简化后的已完成、正在执行和待执行计划；
- 修改文件和产物路径；
- 测试、命令和 Subagent 的结论性证据；
- 已确认决策和必须避免重复的已否定假设；
- 未完成、失败或状态不明的操作；
- 首个未完成步骤的准确入口。

温状态不得保存完整命令输出、源码片段、长篇分析、聊天复述或失效计划。完成项应压缩为“结果 + 证据路径”。

#### 冷状态

默认不注入，包括 `events.jsonl`、原始 transcript、完整日志和历史检查点。只有发现证据冲突、检查点缺项或需要故障调查时，Agent 才按路径读取相关部分。

完整 transcript 默认不复制。状态只记录 Codex 已有 transcript 的路径和必要定位信息，避免重复保存可能包含凭据的对话。

### 5. Token 预算计量合同

第一版不声称与 Codex 内部 tokenizer 完全一致，采用确定性、偏保守估算：

```text
base =
  CJK/Kana/Hangul code point 数
  + Unicode 标点和符号 code point 数
  + ceil(ASCII 字母与数字 code point 数 / 4)
  + ceil(其他字母 code point 数 / 2)
  + 非空行数

estimated_tokens = ceil(base × 1.20)
```

- 空格和制表符不单独计数，换行通过非空行数计入。
- Hot body 必须 `<= 600`；warm checkpoint 必须 `<= 1500`。
- `SessionStart` Hook handler 设置 `additionalContextLimit = 800`，为不超过 600 estimated tokens 的 hot body 及固定恢复指令留出包装空间。
- Seal 只校验，不做确定性截断或自动删字段；预算失败返回可诊断错误并保持 dirty，由 Skill重写。
- 如果恢复时不存在合格 hot state，只注入不超过 250 estimated tokens 的机械 emergency capsule和温状态路径，不把超限正文强塞入上下文。

Wave 3 以确定性估算器、边界/超限自动化和不 spill 合同收口；真实 tokenizer 偏差、跨模型 `additionalContextLimit` 压力样本和系统性校准进入 Wave 4 评测，不反向阻塞 Wave 3。若当前宿主 smoke 实际出现超限注入或连续再压缩，则作为 Wave 3 缺陷重新打开。

### 6. 检查点内容契约

机器可读的 `state.json` 至少保存：

- schema 版本；
- session、workspace、当前任务标识；
- 检查点递增序号及内容摘要；
- 当前状态与 dirty generation；
- 最近一次使状态变脏的工具和 turn；
- 最近一次成功 seal 时间；
- transcript 路径及可选校验信息；
- 最近一次压缩触发方式和处理结果。

模型可读的 `checkpoint.md` 必须覆盖：

1. 当前目标和一句话状态。
2. 用户明确约束、纠正和禁止事项。
3. 已确认方案及仍需保留的被否定假设。
4. 已完成、正在执行和尚未开始的计划状态。
5. 修改文件、命令结论、测试状态、Subagent 状态和 artifact 路径。
6. 首个未完成步骤及其准确入口。
7. 阻塞项和仍需用户决定的问题。
8. 不得无理由重复的工作。
9. 检查点写入时仍未完成或状态不明的操作。

不得将“已启动”误写为“已完成”，不得写入凭据、认证头或秘密原文。

### 7. Seal CLI 合同

第一版提供确定性的本地命令入口，而不是只写一句 Skill 指令：

```text
context-continuity seal --session-dir <absolute-path>
context-continuity inspect --session-dir <absolute-path> --json
context-continuity rotate --session-dir <absolute-path> --task-id <id>
```

`SessionStart` 将 session state dir 的绝对路径注入给 Skill。Seal 从该目录读取 `hot.md`、`checkpoint.md` 和 `state.json`，执行：

1. 校验 schema、session 和 workspace。
2. 校验必填字段和 token 预算。
3. 计算内容摘要。
4. 用临时文件 + 原子替换更新状态。
5. 增加 checkpoint sequence，并将当前 dirty generation 标为 safe。

Seal 必须幂等：同一身份和同一内容摘要重复调用不重复增加 sequence。标准输出仅返回简短 JSON：

```json
{"status":"safe","checkpoint_seq":3,"hot_estimated_tokens":420,"warm_estimated_tokens":1080}
```

退出码：

| 退出码 | 含义 | 状态处理 |
|---|---|---|
| `0` | Seal 成功或幂等成功 | 置为 safe。 |
| `2` | 必填字段或预算校验失败 | 保持 dirty，返回可修复诊断。 |
| `3` | session/workspace 身份不匹配 | 拒绝写入，记录安全警告。 |
| `4` | I/O、锁或原子替换失败 | 保持 dirty，记录运行错误。 |

Seal 命令自身由 PreToolUse 观察时可以先保持 dirty；只有命令成功完成原子写入后才置 safe。`inspect` 不改变状态，`rotate` 创建新任务后保持 dirty，直到新任务首次 seal。

### 8. 工具分类与 Subagent 归属

第一版采用“未知工具默认 dirty”，避免出现假 safe：

| 类别 | 示例 | 状态规则 |
|---|---|---|
| 写入或外部副作用 | `apply_patch`、Edit/Write、shell/Bash、写型 MCP、Computer Use | PreToolUse 必须置 dirty。 |
| 只读但产生任务证据 | 文件读取、搜索、Git 查询、只读 MCP、图片查看 | 默认置 dirty，因为结果可能影响决策；后续可根据评测配置明确豁免。 |
| 状态维护 | `inspect`、`seal`、`rotate` | `inspect` 不变；`seal` 成功后置 safe；`rotate` 保持 dirty。 |
| 未知或新工具 | 无分类匹配 | 默认置 dirty并记录真实工具名。 |

实现必须基于真实 Hook tool name 建立显式 matcher/分类表，并通过 smoke 验证 `Bash`、`apply_patch` 和实际 MCP 名称覆盖。不能只依据文档示例推断。

Subagent 规则：

- 根 session 在 SubagentStart 时置 dirty，并记录 agent/task 标识。
- Subagent 内部工具不直接 seal 根 session。
- SubagentStop 返回的结论由根 Agent 写入 checkpoint 后，根 session 才可 seal。
- 当前 App 复用已完成 agent 执行 follow-up 时可能不再发送 `SubagentStart`，只发送新的 `SubagentStop`；实现用最多 64 条 completion fingerprint 归属这种 follow-up，完全相同的重放幂等，不能把合法 follow-up 制造成 orphan `unknown`。
- 已知历史 stop 的重放不能清除同一 ID 的新 running lifecycle；完全陌生或已被 ledger 淘汰的 stop 仍记为 `unknown` 并阻止 seal。
- 未完成或失联的 Subagent 必须保留为状态不明，不能因主 turn 结束而标完成。

### 9. 检查点生成时机

语义提炼不能推迟到 `PreCompact` 才进行。Skill 应在以下时机增量维护小检查点：

- 初次确认任务目标和约束后；
- 用户纠正、方案决策或范围发生变化后；
- 进入预计较长的工具批次、Subagent 研究或大型测试前，记录准备执行的动作；
- 工具批次产生重要结果后，只补充差异；
- 正常 turn 结束前；
- 当前任务完成或切换到新任务前。

`PreCompact` 不临时要求模型生成语义摘要，只写机械 manifest并放行。

### 10. Stop 与 Compact 的优先级（历史/严格实验路径）

2026-08-08 之后，Wave 3 默认 Fixture 和全局安装器不再注册
`Stop`。以下行为只用于回归历史严格实验路径，不能作为当前默认路径的
验收 oracle；当前默认路径应直接结束普通 turn，并在后续真实 compact
时根据 `clean`/`dirty` 状态进入静默恢复或降级恢复。

`Stop` Hook 的行为：

- 状态为 safe：允许 turn 结束。
- 状态为 dirty 且 `stop_hook_active=false`：发起一次 continuation，只要求整理 checkpoint 并 seal，不得重新执行业务工作。
- continuation 后仍为 dirty：不再续接，显示告警并结束 turn，防止无限循环。
- Stop continuation 期间触发 auto compact：PreCompact 必须放行并进入 dirty 降级恢复，不能返回 `continue: false`。
- Seal 失败：按退出码保留 dirty；同一 turn 不再无限重试。

边界优先主要依靠提前检查点和安全余量，不依赖“先阻止 compact 再获得 seal 窗口”。

### 11. Dirty 压缩与恢复

dirty 状态触发 `PreCompact` 时：

- 原子写入不超过 250 estimated tokens 的 emergency manifest；
- 记录最后一个安全 checkpoint sequence、dirty generation、turn、最近工具、transcript 路径、workspace 标识、Git/artifact 摘要和触发方式；
- 将状态置为 `compacting`，标记 `origin_state=dirty`；
- 返回允许继续的 Hook 结果，不阻止压缩。

压缩后的 `SessionStart(source=compact)`：

- safe origin：注入合格 hot state，恢复后保持 safe。
- dirty origin：注入最后一个安全 hot state + emergency capsule，状态置为 `recovery_required`。
- 身份不匹配：拒绝注入语义状态，仅显示警告。

处于 `recovery_required` 时，模型必须：

1. 校验 session 和 workspace。
2. 读取最后安全 checkpoint；必要时只读 transcript 尾部。
3. 对照工作区、Git 状态、计划和 artifact验证最近工具的实际结果。
4. 检查点声称完成但证据不存在时，降级为待核实。
5. 磁盘已有结果但检查点遗漏时，补记而不重新生成。
6. 从首个未完成步骤继续。
7. 第一次有效核对后 seal，使 `recovery_required -> safe`。

恢复提示必须禁止重新介绍完整方案、重新探索整个仓库或无理由重跑已完成步骤。只有证据过期、输入变化、前次失败或用户明确要求重新验证时才允许重复。

### 12. 存储位置与安全

运行状态默认保存在用户私有目录，而不是业务仓库：

```text
~/.codex/context-continuity/sessions/<session-id>/
├── state.json
├── hot.md
├── checkpoint.md
└── events.jsonl
```

- 业务仓库只保存 Skill、Hook 脚本、配置模板和测试，不保存实际 session 状态。
- 状态文件不得进入 Git，不得包含凭据或完整 transcript 副本。
- 所有机器状态更新必须原子化。
- session 或 workspace 身份不匹配时拒绝注入旧状态，并显示可诊断警告。

## 配置策略

- `model_auto_compact_token_limit` 必须为一个完整长 turn 和检查点维护预留安全余量，不能贴近模型上下文上限。
- 第一版使用 `model_auto_compact_token_limit_scope = "total"`，按完整活跃上下文保守判断。
- 阈值不永久写死为某个模型专用值。优先依据模型窗口和真实长 turn 增量校准：

```text
auto_compact_limit =
context_window - max(25% context_window, 2 × 最长常见 turn 增量)
```

- 没有实测数据时提供偏保守的配置模板，通过真实 App smoke 后再调整。
- `compact_prompt` 只要求保留用户纠正、硬约束、未完成工作、证据路径和准确下一步，并删除日志、源码和重复解释；它不能替代外部检查点。
- 修改用户级 `config.toml` 或安装 Hooks 前，必须展示候选配置值和来源并取得明确确认。

## 非功能要求

- **精简**：hot `<= 600` estimated tokens，warm `<= 1500`，cold 默认不注入。
- **安全**：不保存凭据，不重复复制 transcript，不因恢复覆盖用户已有修改。
- **可恢复**：压缩后以磁盘证据为准，从首个未完成步骤继续。
- **可诊断**：记录压缩触发、失败和恢复事件，但不记录大段内容。
- **无死循环**：默认路径不产生 Stop continuation；同一 compact 生命周期不得无限重试；历史严格实验路径的 Stop continuation 最多一次。
- **身份隔离**：session 或 workspace 不匹配时拒绝误恢复。
- **兼容性**：配置值和 Hook 行为按当前 Codex 版本验证，不把未验证源码行为当作永久合同。

## 可判定验收协议

统一证据目录：

```text
tests/context_continuity/evidence/<YYYY-MM-DD>/<case-id>/
├── precondition.json
├── events.jsonl
├── state-before.json
├── state-after.json
├── transcript-pointer.txt
├── app-observation.md
└── artifacts.txt
```

第一版必须进行真实 Codex App smoke，不能只验证配置语法、CLI help 或 Hook 单元测试。

| ID | 前置状态与操作 | 期望事件/终态 | 禁止现象 | 主要证据 |
|---|---|---|---|---|
| CC-01 | safe；将阈值调低，在下一 prompt 触发 auto compact | `PreCompact(auto) -> PostCompact -> SessionStart(compact)`；终态 safe | 重跑已完成步骤 | events、state-after、App 观察 |
| CC-02 | dirty；auto compact | 写 emergency manifest并放行；压缩后 `recovery_required` | `continue:false`、TurnAborted | events、manifest、App 观察 |
| CC-03 | 默认 no-`Stop` 配置；分别以 clean/dirty 状态结束普通 turn | turn 直接结束且不出现 checkpoint continuation；dirty 状态保留为后续 compact 的恢复义务 | 用户可见 continuation、同一 turn 循环、把 dirty 误标为 safe | events、state-after、App 观察 |
| CC-04 | dirty + 近阈值；auto compact 在 turn 中途发生 | `PreCompact(auto)` 放行并写有限 emergency manifest；随后 `PostCompact -> SessionStart(compact)`，进入 `recovery_required` 并可重新 seal | PreCompact 阻止、TurnAborted、恢复后无依据重跑已完成工作 | events、manifest、终态、App 观察 |
| CC-05 | manual compact，分别覆盖 safe/dirty | 两者均放行；dirty 进入恢复核对 | 用户显式 compact 触发 TurnAborted | events、UI 观察 |
| CC-06 | 构造已完成研究、编辑和测试证据后压缩 | 恢复从首个未完成步骤继续 | 重新研究、覆盖文件、无理由重测 | artifacts、操作日志、恢复回复 |
| CC-07 | hot/warm 位于预算边界和超限 | 边界内 seal；超限退出 `2` 并保持 dirty；注入不 spill | 静默截断、超限注入 | CLI JSON、state、hook output |
| CC-08 | session 或 workspace 指纹不匹配 | 退出 `3` 或拒绝注入并告警 | 载入其他任务状态 | events、UI、state-after |
| CC-09 | 覆盖 Bash/apply_patch/MCP/只读工具/Subagent | 分类与状态变化符合表格 | 写操作后仍假 safe，Subagent 误完成 | 每类工具事件与状态快照 |

### 指标定义

- **安全边界率** = `origin_state=safe` 的 auto compact 次数 / auto compact 总次数。
- **降级恢复率** = `origin_state=dirty` 的成功恢复次数 / dirty compact 次数。
- **重复执行率** = 恢复后无正当理由重复已完成动作数 / 恢复总次数。
- **必须信息召回率** = 恢复后正确保留的 case oracle 必填项数 / oracle 必填项总数。
- **预算合规率** = 未超过对应 estimated-token 预算的成功注入或 seal 次数 / 成功注入或 seal 总次数。
- **循环违规数** = 默认路径出现任何 Stop continuation、历史严格实验路径 Stop continuation 超过一次，或同一 lifecycle 事件被无限重试的 case 数。

指标数据来自 case oracle、`events.jsonl`、seal JSON 输出、状态快照和 App 观察记录；不能仅凭最终自然语言结论打分。

## 方案与决策

### 决策标准

- 优先减少中途压缩导致的任务重启和重复工作。
- 重要信息在压缩前增量落盘，但重新注入必须足够精简。
- 第一版保持轻量，不引入常驻服务或 App Server 控制器。
- 不在 context-limit 路径用 `continue:false` 冒险中止 turn。
- 自动化失败时可见、可恢复，不能静默丢状态或无限循环。

### 选定方向

采用 Skill + Codex 配置 + Hooks：Skill 尽力生成语义检查点；原生自动阈值负责接近上限判断；Hooks 负责机械状态、身份隔离、事件审计和压缩后注入。通过 turn-end seal 让压缩优先发生在边界；dirty 时不阻止压缩，而是写 emergency manifest并在压缩后核对恢复。

### 选择理由

该方向无需常驻控制器即可覆盖提前压缩、关键信息落盘、压缩后恢复和防止重复执行四个目标。它不能精确保证压缩只发生在任务边界，也不能把模型语义提炼提升为硬保证，但能以可审计方式降低风险，并为后续是否需要 App Server 控制器提供量化证据。

### 主要备选

- **纯 Skill 检查点**：实现简单，但无法感知压缩生命周期，也无法提供机械恢复注入。
- **PreCompact 阻止 dirty 压缩**：当前源码证据表明会导致 `TurnAborted`，尤其不适合 context-limit 路径，第一版排除。
- **App Server 伴随控制器**：可监听 token usage，并在 turn 完成且状态安全时主动 compact；第一版不采用，因为会引入额外运行组件。若真实评测显示边界优先方案不足，再重新评估。

## 约束与非目标

- 第一版不引入 App Server 伴随控制器、daemon、数据库或 dashboard。
- 第一版不承诺识别所有语义上的“新任务”；正常 turn 边界即候选安全压缩边界。
- 第一版不复制完整 transcript，不构建长期 memory 系统。
- 第一版不修改 Codex 自身自动压缩实现，只使用公开配置和生命周期扩展点。
- 本文是已经实施的设计基线；后续新增或修改用户级 Codex 配置仍需按安装指南预览、确认和保留回滚证据。

## 验证状态与后续项

- [x] 已在当前 Windows Codex App 观察到 dirty + auto 的 `PreCompact -> PostCompact -> SessionStart(compact)` 成功路径，以及 clean/safe/dirty manual 变体。
- [x] `PreCompact continue:false -> TurnAborted` 已从当前默认合同移除，只保留为历史严格实验项，不再阻塞 Wave 3 默认路径。
- [x] 当前默认 no-`Stop` 路径已观察到 clean/dirty 普通 turn 无 continuation，以及 dirty auto compact 放行、恢复并重新 seal。
- [x] 已观察到真实 tool names、Subagent 事件与 follow-up/replay；session-local lock fault injection 证明 `PreToolUse` 持久化失败不阻止工具或 turn；`additionalContextLimit` 预算边界已有自动化覆盖，跨模型真实压力样本转入 Wave 4 评测。
- [ ] Wave 4：根据实际模型和长 turn 数据判断是否需要自定义 `model_auto_compact_token_limit`。

Wave 3 的版本化证据一致性检查和独立复核均已通过，无 P0/P1/P2 finding，当前支持宿主已关闭。Wave 4 可以从评测样本与指标基线开始；跨模型阈值和长 turn 指标不反向阻塞 Wave 3。

## 其他开放问题

- [ ] 确定用户私有状态的保留周期和清理方式。
- [ ] 评测后决定是否需要 App Server 伴随控制器。

## 下一步

1. 以已关闭的 Wave 3 当前支持宿主证据为基线，启动 Wave 4 的评测样本和指标设计。
2. 将其他机器和宿主版本纳入兼容性矩阵，不把单机关闭结论直接外推。
3. 只有 Wave 4 指标证明必要时才实现 App Server 精确调度扩展。
