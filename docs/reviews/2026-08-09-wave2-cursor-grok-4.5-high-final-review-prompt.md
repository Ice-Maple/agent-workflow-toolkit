# Wave 2 Cursor / Grok 4.5 High final review prompt

用途：Wave 2 实现冻结后的“一次完整验收评审”。这不是开放式头脑风暴，也不是按不同主题拆成多轮的探索。Reviewer 必须在同一轮覆盖下列完整矩阵，并在最终 assistant message 中交付完整报告。

## Prompt

你是本仓库 Wave 2 的独立最终验收 reviewer。使用 Cursor Agent 的 `cursor-grok-4.5-high` 非 Fast 模型，以只读 plan 模式审查当前工作树；不要编辑、创建或删除文件，不要启动任何外部 Agent CLI，不要使用 `--force`、`--yolo`、自动批准或扩大权限。

本轮的目标是一次性回答：Wave 2 的 Controller Skill + Go TaskGateway + Codex App Native action contract + 真实 App Native smoke 是否达到“Native MVP 可用”的验收条件。不要把 Wave 3 外部 CLI Adapter、领域业务语义、SQLite/daemon/dashboard/mailbox 或当前 context-continuity 改动混入 Wave 2 finding。

### 必读范围

先读取 `AGENTS.md`、`README.md`，再读取以下 Wave 2 产物和直接依赖：

- `skills/multi-agent-orchestration/SKILL.md`
- `skills/multi-agent-orchestration/references/native-action-contract.md`
- `internal/adapters/native/native.go`
- `internal/adapters/native/native_test.go`
- `internal/gateway/gateway.go`
- `internal/gateway/gateway_test.go`
- `internal/orchestration/model.go`
- `internal/orchestration/model_test.go`
- `internal/policy/policy.go`
- `internal/policy/policy_test.go`
- `internal/storage/store.go`
- `internal/storage/store_test.go`
- `cmd/agent-workflow-core/main.go`
- `cmd/agent-workflow-core/main_test.go`
- `tests/orchestration/README.md`
- `tests/orchestration/evidence/2026-08-08/W2-native-contract.md`
- `tests/orchestration/evidence/2026-08-09/W2-native-app-smoke.md`
- `docs/plans/2026-08-07-multi-agent-orchestration-mvp-waves.md`
- `docs/guides/agent-capabilities.md`

读取当前工作树中上述文件的实际内容和行号；不要只依据历史报告或提交说明。当前工作树中 `tools/context_continuity/**`、`tests/context_continuity/**`、`docs/plans/2026-08-07-codex-context-continuity-waves.md` 及 `.serena/` 属于其他范围，除非它们直接造成 Wave 2 的安全或可复现性问题，否则标记为 out of scope，不要生成 Wave 2 finding。

同时读取这些历史记录，用来去重而不是照抄：

- `docs/reviews/2026-08-07-multi-agent-orchestration-mvp-rereview-closure.md`
- `docs/reviews/2026-08-08-wave1-closure-status.md`
- `docs/research/2026-08-07-cursor-agent-review-delivery-investigation.md`

### 一次性完整检查矩阵

逐项检查并在 coverage ledger 中给出 `PASS`、`FAIL`、`DEFERRED` 或 `NOT APPLICABLE` 以及文件/测试证据：

1. **权威边界与派发**：Task/Revision/Node/Attempt/Completion/HumanDecision/Event 的唯一事实源；Native 只能执行核心返回的 pending action；Skill 不得自行启动或补记 action。
2. **Action 合同**：launch/observe/send/cancel_host/cleanup 的 action ID、Attempt ID、sequence、lease、parameters hash、幂等、过期和 unknown outcome；未知结果不得静默重发。
3. **Attempt 状态机**：prepared/launching/running/terminal/cancel 状态转换；失败、取消、超时、污染、人工 retry/fallback 的 lineage 和 fail-closed 条件；不能重复创建 in-flight Attempt。
4. **Native 身份绑定**：真实 App Native task handle、provider/session identity、canonical workspace、task canary、execution_started 和 side-effect evidence；错配必须持久化为 contaminated 并进入人工 checkpoint，不能伪造或静默恢复。
5. **Controller 生命周期**：createTask → proposeNode → preflight → prepareAttempt → poll/execute/submit → observe → requestCompletion → artifact validation → awaiting_human_decision → human decision；不得把 provider 成功、tool payload 或 exit code 当成完成。
6. **权限与隔离**：read-only、workspace trust、MCP allowlist/isolation、Goal authority、禁止 force/yolo/隐藏批准；不支持的宿主能力是否明确 unsupported/degraded 并 fail closed。
7. **持久化与恢复**：snapshot/event 顺序、崩溃窗口 reconciliation、原子写、幂等 receipt、污染/检查点/完成证据是否可恢复；日志/证据不得泄露秘密。
8. **Artifact delivery**：canonical path 必须在 task workspace，格式/最小长度/required sections/hash 校验；空正文、tool-only、turn success 但无 final text 是否判定 delivery_missing；artifact 不能只存在 transcript/tool payload。
9. **测试与真实证据**：fake Native 只证明 contract，不冒充宿主；真实 App Native evidence 是否证明 handle、identity、workspace、canary、launch/observe/completion、artifact hash 和人工 approve；race/vet/build/diff evidence 是否与当前代码一致。
10. **范围、文档与退出条件**：Wave 2 文档是否与当前实现和 evidence 一致；不误报 Wave 3/experimental；用户批准后是否确实关闭 Wave 2，而不是自动进入下一 Wave。

### Findings 规则（防止反复扩散）

- 先建立上面的 coverage ledger，再输出 findings；不得因为本轮先看了一个主题就结束并把其他主题留给“下一轮”。
- 只报告当前工作树中有证据、可复现、会影响 Wave 2 安全性/正确性/完成性/退出条件的 P0/P1，以及 release-blocking 的 P2。普通风格、命名、可选增强和 Wave 3 预研不作为阻断 finding。
- 每个 finding 必须包含：`ID`、严重级别、`NEW / ALREADY ADDRESSED / OUT OF SCOPE`、精确文件和行号、影响、最小复现路径、为什么现有测试/证据没有覆盖、最小修复建议、建议新增或运行的验证。
- 与历史记录重复或已被当前代码修复的点，放进 `Already addressed / not a new finding` 表并引用当前证据；不要再次作为新问题提出。
- 如果一个问题已经被更高层 finding 覆盖，合并为一个 finding，避免同一根因拆成多个“新问题”。
- 不得提出“建议再做一次开放式全面复审”作为 finding。只有在确实需要修改代码时，才说明修改后需要一次**针对修改点及其受影响矩阵项**的 targeted re-check；若无 P0/P1 或 release-blocking P2，直接给出最终通过结论。
- 不要为了让报告看起来更谨慎而制造假设性问题；不确定事项标为 `DEFERRED` 并说明它不阻断本轮的理由。

### 最终输出格式（必须在最终 assistant message 重复完整报告）

```markdown
# Wave 2 final acceptance review

- Reviewer/model: Cursor Agent / Grok 4.5 High (non-Fast)
- Scope and commit/worktree basis:
- Verdict: PASS | PASS WITH CONDITIONS | FAIL

## Executive summary

## Coverage ledger
| # | Review axis | Status | Evidence |

## New blocking findings
| ID | Severity | Status | Location | Impact/reproduction | Minimal fix | Verification |

## Already addressed / not new
| Historical item | Current evidence |

## Deferred / out of scope
| Item | Reason it does not block Wave 2 |

## Required gate decision

明确说明：是否可以在当前证据下闭环 Wave 2；若不能，只列出必须先完成的最小动作。不要把未修改的未来增强写成阻断条件。
```

如果使用了 CreatePlan 或其他 tool payload，仍必须把完整报告（包括所有 findings、coverage ledger 和最终 verdict）在最终 assistant message 中再次输出，不能只留下工具调用或 `turn_ended: success`。
