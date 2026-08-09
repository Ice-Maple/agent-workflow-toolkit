# Wave 3 Codex CLI Adapter real smoke

- Date: 2026-08-09
- Provider: Codex CLI
- CLI version: `codex-cli 0.147.0`
- Registered model binding: `gpt-5.6-luna`
- Workspace: repository workspace supplied explicitly; prompt and temporary
  delivery path are intentionally omitted from this evidence
- Sandbox: `read-only`
- Approval: `untrusted`
- Session mode: `ephemeral` direct delivery smoke
- MCP: no task MCP allowlist requested
- User config: ignored by the Adapter invocation; the real environment supplied
  an explicit registered custom provider route without storing its endpoint or
  credentials in this artifact

## Result

The opt-in `TestRealCodexDeliverySmoke` completed successfully:

```text
status=completed
reason=durable_last_message
source=durable_last_message
exit_code=0
final_bytes=16
stdout_bytes=356
```

The returned final text was validated as the expected smoke token
`ADAPTER_SMOKE_OK`. The result was accepted from the explicit
`--output-last-message` file, demonstrating the observed Codex behavior where
JSONL stdout may contain lifecycle events while durable final delivery is the
stronger completion source.

## Real Adapter lifecycle

The opt-in `TestRealCodexAdapterLifecycleSmoke` also completed successfully on
the same CLI/model binding using a non-ephemeral, read-only, untrusted Adapter
launch:

```text
version=codex-cli 0.147.0
launch_status=running
launch_thread_present=true
terminal_status=completed
terminal_reason=terminal_and_final_delivery
final_source=durable_last_message
terminal=true
exit_code=0
final_bytes=16
durable_last_message=true
transcript=true
```

The resumable launch/observe/send/cancel/cleanup lifecycle, thread identity,
task canary, contamination, transcript and Gateway mapping are additionally
covered by deterministic tests under `internal/adapters/codexcli/`.
