# Wave 4 baseline evidence

- Date: 2026-08-10
- Scope: shared external CLI lifecycle and JSONL delivery replay
- Status: baseline and Cursor follow-up passed with conditions; Wave 4 and `experimental` acceptance remain open

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
and Codex CLI using one fixture-process harness per provider. Each case covered:

```text
preflight → launch → session continuity → observe → terminal/final delivery → cleanup
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
locks the three-line burst case and `TestRunnerFlushesUnterminatedOutputBeforeFinished`
locks the no-newline flush path.

## Verification

```text
go test -count=1 ./...                                                       PASS
go test -count=10 ./internal/adapters/cli ./internal/adapters/claude ./internal/adapters/cursor ./internal/adapters/codexcli  PASS
go test -count=3 ./tests/orchestration                                      PASS
go vet ./...                                                               PASS
go build ./...                                                             PASS
git diff --check                                                           PASS
```

`go test -race ./...` remains pending in this environment because
`CGO_ENABLED=0` (`go test -race` rejects the run before compilation); no `gcc`
is available for enabling it.

## Real Cursor follow-up

The follow-up task executed the real Cursor Agent path with the explicit
`agent.cmd` executable, read-only `plan` mode and workspace trust:

- `TestRealCursorAdapterSmoke`: PASS; `status=completed`,
  `reason=terminal_and_final_delivery`, `source=result.result`,
  `session_present=true`, `terminal=true`, `exit_code=0`, `final_bytes=16`.
- `TestRealCursorWave4Review`: PASS; initial delivery completed, the same
  session delivered the complete report without a follow-up, and final delivery was
  `status=completed`, `reason=terminal_and_final_delivery`,
  `source=result.result`, `terminal=true`, `exit_code=0`, `final_bytes=18525`.
- Cursor review artifact: [`2026-08-10-wave4-cursor-review.md`](../../../../docs/reviews/2026-08-10-wave4-cursor-review.md),
  validated atomically with 18526 bytes and SHA-256
  `c13a79013b8a380a112ce6a4ee5584f81073af975d402f3e1285acb22a1db98f`.

The independent Cursor verdict is `PASS WITH CONDITIONS`: this baseline
slice is not `experimental`; full four-path smoke, Windows stability,
policy/contamination, benchmark, race-enabled verification and human
acceptance remain open.

The four-path index is [`W4-smoke-matrix.md`](W4-smoke-matrix.md). It records
current Cursor and Claude passes, the approved historical Native evidence, and
the current Codex CLI authorization blocker without promoting it to success.

The initial parser/artifact measurements are recorded in
[`W4-benchmark-baseline.md`](W4-benchmark-baseline.md); they are reference
numbers, not the complete Wave 4 performance exit criterion.
