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
  1 alloc/op: the local `slabEntry` escapes to the heap (escape analysis:
  `moved to heap: slabEntry`) because its `atomic.Value` field's address is taken
  for `Store` — the `Store` itself does not allocate. The once path costs 3
  allocs/op: that same `slabEntry` escape, plus the value boxed into a non-pointer
  `atomicContainer` on `Store` and again into an empty `atomicContainer` on the
  `Swap` during `Get`.
- **`gbox` adds essentially nothing over `slab`** (53.4 vs 53.2 ns/op, etc.) —
  its cost *is* the slab's cost plus the `minLegalPointer` offset arithmetic.
- **`C.CString` does no Go-heap allocation** (0 allocs/op): the copy is into
  C `malloc` memory, invisible to `-benchmem`. The cost is a fixed ~60 ns floor
  (cgo transition + malloc/free) that every string-argument binding call pays
  even for short strings, rising with length (~87 ns at 256 chars).

## Notes on scope

- **`intern` is benchmarked only in its steady state.** `intern.Get` on a NEW
  object calls `g_object_add_toggle_ref`, and cleanup (`finalizeBox`) defers
  the un-ref to a GLib main-loop iteration via `g_main_context_invoke` —
  benchmarking that path without a running main loop would measure leaks, not
  cost. `BenchmarkInternTake` (in `core/glib`) therefore interns ONE object in
  setup and measures the repeated lookup (`coreglib.Take` on an
  already-interned pointer), which is the path every object-returning binding
  call takes: ~23 ns/op, 0 allocs — the intern layer is not a bottleneck.
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

## Perf decision: no registry sharding (yet)

The contention above is real but does **not** warrant sharding the slab today:

- The hot path is callback *invocation*, which is `gbox.Get` on a non-once entry
  — an `RLock` (shared). Concurrent signal emissions read in parallel and scale;
  they do not serialize.
- The exclusive `Lock` is taken only by `Put`/`Delete` (register / unregister).
  The parallel benchmark's slowdown comes from hammering those in a tight loop —
  i.e. heavy concurrent registration, which is not GTK's dominant pattern. GTK
  has main-loop thread affinity (see `pkg/core/glib`), so signal connect/disconnect
  and dispatch overwhelmingly happen on one thread.
- The single-threaded path is already lean: ~53 ns/op, 1 alloc.

Adding per-shard locking (or a lock-free free-list) would add real complexity to
relieve contention the dominant usage never hits. Deferred until a profile of a
real workload shows registration contention. Upgrade path: shard the free-list
by `id` and route `Put`/`Get`/`Delete` to `shards[id % N]`, each with its own
lock.

Other observed costs were likewise judged not worth optimizing now: the once-path
3-alloc boxing (the `async`-scope path, not the hot path) and the per-string
`C.CString` malloc (unavoidable for NUL-terminated args; an empty-string
fast-path via `gextras.ZeroString` would be a generator change with marginal
payoff).

## Signal dispatch and GValue marshaling (`core/glib`)

`core/glib/marshal_bench_test.go` covers the layer above the registries — the
code that runs on every signal emission and every GValue conversion. Emitter is
`gio.SimpleAction`'s "activate" (synchronous `g_signal_emit`, no main loop).

| Benchmark | ns/op | B/op | allocs/op |
|---|--:|--:|--:|
| `BenchmarkSignalEmitVoid` (`func()` fast path) | 641 | 104 | 4 |
| `BenchmarkSignalEmitVariant` (reflect path, 1 param) | 1587 | 144 | 8 |
| `BenchmarkObjectCast` (`Object.Cast`, registered type) | 714 | 152 | 7 |
| `BenchmarkInternTake` (steady-state re-intern) | 22.9 | 0 | 0 |
| `BenchmarkGoValue/int` | 121 | 0 | 0 |
| `BenchmarkGoValue/string` | 148 | 32 | 2 |
| `BenchmarkNewValue/int` | 498 | 40 | 3 |
| `BenchmarkNewValue/string` | 536 | 40 | 3 |

Observations and decisions:

- **The `func()` fast path works**: a void handler dispatches with no
  reflection; its ~641 ns / 4 allocs are the emission machinery itself
  (cgo signal emit, box + closure registry lookups), not Go-side marshaling.
- **The reflect path costs roughly +950 ns / +4 allocs per parameterized
  signal** (args slice, `reflect.ValueOf`/`Convert`, `Value.Call`, GValue
  read). Eliminating it would require generating typed marshalers per signal
  signature — a generator-wide change; deferred until a real workload profile
  shows signal dispatch itself as a bottleneck.
- **Object-typed parameters declared as `G_TYPE_OBJECT` marshal twice**
  (`goGValue` then `Object.Cast` in `_gotk4_goMarshal`), costing about one
  extra `BenchmarkObjectCast` (~714 ns). Signals with concretely-typed object
  parameters take one marshal only. Narrow case, medium-risk fix (the same
  switch handles `*Variant`); deferred with the same profile-first bar.
- **The intern layer is off the suspect list**: steady-state `Take` is
  ~23 ns / 0 allocs.
