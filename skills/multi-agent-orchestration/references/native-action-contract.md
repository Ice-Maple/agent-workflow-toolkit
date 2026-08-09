# Wave 2 Native action contract

This contract describes the boundary between the Go TaskGateway and the Codex App Native host capability. It is intentionally provider-independent; the actual Native task handle is owned by the host and is never created by a shell-launched CLI.

## Core-signed action

The Controller may execute an action only when `pollHostActions` returns it as pending:

```json
{
  "action_id": "action-...",
  "attempt_id": "attempt-...",
  "kind": "launch | observe | send | cancel_host | cleanup",
  "sequence": 1,
  "lease": "lease-...",
  "parameters": {},
  "parameters_hash": "sha256...",
  "status": "pending",
  "expires_at": "..."
}
```

The Controller must preserve `action_id`, `attempt_id`, `sequence`, `lease`, and `parameters_hash`. It must not rewrite parameters or manufacture a replacement action after an unknown result.

## Structured result

The Controller submits one result for the exact action and lease. Launch and observe results must carry enough evidence for the core to validate provider/session identity, canonical workspace, task canary, execution start, and side-effect state. A typical Native launch result is:

```json
{
  "status": "running",
  "provider_handle": "native-task-handle",
  "provider_session_id": "native-session",
  "execution_started": true,
  "side_effect_state": "none",
  "identity": {
    "provider": "codex-native",
    "session": "native-session",
    "workspace": "C:\\workspace"
  },
  "task_canary": "task:attempt:nonce",
  "terminal": false
}
```

The provider/model/role values remain configurable strings. `codex-native` in this example is a fixture identity, not a permanent provider enum.

## Safety rules

- No pending core action means zero Native host calls.
- An action ID replay with the same content returns the stored receipt; changed lease, sequence, kind, or parameter hash is rejected.
- An expired lease becomes `outcome_unknown` and blocks automatic retry/fallback until the core records a human decision.
- A provider/session/workspace/canary mismatch becomes contaminated or incomplete according to the core; it never silently relaunches.
- Completion is separate from provider exit, turn success, tool payload, or Agent self-report. Required artifacts are validated by the core.
- `send` follow-up is permitted only when the core explicitly issues a delivery action for the already verified session.
