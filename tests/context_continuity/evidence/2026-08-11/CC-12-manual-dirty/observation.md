# Observation

在 dirty 隔离 session 中，通过 App 输入 `/` 并选择“压缩”。宿主依次发送 `PreCompact(manual, origin_status=dirty)`、`PostCompact` 和 `SessionStart(source=compact, status=recovery_required)`；脱敏 emergency manifest 的估算大小为 159 tokens。

恢复阶段核验了合成 marker `W3_DIRTY_MARKER_READY`，随后显式 seal。终态为 `safe`、`checkpoint_seq=2`，`last_compact_result=sealed_after_recovery`。

结论：**PASS**。
