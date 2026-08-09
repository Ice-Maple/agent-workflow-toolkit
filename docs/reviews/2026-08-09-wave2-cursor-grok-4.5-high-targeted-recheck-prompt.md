# Wave 2 Cursor / Grok 4.5 High targeted re-check prompt

这是对 `W2-F1` 的一次**限定范围 targeted re-check**，不是新的全面评审。上一轮完整评审已经覆盖 10 个检查面；本轮只验证证据包补齐后，W2-F1 及其直接影响的检查面 4/5/9/10 是否关闭。

你是独立验收 reviewer。使用 Cursor Agent 的 `cursor-grok-4.5-high` 非 Fast、只读 plan 模式；不要编辑、创建或删除文件，不要启动外部 Agent CLI，不要使用 force/yolo/自动批准。

读取当前实际文件：

- `AGENTS.md`
- `tests/orchestration/evidence/2026-08-09/W2-native-app-smoke.md`
- `tests/orchestration/evidence/2026-08-09/W2-native-app-smoke-record.md`
- `tests/orchestration/evidence/2026-08-08/W2-native-contract.md`
- `skills/multi-agent-orchestration/SKILL.md`
- `skills/multi-agent-orchestration/references/native-action-contract.md`
- `internal/adapters/native/native.go`
- `internal/gateway/gateway.go`
- `docs/plans/2026-08-07-multi-agent-orchestration-mvp-waves.md`
- `docs/guides/agent-capabilities.md`
- 上一轮报告：`docs/reviews/2026-08-09-wave2-cursor-grok-4.5-high-final-review.md`

## 只检查以下项目

1. `W2-native-app-smoke-record.md` 是否独立、脱敏且完整记录 Gateway launch/observe action 和 event 摘要、Native handle/session/provider/workspace、task canary、canary verification、completion decision/reason、artifact path/bytes/SHA-256/validated、`recordHumanDecision=approve` 和最终 node status。
2. 记录中的 ID、workspace、canary、artifact hash 是否与 canonical artifact、Skill/contract、核心字段和上一轮真实 smoke 结果互相一致；是否存在明显伪造、无法解释或把临时污染尝试冒充成功的情况。
3. Wave2 退出文档是否已与补齐后的证据一致，不再在证据完整后错误地声称未验收，也没有越界宣称 Wave3 或 `experimental`。
4. 只读 Native smoke 的边界是否仍然清楚：没有 shell-launched CLI、没有隐藏批准、没有扩大权限。

不要重新审查 retry、Goal、存储、Wave3 或其他上一轮已通过的轴；不要把未来 delivery-only send、cancel/timeout 扩展当成本轮阻断项。上一轮唯一新问题是 W2-F1；若本轮证据关闭它，直接给出 targeted `PASS`，不要提出新的开放式全面复审。只有发现上述四项中证据矛盾或关键字段仍缺失时才给 `FAIL`。

最终 assistant message 必须包含完整报告，不要只返回 Plan/CreatePlan/tool payload：

```markdown
# Wave 2 targeted evidence re-check
- Reviewer/model:
- Scope: W2-F1 + axes 4/5/9/10 only
- Verdict: PASS | FAIL

## Evidence checklist
| Required field | Status | Source |

## Consistency checks
| Check | Status | Evidence |

## Finding status
说明 W2-F1 是否关闭；不得新增无关 finding。

## Gate decision
说明是否可以撤销 evidence blocker 并闭环 Wave2；若 FAIL，只列缺失字段或矛盾。
```
