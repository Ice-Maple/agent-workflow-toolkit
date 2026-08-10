# Wave 4 real smoke matrix

- Date: 2026-08-10
- Scope: four execution paths for the current Wave 4 evidence index
- Status: partial; Codex CLI is blocked by current Provider authorization and
  `experimental` acceptance remains open
- Evidence rule: fixture replay and historical W3 records are retained as
  supporting evidence; only the current real invocations below are marked
  current. No credentials, headers, raw stderr or private prompts are stored.

## Matrix

| Execution path | Current evidence | Lifecycle result | Status |
| --- | --- | --- | --- |
| Codex App Native | [`W2-native-app-smoke-record.md`](../2026-08-09/W2-native-app-smoke-record.md) | Core-issued `launch → observe`, identity/workspace/task-canary verified, artifact validated, human `approve` recorded | PASS (historical W2 evidence) |
| Cursor Agent | `TestRealCursorAdapterSmoke` on 2026-08-10 | `completed`, `terminal_and_final_delivery`, `result.result`, session present, terminal true, exit code 0, final bytes 16 | PASS (current) |
| Claude Code | `TestRealClaudeAdapterSmoke` on 2026-08-10 | version `2.1.218`, launch session present, `completed`, `terminal_and_final_delivery`, `result.result`, terminal true, exit code 0, final bytes 16, transcript true | PASS (current) |
| Codex CLI | `TestRealCodexAdapterLifecycleSmoke` on 2026-08-10 | version `0.144.1`, launch thread present, terminal `failed`, reason `process_exit`, exit code 1, no final delivery | BLOCKED (Provider authorization) |

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

The real lifecycle smoke reached `preflight`, `launch` and a provider thread
identity. The provider then returned a redacted `401 Unauthorized` response;
the adapter classified the terminal result as `failed/process_exit` with no
final delivery. This is a current environment authorization blocker, not a
successful smoke, and must not be hidden by the historical W3 record.

The retry action is to refresh the authorized local Codex provider/profile
outside the repository and rerun `TestRealCodexAdapterLifecycleSmoke`; do not
put credentials or route secrets in source, evidence or test output.

The smoke failure was rerun after hardening the opt-in test diagnostics; the
reported output contains only status/reason/source/identity/terminal/size
fields and does not dump Provider `stdout` or `stderr`.

## Acceptance boundary

This matrix is not yet a four-path complete smoke pass. The Native path has
approved W2 evidence, Cursor and Claude have current real passes, and Codex
requires a fresh authorized run. After Codex passes, the matrix still needs
the Wave 4 Windows stability, policy/contamination, recovery and benchmark
evidence before any human `experimental` decision.
