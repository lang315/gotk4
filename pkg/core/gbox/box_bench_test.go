package gbox

import "testing"

// The boxed value is created once so the benchmarks measure the gbox registry
// machinery (the global slab plus the minLegalPointer offset arithmetic) rather
// than per-iteration interface{} conversion.
var benchValue interface{} = "gotk4"

// BenchmarkGboxAssignDelete measures the steady-state non-once path against the
// single global registry.
func BenchmarkGboxAssignDelete(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p := Assign(benchValue)
		Delete(p)
	}
}

// BenchmarkGboxAssignOnceGet measures the once path: Get reclaims the slot.
func BenchmarkGboxAssignOnceGet(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p := AssignOnce(benchValue)
		_ = Get(p)
	}
}

// BenchmarkGboxParallel hammers the single global registry from multiple
// goroutines to surface contention on its shared RWMutex.
func BenchmarkGboxParallel(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			p := Assign(benchValue)
			_ = Get(p)
			Delete(p)
		}
	})
}
