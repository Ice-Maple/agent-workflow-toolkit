# 评审：Codex 上下文连续性与安全压缩方案

- 日期：2026-08-07
- 评审对象：`docs/brainstorms/2026-08-07-codex-context-continuity.md`
- 对照材料：Codex Hooks 文档、`config-schema.json`、Codex 源码 `compact.rs` / `compact_remote.rs` / `session/turn.rs` / `hook_runtime.rs`、`AGENTS.md`、`docs/brainstorms/2026-08-07-agent-workflow-toolkit.md`
- 评审方式：缺陷优先；只记录会影响正确性、可实施性、可验证性或与宿主真实行为一致性的问题

## Findings

### [P0] “脏状态首次阻止压缩”在 Codex 上会直接中止 turn，而不是延迟压缩 — `docs/brainstorms/2026-08-07-codex-context-continuity.md:151-169`

方案第 7 节假定：`PreCompact` 返回 `continue: false` 后，压缩被“延迟”，可写入 emergency manifest、提示 UI，并给模型一次整理检查点 / seal 的机会；同 dirty generation 再次触发时再放行。

对照当前 Codex 实现（本地与 remote compact 路径一致）：

1. `PreCompact` 的 `continue: false` 使 hook 产出 `should_stop = true`；
2. compact 任务将其映射为 `CodexErr::TurnAborted` 并立即返回；
3. mid-turn / context-limit 自动压缩路径对 `TurnAborted` 会向上传播，直接结束当前 turn；
4. 因为压缩未成功完成，压缩后的 `SessionStart(source=compact)` 不会执行。

因此“第一次阻止 = 争取 seal 窗口”不成立。更糟的是，在 `CompactionReason::ContextLimit` 场景下阻止压缩，等于在上下文已触顶时放弃压缩并中止 turn，比放行降级恢复更危险。文档里写的“压缩因未封存的工具结果而被延迟”也与真实语义不符。

**建议：** 在进入实施计划前重写自适应保护策略，并先用真实 App smoke 验证：

- 默认：**不要**用 `PreCompact continue: false` 作为 dirty 场景的主路径；
- dirty 时优先：**放行压缩**，注入最后一个安全检查点 + emergency manifest，走第 8 节核对协议；
- 若仍保留阻止，必须写清它实际等于 `TurnAborted`、适用前提、以及如何避免在 context-limit 路径上阻止；
- 把“事件顺序验证”从开放问题提升为阻塞项：确认前不得把第 7 节当作已确认合同。

### [P1] `recovery_required` 与“二次放行”状态机建立在错误的阻止语义上 — `docs/brainstorms/2026-08-07-codex-context-continuity.md:48-56,151-169,171-181`

状态图与第 7/8 节把流程写成：

```text
dirty PreCompact -> 阻止 -> recovery_required ->（再次压缩）-> SessionStart 恢复
```

按真实行为，首次阻止后 turn 已中止，且不会进入 compact 后的 `SessionStart`。于是：

- 在“未压缩”路径上进入 `recovery_required`，却缺少对应的恢复入口；
- “同一 dirty generation 再次撞上压缩”依赖下一次用户/系统请求，而不是同一次被拖延的 compact；
- 第 8 节恢复 capsule 只覆盖“压缩已发生”的情况，未覆盖“因阻止而 TurnAborted、上下文仍未压缩”的故障恢复。

**建议：** 拆成两条互斥路径并各自写清入口/出口：

1. **允许压缩的降级恢复**：`compacting -> SessionStart(compact) -> recovery_required/核对 -> seal -> safe`；
2. **TurnAborted / 未压缩失败**：如何提示用户、是否靠 `Stop`/下一 prompt 强制 seal、上下文触顶时怎么办。

同时补全状态转换：`recovery_required -> safe`（核对后 seal）、`compacting` 失败回退、以及 manual/auto 是否共用策略。

### [P1] Stop continuation 与 PreCompact 中止会互相打架，seal 窗口不可靠 — `docs/brainstorms/2026-08-07-codex-context-continuity.md:145-149,151-169`

第 6 节依赖 `Stop` 在 dirty 时发起一次 continuation，只整理检查点并 seal。但若上下文已接近或达到自动压缩阈值，continuation 触发的下一次模型请求本身就可能先走 auto compact；若此时仍按第 7 节阻止，则 seal continuation 会以 `TurnAborted` 失败，检查点永远封不上。

方案把 Stop 循环保护和 dirty compact 阻止分开写了次数上限，但没有定义二者并发/嵌套时的优先级与失败处置。

**建议：** 明确优先级，例如：

- 接近阈值且 dirty：禁止再依赖“先阻止 compact 再 seal”；改为允许 compact + 降级恢复，或在工具批次之间提前 seal；
- Stop continuation 期间若发生 compact：必须走放行 + capsule，不得再次 `continue: false`；
- 验收至少增加一条：dirty + 近阈值 + Stop continuation 不会因 PreCompact 阻止而无法 seal。

### [P1] seal 机制仍是软约束，机械层无法保证“turn 结束前必有安全检查点” — `docs/brainstorms/2026-08-07-codex-context-continuity.md:33-37,122-143`

目标写“每个正常 turn 结束前必须形成可恢复安全检查点”，但语义提炼完全依赖 Skill/模型在约定时机调用 seal；Hooks 只能做脏标记、阻止/放行和注入。`checkpoint seal` 是工具、脚本还是 Skill 指令未定义；若 seal 走普通 `Bash`/工具路径，还需规定它如何不被“业务工具 = dirty”规则误伤，以及失败时如何从 Stop continuation 可靠调用同一入口。

结果是：边界优先压缩的主收益高度依赖模型遵从，而方案把很多保证语气写得像机械合同。

**建议：** 在需求层区分：

- **硬保证（Hooks）**：身份校验、事件审计、compact 后注入、循环上限、凭据不入库；
- **尽力而为（Skill）**：增量语义检查点与 turn-end seal；

并规定最小可调用 seal CLI/脚本合同（入参、退出码、幂等、与 PreToolUse 的交互）。没有该合同前，不要把“必须 seal”写成可验收硬条件，或把它改成“Stop 最多续一次；仍 dirty 则降级告警并可审计”。

### [P2] 热/温状态 token 预算缺少计量合同，seal 校验可能假失败或假通过 — `docs/brainstorms/2026-08-07-codex-context-continuity.md:60-85,133-141,218`

方案要求 seal 校验约 600 / 1,500 token 硬预算，并在超预算时按优先级重写。但未规定：

- 使用哪套 tokenizer / 估算器（与 Codex 内部估算是否一致）；
- 估算误差容忍；
- 重写由模型完成还是由确定性压缩器完成；
- 与 Hooks `additionalContext` 默认约 2500 token spill 阈值如何对齐。

若估算偏松，恢复 capsule 会加速再次压缩（验收项 6 想防的问题）；偏紧则 seal 频繁失败，逼出 Stop continuation / dirty 路径。

**建议：** 指定第一版估算方法（哪怕是保守字符近似）、误差策略，以及 capsule 注入时的 `additionalContextLimit`。预算校验失败时的机械行为（拒绝 seal 并保持 dirty，还是写入截断版并标 `degraded`）也需写死。

### [P2] PreToolUse 脏标记的工具覆盖面未定义，容易出现假 safe / 假 dirty — `docs/brainstorms/2026-08-07-codex-context-continuity.md:36-37,48-56`

“任何业务工具开始执行前立即转 dirty”依赖 PreToolUse matcher 覆盖真实工具名（`Bash`、`apply_patch`、MCP 名等）。方案未给出：

- 业务工具白名单/黑名单；
- 只读探索是否也标 dirty；
- seal / 状态维护命令是否豁免；
- Subagent 内工具是否写入同一 session 状态。

覆盖不足会出现“其实已改文件但仍 safe，自动压缩被误放行”；覆盖过宽则长探索永远 dirty，把系统推向第 7 节那条有问题的阻止路径。

**建议：** 增加工具分类表：写状态变更类默认 dirty；只读类可配置；状态维护类豁免或“Pre dirty + 成功后 seal 置 safe”。Subagent 与根 session 的状态归属也要写明。

### [P2] 验收场景正确，但缺少可判定证据协议 — `docs/brainstorms/2026-08-07-codex-context-continuity.md:227-250`

九条 smoke 方向和指标清单有价值，且与 `AGENTS.md`“真实 smoke、不能只看帮助文本”一致。但当前仍缺少：

- 每条场景的最小再现步骤 / fixture；
- 通过/失败观察点（事件日志字段、UI 警告、是否出现 TurnAborted、是否重跑已完成步骤）；
- 证据落盘路径；
- “必须信息召回率”的抽样与计分方法。

按现状，实施后容易把“跑过几次 App”写成完成。

**建议：** 为 9 条场景各补一行验收表：前置状态、操作、期望 events、禁止现象、证据路径。指标项至少定义计算公式和采集来源（`events.jsonl` 字段）。

### [P3] 状态已标“已确认”，但关键宿主语义仍列在开放问题且未被证伪 — `docs/brainstorms/2026-08-07-codex-context-continuity.md:3,282-287`

开放问题第 3 条要求验证 `PreCompact` / `Stop` / compact 后 `SessionStart` 顺序；而第 7 节阻止策略的正确性正取决于该顺序与 `continue: false` 语义。在源码已表明阻止会 `TurnAborted` 的情况下，全文仍维持“已确认”，会误导实施者把未验证假设当合同。

**建议：** 将宿主行为依赖项改回“草案/待验证”，或在修正第 7 节并完成针对性 smoke 前，不要进入“已确认可实施”状态。

### [P3] manual 与 auto 压缩未区分策略 — `docs/brainstorms/2026-08-07-codex-context-continuity.md:17-18,151-169`

能力边界正确指出 `PreCompact` 可区分 `manual` / `auto`，但保护策略未说明用户显式 compact 时是否仍阻止、是否仍走 emergency。若对 manual 也 `continue: false`，用户主动压缩会被变成 TurnAborted，体验更差。

**建议：** 明确 manual 默认放行（可注入警告 + 最新检查点）；auto 再套用修订后的降级策略。

## 总体评估

方向与仓库定位匹配：上下文生命周期能力适合落在 `agent-workflow-toolkit`；“Skill 做语义检查点 + 原生阈值触发 + Hooks 做机械保护/注入”、热/温/冷分层、身份隔离、不复制 transcript、真实 App smoke 等主干判断成立，也正确推迟了 App Server 伴随控制器。

当前阻塞点不是“要不要做外部检查点”，而是**第 7 节把 `PreCompact continue: false` 误解成可恢复的延迟压缩**。在 Codex 当前实现里，它更接近“中止压缩并 `TurnAborted`”。在改写该策略并补齐状态机、seal 合同、Stop/Compact 交互之前，不应按本文直接进入实现。

配置项层面（`model_auto_compact_token_limit`、`model_auto_compact_token_limit_scope=total`、`compact_prompt`、compact 后 `SessionStart` 注入）与公开 schema/文档基本一致，这部分可作为保留资产。

## 建议的确认前修改顺序

1. 按 Codex 真实语义重写第 7 节：dirty 默认放行压缩并降级恢复；若保留阻止，必须写明 `TurnAborted` 后果且禁止在 context-limit 路径使用。
2. 重画状态机，区分“压缩后恢复”与“未压缩中止”两条路径；理顺与 Stop continuation 的优先级。
3. 定义 seal 的可调用机械合同，并区分 Hooks 硬保证 vs Skill 尽力而为。
4. 补工具分类、token 预算计量、manual/auto 差异。
5. 把 9 条 smoke 落成带证据路径的验收表后，再把状态从“已确认”推进到可实施。
