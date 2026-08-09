# Wave 2 Cursor / Grok 4.5 High final acceptance review

- Date: 2026-08-09
- Reviewer/model: Cursor Agent / `cursor-grok-4.5-high` (non-Fast)
- Invocation: `cursor-agent --print --output-format json --mode plan --trust --workspace E:\projects\agent-workflow-toolkit --model cursor-grok-4.5-high`
- Review session: `09de80a6-5d9a-4b61-a5ea-349f276fc08d`
- Prompt: [one-pass comprehensive review prompt](./2026-08-09-wave2-cursor-grok-4.5-high-final-review-prompt.md)
- Delivery: first turn exited 0 but returned only a Plan summary; a same-session delivery-only follow-up returned the complete report. This follow-up was not a second review.
- Verdict: `FAIL`

## Executive summary

Go TaskGateway + Fake Native contract + Controller Skill boundaries satisfy the Native MVP contract in code and tests; contamination, lease expiry, artifact, checkpoint and idempotency paths have Gateway regression coverage. However, `W2-native-app-smoke.md` contains only a narrative attempt ID and lacks independently verifiable redacted Gateway requests/events, Native handle, identity/canary, artifact hash, completion and `approve` receipt. `W2-native-contract.md` and the capability/plan documents therefore overclaim acceptance. Wave 2 cannot be closed on the current evidence package.

## Coverage ledger

| # | Review axis | Status | Evidence |
| --- | --- | --- | --- |
| 1 | Authority boundary and dispatch | PASS | `SKILL.md:12-17`; `gateway.go:548-577`, `1236-1256`; `native.go:86-88`, `native_test.go:14-18` |
| 2 | Host-action contract | PASS | Native contract; lease/unknown/idempotency paths in `gateway.go:645-675`, `gateway_test.go:570-606`, `native_test.go:11-41` |
| 3 | Attempt state machine | PASS | `model.go:314-330`; in-flight/lineage/unknown coverage in `gateway.go:455-457`, `548-556`, `gateway_test.go:609-636`, `820-839` |
| 4 | Native identity binding | PASS (core) / FAIL (smoke evidence) | Core contamination checks in `gateway.go:680-749`, `1446-1459` and tests; real evidence does not expose handle/identity/canary values |
| 5 | Controller lifecycle | PASS (Skill/core) / FAIL (real evidence) | `SKILL.md:21-30`; fake E2E; real approve/completion receipts are not persisted in the evidence file |
| 6 | Permission and isolation | PASS | `SKILL.md:16`; `policy.go:124-133`; `gateway.go:949-951`; Goal disabled |
| 7 | Persistence and recovery | PASS | `store.go:162-207`, `370-428`; `store_test.go:55-205` |
| 8 | Artifact delivery | PASS (core) / DEFERRED (send recovery) | `gateway.go:1525-1579`; `gateway_test.go:273-312`; no core-issued `send` in this slice |
| 9 | Tests and real evidence | FAIL | Fake contract is covered; the W2 smoke file does not contain the required evidence package |
| 10 | Scope, docs and exit criteria | FAIL | Plan/guide/contract claim acceptance before the evidence package is independently auditable; Wave3 was not started |

## New blocking findings

| ID | Severity | Status | Location | Impact/reproduction | Minimal fix | Verification |
| --- | --- | --- | --- | --- | --- | --- |
| W2-F1 | P1 | NEW | `tests/orchestration/evidence/2026-08-09/W2-native-app-smoke.md:1-25`; related claims in `tests/orchestration/evidence/2026-08-08/W2-native-contract.md`, `docs/guides/agent-capabilities.md`, and `docs/plans/2026-08-07-multi-agent-orchestration-mvp-waves.md` | Wave2 exit evidence must allow an independent reader to verify launch → observe → completion → approve. The current file only narrates the attempt and cannot verify the Gateway actions, core events, Native handle/identity/canary, artifact SHA-256, completion reason, or approval receipt. The surrounding docs claim Native MVP acceptance. | Add a sanitized evidence package, or rerun the real App Native smoke and persist the required fields; remove acceptance claims until the targeted review passes. | Evidence must show handle, provider/session, workspace, canary, action/event summaries, artifact SHA-256, completion reason, and `recordHumanDecision=approve`; then run a targeted re-check for matrix axes 4/5/9/10. |

## Already addressed / not new

| Historical item | Current evidence |
| --- | --- |
| Core must sign Native host actions | Fake rejects unissued actions; Gateway `issueAction`; Skill forbids self-dispatch |
| Lease expiry/unknown blocks automatic retry | Expiry regression and retry/fallback rejection tests |
| Contamination persists a human checkpoint | `TestMismatchedTaskCanaryFreezesAttempt` and related tests |
| Cancelled attempt can be manually retried | `TestConfirmedCancellationCanBeManuallyRetried` |
| Fallback candidate preflight and attempt-bound approval | `TestFallbackRunsCandidate...`, `TestFallbackApprovalIsAttemptBound...` |
| Goal authority validation | `TestGoalAuthorityAndAuthorityReferenceAreValidated` |
| Wave1 core/storage/CLI | W1 evidence and current Gateway/storage tests |

## Deferred / out of scope

| Item | Reason it does not block this Wave2 review |
| --- | --- |
| Core does not currently issue `send`/`cleanup` in this slice | Skill forbids self-issuing; missing artifact is `delivery_missing` plus checkpoint; delivery-only recovery remains a follow-up capability and must not be claimed as tested here |
| cancel/timeout/`request_revision` in the real App smoke | The first smoke is the read-only launch/observe/completion/approve path; Gateway unit tests cover the state primitives |
| `tools/context_continuity/**`, Wave3 CLI adapters and unrelated untracked `.serena/` | Explicitly outside Wave2 scope |
| `AttemptPrepared` not used by `prepareAttempt` | Current transition validation remains sound and this is not a release blocker |

## Required gate decision

Wave2 cannot be closed on the current evidence package.

Minimum next actions:

1. Persist or rerun the real Codex App Native smoke with sanitized handle, identity, workspace, canary, event/action summaries, artifact hash, completion and `approve` receipt under `tests/orchestration/evidence/<date>/W2-*`.
2. Keep acceptance claims withdrawn or explicitly provisional until the evidence package is independently auditable.
3. After that evidence-only change, perform a targeted re-check of W2-F1 and coverage axes 4/5/9/10; do not start another open-ended full review and do not enter Wave3 automatically.

## Follow-up disposition

The evidence-only remediation and the explicitly scoped targeted re-check are recorded in [the targeted re-check report](./2026-08-09-wave2-cursor-grok-4.5-high-targeted-recheck.md). That re-check returned `PASS`, closed W2-F1, and did not introduce new findings.
