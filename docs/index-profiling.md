# Index profiling

Use profiling for a representative, disposable index run:

```sh
ce --data-dir /tmp/ce-profile-data index /path/to/project --full \
  --profile-dir /tmp/ce-profile
```

The profile directory contains no source or graph facts:

- `summary.json`: per-phase totals/min/max, counters, final index statistics, and result;
- `cpu.pprof`, `heap.pprof`, `mutex.pprof`, `block.pprof`: standard Go profiles.

Inspect them with the same CE binary used for the run:

```sh
go tool pprof -top /path/to/ce /tmp/ce-profile/cpu.pprof
go tool pprof -top -cum /path/to/ce /tmp/ce-profile/cpu.pprof
go tool pprof -top /path/to/ce /tmp/ce-profile/heap.pprof
```

`--profile-trace` additionally writes `trace.out`. Execution traces can grow
quickly on corpus-scale runs, so use that option only for short, focused cases.

The phase summary separates walking, admission/backpressure, preparation
(read/hash/plan/parse), per-plugin extraction, staging SQLite batches, graph
flush, and final reconciliation. Use it alongside CPU profiles: phase totals
are cumulative across concurrent workers, while CPU samples identify the
functions consuming processor time.
