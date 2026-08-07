# External CLI preflight smoke evidence

- Date: 2026-08-07
- Scope: one minimal real, read-only, no-tool completion call per external CLI
- Workspace: `E:\projects\agent-workflow-toolkit`
- Write policy: Cursor `plan`, Claude `plan` with tools disabled, Codex `read-only` with `untrusted` approval
- This is a preflight smoke, not completion of the integrated Go Adapter matrix. It does not prove process cancellation, MCP isolation, artifact staging, or full stream event normalization.

## Cursor Agent

Binding:

```text
Adapter: Cursor Agent
Model: cursor-grok-4.5-high (non-Fast)
Mode: plan
Workspace: explicitly trusted for this authorized repository
```

Command shape:

```text
cursor-agent --print --output-format json --mode plan --trust
  --workspace E:\projects\agent-workflow-toolkit
  --model cursor-grok-4.5-high
```

Result:

```json
{
  "type": "result",
  "subtype": "success",
  "is_error": false,
  "result": "ADAPTER_SMOKE_OK",
  "session_id_present": true,
  "usage_present": true
}
```

Verdict: minimum read-only launch and final delivery passed. No workspace edits were observed. Full Cursor smoke remains required for workspace trust preflight, Windows sandbox unsupported, stream/transcript, empty-final recovery, MCP and cancellation.

## Claude Code

The first call using the environment default model was rejected before inference:

```text
configured default: mimo-v2.5-pro
server response: supported API model names are deepseek-v4-pro or deepseek-v4-flash
```

This was recorded as a binding/preflight failure; no fallback was performed. The retry explicitly used the repository baseline `deepseek-v4-pro` with plan mode, no tools, no session persistence and safe mode.

Result:

```json
{
  "is_error": false,
  "stop_reason": "end_turn",
  "result": "ADAPTER_SMOKE_OK",
  "session_id_present": true,
  "permission_denials": []
}
```

Verdict: minimum no-tool completion passed with the registered model. The provider response also reported usage for both `deepseek-v4-pro` and `deepseek-v4-flash`; the integrated Adapter must record the configured primary binding separately and decide whether provider-reported helper-model usage is acceptable or requires a visible diagnostic. It must not silently relabel the Attempt.

## Codex CLI

Binding:

```text
Adapter: Codex CLI
Model: gpt-5.6-luna
Sandbox: read-only
Approval: untrusted
Session: ephemeral for the first probe
```

The first correctly authenticated invocation with `--json` returned exit code 0 but no stdout. The retry used `--output-last-message` and returned a durable final message:

```text
exit=0
durable_last_message_bytes=17
durable_last_message=ADAPTER_SMOKE_OK
stdout_from_json_mode=empty
```

Verdict: provider execution and durable final-message delivery passed, but stdout-only completion is not reliable. The integrated Adapter must collect JSONL/transcript events and validate `--output-last-message` or an equivalent artifact. Exit 0 with no final text is `delivery_missing` until the durable artifact is verified; it must not be marked completed from the exit code alone.

## Cross-Adapter conclusions

- All three CLIs can perform a minimal real no-tool call from this workspace.
- Cursor requires explicit trust for this workspace and must not receive Windows sandbox flags.
- Claude’s environment default model is not an accepted model for the configured endpoint; the registered `deepseek-v4-pro` binding works.
- Codex CLI reproduces the empty-stdout condition and confirms the need for durable output handling.
- No force/yolo, dangerous bypass, automatic MCP approval, or workspace write capability was used.
- These calls do not close the Wave 3 Adapter matrix; integrated Go Adapter tests must still cover preflight, process lifecycle, identity, cancellation, MCP branches, artifact staging, retries and contamination.
