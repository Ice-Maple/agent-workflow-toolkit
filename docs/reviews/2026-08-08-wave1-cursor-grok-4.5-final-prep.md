# Wave 1 Cursor / Grok 4.5 High final-prep review record

- Date: 2026-08-08
- Reviewer: Cursor Agent / `cursor-grok-4.5-high` (non-Fast)
- Invocation: `cursor-agent --print --output-format json --mode plan --trust --workspace E:\projects\agent-workflow-toolkit --model cursor-grok-4.5-high`
- Session: `652045ca-480f-4d44-ae31-ea4aae11c223`
- Verdict: `FAIL`

## Findings from the third review

The review confirmed that lease state, fallback candidate preflight, in-flight duplicate detection, retry checkpoint gating, provider identity persistence and event/snapshot reconciliation were present. It then identified three remaining P1 boundaries:

- An Attempt with no predecessor could still be relaunched after a terminal failure.
- A task/node at `awaiting_human_decision` did not block same-node prepare or downstream node proposal.
- Unknown outcome blocked automatic retry but could still enter fallback.

It also requested stronger checkpoint handling for failed Attempts and workspace identity evidence.

## Remediation before final re-review

- Any node with an existing Attempt now requires an explicit predecessor for later preparation; generic manual retry additionally requires a recorded `retry_node` HumanDecision.
- `ProposeNode`, `runPreflight`, `prepareAttempt`, `prepareRetryAttempt` and `prepareFallbackAttempt` fail closed while the task/node awaits human decision. Terminal failed/incomplete completion requests enter that checkpoint; `fallback: ask` requires a recorded core HumanDecision approval.
- Unknown side-effect state blocks fallback as well as automatic retry.
- Required launch/observe identity evidence now includes canonical workspace; fake E2E returns and tests verify it.
- MCP preflight records whether the selected capability is `strict` or `config-isolated`; unsupported allowlists remain blocked.

This record is not final acceptance. A fresh Cursor read-only re-review and final local verification are still required.
