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

If the registered model uses a custom provider, supply its non-secret route
configuration through `CODEX_ADAPTER_SMOKE_MODEL_PROVIDER`,
`CODEX_ADAPTER_SMOKE_PROVIDER_BASE_URL`,
`CODEX_ADAPTER_SMOKE_PROVIDER_WIRE_API` and
`CODEX_ADAPTER_SMOKE_PROVIDER_AUTH=1`; do not put credentials in the
repository or evidence. Adapter invocations ignore the user's global Codex
config so unrequested MCP servers are not inherited. The Adapter always
requests `--output-last-message`; exit code 0 with no durable or structured
final message is reported as `delivery_missing`.

The real resumable Adapter path is separately opt-in:

```powershell
$env:CODEX_ADAPTER_REAL_LIFECYCLE_SMOKE='1'
go test -run '^TestRealCodexAdapterLifecycleSmoke$' -v ./internal/adapters/codexcli
```

The Wave 3 Cursor Agent Adapter is implemented in
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
This is a closed Cursor implementation/checkpoint slice, not an
`experimental` declaration. Cross-Adapter Wave 4 conformance is covered by
`TestWave4ExternalAdapterLifecycleConformance` under `tests/orchestration/`.

The Wave 4 independent Cursor review is also opt-in and uses the same
read-only, trusted workspace boundary:

```powershell
$env:CURSOR_ADAPTER_WAVE4_REVIEW='1'
$env:CURSOR_ADAPTER_EXECUTABLE='C:\Users\<user>\AppData\Local\cursor-agent\agent.cmd'
$env:CURSOR_ADAPTER_SMOKE_WORKSPACE='E:\projects\agent-workflow-toolkit'
$env:CURSOR_ADAPTER_SMOKE_MODEL='cursor-grok-4.5-high'
go test -run '^TestRealCursorWave4Review$' -v ./internal/adapters/cursor
```

The sanitized review artifact is
[`2026-08-10-wave4-cursor-review.md`](../../docs/reviews/2026-08-10-wave4-cursor-review.md).

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

The Codex CLI Adapter is implemented in `internal/adapters/codexcli/`. Its
Wave 3 path is read-only with explicit sandbox/approval values, ignored user
configuration, durable `--output-last-message` delivery and core-authorized
thread resume. Its real lifecycle smoke is opt-in:

```powershell
$env:CODEX_ADAPTER_REAL_LIFECYCLE_SMOKE='1'
$env:CODEX_ADAPTER_SMOKE_WORKSPACE='E:\projects\agent-workflow-toolkit'
$env:CODEX_ADAPTER_SMOKE_MODEL='gpt-5.6-luna'
go test -run '^TestRealCodexAdapterLifecycleSmoke$' -v ./internal/adapters/codexcli
```

The sanitized run record is [`W3-codex-adapter-smoke.md`](evidence/2026-08-09/W3-codex-adapter-smoke.md).
This is a closed Codex implementation/checkpoint slice, not an
`experimental` declaration.

Wave 4 replay fixtures are under `tests/orchestration/fixtures/wave4/replay/`.
They exercise provider-neutral final delivery, terminal evidence, tool-only
incomplete delivery, malformed diagnostics and provider-error delivery. Real
Provider calls remain opt-in and must not be placed in the replay fixtures.

The current four-path real smoke index is
[`W4-smoke-matrix.md`](evidence/2026-08-10/W4-smoke-matrix.md). It separates
current real results, historical Native evidence and Provider authorization
blockers; it is not an `experimental` acceptance record.

The initial parser/artifact benchmark is recorded in
[`W4-benchmark-baseline.md`](evidence/2026-08-10/W4-benchmark-baseline.md).

Wave evidence belongs under `tests/orchestration/evidence/<YYYY-MM-DD>/W*-*`. Do not store credentials, authentication headers, complete environment variables, or private user prompts in evidence.
