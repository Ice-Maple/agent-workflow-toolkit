# Wave 3 Cursor Agent Adapter smoke

- Date: 2026-08-09
- Scope: Cursor Agent external CLI Adapter, read-only `plan` mode, Windows
- CLI version: `2026.08.04-aaa8809`
- Registered model: `cursor-grok-4.5-high`
- Workspace: the authorized repository workspace
- Prompt: sanitized no-tool token prompt; the private prompt text is not stored here

## Preflight

The Go Adapter ran the executable version and model probes before launching an
Attempt. The requested model was listed, workspace trust was explicitly
authorized by the caller, and the decision was `passed`.

The invocation used `--print --output-format stream-json --mode plan
--trust --workspace <authorized-workspace> --model cursor-grok-4.5-high`.
It did not pass `--force`, `--yolo`, `--sandbox enabled`, `--approve-mcps` or
an additional workspace root.

## Result

```text
launch: status=running, provider_session_present=true, identity=cursor/workspace
observe: status=completed, reason=terminal_and_final_delivery
observe: source=result.result, terminal=true, exit_code=0, final_bytes=16
final token: ADAPTER_SMOKE_OK
```

The result was obtained through the Go Adapter's stream-json parser. The
adapter required both a terminal event and non-empty final text; exit code 0
alone would have remained `delivery_missing`.

## Contract coverage

Automated tests also cover trust blocking before executable probes, Windows
sandbox unsupported, strict/config-isolated/unsupported MCP branches,
tool-only and empty-final delivery, session identity mismatch handling,
process timeout/cancel, core-authorized resume, atomic artifact staging, and
the Gateway launch → observe → CompletionDecision → human checkpoint path.

This evidence closes the first Cursor implementation/smoke slice only. The
Cursor Adapter remains at review/checkpoint and is not marked `experimental`;
Claude Code and Codex CLI Wave 3 work has not been started by this slice.
