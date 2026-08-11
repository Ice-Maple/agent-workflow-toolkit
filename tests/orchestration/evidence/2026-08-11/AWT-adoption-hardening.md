# Project adoption hardening evidence

- Date: 2026-08-11
- Scope: public `agent-workflow-core --gateway`, protocol v1, Controller Skill distribution, project adoption guide
- Audit source: `docs/reviews/2026-08-11-project-adoption-issues-and-recommendations.md`
- Base HEAD before fixes: `9f048aae710fed3c5736281ae292bf626867d784`
- Maturity boundary: controlled read-only `experimental`; no write Agent, Goal autonomy, Wave 5, daemon, dashboard or production SLA

## Implemented contract

- Gateway v1 public HostActions are `launch`, `observe`, and `cancel_host`; `send`/`cleanup` are reserved Adapter lifecycle primitives.
- `delivery_missing` reaches a human checkpoint and recovers through `request_revision` or `abort`.
- Provider cleanup is a Native host/Adapter runner `finally` responsibility.
- Workflow node role is non-empty admission data and must equal binding role.
- policy JSON, protocol envelope and operation payloads fail closed on unknown fields/trailing JSON at their documented layer.
- one-shot CLI processes sharing a state-dir serialize through an OS file lock.
- request journal states are `pending`, `completed`, `failed`, and `outcome_unknown`; unknown outcomes never redispatch automatically.
- state snapshots carry schema/protocol/writer metadata; incompatible or unversioned state fails closed.
- consumer repositories install the Controller Skill under `.agents/skills` and retain a receipt that checks toolkit/core/Skill provenance.

## Public-path automation

`cmd/agent-workflow-core/adoption_test.go` executes the published boundary rather than importing a domain workflow:

1. build and inspect the real binary version metadata;
2. validate protocol v1 envelope-only semantics;
3. execute create → propose → preflight → attempt → host action result → completion → ordinary approve;
4. prove a successful attempt-bound approve is rejected;
5. prove `delivery_missing` emits no `send` and can enter `request_revision`;
6. run eight concurrent one-shot processes with the same request and observe one Task/outcome;
7. verify structured `state_dir_busy`;
8. install and `-Check` the Controller Skill in a clean temporary consumer repository.

The test does not impersonate a real Provider. Provider and Codex App Native smoke evidence remains separate.

## Verification

Final command results after implementation/doc closure:

```text
go test -count=1 ./...                                  PASS (all 14 packages)
go vet ./...                                            PASS
go build ./...                                          PASS
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build ./...    PASS
git diff --check                                        PASS

Focused public CLI adoption tests:
TestPublishedVersionAndEnvelopeValidationAreExplicit              PASS
TestProjectAdoptionGuideHappyPathAndDeliveryFailureCheckpoint     PASS
TestGatewayCLIConcurrentSameRequestIsSerialized                   PASS
TestGatewayCLIReturnsStructuredBusyError                          PASS
TestControllerSkillInstallerCreatesVerifiableReceipt              PASS
```

## Real Codex App Native smoke

The core-signed Native path was executed through the public Gateway contract, not by shell-launching Codex CLI:

```text
create/propose/preflight/prepare
→ core launch
→ real Codex App Native reviewer
→ immediate running launch result
→ core observe (15-minute lease)
→ terminal reviewer artifact
→ completion status=completed
→ completion reason=terminal_event_identity_and_output_validated
→ ordinary approve with no attempt_id
→ node status=approved
```

The first real review found two additional P1 issues that deterministic tests had missed: `delivery_missing` could accept a dead-end `retry_node`, and the guide's in-checkout binary path made the installer reject the dirty checkout. Both were fixed and the targeted Native re-review returned PASS with no findings. The same execution showed that a real reviewer can exceed five minutes, so observe now has a 15-minute lease while launch/cancel retain five-minute control leases.

Canonical sanitized report: [`AWT-adoption-native-smoke.md`](AWT-adoption-native-smoke.md). Core-validated artifact SHA-256: `49243d74cfd7737742cde1e2e9a2d0bfb6c21a85e420bbef2d560cc6ea30d3e5`.

## Independent review

The adoption guide was created after the original Wave 4 verdict, so that verdict was not reused.

- Broad review: [`2026-08-11-project-adoption-cursor-review.md`](../../../../docs/reviews/2026-08-11-project-adoption-cursor-review.md), real Cursor Agent, terminal artifact validated by the Adapter, SHA-256 `97a0eb959127d5992fa604ea81e174087bd9ead334a289d8b2b228e7c63c136e`. The current matrix marks AWT-ADOPT-001–010 PASS and reports no open P0/P1 implementation defect.
- Targeted closure: [`2026-08-11-project-adoption-cursor-targeted-recheck.md`](../../../../docs/reviews/2026-08-11-project-adoption-cursor-targeted-recheck.md), real Cursor Agent, Adapter-validated terminal delivery SHA-256 `09d2ba0c6980bdcaf0832384feedbabcae0b0af83f0db2caec6d5e77823da0da`, gate `PASS`; AWT-ADOPT-003A and AWT-ADOPT-011 may close. The repository copy only normalizes Markdown trailing-space line breaks for `git diff --check`; final file SHA-256 is `90e694ac293fc53d5d28952cf95d77e250712057f2dc9da0525ca52ffb254ca3`.

The targeted report contains one non-blocking P3 about the broad report retaining a self-referential description of the file that existed when that review started. Reviewer artifacts are preserved as delivered rather than rewritten; the targeted PASS is the final closure authority.

## Manual activation boundary

The installer smoke proves the exact Codex discovery location and receipt integrity in a clean temporary consumer. Codex loads repository Skills at task start; therefore the final host-discovery check is to open a new Codex task in the consumer repository after installation and confirm that `.agents/skills/multi-agent-orchestration` is the selected Skill. This host UI lifecycle check is not represented as an automated test.
