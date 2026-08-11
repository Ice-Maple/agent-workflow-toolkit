# Oracle

- Baseline 必须证明用户级真实 `PreToolUse` 已加载，并能正常追加 `read_evidence` 事件。
- 独占 session lock 后必须通过独立 open 失败确认故障已生效；锁期间持久化失败不得阻止真实只读工具或 assistant turn 完成。
- locked probe 必须来自动态文件内容，不得预先出现在目标 prompt/context；实际输出必须包含该值及 completed marker。
- 锁期间 `state`/`events` hash 和 events 长度必须完全不变，不得伪造成功事件。
- locked turn 必须 completed/idle，且没有 error、continuation、`TurnAborted`。
- 解锁后的 control 必须再次追加 `read_evidence`，证明恢复的是持久化能力而不是绕过 Hook。
- 用户级 `hooks.json` hash 必须保持不变；helper 和临时 probe 必须清理。
- 本 gate 使用已加载的用户级 Hook，不依赖项目 Hook trust、项目 wrapper 或 App chat 中不存在的 `/hooks` 命令。
