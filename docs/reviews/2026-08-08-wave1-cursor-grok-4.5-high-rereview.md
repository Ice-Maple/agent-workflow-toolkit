# Wave 1 Cursor / Grok 4.5 High re-review

- Date: 2026-08-08
- Reviewer: Cursor Agent / `cursor-grok-4.5-high` (non-Fast)
- Invocation: `cursor-agent --print --output-format json --mode plan --trust --workspace E:\projects\agent-workflow-toolkit --model cursor-grok-4.5-high`
- Scope: fresh read-only review after the first remediation
- Session: `83444b0a-4af2-4ef1-930b-a54953ce04ac`
- Verdict: `FAIL`

## Findings

The reviewer confirmed that the first P1 was fixed: missing execution/side-effect evidence on launch/observe failures is now marked `unknown`, and the regression test blocks automatic retry. It then found six further P1 boundaries:

1. Lease expiry/outcome-unknown left `side_effect_state` as `none`, allowing automatic retry.
2. Fallback did not run candidate preflight, allowing a candidate MCP binding to bypass an unsupported MCP gate.
3. Repeated `PrepareAttempt` calls for one node could issue multiple in-flight launches.
4. Generic `PrepareAttempt(previous_attempt_id=...)` could create a retry without policy evidence or a human retry decision.
5. `BindProviderHandle` marked an identity mismatch in memory but returned before persisting contamination.
6. Snapshot/event JSONL writes were ordered without load-time reconciliation after a crash between the two files.

## Remediation

- Lease expiry now marks the Attempt side-effect state `unknown`; expiry regression coverage asserts automatic retry is rejected.
- Fallback now runs candidate `Preflight` before `DecideFallback` and launch issuance; unsupported MCP fallback is covered.
- One node cannot have multiple in-flight Attempts.
- Generic retry lineage requires a recorded `retry_node` HumanDecision; automatic retry remains behind `RetryAllowed`.
- Bind-provider identity mismatches persist contamination and an event before returning the error.
- Event persistence is snapshot-first with append-if-missing and load-time snapshot/JSONL reconciliation; crash-window tests cover both directions.
- The protocol registers `prepareRetryAttempt` and `prepareFallbackAttempt`, and dispatch prompts include reasoning.

This report is not a final acceptance. A fresh Cursor re-review is required after the remediation; local test/race/vet/build evidence must also be rerun.
