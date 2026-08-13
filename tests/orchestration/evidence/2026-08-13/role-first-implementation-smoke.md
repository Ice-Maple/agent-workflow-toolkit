# Role-first collaboration implementation smoke

- Date: 2026-08-13
- Scope: U0-U3 implementation worktree before acceptance commit
- Secret boundary: no Provider endpoint, credential, auth header, Prompt body, raw stdout, or raw stderr is stored here

## Deterministic verification

- `go test -count=1 ./...`: PASS
- `go vet ./...`: PASS
- `go build ./...`: PASS
- Linux `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build ./...`: PASS
- `git diff --check`: PASS
- clean consumer Skill + Role Package install and `-Check`: PASS
- direct route with requested count 2: reported `effective_mode=direct`, `actual_instances=0`, `degraded=true`; no Gateway run was invented
- framework route with requested count 2: created one Task, one frozen run and two distinct ready instances with the machine-local Cursor binding
- bridge mismatch/stale owner/receipt redaction tests: PASS
- public bridge executable E2E with JSON transport, detached owner, Controller restart-safe observe, completion, technical release and cleanup: PASS for Cursor, Claude and Codex CLI fixture profiles
- HostAction parameter hash is now canonical across typed Go values and JSON-decoded maps; the public executable smoke caught and closed the prior mismatch
- technical-release rejection matrix for failed completion, `delivery_missing`, contamination and outcome unknown: PASS

## Real external CLI execution

| Path | Current result | Sanitized evidence |
| --- | --- | --- |
| Cursor Agent | PASS | terminal completed, `terminal_and_final_delivery`, session present, exit 0, final bytes 16 |
| Claude Code | PASS | version 2.1.218, terminal completed, `terminal_and_final_delivery`, session present, transcript present, final bytes 16 |
| Codex CLI | PASS after explicit local custom route | version 0.144.1, launch thread present, terminal completed, `terminal_and_final_delivery`, durable last message present, transcript present, final bytes 16 |

The first Codex rerun intentionally used an incorrect placeholder route and failed with process exit 1. The corrected rerun read only the non-secret route fields from the existing local Codex config and passed. This confirms the same boundary already recorded in Wave 4: `--ignore-user-config` requires the selected custom route to be supplied explicitly; `codex login status` alone does not identify the matching endpoint.

## Public bridge role-first handoff

The reproducible `TestPublicBridgeRoleFirstHandoff` builds and invokes the real
`agent-workflow-core` and `agent-workflow-bridge` executables for all three
external CLI profiles. Each path runs:

```text
frozen CollaborationRun/Instance
→ prepareAttempt
→ public bridge launch
→ detached private owner + core result
→ a fresh bridge process observes the same owner
→ completion
→ technical release
→ Adapter cleanup
```

The provider processes in this deterministic handoff are local protocol
fixtures, while the table above separately records the real Provider lifecycle
calls. Together they prove the public bridge wiring and each real Adapter; the
fixture is not represented as another Provider smoke.

## Codex App Native role-first smoke

- Path: core-created role-first run and Attempt → core-signed Native `launch` → Codex App native subagent → completion → technical release.
- Native handle/session: `019ff946-0f15-72a2-bec3-127310b9d6dd`.
- Run/Instance/Attempt: `run-6aa6a60ce20980a2c4edcb6c` / `instance-db1536890ebf966d892ccd23` / `attempt-5487cc097937384e34802f6b`.
- Native result: exact `NATIVE_ROLE_FIRST_OK`; no tool, file, network or external CLI action was requested.
- Core validation: Attempt `completed`; completion `completed` with `terminal_event_identity_and_output_validated`; Instance `technically_delivered`; technical release `release-aa4f7c5694523b65deade1ad`.
- No `HumanDecision`, `user-input:*` or business approval was created; this is framework technical release only.

## Independent closure

The independent reviewer initially reported five P1 findings, then two
remaining P1 findings and one follow-up durable-write P1. The implementation
was revised for multi-attempt disable convergence, private capability handling,
upstream outputs, shared Role Package installation, the canonical Controller
receipt, truthful shutdown status classification and durable convergence
writeback. The final targeted re-check returned `PASS` with no remaining P0/P1;
the reviewer also reran `go test -count=1 ./...` and `git diff --check`.

## Remaining acceptance boundary

- `go test -race` remains non-blocking by the user's prior decision because this machine has `CGO_ENABLED=0` and no required C toolchain.
