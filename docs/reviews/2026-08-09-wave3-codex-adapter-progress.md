# Wave 3 Codex CLI Adapter progress checkpoint

- Date: 2026-08-09
- Scope: third Wave 3 external CLI Adapter slice
- Status: implementation, deterministic lifecycle/Gateway validation, real delivery/lifecycle smoke and independent review targeted closure passed; Codex checkpoint closed
- Maturity: review candidate, not `experimental`

## Delivered

- `internal/adapters/codexcli/`: Codex CLI preflight/capabilities, explicit
  read-only sandbox and untrusted approval admission, configurable model and
  provider reasoning token, JSONL thread/session identity, durable
  `--output-last-message` delivery, launch/observe/resume-send/cancel/cleanup,
  identity/canary evidence and transcript retention.
- The Adapter uses the shared argv-only runner, hidden Windows process
  creation, process-tree cancellation, stream normalization and workspace
  artifact staging.
- Codex invocations ignore the user's global `config.toml`; an explicitly
  registered provider route may be supplied as non-secret argv configuration so
  model routing remains configurable without inheriting global MCP servers.
- Codex task MCP allowlists fail closed because the verified CLI contract does
  not provide a task/session isolation boundary.

## Evidence

- Real smoke: [`W3-codex-adapter-smoke.md`](../../tests/orchestration/evidence/2026-08-09/W3-codex-adapter-smoke.md)
- Independent review: [`wave3-codex-adapter-review.md`](2026-08-09-wave3-codex-adapter-review.md)
- Targeted closure: [`wave3-codex-adapter-targeted-recheck.md`](2026-08-09-wave3-codex-adapter-targeted-recheck.md)
- Unit, lifecycle and Gateway integration tests are colocated under
  `internal/adapters/codexcli/`.

## Independent review checkpoint

- Reviewer/model: Cursor Agent / `cursor-grok-4.5-high`
- Initial verdict: `PASS WITH CONDITIONS`; W3CX-F1 found that an empty MCP
  allowlist still inherited user Codex config, and W3CX-F2 found that the real
  smoke did not cover the non-ephemeral Adapter lifecycle.
- Closure: W3CX-F1 is closed by the mandatory `--ignore-user-config` argv
  boundary plus explicit non-secret provider route configuration and URL
  credential rejection. W3CX-F2 is closed by the opt-in real
  `Preflight → Launch → thread identity → Observe → durable delivery → Cleanup`
  smoke. The targeted re-check returned `PASS`.
- Invocation optimization: the Cursor review harness used explicit read-only
  plan/trust, required-section validation, same-session delivery recovery,
  cancellation before cleanup on observe failure and sanitized evidence. The
  Codex smoke uses explicit provider route values without storing credentials or
  global user config.

## Verified boundaries

- Missing workspace authorization, write permission, write sandbox, non-untrusted
  approval, invalid reasoning and task MCP allowlists fail closed before
  provider launch.
- `--ask-for-approval untrusted`, `--sandbox read-only`, explicit model and
  optional `model_reasoning_effort` are passed as argv data; user config is
  ignored and no dangerous bypass or extra workspace root is added.
- Exit 0, tool-only JSONL and terminal-without-final output do not count as
  completion. Thread identity, terminal success and non-empty final delivery
  are required; the durable last-message file is preferred when present.
- Resume follow-up requires a core authorization bit, one-use ActionID,
  matching task canary and the locally verified provider thread. A mismatched
  resumed thread is reported as `contaminated` without replacing local resume
  authority.
- The real smoke used Codex CLI `0.147.0`, model `gpt-5.6-luna`, read-only
  sandbox, untrusted approval and ephemeral direct delivery, returning
  `ADAPTER_SMOKE_OK` through the durable last-message path. A second opt-in
  real smoke used non-ephemeral `Adapter.Launch` → thread identity →
  `Observe`, and completed with durable delivery and transcript evidence.

## Not closed by this checkpoint

- Cross-Adapter hardening, Wave 4 smoke matrix and `experimental` acceptance
  remain future scope.
