# Wave 3 Claude Code Adapter smoke

- Date: 2026-08-09
- Scope: Claude Code external CLI Adapter, read-only `plan` mode, Windows
- CLI version: `2.1.226 (Claude Code)`
- Registered model: `deepseek-v4-pro`
- Workspace: the authorized repository workspace
- Prompt: sanitized no-tool token prompt; the private prompt text is not stored here

## Preflight

The Go Adapter ran the executable version probe before launching an Attempt.
The requested model was retained as the explicit binding; Claude Code has no
verified model-list preflight command, so the Adapter does not claim provider
availability beyond the binding and never enables a fallback model. Workspace
trust was explicitly authorized by the caller, permission was `read-only`, and
the MCP boundary was strict with no task MCP config.

The invocation used `--model deepseek-v4-pro --output-format stream-json
--verbose --permission-mode plan --strict-mcp-config --tools=`. It did not pass
`--dangerously-skip-permissions`, `--allow-dangerously-skip-permissions`,
`--fallback-model`, `--add-dir` or `--no-session-persistence`.

## Result

```text
preflight: status=passed, version=2.1.226 (Claude Code)
launch: status=running, provider_session_present=true
observe: status=completed, reason=terminal_and_final_delivery
observe: source=result.result, terminal=true, exit_code=0, final_bytes=16, transcript=true
final token: ADAPTER_SMOKE_OK
```

The result was obtained through the Go Adapter's stream-json parser. The
Adapter required a provider session identity, a terminal event and non-empty
final text; exit code 0 alone would remain `delivery_missing`.

## Contract coverage

Automated tests cover explicit trust and read-only permission gates, reasoning
validation, strict/config-isolated/unsupported MCP branches, task-config
allowlist matching, session identity, core-authorized resume, action-ID
rollback after process start failure, timeout/cancel, transcript evidence,
tool-only delivery and the Gateway launch → observe → CompletionDecision →
human checkpoint path.

This evidence closes the Claude implementation/smoke slice only. The Claude
Adapter remains at review/checkpoint and is not marked `experimental`; Cursor's
checkpoint is closed, while Codex CLI and cross-Adapter hardening remain
pending.
