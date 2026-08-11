# Adoption Native Smoke

- Path: core-signed Codex App Native `launch → observe → completion → human decision`
- Binding: `reviewer / codex-native / gpt-5.6-sol / high / read-only`
- Workspace: canonical repository root; no write permission delegated to the reviewer
- Delivery: reviewer final text staged by the Controller as the required Markdown artifact
- Recovery history: the first real review found two P1 issues; both were fixed and this report is the targeted re-review

## Findings

No findings

Closed during the real smoke:

- `delivery_missing` 后 `retry_node` 现在返回 `retry_not_allowed`，checkpoint 与 Attempt 绑定保持不变，仍可执行 `request_revision` 或 `abort`。
- 指南现在将 core 构建到 checkout 外；临时 clean checkout 在 build 后仍为 clean，installer 不使用 `-AllowDirty` 即可完成安装和 `-Check`，revision/dirty 一致。
- 真实 reviewer 的 observe 超过旧的 5 分钟 lease；Gateway 已把 observe lease 调整为 15 分钟，并保留 launch/cancel 的 5 分钟 control lease。

Reviewer 还确认普通 `approve`、公开 Action、role admission、root lock、request journal、state schema 和 Skill receipt 与实现一致；16 项定向测试及 `git diff --check` 通过。

## Verdict

PASS

这证明当前 host 能在 core-signed public path 上真实启动、观察并交付 Codex App Native reviewer。消费仓库安装 Skill 后仍需新建 Codex 任务完成宿主 discovery 的最终人工激活检查；本 smoke 不把该 UI 生命周期冒充为自动化结果。
