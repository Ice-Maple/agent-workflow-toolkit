# Wave 4 real smoke matrix

- Date: 2026-08-10
- Scope: four execution paths for the current Wave 4 evidence index
- Status: four-path smoke matrix is complete for the current evidence boundary;
  Wave 4 hardening and `experimental` acceptance remain open
- Evidence rule: fixture replay and historical W3 records are retained as
  supporting evidence; only the current real invocations below are marked
  current. No credentials, headers, raw stderr or private prompts are stored.

## Matrix

| Execution path | Current evidence | Lifecycle result | Status |
| --- | --- | --- | --- |
| Codex App Native | [`W2-native-app-smoke-record.md`](../2026-08-09/W2-native-app-smoke-record.md) | Core-issued `launch → observe`, identity/workspace/task-canary verified, artifact validated, human `approve` recorded | PASS (historical W2 evidence) |
| Cursor Agent | `TestRealCursorAdapterSmoke` on 2026-08-10 | `completed`, `terminal_and_final_delivery`, `result.result`, session present, terminal true, exit code 0, final bytes 16 | PASS (current) |
| Claude Code | `TestRealClaudeAdapterSmoke` on 2026-08-10 | version `2.1.218`, launch session present, `completed`, `terminal_and_final_delivery`, `result.result`, terminal true, exit code 0, final bytes 16, transcript true | PASS (current) |
| Codex CLI | `TestRealCodexDeliverySmoke` + `TestRealCodexAdapterLifecycleSmoke` on 2026-08-10 | version `0.144.1`, launch thread present, terminal `completed`, reason `terminal_and_final_delivery`, source `durable_last_message`, exit code 0, final bytes 16, transcript true | PASS (current; explicit custom Provider route) |

## Current real invocations

### Cursor Agent

The real read-only `plan` smoke used explicit workspace trust and the
registered `cursor-grok-4.5-high` model. The adapter required both terminal
and final delivery; the process exit code was not used as a substitute. The
same repository also passed the independent Cursor Wave 4 review; see
[`2026-08-10-wave4-cursor-review.md`](../../../../docs/reviews/2026-08-10-wave4-cursor-review.md).

### Claude Code

The real read-only `plan` smoke used explicit workspace trust, disabled tools,
the registered `deepseek-v4-pro` binding and a temporary transcript. The
adapter returned the expected token only after session, terminal and final
delivery evidence were present; cleanup completed through the test harness.

### Codex CLI

The real delivery and lifecycle smokes passed with the non-secret custom
Provider route configuration supplied explicitly to the isolated Adapter:

- `TestRealCodexDeliverySmoke`: `completed`, `durable_last_message`, exit code
  0, final bytes 16.
- `TestRealCodexAdapterLifecycleSmoke`: version `0.144.1`, launch thread
  present, `completed`, `terminal_and_final_delivery`, exit code 0, final bytes
  16, durable last message and transcript present.

The earlier 401 was caused by an invocation-boundary mismatch: exploration
used the user's `model_provider=custom` configuration, while the Adapter
intentionally adds `--ignore-user-config`. The original smoke passed no
Provider configuration, so Codex used its default OpenAI route and rejected
the available authorization. `codex login status` only proves local login
material exists; it does not prove that material is valid for the selected
endpoint. The successful rerun supplied provider name, base route, wire API
and auth requirement as non-secret environment values; credentials remain
outside the repository and evidence.

The smoke failure was rerun after hardening the opt-in test diagnostics; the
reported output contains only status/reason/source/identity/terminal/size
fields and does not dump Provider `stdout` or `stderr`.

## Acceptance boundary

This matrix now has current real passes for Cursor, Claude and Codex CLI; the
Native path remains approved historical W2 evidence. It is not the complete
Wave 4 acceptance record: Windows stability, policy/contamination, recovery,
benchmark, race-enabled verification and human `experimental` confirmation
remain separate exit conditions.
