# Observation

在 safe 隔离 session 中，通过 App 输入 `/` 并选择“压缩”。宿主依次发送 `PreCompact(manual, origin_status=safe)`、`PostCompact` 和 `SessionStart(source=compact, status=safe)`。

恢复后 `last_compact_result=recovered_safe`，没有形成额外恢复义务。

结论：**PASS**。
