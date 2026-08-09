# Wave 2 Cursor / Grok 4.5 High targeted evidence re-check

- Date: 2026-08-09
- Reviewer/model: Cursor Agent / `cursor-grok-4.5-high` (non-Fast, read-only plan mode)
- Scope: W2-F1 plus coverage axes 4/5/9/10 only
- Session: `fa8377f1-5fd5-4198-b890-57e10aac61b9`
- Delivery: same-session delivery-only follow-up; not a second review
- Verdict: `PASS`

## Evidence checklist

| Required field | Status | Source |
| --- | --- | --- |
| Gateway `launch` action | Present | `W2-native-app-smoke-record.md`, sequence 1, `action-bd29b01c61ad26d7e100f9f5` |
| Gateway `observe` action | Present | Same record, sequence 2, `action-f6bc81a47799926eaf09f695` |
| Core-issued poll/lease summary | Present | Record states `pollHostActions` returned both actions and exact IDs/leases were submitted |
| Terminal event | Present | `event-d0ac619d95e2107a792b4f40` |
| Native handle/provider/session/workspace | Present | Opaque handle/session `019fe47a-d29a-71f3-840e-4a1b2b01d46b`, provider `codex-native`, repository workspace |
| Task canary and verification | Present | Full `task:attempt:nonce` value and `canary_verified=true` |
| Completion decision/status/reason | Present | `completion-8d757c17afab2dc95a09da52`, `completed`, `terminal_event_identity_and_output_validated` |
| Artifact path/bytes/SHA-256/validated | Present | Canonical artifact, `1513` bytes, `fe1b9c6d0d81e0bd3083ca5792d39f6d6aa3c0e54f69d89acb92e28c32a04272`, `validated=true` |
| `recordHumanDecision=approve` and final node status | Present | `decision-1f650def520de18f0e56c535`, `approve`, node `approved` |
| Sanitization | Present | No credentials, tokens, full environment variables or private prompt |

## Consistency checks

- Attempt ID, action IDs, event ID, completion decision, artifact hash and approval record are internally consistent with the real smoke record and Gateway success fields.
- Workspace/canary format matches the Skill, contract and Gateway canonicalization; the separate mismatched Temp harness was explicitly recorded as `contaminated`, not treated as success.
- The plan, guide and contract now match the evidence and continue to state that Wave3 has not started and `experimental` is not claimed.
- The read-only Native boundary remains intact: no shell-launched CLI, force/yolo, hidden approval or permission expansion.

## Finding status

W2-F1 is closed. The evidence record independently covers the previously missing launch/observe, Native identity/canary, completion, artifact hash, approval and final node status. No unrelated finding was added.

## Gate decision

The evidence blocker is removed and Wave2 can be closed. Do not automatically enter Wave3 or mark the capability `experimental`.
