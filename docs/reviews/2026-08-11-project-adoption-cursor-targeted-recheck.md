# Project adoption hardening targeted re-check

- Reviewer/model: Cursor Agent / cursor-grok-4.5-high
- Adapter: Cursor (`cursor`)
- Execution: read-only `plan` mode with explicit workspace trust
- Initial delivery: status `completed`, reason `terminal_and_final_delivery`, final bytes `460`
- Final delivery: status `completed`, reason `terminal_and_final_delivery`, final source `result.result`, terminal `true`, exit code `0`, final bytes `4371`
- Delivery recovery: same-session core-authorized follow-up used: `true`
- Prompt: sanitized; private prompt text is intentionally not stored in this artifact

## Scope

独立 Cursor reviewer，只读核对 project-adoption hardening 的闭环条件。不以作者 `docs/reviews/2026-08-11-project-adoption-issues-and-recommendations.md` §9 为权威，也不继承 broad review 的 PASS WITH CONDITIONS 结论；对照当前磁盘实现、测试与证据链重新判定。

核对材料：`docs/reviews/2026-08-11-project-adoption-cursor-review.md`、`tests/orchestration/evidence/2026-08-11/AWT-adoption-hardening.md`、`tests/orchestration/README.md`、`docs/reviews/2026-08-11-project-adoption-issues-and-recommendations.md`、`internal/gateway/gateway.go`、`internal/gateway/gateway_test.go`、`cmd/agent-workflow-core/adoption_test.go`、`docs/guides/multi-agent-orchestration-project-adoption.md`、`skills/multi-agent-orchestration/SKILL.md`。Plan 模式未现场重跑 `go test` / 完整 `git diff`；实现断言以当前文件为准。

## Re-check results

1. Broad review artifact describes current immutable `delivery_missing` checkpoint — **PASS**
   `docs/reviews/2026-08-11-project-adoption-cursor-review.md:25` 将 AWT-ADOPT-003A 标为 PASS，并引用现行错误码 `completion_already_recorded`、`delivery_revision_required`、`retry_not_allowed`。与实现一致：`internal/gateway/gateway.go:934-935`、`:1077-1078`、`:1091-1092`；`internal/gateway/gateway_test.go:441-449`。不再把补 artifact 后重算 completion 或 `fallback_attempt_required` 写成当前行为。

2. Evidence and README link current broad review and distinguish verdict/limitations — **PASS**
   `tests/orchestration/README.md:40-41` 指向现行 broad review；`:53-54` 指向 targeted recheck 规范路径。`tests/orchestration/evidence/2026-08-11/AWT-adoption-hardening.md:76-78` 区分确定性公共测试、真实 Native smoke、broad review（001–010 PASS + 原 P2 证据替换条件），并要求另用 targeted re-check 关闭 011；人工 Skill discovery 边界在 `:80-82`。

3. AWT-ADOPT-003A has no open contract drift — **PASS**
   公开 HostAction 仅 `launch`/`observe`/`cancel_host`：`internal/gateway/gateway.go:1371-1377`；reserved `send`/`cleanup` 拒绝：`internal/gateway/gateway_test.go:259-278`。再次 `requestCompletion` → `completion_already_recorded`：`gateway.go:934-935`、`gateway_test.go:441-444`、指南 `:377`。普通 `approve` → `delivery_revision_required`：`gateway.go:1091-1092`、`cmd/agent-workflow-core/adoption_test.go:186-187`、指南 `:434-435`。`retry_node` → `retry_not_allowed`：`gateway.go:1077-1078`、`adoption_test.go:198-199`、指南 `:401`。`request_revision`/`abort` 仍可用：`adoption_test.go:201-205`、`skills/multi-agent-orchestration/SKILL.md:33`、指南 `:400`、`:434`。

4. AWT-ADOPT-011 evidence chain is complete — **PASS**
   公共 binary E2E：`cmd/agent-workflow-core/adoption_test.go`。真实 Native smoke：`tests/orchestration/evidence/2026-08-11/AWT-adoption-native-smoke.md`（Verdict PASS）。现行 broad Cursor review：canonical 矩阵已对齐实现。独立 targeted closure：本 re-check + `internal/adapters/cursor/cursor_adoption_review_test.go` 中 `TestRealCursorProjectAdoptionTargetedRecheck`。

5. No new P0/P1 regression in the targeted scope — **PASS**
   对 Gateway recovery、adoption E2E、lock/journal/schema、Skill installer、指南/Skill 合同抽查后，未见新的正确性或安全回归。残留仅为证据文本自相矛盾（见 Findings），不构成 P0/P1。

## Findings

[P3] Broad review Findings 未 scrub 自引用过时描述 — `docs/reviews/2026-08-11-project-adoption-cursor-review.md:44-46`

矩阵已写现行 PASS 合同，Findings 仍声称“文件将 003A/011 标为 FAIL”。建议在接受本 re-check 后清理该段，避免后续读者误读；不阻断 003A/011 关闭。

## Gate decision

**PASS**

- **AWT-ADOPT-003A：可关闭**（明确 unsupported in-place `send`；checkpoint 不可变；revision/abort 为恢复方向）。
- **AWT-ADOPT-011：可关闭**（公共自动化 + Native smoke + 现行 broad review + 本独立 targeted re-check）。

不复用早于 adoption guide 的 Wave 4 verdict。仍保留已声明边界：无统一外部 Adapter public executor、无 state migration、粗粒度 state-dir 锁、宿主 Skill discovery 需新建 Codex 任务人工确认。
