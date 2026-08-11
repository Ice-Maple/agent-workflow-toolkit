# Oracle

- App UI 的真实 manual compact 必须依次产生 `PreCompact(trigger=manual, origin_status=dirty)`、`PostCompact` 和 `SessionStart(source=compact, status=recovery_required)`。
- emergency manifest 必须保留足以恢复的最小信息，且不得把 dirty 状态静默改成 safe。
- 恢复后必须核验合成 marker `W3_DIRTY_MARKER_READY`，再显式执行 seal。
- seal 后状态必须为 `safe`，`checkpoint_seq=2`，`last_compact_result=sealed_after_recovery`。
- 直接发送文本 `/compact` 不计入本 case；本 case 只接受输入 `/` 后从 App 菜单选择“压缩”的宿主路径。
