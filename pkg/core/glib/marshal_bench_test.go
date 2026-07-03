package glib_test

// Baseline benchmarks for the signal-dispatch and GValue hot paths that every
// gotk4 application pays on every signal emission: _gotk4_goMarshal (closure
// lookup + reflect dispatch), Value.GoValue/NewValue (per signal parameter),
// Object.Cast (object-typed parameters), and intern lookup (coreglib.Take on
// every object return). These are baselines only; see pkg/core/BENCHMARKS.md.
//
// The emitter is gio.SimpleAction's "activate" signal, which g_signal_emit
// dispatches synchronously on the calling thread — no main loop is needed.
// This lives in package glib_test (external) so it can import pkg/gio/v2 and
// pkg/glib/v2 without an import cycle.

import (
	"testing"
	"unsafe"

	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	gio "github.com/diamondburned/gotk4/pkg/gio/v2"
	glibv2 "github.com/diamondburned/gotk4/pkg/glib/v2"
)

// BenchmarkSignalEmitVoid measures the goMarshal fast path: a func() handler
// dispatches without any reflection.
func BenchmarkSignalEmitVoid(b *testing.B) {
	action := gio.NewSimpleAction("bench-void", nil)
	action.Connect("activate", func() {})

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		action.Activate(nil)
	}
}

// BenchmarkSignalEmitVariant measures the goMarshal reflect path: the
// "activate" parameter is a *glib.Variant GValue that goes through goGValue,
// the reflect args slice, and fsValue.Call.
func BenchmarkSignalEmitVariant(b *testing.B) {
	action := gio.NewSimpleAction("bench-variant", glibv2.NewVariantType("s"))
	action.Connect("activate", func(p *glibv2.Variant) {})
	param := glibv2.NewVariantString("payload")

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		action.Activate(param)
	}
}

// BenchmarkObjectCast measures Object.Cast on an interned object with a
// registered concrete marshaler — the path taken for every object-typed
// signal parameter (goMarshal's *Object case) and every WalkCast assertion.
func BenchmarkObjectCast(b *testing.B) {
	action := gio.NewSimpleAction("bench-cast", nil)
	obj := coreglib.Take(unsafe.Pointer(coreglib.InternObject(action).Native()))

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if v := obj.Cast(); v == nil {
			b.Fatal("Cast returned nil")
		}
	}
}

// BenchmarkInternTake measures the steady-state intern lookup that every
// object-returning binding call performs (coreglib.Take on an
// already-interned pointer).
func BenchmarkInternTake(b *testing.B) {
	action := gio.NewSimpleAction("bench-take", nil)
	ptr := unsafe.Pointer(coreglib.InternObject(action).Native())

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if o := coreglib.Take(ptr); o == nil {
			b.Fatal("Take returned nil")
		}
	}
}

// BenchmarkGoValue measures reading a Go value out of a GValue, as goMarshal
// does once per signal parameter.
func BenchmarkGoValue(b *testing.B) {
	b.Run("int", func(b *testing.B) {
		v := coreglib.NewValue(42)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = v.GoValue()
		}
	})
	b.Run("string", func(b *testing.B) {
		v := coreglib.NewValue("benchmark")
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = v.GoValue()
		}
	})
}

// BenchmarkNewValue measures building a GValue from a Go value — the signal
// return-value and property-write path.
func BenchmarkNewValue(b *testing.B) {
	b.Run("int", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = coreglib.NewValue(42)
		}
	})
	b.Run("string", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = coreglib.NewValue("benchmark")
		}
	})
}
