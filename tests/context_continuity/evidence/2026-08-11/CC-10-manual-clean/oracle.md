# Oracle

- App UI 的真实 manual compact 必须依次产生 `PreCompact(trigger=manual)`、`PostCompact` 和 `SessionStart(source=compact)`。
- clean 前置状态不得被 compact 制造恢复义务；恢复后的权威状态仍应为 `clean`。
- `checkpoint_seq` 必须保持 `0`，且不得生成 emergency manifest。
- 直接发送文本 `/compact` 不计入本 case；本 case 只接受输入 `/` 后从 App 菜单选择“压缩”的宿主路径。
