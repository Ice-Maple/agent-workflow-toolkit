# Wave 4 benchmark baseline

- Date: 2026-08-10
- Host: Windows `amd64`, Intel Core(TM) i7-7700T CPU @ 2.90GHz
- Scope: JSONL parsing and artifact atomic-replace measurements, including
  large delivery/artifact payloads
- Status: extended reference baseline only; full cold-start, memory,
  concurrency and cancellation metrics remain open
- Command form: `go test -run '^$' -bench <benchmark> -benchmem -count=3`

## Results

### Shared JSONL parser

`./internal/adapters/cli`:

```text
BenchmarkParseJSONLReplay-8        9047-9968 ns/op   15.45-17.02 MB/s   2920 B/op   53 allocs/op
BenchmarkStreamSummaryAddLine-8    7385-8568 ns/op   17.97-20.85 MB/s   2824 B/op   52 allocs/op
BenchmarkParseJSONLLargeDelivery-8  1650691-1832339 ns/op  143.12-158.87 MB/s  534408-534410 B/op  36 allocs/op
```

The benchmark uses the Cursor success replay fixture and asserts terminal plus
final delivery on every iteration. The range reflects three independent
benchmark runs, not a performance threshold.

### Artifact staging

`./internal/adapters/artifact`:

```text
BenchmarkStageAtomicReplace-8    4364208-4733282 ns/op   15995-16118 B/op   221 allocs/op
BenchmarkStageLargeAtomicReplace-8  5181547-5357926 ns/op  48.93-50.60 MB/s  288194-288355 B/op  228 allocs/op
```

These benchmarks measure repeated validated Markdown staging to one
workspace-bounded path, including the existing-file atomic replacement path,
filesystem sync and a larger artifact payload. They are not crash-recovery or
concurrent-writer benchmarks.

## Boundary and next measurements

These numbers establish a reproducible local reference only. Wave 4 still
needs cold external CLI startup, retained/peak memory, long-log throughput,
parallel different-actor isolation, cancellation response, event append and
snapshot replacement measurements before performance can be considered an
exit criterion.
