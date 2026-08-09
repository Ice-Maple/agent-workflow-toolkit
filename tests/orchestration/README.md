# Orchestration tests

This directory contains contract, integration, and real Adapter smoke tests for the multi-agent orchestration MVP.

Wave 1 now includes the provider-independent core: task/revision/node/attempt state, file-backed snapshots/events with recovery and redaction, explicit binding/MCP/workspace preflight, host-action leases, request/action idempotency, checkpoint decisions, retry/fallback lineage, and a fake Adapter E2E. It is intentionally not a real Native or external Provider integration; those begin only after independent Wave 1 acceptance.

Run the Go checks from the repository root:

```powershell
go test -count=1 ./...
go test -race ./...
go vet ./...
go build ./...
go run ./cmd/agent-workflow-core --version
Get-Content tests/orchestration/fixtures/wave0/valid-request.json |
  go run ./cmd/agent-workflow-core --validate-request
```

The Codex CLI delivery regression smoke is opt-in because it performs a real provider call:

```powershell
$env:CODEX_ADAPTER_REAL_SMOKE='1'
$env:CODEX_ADAPTER_SMOKE_WORKSPACE='E:\projects\agent-workflow-toolkit'
$env:CODEX_ADAPTER_SMOKE_MODEL='gpt-5.6-luna'
go test -run '^TestRealCodexDeliverySmoke$' -v ./internal/adapters/codexcli
```

The Adapter always requests `--output-last-message`; exit code 0 with no durable or structured final message is reported as `delivery_missing`.

The first Wave 3 Cursor Agent Adapter is implemented in
`internal/adapters/cursor/`. Its default Windows path is read-only `plan` mode:
workspace trust is explicit, sandbox enabled mode and task/session MCP
allowlists fail closed, and `stream-json` must contain both a terminal event
and final text. The real smoke is opt-in:

```powershell
$env:CURSOR_ADAPTER_REAL_SMOKE='1'
$env:CURSOR_ADAPTER_SMOKE_WORKSPACE='E:\projects\agent-workflow-toolkit'
$env:CURSOR_ADAPTER_SMOKE_MODEL='cursor-grok-4.5-high'
go test -run '^TestRealCursorAdapterSmoke$' -v ./internal/adapters/cursor
```

The sanitized run record is [`W3-cursor-adapter-smoke.md`](evidence/2026-08-09/W3-cursor-adapter-smoke.md).
This is a Cursor review/checkpoint slice, not the Wave 3 completion or an
`experimental` declaration.

The Claude Code Adapter is implemented in `internal/adapters/claude/`. Its
Wave 3 path is read-only `plan` mode with explicit workspace authorization,
strict MCP configuration, stream-json session/final/terminal evidence, and
core-authorized session resume. Claude model availability is kept as an
explicit binding because the CLI has no verified model-list preflight command;
the Adapter never enables a fallback model. Its real smoke is opt-in:

```powershell
$env:CLAUDE_ADAPTER_REAL_SMOKE='1'
$env:CLAUDE_ADAPTER_SMOKE_WORKSPACE='E:\projects\agent-workflow-toolkit'
$env:CLAUDE_ADAPTER_SMOKE_MODEL='deepseek-v4-pro'
go test -run '^TestRealClaudeAdapterSmoke$' -v ./internal/adapters/claude
```

The sanitized run record is [`W3-claude-adapter-smoke.md`](evidence/2026-08-09/W3-claude-adapter-smoke.md).
The Claude independent review and targeted closure are recorded in
`docs/reviews/2026-08-09-wave3-claude-adapter-review.md` and
`docs/reviews/2026-08-09-wave3-claude-adapter-targeted-recheck.md`. This is a
closed Claude implementation/checkpoint slice, not a Wave 3 completion or an
`experimental` declaration.

Wave evidence belongs under `tests/orchestration/evidence/<YYYY-MM-DD>/W*-*`. Do not store credentials, authentication headers, complete environment variables, or private user prompts in evidence.
