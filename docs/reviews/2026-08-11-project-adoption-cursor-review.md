# Project adoption hardening review

- Reviewer/model: Cursor Agent / cursor-grok-4.5-high
- Adapter: Cursor (`cursor`)
- Execution: read-only `plan` mode with explicit workspace trust
- Initial delivery: status `completed`, reason `terminal_and_final_delivery`, final bytes `767`
- Final delivery: status `completed`, reason `terminal_and_final_delivery`, final source `result.result`, terminal `true`, exit code `0`, final bytes `8300`
- Delivery recovery: same-session core-authorized follow-up used: `true`
- Prompt: sanitized; private prompt text is intentionally not stored in this artifact

## Scope and evidence

只读复审当前 `agent-workflow-toolkit` worktree 中的 project-adoption hardening。未采信作者 closure 摘要（含 `docs/reviews/2026-08-11-project-adoption-issues-and-recommendations.md` §9），也未沿用早于 adoption guide 的 Wave 4 verdict；既有 `docs/reviews/2026-08-11-project-adoption-cursor-review.md` 仅作对照，不作为本 verdict。

Plan 模式无法执行 shell，未能现场重跑 `git status` / 完整 `git diff` / `go test`。变更范围取自会话开始时的 git status 快照，并对当前磁盘文件直接核对。

已核对：`AGENTS.md`；`docs/reviews/2026-08-11-project-adoption-issues-and-recommendations.md`；`docs/guides/multi-agent-orchestration-project-adoption.md`；`docs/architecture/multi-agent-orchestration-architecture.md`；`tests/orchestration/evidence/2026-08-11/AWT-adoption-hardening.md`；`tests/orchestration/evidence/2026-08-11/AWT-adoption-native-smoke.md`；`tests/orchestration/README.md`；`skills/multi-agent-orchestration/SKILL.md`；`skills/multi-agent-orchestration/references/native-action-contract.md`；`tools/orchestration/install_controller_skill.ps1`；`cmd/agent-workflow-core/main.go`；`cmd/agent-workflow-core/adoption_test.go`；`internal/gateway/gateway.go`；`internal/gateway/gateway_test.go`；`internal/storage/store.go`；`internal/storage/root_lock.go` / `root_lock_windows.go` / `root_lock_unix.go`；`internal/storage/store_test.go`；`internal/policy/policy.go`；`internal/policy/policy_test.go`；`internal/protocol/v1/envelope_test.go`；`internal/orchestration/model.go`；`protocol/v1/request.schema.json`。

## Finding closure matrix

| ID | Status | Current evidence |
| --- | --- | --- |
| AWT-ADOPT-001 | PASS | 指南普通 approve 无 `attempt_id`：`docs/guides/multi-agent-orchestration-project-adoption.md:380-389`、决策表 `:394-397`；成功 Attempt 带 `attempt_id` → `invalid_fallback_approval`：`internal/gateway/gateway.go:1082-1084`、`validFallbackApproval` `:1490-1503`；公共路径：`cmd/agent-workflow-core/adoption_test.go:142-158` |
| AWT-ADOPT-002 | PASS | 安装器核验 checkout/core revision·dirty·hash 与 Skill tree hash：`tools/orchestration/install_controller_skill.ps1:105-123`、`:125-163`、`:182-210`；指南写明新建 Codex 任务的人工 discovery 边界：`docs/guides/multi-agent-orchestration-project-adoption.md:111`；自动化仅覆盖临时 consumer + `-AllowDirty` 开发 smoke：`cmd/agent-workflow-core/adoption_test.go:283-336` |
| AWT-ADOPT-003A | PASS | Gateway 仅承认 `launch`/`observe`/`cancel_host`：`internal/gateway/gateway.go:1371-1377`；reserved `send`/`cleanup` 拒绝：`internal/gateway/gateway_test.go:259-278`；`delivery_missing` 进入 checkpoint 后再次 `requestCompletion` → `completion_already_recorded`：`internal/gateway/gateway.go:934-935`、`:976-979`，测试 `internal/gateway/gateway_test.go:441-444`；普通 approve → `delivery_revision_required`：`gateway.go:1091-1092`、`gateway_test.go:446-449`、`adoption_test.go:177-187`；`retry_node` → `retry_not_allowed`：`gateway.go:1077-1078`、`adoption_test.go:189-200`；与指南 `:377`、`:434-435`、Skill `:33`/`:50`、架构 `:245`、Native contract `:57-58` 一致 |
| AWT-ADOPT-003B | PASS | cleanup 归 host/Adapter `finally`：`skills/multi-agent-orchestration/SKILL.md:34`、`skills/multi-agent-orchestration/references/native-action-contract.md:57-59`、指南 `:316`；Gateway 不接受 reserved kinds：`internal/gateway/gateway.go:1371-1377` |
| AWT-ADOPT-004 | PASS | propose 要求非空 role：`internal/gateway/gateway.go:402-405`；preflight 精确匹配 binding role：`:438-441`；测试：`internal/gateway/gateway_test.go:218-257`；role 仍为开放字符串：`internal/orchestration/model.go:78-79`（Binding/Role 字段） |
| AWT-ADOPT-005 | PASS | CLI `0.2.0` + protocol/maturity/revision/dirty：`cmd/agent-workflow-core/main.go:20-44`、`:128-144`；installer 解析并核对：`tools/orchestration/install_controller_skill.ps1:76-86`、`:117-123`；adoption 测试：`cmd/agent-workflow-core/adoption_test.go:23-37` |
| AWT-ADOPT-006 | PASS | schema 含 retry/fallback：`protocol/v1/request.schema.json:13-29`；`--validate-request` → `envelope_valid`：`cmd/agent-workflow-core/main.go:57-62`；runtime/schema 锁定：`internal/protocol/v1/envelope_test.go:13-45`；payload 仍由 Gateway `decodePayload` 严格校验：`internal/gateway/gateway.go:1323-1333` |
| AWT-ADOPT-007 | PASS | strict decoder + trailing 拒绝：`internal/policy/policy.go:311-324`；顶层/nested/trailing 测试：`internal/policy/policy_test.go:12-25` |
| AWT-ADOPT-008 | PASS | CLI 先取 OS lock：`cmd/agent-workflow-core/main.go:87-100`；busy → `state_dir_busy`：`:89-91`；锁实现：`internal/storage/root_lock.go:24-54`、`root_lock_windows.go:26-44`、`root_lock_unix.go:11-19`；并发/busy 测试：`cmd/agent-workflow-core/adoption_test.go:208-280` |
| AWT-ADOPT-009 | PASS | pending → completed/failed/outcome_unknown：`internal/gateway/gateway.go:240-268`；pending/unknown 重放不 dispatch：`:229-235`；fault injection：`internal/gateway/gateway_test.go:163-216`；指南未谎称可恢复 exactly-once：`docs/guides/multi-agent-orchestration-project-adoption.md:163` |
| AWT-ADOPT-010 | PASS | snapshot metadata：`internal/storage/store.go:36-38`、`:94-98`；校验 fail closed：`:402-412`；指南升级用新 state-dir、无 migration：`docs/guides/multi-agent-orchestration-project-adoption.md:113`、`:441` |
| AWT-ADOPT-011 | PASS WITH CONDITIONS | 公共 binary adoption E2E 已存在：`cmd/agent-workflow-core/adoption_test.go`、`tests/orchestration/evidence/2026-08-11/AWT-adoption-hardening.md:21-53`；evidence 区分确定性测试、真实 Native smoke 与人工 Skill discovery：`AWT-adoption-hardening.md:34`、`:76-82`；本复审覆盖 001–011。但 worktree 中 staged 的 `docs/reviews/2026-08-11-project-adoption-cursor-review.md` 相对现行实现过时（见 Findings），在替换该证据前不能把“独立评审已闭环”写进 acceptance |

## Guide-to-implementation consistency

一致：普通 approve 无 `attempt_id`；`delivery_missing` 不可原地重算 completion，错误码指向 revision/abort；version/`envelope_valid`；strict policy；state-dir OS lock 与 `state_dir_busy`；request journal；state schema fail closed；Skill installer/receipt；observe 15 分钟 / launch·cancel 5 分钟（`internal/gateway/gateway.go:28-29`、`:1311-1316`）；公开 HostAction 集合；PowerShell 示例字段名与 JSON tag（`task_id`/`node_id`/`attempt_id`/`ok`）对齐；构建到 checkout 外的 `dist\<commit>` 与 version-specific state-dir 分工清楚。

文档边界（非缺陷）：无统一外部 Adapter executor、无 state migration、粗粒度 state-dir 锁、可选 Windows race、Wave 5/Goal/daemon/生产 SLA。

## Findings

[P2] 替换过时的 staged 独立评审证据 — `docs/reviews/2026-08-11-project-adoption-cursor-review.md:23-54`；`tests/orchestration/README.md:40-41`

该文件将 AWT-ADOPT-003A/011 标为 FAIL，并声称 `TestRequiredArtifactControlsCompletion` 仍允许补 artifact 后再次 `requestCompletion` 成功、普通 approve 返回 `fallback_attempt_required`。现行实现与测试已相反：再次 completion 期望 `completion_already_recorded`（`internal/gateway/gateway_test.go:441-444`），普通 approve 期望 `delivery_revision_required`（`:446-449`；`internal/gateway/gateway.go:934-935`、`:1091-1092`）。在用本复审（或等效现行证据）替换前，AWT-ADOPT-011 的证据链不完整。

实现合同本身：相对 adoption guide 的公开路径，未发现仍开放的 P0/P1 正确性缺陷；先前评审中的 `delivery_missing` 原地重算 / 误导 fallback 错误码问题在当前文件上已闭合。

## Verdict

**PASS WITH CONDITIONS**

AWT-ADOPT-001–010 在当前文件上成立。AWT-ADOPT-011 的自动化与本复审内容成立，但受过时 staged Cursor review 证据拖累，需替换该报告并更新 evidence/README 链接后才可标为全部关闭。不复用早于 adoption guide 的 Wave 4 verdict。
