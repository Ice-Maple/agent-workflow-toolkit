# Wave 4 benchmark baseline

- Date: 2026-08-10
- Host: Windows `amd64`, Intel Core(TM) i7-7700T CPU @ 2.90GHz
- Scope: initial JSONL parsing and artifact atomic-replace measurements
- Status: initial baseline only; full cold-start, memory, concurrency and
  cancellation metrics remain open
- Command form: `go test -run '^$' -bench <benchmark> -benchmem -count=3`

## Results

### Shared JSONL parser

`./internal/adapters/cli`:

```text
BenchmarkParseJSONLReplay-8        9047-9968 ns/op   15.45-17.02 MB/s   2920 B/op   53 allocs/op
BenchmarkStreamSummaryAddLine-8    7385-8568 ns/op   17.97-20.85 MB/s   2824 B/op   52 allocs/op
```

The benchmark uses the Cursor success replay fixture and asserts terminal plus
final delivery on every iteration. The range reflects three independent
benchmark runs, not a performance threshold.

### Artifact staging

`./internal/adapters/artifact`:

```text
BenchmarkStageAtomicReplace-8    4364208-4733282 ns/op   15995-16118 B/op   221 allocs/op
```

This measures repeated validated Markdown staging to one workspace-bounded
path, including the existing-file atomic replacement path and filesystem
sync. It is not a crash-recovery or concurrent-writer benchmark.

## Boundary and next measurements

These numbers establish a reproducible local reference only. Wave 4 still
needs cold external CLI startup, retained/peak memory, long-log throughput,
parallel different-actor isolation, cancellation response, event append and
snapshot replacement measurements before performance can be considered an
exit criterion.
