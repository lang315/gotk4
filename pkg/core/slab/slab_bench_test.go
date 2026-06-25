package slab

import "testing"

// The boxed value is created once outside the loops so the benchmarks measure
// the slab's own work (locking, free-list bookkeeping, atomic.Value churn)
// rather than the cost of converting a concrete value to interface{} on every
// iteration.
var benchValue interface{} = "gotk4"

// BenchmarkSlabPutDelete measures the steady-state non-once path: every
// iteration stores an entry and immediately frees it, so the free list is
// reused and the backing slice does not grow. This isolates the reuse cost from
// the one-time append cost.
func BenchmarkSlabPutDelete(b *testing.B) {
	var s Slab
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx := s.Put(benchValue, false)
		s.Delete(idx)
	}
}

// BenchmarkSlabPutGetOnce measures the once path. Get auto-deletes a once entry,
// so the slot is reclaimed each iteration and the slice stays at length one.
func BenchmarkSlabPutGetOnce(b *testing.B) {
	var s Slab
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx := s.Put(benchValue, true)
		_ = s.Get(idx)
	}
}

// BenchmarkSlabParallel shares one *Slab across goroutines to expose whether the
// single global sync.RWMutex is a contention bottleneck under concurrency. Each
// goroutine owns the index it Puts until it Deletes it, so there is no overlap
// on a slot. The Get result is discarded with _ rather than written to a shared
// sink to avoid a data race; the mutex side effects keep the call from being
// optimized away.
func BenchmarkSlabParallel(b *testing.B) {
	var s Slab
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			idx := s.Put(benchValue, false)
			_ = s.Get(idx)
			s.Delete(idx)
		}
	})
}
