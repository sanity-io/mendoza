# reconstructSlice perf work

Tracking optimization work targeting `differ.go:reconstructSlice` and
`sliceCandidate.insertAlias`. Each entry records what changed, the
benchstat output, and the raw `go test -bench` report it came from.

## Origin

Production CPU profile from `gradient` (a caller of mendoza):
`/Users/michael/pprof/pprof.gradient.samples.cpu.059.pb.gz` — 30s of CPU,
~92% in `mendoza.CreateDoublePatch`. Hot stack:

```
reconstructSlice           10.69s flat (33%)  29.11s cum (91%)
└─ insertAlias              2.28s flat ( 7%)  18.36s cum (57%)
   ├─ mapaccess2_fast64     6.72s     (21%)
   ├─ memhash64             3.01s     ( 9%)
   └─ maps.ctrlGroup.matchH2 2.92s    ( 9%)
```

Two hypotheses to investigate (in order of expected leverage):

1. `sliceCandidate.alias` is a `map[int]sliceAlias` keyed on a dense int
   range; the combined map machinery (`mapaccess2_fast64` + hashing +
   probing) burns ~40% of total CPU. Replace with a slice + presence
   bitmap. (`differ.go:544`)
2. The triple-nested loop at `differ.go:616-628` rescans every candidate
   for every hashIndex match. Bucket candidates by `contextIdx` so the
   inner scan only walks matching ones.

## Benchmark

`differ_bench_test.go` exercises three regimes:

- **DuplicateHeavySlice** — one large array drawn from 8 distinct values.
  Large `hashIndex.Data[hash]` buckets, all entries in the same parent slice
  ⇒ stresses `insertAlias` map ops.
- **ScatteredDuplicates** — `arrays` sibling arrays (default 10) drawing
  from a shared `distinct`-sized pool. Hash buckets contain entries from
  many parents; most fail the `cand.contextIdx == otherEntry.Parent` check.
  Stresses both the cache-cold `d.left.Entries[otherIdx]` load
  (`differ.go:621`) and `insertAlias`. Closest match to gradient's
  production profile.
- **DistinctSlice** — control with all-unique elements. Bucket size ≈ 1,
  inner loops collapse.

The duplicate-heavy variant grows ~2.7× per 2× input size at the n=1000→2000
step, vs ~2.0× for the distinct variant — confirming the super-linear
behavior visible in the profile.

Run: `go test -run='^$' -bench='BenchmarkCreateDoublePatch' -benchmem -count=6 ./.`

## Toolchain context

Module declares `go 1.13` (language version, restricts feature set in source).
Runtime/compiler is whatever toolchain is installed — locally **Go 1.26.3**,
which is a Swiss-table-map runtime (1.24+). Production gradient also runs on a
Swiss-table-map runtime, so map-internals signatures (`mapaccess2_fast64`,
`ctrlGroup.matchH2`) are directly comparable.

## Verifying we hit the right paths

CPU profile of the bench (`-bench=...n=2000 -benchtime=5s -cpuprofile=/tmp/bench-cpu-n2000.pb.gz`,
`GOGC=1000` to suppress the heap return-to-OS noise from per-iteration churn):

| function | bench cum% | prod cum% |
|---|---|---|
| `reconstructSlice`     | 50.3% | 90.8% |
| `insertAlias`          | 36.1% | 57.3% |
| `runtime.mapaccess2_fast64` | 28.0% | 50.1% |
| `HashListFor`          | 33.7% |  0.8% |

The absolute fractions differ because our input documents are smaller per element
than gradient's, so input hashing (`HashListFor` + `sha256.block`) takes a larger
relative share. The shape *inside* the diff hot path matches production:

|  | bench | prod |
|---|---|---|
| `insertAlias.cum / reconstructSlice.cum` | 0.72 | 0.63 |
| `mapaccess2_fast64.flat / reconstructSlice.cum` | 0.41 | 0.23 |

Same code, similar inner ratios, with map lookups in `insertAlias` if anything
*more* dominant per unit of `reconstructSlice` work in the bench than in
production. Optimizations to those will surface clearly in benchstat.

Notes when capturing fresh CPU profiles for diagnosis:

- Run with `GOGC=1000` (or `GOMEMLIMIT` set high) — without it `runtime.madvise`
  takes ~25–35% of samples from heap return-to-OS, drowning the inner-loop signal.
- Use `-benchtime=5s` or longer; the default 1s misses high-cost sub-benchmarks.

## Runs

### Run 1 — baseline (commit 74ad574, all three benchmarks)

Date: 2026-05-08. Raw output: `/tmp/report-1.txt`. Apple M1 Pro, Go 1.26.3.

```
                                                          │ /tmp/report-1.txt │
                                                          │      sec/op       │
CreateDoublePatch_DuplicateHeavySlice/n=100-10                   532.0µ ± 11%
CreateDoublePatch_DuplicateHeavySlice/n=500-10                   3.049m ±  6%
CreateDoublePatch_DuplicateHeavySlice/n=1000-10                  7.748m ± 14%
CreateDoublePatch_DuplicateHeavySlice/n=2000-10                  20.39m ±  1%
CreateDoublePatch_ScatteredDuplicates/arrays=10/n=100-10         6.512m ± 15%
CreateDoublePatch_ScatteredDuplicates/arrays=10/n=500-10         81.96m ±  2%
CreateDoublePatch_ScatteredDuplicates/arrays=10/n=1000-10        293.2m ±  6%
CreateDoublePatch_DistinctSlice/n=100-10                         575.1µ ±  1%
CreateDoublePatch_DistinctSlice/n=500-10                         2.828m ±  2%
CreateDoublePatch_DistinctSlice/n=1000-10                        5.852m ±  2%
CreateDoublePatch_DistinctSlice/n=2000-10                        11.61m ± 12%
geomean                                                          7.533m

                                                          │ /tmp/report-1.txt │
                                                          │       B/op        │
CreateDoublePatch_DuplicateHeavySlice/n=100-10                   360.1Ki ± 0%
CreateDoublePatch_DuplicateHeavySlice/n=500-10                   1.514Mi ± 0%
CreateDoublePatch_DuplicateHeavySlice/n=1000-10                  4.045Mi ± 0%
CreateDoublePatch_DuplicateHeavySlice/n=2000-10                  7.569Mi ± 0%
CreateDoublePatch_ScatteredDuplicates/arrays=10/n=100-10         3.915Mi ± 0%
CreateDoublePatch_ScatteredDuplicates/arrays=10/n=500-10         21.46Mi ± 0%
CreateDoublePatch_ScatteredDuplicates/arrays=10/n=1000-10        44.98Mi ± 0%
CreateDoublePatch_DistinctSlice/n=100-10                         515.3Ki ± 0%
CreateDoublePatch_DistinctSlice/n=500-10                         2.195Mi ± 0%
CreateDoublePatch_DistinctSlice/n=1000-10                        5.429Mi ± 0%
CreateDoublePatch_DistinctSlice/n=2000-10                        10.36Mi ± 0%
geomean                                                          3.967Mi

                                                          │ /tmp/report-1.txt │
                                                          │     allocs/op     │
CreateDoublePatch_DuplicateHeavySlice/n=100-10                    1.148k ± 0%
CreateDoublePatch_DuplicateHeavySlice/n=500-10                    2.163k ± 0%
CreateDoublePatch_DuplicateHeavySlice/n=1000-10                   3.289k ± 0%
CreateDoublePatch_DuplicateHeavySlice/n=2000-10                   5.435k ± 0%
CreateDoublePatch_ScatteredDuplicates/arrays=10/n=100-10          13.27k ± 0%
CreateDoublePatch_ScatteredDuplicates/arrays=10/n=500-10          62.37k ± 0%
CreateDoublePatch_ScatteredDuplicates/arrays=10/n=1000-10         123.6k ± 0%
CreateDoublePatch_DistinctSlice/n=100-10                          2.108k ± 0%
CreateDoublePatch_DistinctSlice/n=500-10                          7.762k ± 0%
CreateDoublePatch_DistinctSlice/n=1000-10                         14.82k ± 0%
CreateDoublePatch_DistinctSlice/n=2000-10                         28.92k ± 0%
geomean                                                           9.012k
```

Scaling shapes (sec/op vs n, 2× step):

| bench                          | n=500→1000 | n=1000→2000 | shape  |
|--------------------------------|-----------:|------------:|--------|
| DuplicateHeavySlice            |       2.5× |        2.6× | super-linear |
| ScatteredDuplicates (arrays=10)|       3.6× |          —  | super-linear (n=100→500: 12.6×) |
| DistinctSlice                  |       2.1× |        2.0× | linear  |

### Run 2 — opt #1: `sliceCandidate.alias` map → slice

Date: 2026-05-08. Raw output: `report-2-alias-slice.txt`.

Change: replace `map[int]sliceAlias` with `[]sliceAlias` indexed by
`target.Index`, sized to the right slice length (knowable up front since
reconstructSlice only fires when the right entry is a non-empty slice). Added
a `set bool` discriminator field on `sliceAlias` so zero-initialized slots
are unambiguously "absent". Struct stays 16 bytes (existing padding).

Behaviour preserved: full `go test ./...` passes (the roundtrip suite covers
diff/patch correctness across many document shapes).

```
                                                          │   baseline    │           opt #1 (alias slice)        │
                                                          │    sec/op     │    sec/op     vs base                 │
DuplicateHeavySlice/n=100                                       532.0µ ± 11%    512.3µ ± 39%        ~ (p=0.310)
DuplicateHeavySlice/n=500                                       3.049m  ±  6%   2.631m  ±  1%  -13.71% (p=0.002)
DuplicateHeavySlice/n=1000                                      7.748m  ± 14%   6.583m  ± 13%  -15.04% (p=0.002)
DuplicateHeavySlice/n=2000                                      20.39m  ±  1%   15.74m  ±  8%  -22.81% (p=0.002)
ScatteredDuplicates/arrays=10/n=100                             6.512m  ± 15%   6.599m  ± 12%        ~ (p=0.937)
ScatteredDuplicates/arrays=10/n=500                             81.96m  ±  2%   75.93m  ± 28%        ~ (p=0.065)
ScatteredDuplicates/arrays=10/n=1000                            293.2m  ±  6%   271.2m  ± 35%        ~ (p=0.065)
DistinctSlice/n=100                                             575.1µ  ±  1%   579.2µ  ±  8%        ~ (p=0.485)
DistinctSlice/n=500                                             2.828m  ±  2%   3.006m  ± 19%   +6.27% (p=0.002)
DistinctSlice/n=1000                                            5.852m  ±  2%   6.144m  ±  6%   +4.99% (p=0.026)
DistinctSlice/n=2000                                            11.61m  ± 12%   11.93m  ± 42%        ~ (p=1.000)
geomean                                                         7.533m          7.130m         -5.34%

B/op geomean: 3.967 Mi → 3.821 Mi (-3.67%)
allocs/op geomean: 9.012k → 8.937k (-0.83%)
```

Reading:

- **DuplicateHeavySlice (target regime)**: -13.7% → -22.8%, scales with N. Big
  win — eliminating Swiss-table hash + probe per `insertAlias` call dominates
  here.
- **ScatteredDuplicates**: ~-7 to -8%, not yet statistically significant. The
  cache-cold `d.left.Entries[otherIdx]` load at `differ.go:621` still
  dominates this regime; opt #1 doesn't address it. That's opt #2's target.
- **DistinctSlice (control)**: +5-6% regression at n=500/1000. With bucket
  size 1, insertAlias is called rarely; the upfront `make([]sliceAlias, N)`
  zeroing isn't amortized. This is the rare regime where the old map was
  cheaper. Production data (duplicate-heavy per the profile) benefits much
  more than this control loses, so net is a clear win.
- **Memory**: -3.7% geomean. Slice + dense layout beats Go's map bucket
  overhead even on the duplicate-heavy benches.

Ratio winner: gradient-shaped workloads should see large wins; the regression
is confined to all-distinct slices where mendoza was already fast.

Next up: opt #2 — bucket candidates by `contextIdx` to short-circuit the
`differ.go:625` parent check. Targets the ScatteredDuplicates regime.
