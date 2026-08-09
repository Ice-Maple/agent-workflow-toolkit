# Wave 3 current status

- Date: 2026-08-09
- Repository: `E:\projects\agent-workflow-toolkit`
- Branch: `main`
- Baseline before this status record: `90a0295 docs: clarify context continuity implementation status`
- Status: Wave 3 external CLI Adapter implementation slices are complete and
  closed as review candidates; they are not yet accepted as `experimental`.

## Closure summary

| Adapter | Delivery commit | Independent review closure | Real smoke evidence | Status |
|---|---|---|---|---|
| Cursor Agent | `80ac472` | Initial `PASS WITH CONDITIONS`; `W3C-F1`–`W3C-F5` closed; targeted re-check `PASS` | `tests/orchestration/evidence/2026-08-09/W3-cursor-adapter-smoke.md` | Closed |
| Claude Code | `2ff6bb8` | Initial `PASS WITH CONDITIONS`; `W3CL-F1` closed; targeted re-check `PASS` | `tests/orchestration/evidence/2026-08-09/W3-claude-adapter-smoke.md` | Closed |
| Codex CLI | `7376f9e` | Initial `PASS WITH CONDITIONS`; `W3CX-F1`–`W3CX-F2` closed; targeted re-check `PASS` | `tests/orchestration/evidence/2026-08-09/W3-codex-adapter-smoke.md` | Closed |

The detailed progress, review and targeted-closure records remain in the
corresponding `wave3-*-adapter-*.md` files in this directory.

## Implemented and verified boundaries

- All three adapters use the shared argv-only process runner, Windows hidden
  process creation, stream normalization, cancellation evidence and workspace-
  bounded artifact staging.
- Preflight and launch enforce workspace authority, explicit read-only modes and
  provider-specific approval boundaries. Dangerous bypass flags and additional
  workspace roots are not injected.
- Task/session MCP policy is fail-closed when isolation cannot be verified.
  Codex additionally passes `--ignore-user-config` and accepts only a
  non-secret, explicitly configured provider route; credentials are not stored
  in source or evidence.
- Exit code `0`, a terminal event alone, `turn_ended: success`, or tool-only
  output is not treated as delivery. A terminal state and non-empty final text
  are required; Codex prefers its durable `--output-last-message` delivery.
- Provider session/thread identity, task canary, resume authorization,
  one-use ActionID, cancellation/cleanup and `contaminated` mismatch handling
  are covered by deterministic lifecycle and Gateway tests.
- Transcript and artifact references are emitted only after successful writes;
  artifact sections, workspace boundaries, SHA-256 references and atomic
  replacement are validated.

## Review invocation hardening

The opt-in Cursor review harness is now part of the evidence boundary rather
than an informal shell workflow. It uses explicit read-only plan/trust
authorization, sanitized prompts and reports, required-section validation
before staging, same-session core-authorized delivery recovery, cancellation
when observation fails, and a running-state probe before cleanup. Prompt text,
transcripts and credentials are not copied into review reports. The Codex real
smoke follows the same config-isolated principle with explicit non-secret
provider values.

## Validation evidence

After the Wave 3 closures, the repository checks were recorded as passing:

```text
go test -count=1 ./...
go test -race ./...
go vet ./...
go build ./...
git diff --check
```

The opt-in real smoke evidence records successful Cursor, Claude Code and Codex
CLI delivery. Codex additionally records a non-ephemeral
`Preflight → Launch → thread identity → Observe → durable delivery → Cleanup`
Adapter lifecycle, while the three adapters' Gateway and failure-path behavior
is covered by deterministic tests.

## Current boundary and next work

There is no pending Wave 3 Adapter implementation or review finding. The next
scope is Wave 4 cross-Adapter hardening, a complete real smoke matrix and the
separate `experimental` acceptance decision. These are not represented as
completed by this status record.

The pre-existing `.serena/` directory is local tooling metadata and remains
untracked intentionally; it is preserved in the workspace but is not part of
the project commit.
