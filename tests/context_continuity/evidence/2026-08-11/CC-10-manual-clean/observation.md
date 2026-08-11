# Observation

在 clean、`checkpoint_seq=0` 且无 emergency manifest 的隔离 session 中，通过 App 输入 `/` 并选择“压缩”。宿主依次发送 `PreCompact(manual)`、`PostCompact` 和 `SessionStart(source=compact)`；恢复后的状态为 `clean`，没有恢复义务，也没有生成 emergency manifest。

`PreCompact` 脱敏 Hook 事件中的实际值为 `origin_status=safe`；本 case 的 clean 前置条件与终态均由持久化状态核验，未据该单字段推导状态。

结论：**PASS**。
