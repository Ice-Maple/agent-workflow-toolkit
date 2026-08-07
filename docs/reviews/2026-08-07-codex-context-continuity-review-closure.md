# Codex 上下文连续性方案评审处理记录

- 日期：2026-08-07
- 原评审：`docs/reviews/2026-08-07-codex-context-continuity-review.md`
- 修订对象：`docs/brainstorms/2026-08-07-codex-context-continuity.md`
- 处理状态：文档修订完成；宿主行为验证未关闭

## 总体处理结论

接受评审的核心结论：原方案错误地把 `PreCompact continue:false` 当作“延迟压缩并争取 seal 窗口”，而当前 Codex 源码证据表明它会使 compact 返回 `TurnAborted` 并中止 turn。该错误影响状态机、Stop continuation 和 dirty 恢复主路径。

修订后的方案不再阻止 dirty 压缩。Manual 和 auto 均默认放行；dirty 时先写机械 emergency manifest，压缩后进入 `recovery_required` 并核对真实证据。原方案状态已从“已确认”退回“草案（阻塞于宿主行为 smoke 验证）”。

本记录只关闭“评审意见是否已进入设计”的文档处理，不把尚未执行的真实 App smoke 或实现验证标记为完成。

## Findings 处理表

| Finding | 处理 | 修订结果 | 验证状态 |
|---|---|---|---|
| P0：首次阻止会中止 turn | 接受 | 删除“一次阻止、二次放行”主路径；第一版 `PreCompact` 不返回 `continue:false`；dirty 写 manifest 后放行。 | 待 CC-02 及源码语义 smoke。 |
| P1：`recovery_required` 状态机建立在错误语义上 | 接受 | 拆成成功压缩恢复与压缩失败/TurnAborted 两条路径；补 `recovery_required -> safe`、`compact_failed`。 | 待 CC-01、CC-02。 |
| P1：Stop continuation 与 compact 冲突 | 接受 | 明确 compact 优先：Stop continuation 期间发生 auto compact 必须放行，压缩后再恢复 seal。 | 待 CC-04。 |
| P1：seal 仍是软约束 | 接受 | 区分 Hooks 硬保证与 Skill 尽力而为；定义 seal/inspect/rotate CLI、入参、幂等、输出和退出码。 | CLI 实现与 smoke 待办。 |
| P2：token 预算无计量合同 | 接受 | 定义保守字符估算公式、20% margin、hot/warm 上限、`additionalContextLimit=800` 和预算失败保持 dirty。 | 待 CC-07。 |
| P2：PreToolUse 覆盖未定义 | 接受 | 增加写入、只读证据、状态维护、未知工具分类；未知默认 dirty；明确 Subagent 归属。 | 待 CC-09。 |
| P2：验收缺少证据协议 | 接受 | 为 CC-01 至 CC-09 定义前置、期望、禁止现象和证据；增加统一目录与指标公式。 | Harness 和真实证据待办。 |
| P3：已确认状态与开放问题冲突 | 接受 | 文档退回草案；宿主事件顺序和 TurnAborted 语义提升为阻塞验证项。 | 阻塞项完成后再确认。 |
| P3：manual/auto 未区分 | 接受 | 增加策略表；manual 始终放行，dirty 时告警并走降级恢复；auto 同样不阻止。 | 待 CC-05。 |

## 关键设计变化

### 原路径

```text
dirty -> PreCompact 阻止 -> 尝试 seal -> 再次 compact -> 恢复
```

### 修订路径

```text
dirty -> PreCompact 写 emergency manifest并放行
      -> compact
      -> SessionStart(compact)
      -> recovery_required
      -> 核对 transcript/workspace/artifact
      -> seal -> safe
```

### 失败路径

```text
PreCompact -> compacting -> 未观察到成功恢复入口
           -> compact_failed
           -> 下一可用入口告警并保留最后安全检查点
```

## 尚未关闭的风险

- 当前事件顺序和 `TurnAborted` 行为尚未在真实 Codex App 中保存复现证据。
- Skill 是否能稳定在 turn 边界生成高质量短检查点仍是尽力而为，需要场景评测。
- 字符估算与真实 tokenizer、Hook spilling 的偏差尚未测量。
- 实际 Hook tool names、MCP 和 Subagent 覆盖尚未验证。
- 首个自动压缩阈值必须依据真实模型窗口和长 turn 数据确定。

## 进入实施前的门槛

1. 实现最小 Hook 探针、状态记录和 seal CLI，不先扩展完整 Skill。
2. 完成修订方案中的 CC-01 至 CC-09，证据保存到约定目录。
3. 对照真实 events、UI 和状态快照修订宿主行为合同。
4. 再次进行独立评审。
5. 评审通过后，才将设计状态改为“已确认可实施”并制定完整实施计划。
