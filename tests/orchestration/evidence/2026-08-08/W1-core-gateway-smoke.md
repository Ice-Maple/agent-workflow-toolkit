# Wave 1 core Gateway smoke evidence

- Date: 2026-08-08
- Scope: provider-independent TaskGateway vertical slice and fake Adapter E2E
- Status: implementation complete; local validation complete; final independent reviewer acceptance pending
- Real Provider execution: not performed (intentionally deferred to Wave 2/3)
- Workspace writes: only test temporary directories and the configured state directory

## Implemented boundary

The Wave 1 slice now covers:

- immutable task intent and linked task revisions;
- workflow nodes with `checkpointed` default behavior;
- Attempt and HostAction state with launch/observe/cancel action leases;
- append-only JSONL events plus atomic task snapshots;
- persisted request receipts and duplicate action result handling;
- exact open-string role/adapter/model/reasoning/permission binding admission;
- task canary verification and fail-closed contamination status;
- CompletionDecision and HumanDecision as separate records;
- retry/fallback Attempt lineage with explicit predecessor references;
- MCP isolation capability gates (`strict`, `config-isolated`, `unsupported`);
- canonical workspace/artifact checks, secret redaction, and interrupted snapshot recovery;
- persisted action lease expiry and explicit terminal-event evidence.
- protocol v1 `--gateway` dispatch through the shared Go core.
- contamination freezes the owning Task/Node at a durable human checkpoint;
- selected MCP isolation mode is recorded in preflight, launch and observe evidence.

## Verification

Commands run from the repository root:

```text
go test -count=1 ./...       PASS
go test -race ./...          PASS
go vet ./...                 PASS
go build ./...              PASS
go run ./cmd/agent-workflow-core --version
0.1.0-wave1
go test -count=1 ./cmd/agent-workflow-core -run TestGatewayCLIExecutesRequestAndPersistsState
PASS
```

The fake E2E test proves:

```text
createTask
  -> proposeNode
  -> runPreflight
  -> prepareAttempt
  -> launch action
  -> observe action
  -> requestCompletion
  -> awaiting_human_decision
  -> approve
```

The same test verifies that duplicate action results do not invoke the fake Adapter twice. A separate Gateway contract test verifies that reusing a persisted request ID with identical content returns the same result, while changed content is rejected.

The canary contract test submits a mismatched task canary and verifies that the Attempt becomes `contaminated` with no follow-up observe action.

Additional boundary tests cover:

- missing and valid required Markdown artifacts, including workspace containment, minimum bytes, required sections and SHA-256 recording;
- `limited_auto` pre-start/post-start-readonly retry separation and fallback `ask`/`auto_registered`/`disabled` decisions;
- confirmed cancellation, unconfirmed cancellation and timed-out observe results;
- workspace-mismatched bindings, forged/revoked Goal authority, Goal scope and untyped authority references;
- unspecified node creation that remains fail-closed, unknown action kinds, expired leases and late results;
- provider session/workspace identity mismatch, core terminal-event evidence and immutable revision history;
- contamination isolation, MCP mode evidence and downstream-dispatch blocking;
- pre-start retry and registered fallback Attempts with `retry_of`/`fallback_of` lineage;
- secret/token/auth-header filtering in persisted event, snapshot and receipt data, plus interrupted atomic replacement recovery.

The executable CLI smoke created a persisted Task/Revision through
`--gateway --state-dir`, verified `snapshot.json` and `events.jsonl`, and then
sent a second real executable request whose missing-node lookup returned the
structured `unknown_node` error. The reproducible test is recorded separately
in [W1-cli-gateway-smoke.md](W1-cli-gateway-smoke.md). No credentials,
authentication headers or private prompts were recorded.

## Deferred boundary (not Wave 1 exit criteria)

Native Controller Skill, real Codex Native execution, Adapter-driven artifact staging, provider process-tree cancellation/timeout orchestration and the Claude/Cursor/Codex CLI Adapter matrix remain deferred to Wave 2/3. They are not used as evidence for this Wave 1 core acceptance.
