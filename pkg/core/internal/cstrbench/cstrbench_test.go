package cstrbench

import (
	"strings"
	"testing"
)

// BenchmarkCStringRoundtrip quantifies the per-string C.malloc + C.free cost of
// marshaling a Go string into C memory across representative lengths. The C
// malloc is not visible to Go's -benchmem allocs/op (which counts only Go-heap
// allocations); the dominant cost is the cgo transition plus the C allocator.
func BenchmarkCStringRoundtrip(b *testing.B) {
	cases := []struct {
		name string
		s    string
	}{
		{"empty", ""},
		{"short", "border-width"}, // 12 chars, a typical property name
		{"long", strings.Repeat("a", 256)},
	}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				Roundtrip(c.s)
			}
		})
	}
}
