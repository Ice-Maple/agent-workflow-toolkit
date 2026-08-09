# Wave 2 Native smoke execution record

- Date: 2026-08-09
- Execution path: `agent-workflow-core --gateway` → Codex App Native Subagent → Gateway receipts
- Controller: `codex-controller-wave2-repo-20260809`
- Task: `task-fb6fb3129545f8eb5fc2cb14`
- Revision: `revision-72c11769faadb93d55791beb`
- Node: `node-47eca04f15b2bfb51c702455` (`native-readonly-review`)
- Attempt: `attempt-455a9bcba35af74e85428670`
- Binding: `reviewer / codex-native / gpt-5.6 / high / read-only`
- Workspace: `E:\projects\agent-workflow-toolkit`
- MCP isolation: `strict`

## Sanitized Gateway action record

| Sequence | Action | Action ID | Core status | Result summary |
| ---: | --- | --- | --- | --- |
| 1 | `launch` | `action-bd29b01c61ad26d7e100f9f5` | `completed` | `running`, `execution_started=true`, `side_effect_state=none` |
| 2 | `observe` | `action-f6bc81a47799926eaf09f695` | `completed` | `completed`, `terminal=true`, `execution_started=true`, `side_effect_state=none` |

Both actions were returned by `pollHostActions` with core-issued sequence and lease. The Controller submitted the exact action ID and lease; no action was manufactured or replayed.

## Native identity and canary

- Provider: `codex-native` (Codex App Native capability)
- Native task handle: `019fe47a-d29a-71f3-840e-4a1b2b01d46b` (opaque host handle)
- Provider session: `019fe47a-d29a-71f3-840e-4a1b2b01d46b`
- Identity workspace: `E:/projects/agent-workflow-toolkit`
- Task canary: `task-fb6fb3129545f8eb5fc2cb14:attempt-455a9bcba35af74e85428670:nonce-8d82dd69282c12b281521049`
- Core result: `canary_verified=true`; identity and workspace matched the approved binding.
- Terminal event: `event-d0ac619d95e2107a792b4f40`

## Completion and artifact

- Completion decision: `completion-8d757c17afab2dc95a09da52`
- Status: `completed`
- Reason: `terminal_event_identity_and_output_validated`
- Evidence: `terminal_action_result`, `provider_identity`, `task_canary`, `launch_ticket`, `expected_output_artifact`
- Canonical artifact: [W2-native-app-smoke.md](E:/projects/agent-workflow-toolkit/tests/orchestration/evidence/2026-08-09/W2-native-app-smoke.md)
- Artifact bytes: `1513`
- Artifact SHA-256: `fe1b9c6d0d81e0bd3083ca5792d39f6d6aa3c0e54f69d89acb92e28c32a04272`
- Artifact validation: `format=markdown`, required sections present, `validated=true`

## Human checkpoint

- Decision ID: `decision-1f650def520de18f0e56c535`
- Gateway operation: `recordHumanDecision`
- Decision: `approve`
- Authority: `user-input:approve-wave2-native-smoke`
- Resulting node status: `approved`

## Safety and verification notes

- The Native Subagent only read the Controller Skill, Native action contract and Native contract fixture; it did not create, modify or delete repository files and did not launch an external Agent CLI.
- A separate temporary harness attempt with a mismatched Temp workspace was recorded by the core as `contaminated` and is excluded from the successful execution above; its mismatch was not hidden or retried.
- Local verification for this evidence set passed: `go test -race ./...`, `go vet ./...`, `go build ./...`, `git diff --check`.
- No credentials, tokens, headers, full environment variables or private prompts are stored here.
