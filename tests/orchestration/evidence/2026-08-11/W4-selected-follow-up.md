# Wave 4 selected follow-up evidence

- Date: 2026-08-11
- Scope: Windows stability/fault injection, cross-Adapter delivery recovery and contamination, policy/Goal/revision/secret-filtering expansion, complete benchmark
- Optional supplementary gate: race-enabled verification; not a project dependency or minimum `experimental` blocker
- Acceptance boundary: accepted by the user as `experimental`; canonical decision is `W4-experimental-acceptance.md`

## Focused results

| Area | Evidence | Result |
| --- | --- | --- |
| Real Windows process tree | `TestRunnerKillsRealProcessTree` | PASS |
| Unexpected process exit | `TestRunnerReportsUnexpectedProcessExit` | PASS |
| Atomic artifact interruption | `TestStageRecoversAfterInjectedAtomicReplaceInterruption` | PASS |
| Per-Adapter delivery recovery | Cursor/Claude/Codex `TestDeliveryMissingRecovers*` | PASS |
| Shared delivery recovery | `TestWave4ExternalAdapterDeliveryRecoveryConformance` (three fixture Adapters) | PASS |
| Shared contamination boundary | `TestWave4ExternalAdapterContaminationConformance` (three fixture Adapters) | PASS |
| Policy combinations | `TestWave4PolicyMatrixFailsClosed`, `TestWave4PolicyMatrixRejectsRoleAdapterReasoningAndWorkspaceConfusion` | PASS |
| Goal/revision boundary | `TestGoalAuthorityCannotCrossRevisionBoundary` | PASS |
| Secret filtering | `TestStorageFiltersSecretsFromEventsAndReceipts` including Goal evidence | PASS |
| Complete benchmark | `W4-benchmark-complete.md`; all seven planned dimensions | PASS |
| Independent final review | `docs/reviews/2026-08-11-wave4-experimental-readiness-review.md` | PASS; no findings; no open P0/P1 |

Fixture Adapter conformance is deterministic lifecycle evidence, not a replacement for real Provider smoke. The current real Cursor, Claude and Codex CLI smoke results remain recorded in [`W4-smoke-matrix.md`](../2026-08-10/W4-smoke-matrix.md).

## Commands

```text
go test -count=1 ./internal/adapters/cli ./internal/adapters/artifact ./internal/adapters/cursor ./internal/adapters/claude ./internal/adapters/codexcli ./internal/policy ./internal/storage ./internal/gateway ./tests/orchestration  PASS
go test -run '^$' -bench . -benchmem -benchtime=10x -count=3 ./internal/adapters/cli ./internal/adapters/artifact ./internal/storage  PASS
git diff --check  PASS
```

Complete measurements are recorded in [`W4-benchmark-complete.md`](W4-benchmark-complete.md). `go test -race` remains unrun because the current Windows Go toolchain has `CGO_ENABLED=0`; the repository does not use CGO, so this is retained only as optional supplementary evidence.

The final real Cursor review first exposed and drove closure of a Goal evidence redaction false positive, then rechecked the strengthened contamination ActionID assertion. Its final current-tree verdict is PASS with no findings. The subsequent human decision is recorded in [`W4-experimental-acceptance.md`](W4-experimental-acceptance.md).
