# Observation

正式 A-B-A smoke 使用 pseudonym 为 `5413c0de69096a99` 的隔离 session。Baseline 在 `06:18:18.369187Z` 观察到用户级真实 `PreToolUse` 追加 `read_evidence` 事件。

随后由 hidden helper 以 `FileShare.None` 独占 session lock，并通过独立 open 失败确认 lock 生效。锁定期间，目标只读工具返回动态合成值 `W3_FAIL_OPEN_LOCKED_9B32D6E145AC407F`，assistant 返回 `W3_FAIL_OPEN_LOCKED_TURN_COMPLETED`；该动态值事先不在目标 prompt/context。turn 在 18,646 ms 后 completed/idle，没有 error、continuation 或 `TurnAborted`。

锁定 turn 前后 `state`/`events` hash 与 events 长度 4,475 bytes 完全不变，证明持久化失败没有伪造事件，而工具与 turn 仍 fail-open 完成。主动停止 helper 并确认进程退出后，control 返回新的合成值和 completed marker；`06:22:25.425005Z` 再次追加 `read_evidence`，events 增长至 4,715 bytes，state/events hash 均相应变化。

用户级 `hooks.json` hash 前后保持 `8dcdff5...7131575`；helper 和临时 probe 均已清理。App chat 中的 `/hooks` 不是本宿主命令；本 gate 没有依赖项目 Hook trust，项目 wrapper 未执行且不计证据。

结论：**PASS**。
