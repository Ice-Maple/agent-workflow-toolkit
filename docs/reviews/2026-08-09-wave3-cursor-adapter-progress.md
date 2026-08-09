# Wave 3 Cursor Agent Adapter progress checkpoint

- Date: 2026-08-09
- Scope: first Wave 3 external CLI Adapter slice
- Status: implementation, real smoke, and targeted independent re-check passed; Cursor checkpoint closed
- Maturity: review candidate, not `experimental`

## Delivered

- `internal/adapters/cli/`: argv-only process runner, stdin/stdout/stderr capture,
  Windows hidden process creation, process-tree cancellation, timeout evidence,
  and stream completion primitives.
- `internal/adapters/artifact/`: task-workspace boundary checks, required-section
  validation, SHA-256 references and atomic artifact replacement.
- `internal/adapters/cursor/`: Cursor Agent preflight, capabilities, `plan`/`ask`
  read-only invocation, stream-json session/final/terminal normalization,
  launch/observe/resume-send/cancel/cleanup, identity/canary evidence and
  transcript retention.
- Core `HostActionResult.reason` propagation so adapter delivery classifications
  such as `delivery_missing` survive into `CompletionDecision`.

## Evidence

- Real smoke: [`W3-cursor-adapter-smoke.md`](../../tests/orchestration/evidence/2026-08-09/W3-cursor-adapter-smoke.md)
- Cursor unit, lifecycle and Gateway integration tests are colocated under
  `internal/adapters/cursor/`; shared runner/stream/artifact tests are under
  their respective packages.

## Independent review checkpoint

- Review report: [`wave3-cursor-adapter-review.md`](2026-08-09-wave3-cursor-adapter-review.md)
- Targeted closure report: [`wave3-cursor-adapter-targeted-recheck.md`](2026-08-09-wave3-cursor-adapter-targeted-recheck.md)
- Reviewer/model: Cursor Agent / `cursor-grok-4.5-high`
- Initial verdict: `PASS WITH CONDITIONS`; the report was delivered through a same-session core-authorized follow-up after the first final payload did not satisfy the required report sections.
- Targeted verdict: `PASS`; `W3C-F1`–`W3C-F5` are all `CLOSED` with regression coverage, and the review invocation path passed its targeted check.
- Closure: `W3C-F1` startup-timeout lifecycle, `W3C-F2` Send ActionID rollback, `W3C-F3` generic stream `name`, `W3C-F4` MCP fail-closed consistency, and `W3C-F5` transcript evidence references are fixed and verified.
- Invocation optimization: the opt-in harness now records initial/final delivery evidence, validates required sections before staging, explicitly cancels after observation failure, probes running state before cleanup, and keeps prompt/transcript data out of the report artifact.
- Claude Code, Codex CLI, and `experimental` acceptance remain out of scope for this checkpoint.

## Verified boundaries

- Untrusted workspace blocks before version/model probes or process launch.
- Windows sandbox enabled mode and unproven write mode fail closed.
- Strict/config-isolated/unsupported MCP allowlists fail closed; unverified
  config-isolated MCP is reported as `unsupported` until an external config
  boundary is implemented and verified.
- `--force`, `--yolo`, `--approve-mcps`, `--sandbox enabled` and extra workspace
  roots are never added by the adapter.
- Exit 0, `turn_ended: success` and tool-only `CreatePlan` payloads do not count
  as final delivery; terminal plus non-empty final text is required.
- Resume follow-up requires a core authorization bit and action ID, and the
  session identity/canary remains bound.

## Not closed by this checkpoint

- Claude Code and Codex CLI adapters remain pending.
- Cross-Adapter hardening, full smoke matrix and `experimental` acceptance
  remain Wave 4 work.
