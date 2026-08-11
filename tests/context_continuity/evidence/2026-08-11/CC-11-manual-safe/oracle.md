# Oracle

- App UI 的真实 manual compact 必须依次产生 `PreCompact(trigger=manual)`、`PostCompact` 和 `SessionStart(source=compact)`。
- safe 前置状态必须在 compact 后恢复为 `safe`，不得误报 `recovery_required` 或降级为假 `clean`。
- `last_compact_result` 必须记录为 `recovered_safe`。
- 直接发送文本 `/compact` 不计入本 case；本 case 只接受输入 `/` 后从 App 菜单选择“压缩”的宿主路径。
