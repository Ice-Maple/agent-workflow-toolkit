# Wave 4 baseline evidence

- Date: 2026-08-10
- Scope: shared external CLI lifecycle and JSONL delivery replay
- Status: four-path smoke baseline passed with conditions; remaining Wave 4 and `experimental` acceptance work stays open

## Deterministic replay

`TestWave4StreamReplayMatrix` passed all six fixtures under
`tests/orchestration/fixtures/wave4/replay/`:

- Cursor assistant final + `turn_ended`;
- Claude assistant content + `result`;
- Codex `thread.started` + `agent_message` + `turn.completed`;
- tool-only terminal output;
- malformed provider diagnostic;
- provider-error result.

The first three cases require a terminal event and non-empty final text. The
last three remain incomplete and cannot be promoted by terminal status or exit
code alone.

## Cross-Adapter lifecycle

`TestWave4ExternalAdapterLifecycleConformance` passed for Cursor, Claude Code
and Codex CLI using one fixture-process harness per provider. Each case covered
one same-session follow-up delivery in addition to the initial path:

```text
preflight → launch → session continuity → observe → terminal/final delivery
→ same-session follow-up → terminal/final delivery → cleanup
```

The assertions use the common lifecycle shape rather than provider-specific
completion wording. The fixture process is not real Provider evidence; the
existing opt-in W3 real smoke records remain authoritative for that boundary.

## Shared runner hardening

Parallel `go test ./...` exposed an intermittent Windows `StdoutPipe`/`cmd.Wait`
race: the pipe could report `file already closed` before the final JSONL lines
were delivered to the adapter, producing a false `terminal_event_missing`.
The shared runner now assigns custom stdout/stderr collectors to `exec.Cmd`,
so `cmd.Wait()` waits for the copy goroutines before publishing `Finished` and
the final unterminated line is flushed. `TestRunnerDrainsAllFastOutputLinesBeforeFinished`
locks the three-line burst case; `TestRunnerFlushesUnterminatedOutputBeforeFinished`
and `TestRunnerFlushesUnterminatedStdoutAndStderrWithCR` lock no-newline flushes;
`TestRunnerCapturesLongOutputWithoutTruncation` covers 256 KiB stdout plus
128 KiB stderr; `TestRunnerRepeatedCancelIsStable` covers injected tree-kill,
timeout evidence and an idempotent second cancellation.

## Verification

```text
go test -count=1 ./...                                                       PASS
go test -count=10 ./internal/adapters/cli ./internal/adapters/claude ./internal/adapters/cursor ./internal/adapters/codexcli  PASS
go test -count=10 ./internal/adapters/artifact ./internal/policy ./tests/orchestration  PASS
go test -count=3 ./tests/orchestration                                      PASS
go vet ./...                                                               PASS
go build ./...                                                             PASS
git diff --check                                                           PASS
```

`go test -race ./...` remains pending in this environment because
`CGO_ENABLED=0` (`go test -race` rejects the run before compilation); no `gcc`
is available for enabling it.

## Automated Wave 4 follow-up

- Artifact: `TestStageRecoversInterruptedPreviousArtifactBeforeReplacement`
  and `TestStageRemovesStalePreviousAfterSuccessfulReplacement` pass; the
  implementation restores `.previous` after an interrupted Windows replace
  and removes stale backup state after a successful install.
- Policy: `TestWave4PolicyMatrixFailsClosed` covers unregistered binding,
  permission expansion, MCP expansion without isolation, goal scope wildcard
  or overreach, and fallback scope expansion.
- Adapter symmetry: Claude and Codex now have explicit concurrent follow-up,
  missing-identity and provider-terminal-error regressions; Codex also asserts
  that a foreign resume response does not replace local authority.
- Remaining work is cross-Adapter contamination/recovery fault injection,
  complete benchmark dimensions, and race-enabled verification; no item here
  changes the manual `experimental` confirmation boundary.

## Real Cursor follow-up

The follow-up task executed the real Cursor Agent path with the explicit
`agent.cmd` executable, read-only `plan` mode and workspace trust:

- `TestRealCursorAdapterSmoke`: PASS; `status=completed`,
  `reason=terminal_and_final_delivery`, `source=result.result`,
  `session_present=true`, `terminal=true`, `exit_code=0`, `final_bytes=16`.
- `TestRealCursorWave4Review`: PASS; initial delivery completed and the same
  session used a core-authorized delivery follow-up; final delivery was
  `status=completed`, `reason=terminal_and_final_delivery`,
  `source=result.result`, `terminal=true`, `exit_code=0`, `final_bytes=17042`.
- Cursor review artifact: [`2026-08-10-wave4-cursor-review.md`](../../../../docs/reviews/2026-08-10-wave4-cursor-review.md),
  validated atomically with 17540 bytes and SHA-256
  `00c36463851e57c841c7e6e2d3db705c33c21a4c60400c57faceab92f223e349`.

The independent Cursor verdict is `PASS WITH CONDITIONS`: this baseline
slice is not `experimental`; the current four-path smoke matrix and the
automated follow-up subset are complete, while cross-Adapter fault recovery,
complete benchmark, race-enabled verification and human acceptance remain
open.

The four-path index is [`W4-smoke-matrix.md`](W4-smoke-matrix.md). It records
current Cursor, Claude and Codex CLI passes, plus the approved historical Native
evidence. The Codex entry requires explicit non-secret custom Provider route
values because the Adapter intentionally ignores user-global Codex config.

The earlier Codex 401 was not evidence that the local login was absent. The
exploration command inherited `model_provider=custom`; the isolated Adapter
adds `--ignore-user-config`, and the original smoke did not provide the custom
Provider route, so it selected the default OpenAI endpoint. With the custom
Provider name, base route, wire API and auth requirement supplied explicitly,
both real Codex smokes passed. No credentials or route secrets are recorded.

The initial parser/artifact measurements are recorded in
[`W4-benchmark-baseline.md`](W4-benchmark-baseline.md); they are reference
numbers, not the complete Wave 4 performance exit criterion.
