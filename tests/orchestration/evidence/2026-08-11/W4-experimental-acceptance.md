# Wave 4 experimental acceptance record

- Date: 2026-08-11
- Decision: `accept`
- Maturity: `experimental`
- Capability: multi-agent orchestration MVP, four controlled read-only execution paths
- Decision authority: `user-input:accept-wave4-experimental`
- Accepted implementation: the Wave 4 implementation, tests, review and evidence versioned together with this record
- Next Wave: not started

## Human decision

The user explicitly confirmed:

> 确认按当前证据和只读边界接受 Wave 4 为 experimental，go test -race 暂不作为阻塞项

This decision closes the Wave 4 human checkpoint. It does not authorize a
write-capable workflow, Goal autonomy, production rollout, user-configuration
promotion or automatic entry into Wave 5.

## Accepted evidence

| Exit area | Evidence | Result |
| --- | --- | --- |
| Four execution paths | [`W4-smoke-matrix.md`](../2026-08-10/W4-smoke-matrix.md) | Native historical real evidence plus current real Cursor/Claude/Codex CLI smoke PASS |
| Windows stability and recovery | [`W4-selected-follow-up.md`](W4-selected-follow-up.md) | Real process tree, unexpected exit and atomic-replace recovery PASS |
| Delivery, contamination and policy | [`W4-selected-follow-up.md`](W4-selected-follow-up.md) | Per-Adapter/shared recovery, identity contamination, Goal/revision and secret filtering PASS |
| Performance and resource boundary | [`W4-benchmark-complete.md`](W4-benchmark-complete.md) | All seven planned benchmark dimensions PASS |
| Independent review | [`2026-08-11-wave4-experimental-readiness-review.md`](../../../../docs/reviews/2026-08-11-wave4-experimental-readiness-review.md) | Cursor Agent PASS, no findings, no open P0/P1 |
| Final local verification | `go test -count=1 ./...`; `go vet ./...`; `go build ./...`; `git diff --check` | PASS |

## Accepted boundary

- Codex App Native, Cursor Agent, Claude Code and Codex CLI are accepted only
  for the currently evidenced controlled read-only paths.
- Native uses the approved historical Wave 2 real-host evidence. Deterministic
  fixture conformance does not replace real Provider smoke.
- Installed CLI `--version` benchmark probes establish startup cost only; they
  are not counted as Provider calls.
- Benchmark values are a Windows host reference, not a production SLA.
- The repository has no CGO runtime dependency. Windows `go test -race`
  remains optional supplementary verification and is not a blocker for this
  acceptance.
- `experimental` means the capability is suitable for controlled use with the
  documented checkpoints and rollback boundary. It does not mean
  `pilot-validated` or production-ready.

## Activation and rollback

This acceptance updates repository maturity documentation only. It does not
change global/local user configuration or enable an Adapter registration.
Wave 5 requires a separately authorized domain pilot.

If a P0/P1, duplicate launch, cross-task contamination, silent fallback, Goal
authority bypass or unbounded retry is later reproduced, retain the evidence,
disable the affected capability/Adapter registration and return to the last
accepted minimal path as defined by the Wave plan.

## Result

Wave 4 is accepted as `experimental` within the boundary above. The multi-agent
orchestration MVP milestone is complete at `experimental`; Wave 5 remains
unstarted.
