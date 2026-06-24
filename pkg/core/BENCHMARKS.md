# pkg/core hot-path benchmarks

Baseline microbenchmarks for the `pkg/core` runtime machinery that sits on the
hot path of every GTK callback: the `slab` free-list registry, the `gbox`
Go-value box (which wraps a single global `slab.Slab`), and the per-call
`C.CString` marshal cost. These are **baseline numbers only** — no optimization
has been applied.

## How to run

```sh
# macOS (brew GTK): GTK must be on the pkg-config path before building pkg/ (cgo).
export PKG_CONFIG_PATH="$(brew --prefix)/lib/pkgconfig:$(brew --prefix)/share/pkgconfig:$PKG_CONFIG_PATH"

go -C pkg test ./core/slab/ ./core/gbox/ ./core/internal/cstrbench/ \
    -bench=. -benchmem -run='^$' -benchtime=200ms
```

## Environment

| | |
|---|---|
| OS / arch | macOS (Darwin), arm64 |
| CPU | Apple M1 Pro (GOMAXPROCS = 10) |
| Go | go1.26.4 darwin/arm64 |
| GTK | 4.22.4 (Homebrew) |
| GLib | 2.88.1 (Homebrew) |

## Results

Captured with `-benchtime=200ms`. Microbenchmark numbers jitter run-to-run
(parallel ones most); treat these as ballpark baselines.

| Benchmark | ns/op | B/op | allocs/op |
|---|--:|--:|--:|
| **slab** (pure Go) | | | |
| `BenchmarkSlabPutDelete` (non-once, steady state) | 53.2 | 32 | 1 |
| `BenchmarkSlabPutGetOnce` (once path) | 90.4 | 80 | 3 |
| `BenchmarkSlabParallel` (10 goroutines, shared slab) | 334.0 | 64 | 2 |
| **gbox** (cgo; wraps the global slab) | | | |
| `BenchmarkGboxAssignDelete` (non-once) | 53.4 | 32 | 1 |
| `BenchmarkGboxAssignOnceGet` (once path) | 91.2 | 80 | 3 |
| `BenchmarkGboxParallel` (10 goroutines, global registry) | 335.8 | 64 | 2 |
| **C string marshal** (`C.CString` + `C.free`) | | | |
| `BenchmarkCStringRoundtrip/empty` (`""`) | 60.7 | 0 | 0 |
| `BenchmarkCStringRoundtrip/short` (12 chars) | 60.2 | 0 | 0 |
| `BenchmarkCStringRoundtrip/long` (256 chars) | 87.4 | 0 | 0 |

## Observations

- **The single global `sync.RWMutex` is a contention bottleneck under
  concurrency.** Single-threaded `Put`+`Delete` is ~53 ns/op (~19M ops/s); the
  10-goroutine parallel loop reports ~334 ns/op, i.e. aggregate throughput
  *drops* to ~3M ops/s instead of scaling up — far from the ~5 ns/op ideal
  linear scaling. `Put` and `Delete` both take the exclusive write lock, so all
  goroutines serialize on it.
- **The once path allocates more than the non-once path.** Non-once `Put` costs
  1 alloc/op (the `atomic.Value` first-store of the entry); the once path costs
  3 allocs/op because the value is boxed into a non-pointer `atomicContainer`
  struct on store and again on the `Swap` during `Get`.
- **`gbox` adds essentially nothing over `slab`** (53.4 vs 53.2 ns/op, etc.) —
  its cost *is* the slab's cost plus the `minLegalPointer` offset arithmetic.
- **`C.CString` does no Go-heap allocation** (0 allocs/op): the copy is into
  C `malloc` memory, invisible to `-benchmem`. The cost is a fixed ~60 ns floor
  (cgo transition + malloc/free) that every string-argument binding call pays
  even for short strings, rising with length (~87 ns at 256 chars).

## Notes on scope

- **`intern` is intentionally not benchmarked.** `intern.Get` requires a real
  `GObject` and calls `g_object_add_toggle_ref`; cleanup (`finalizeBox`) defers
  the un-ref to a GLib main-loop iteration via `g_main_context_invoke`. Without
  a running main loop in the benchmark, every iteration would leak a toggle ref
  and grow the `shared.strong` map unbounded — the benchmark would measure map
  growth and leaks, not steady-state intern cost. Standing up GObjects and
  driving a main loop per iteration is too heavy/fragile for a baseline, so it
  is skipped.
- **The `C.CString` benchmark lives in `core/internal/cstrbench`, not in a
  `gbox` test file.** Go does not support `import "C"` inside `_test.go` files
  (golang.org/issue/4030), so the cgo call must sit in a regular source file.
  `cstrbench` is a benchmark-only package under `internal/`, imported by nothing
  in production, and needs only libc (no GTK/GLib pkg-config) — it changes no
  shipping code.

## Race detection

`slab` and `gbox` are exercised under the race detector in CI via their parallel
benchmarks:

```sh
go test -race -run='^$' -bench=Parallel -benchtime=10x ./core/slab/ ./core/gbox/
```

This drives concurrent `Put`/`Get`/`Delete` on a shared registry under the race
detector (and `checkptr`); it currently passes — the global `RWMutex` keeps the
free-list race-clean.

A blanket `go test -race ./core/...` is **not** usable: it aborts with a
`checkptr: pointer arithmetic result points to invalid allocation` fatal inside
the third-party `github.com/KarpelesLab/weak` dependency, reached through
`intern`'s toggle-ref notify path (`intern.gets` → `weak.Map.Get`). Weak-pointer
libraries reconstruct pointers from stored bits, which `checkptr` (enabled by
`-race`) flags even though it is sound under this module's `assume-no-moving-gc`.
Until that is resolved upstream, race coverage is scoped to the `weak`-free
primitives.
