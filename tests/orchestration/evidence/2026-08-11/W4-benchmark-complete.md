# Wave 4 complete benchmark evidence

- Date: 2026-08-11
- Revision under test: `1be95a5` plus the current uncommitted Wave 4 follow-up and benchmark diff
- Host: Windows `amd64`, Intel(R) Core(TM) i7-7700T CPU @ 2.90GHz
- Go: `go1.26.5 windows/amd64`, `CGO_ENABLED=0`
- Installed CLI probes: Cursor `2026.08.04-aaa8809`, Claude Code `2.1.218`, Codex CLI `0.144.1`
- Command: `go test -run '^$' -bench . -benchmem -benchtime=10x -count=3 ./internal/adapters/cli ./internal/adapters/artifact ./internal/storage`
- Result: PASS; all seven benchmark dimensions in the Wave 4 plan have current measurements

## Results

The ranges below are the minimum and maximum of three independent ten-iteration
runs. They are local reference measurements, not production SLAs.

| Dimension | Benchmark | Result |
| --- | --- | --- |
| Deterministic fresh-process startup | `BenchmarkRunnerColdStart` | 30.75–32.98 ms/op |
| Installed CLI fresh-process startup | `BenchmarkInstalledCLIColdStart` | Cursor 1.268–1.283 s/op; Claude 0.508–0.512 s/op; Codex 0.395–0.412 s/op |
| Working/peak set | `BenchmarkRunnerWorkingSet` | 0 MiB payload: working 7.39–7.44 MiB, peak 7.40–7.44 MiB; 32 MiB touched payload: working 40.25–40.27 MiB, peak 40.26–40.27 MiB |
| JSON/JSONL throughput | `BenchmarkParseJSONLReplay`, `BenchmarkStreamSummaryAddLine`, `BenchmarkParseJSONLLargeDelivery` | replay 7.58–13.43 MB/s; incremental lines 10.01–15.07 MB/s; 256 KiB delivery 107.41–155.74 MB/s |
| Parallel different-actor isolation | `BenchmarkRunnerConcurrentActorIsolation` | 8 isolated actor workers; 11.16–11.68 ms/op aggregate benchmark time; every result matched its actor-specific workspace/environment value |
| Cancellation response | `BenchmarkRunnerCancellationResponse` | 189.27–194.84 ms from cancel request through confirmed process-tree termination |
| Event append | `BenchmarkStoreEventAppend` | 0.891–1.033 ms/op, including JSON encode, file open/append, sync and close |
| Snapshot replace | `BenchmarkStoreSnapshotReplace` | 8.490–8.497 ms/op for a 100-event state, including sanitized marshal, journal, sync and atomic replacement |
| Artifact replace reference | `BenchmarkStageAtomicReplace`, `BenchmarkStageLargeAtomicReplace` | small artifact 4.164–4.411 ms/op; large artifact 41.68–51.32 MB/s |

## Measurement boundaries

- `BenchmarkRunnerColdStart` starts a new local Go helper process per iteration;
  the installed CLI sub-benchmarks start each CLI's PowerShell shim with
  `--version`. The latter prove real executable startup only and do not contact
  a Provider or replace the real smoke matrix.
- “Fresh process” does not mean a flushed filesystem/OS cache. Repeated runs
  intentionally represent a warm developer host with a new process each time.
- The memory benchmark allocates and touches every page before querying Win32
  `GetProcessMemoryInfo`. The sampling code uses pure Go `syscall`; it does not
  add CGO or another project dependency.
- Parallel isolation is an executable correctness benchmark as well as a
  timing measurement: each worker has a distinct workspace and actor value,
  and the run fails on any cross-actor result.
- The cancellation number includes the current Windows `taskkill /T /F`
  process-tree path. It is a host baseline, not a universal latency guarantee.
- Event append isolates the append-only JSONL primitive. Snapshot replace uses
  the public `Store.Save` path; `SaveWithEvent` combines both costs and is not
  double-counted as a separate plan dimension.

## CGO and race boundary

No Go source in the repository imports `C`, and the complete benchmark passes
with `CGO_ENABLED=0`. `go test -race` on Windows is a separate optional
toolchain verification that requires CGO plus a supported C compiler; it is
not a runtime dependency, not one of the original Wave 4 benchmark dimensions,
and is not used as a blocker for the minimum `experimental` acceptance path.
