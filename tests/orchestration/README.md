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

Wave evidence belongs under `tests/orchestration/evidence/<YYYY-MM-DD>/W*-*`. Do not store credentials, authentication headers, complete environment variables, or private user prompts in evidence.
