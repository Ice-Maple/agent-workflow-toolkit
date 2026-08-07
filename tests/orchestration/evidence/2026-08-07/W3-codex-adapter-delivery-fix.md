# Codex Adapter durable delivery fix

- Date: 2026-08-07
- Scope: Go Adapter command construction, delivery normalization and real Codex CLI regression smoke

## Fix

The Codex Adapter now:

1. always passes `--output-last-message <absolute assigned path>`;
2. captures stdout JSONL and stderr separately;
3. reads the durable last-message file after process exit;
4. prefers a non-empty durable final message, then recognized structured agent-message events, then plain non-JSON stdout;
5. reports `incomplete` with reason `delivery_missing` when exit code is 0 but no final evidence exists;
6. reports a command-start error separately from a provider process failure;
7. never adds force, yolo or dangerous bypass flags.

## Verification

Unit/contract checks:

```text
go test -count=1 ./...
go vet ./...
go build ./...
```

All passed. The unit tests cover empty stdout, durable recovery, tool-only JSONL, structured agent messages, unsafe flag rejection, duplicate output and missing executable handling.

Real regression smoke through the Go Adapter:

```text
TestRealCodexDeliverySmoke
status=completed reason=durable_last_message source=durable_last_message
exit_code=0 final_bytes=16 stdout_bytes=626
PASS
```

The smoke used `gpt-5.6-luna`, read-only sandbox, untrusted approval and an ephemeral session. The last-message file was created under the test temporary directory; no repository artifact or source file was written by the provider call.

## Boundary

This closes the observed Codex final-delivery bug at the Adapter boundary. Full Wave 3 still needs process-tree cancellation, transcript retention, identity/canary binding, MCP capability branches, artifact staging and core CompletionDecision integration.
