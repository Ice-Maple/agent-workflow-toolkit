# Wave 3 Claude Code Adapter progress checkpoint

- Date: 2026-08-09
- Scope: second Wave 3 external CLI Adapter slice
- Status: implementation, real smoke and targeted independent re-check passed; Claude checkpoint closed
- Maturity: review candidate, not `experimental`

## Delivered

- `internal/adapters/claude/`: Claude Code preflight, explicit trust and
  read-only plan enforcement, configurable model/reasoning, strict task MCP
  config validation, stream-json session/final/terminal normalization,
  launch/observe/resume-send/cancel/cleanup, identity/canary evidence and
  transcript retention.
- The Adapter uses the shared argv-only runner, hidden Windows process
  creation, process-tree cancellation, stream normalization and workspace
  artifact staging.
- Claude's lack of a verified model-list CLI is reported as a degraded
  preflight capability. The explicit model binding is preserved and no
  fallback model is passed.

## Evidence

- Real smoke: [`W3-claude-adapter-smoke.md`](../../tests/orchestration/evidence/2026-08-09/W3-claude-adapter-smoke.md)
- Independent review: [`wave3-claude-adapter-review.md`](2026-08-09-wave3-claude-adapter-review.md)
- Targeted closure: [`wave3-claude-adapter-targeted-recheck.md`](2026-08-09-wave3-claude-adapter-targeted-recheck.md)
- Unit, lifecycle and Gateway integration tests are colocated under
  `internal/adapters/claude/`.

## Independent review checkpoint

- Reviewer/model: Cursor Agent / `cursor-grok-4.5-high`
- Initial verdict: `PASS WITH CONDITIONS`; W3CL-F1 found that a mismatched
  resumed session could overwrite local resume authority.
- Closure: W3CL-F1 is fixed and covered by
  `TestMismatchedResumeSessionIsContaminatedWithoutReplacingResumeAuthority`.
  The Adapter reports the observed foreign session for Gateway contamination
  evidence, retains the original verified session locally, and rejects later
  Send actions on the contaminated handle.
- Targeted verdict: `PASS`; the Claude review/checkpoint is closed. The
  targeted review invocation also passed its sanitization, required-section,
  follow-up, cancellation and cleanup checks.

## Verified boundaries

- Missing trust authorization, write permission, unsupported reasoning and
  invalid MCP configuration fail closed before launch.
- `--permission-mode plan` and `--strict-mcp-config` are explicit; no
  dangerous permission bypass, fallback model, additional workspace root or
  non-resumable session flag is added.
- Exit 0, a terminal event without final text and tool-only output do not count
  as completion; terminal plus non-empty final text and session identity are
  required.
- Resume follow-up requires a core authorization bit, a one-use action ID,
  matching task canary and the verified provider session. Start failure releases
  the action ID for retry.
- The real smoke used Claude Code `2.1.226`, model `deepseek-v4-pro`, Windows,
  read-only plan mode, strict no-MCP config and disabled tools; it returned
  `ADAPTER_SMOKE_OK` through stream-json with a transcript reference.

## Not closed by this checkpoint

- Cursor and Claude review/checkpoints are closed; Codex CLI, cross-Adapter hardening and
  `experimental` acceptance remain future scope.
