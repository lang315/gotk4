package girgen_test

import (
	"strings"
	"testing"
)

// This file drives the real generator on in-memory .gir fixtures to pin the
// C->Go array marshaling paths in gir/girgen/types/typeconv/c-go.go. It reuses
// generateNamespace from transfer_ownership_test.go (no pkg-config, no system
// GIR). Every array element type is a primitive ("gint" -> Go int via the
// static girToBuiltin map in types/types.go), so the fixtures pull in no
// GObject/GLib type resolution.
//
// c-go.go forks on the array shape:
//   - array.FixedSize > 0  -> reinterpret a [N] array (no allocation/length)
//   - array.Length != nil  -> view via unsafe.Slice and copy into a Go slice
// These tests pin both forks and the boundary between them.

// fixedSizeReturnFixture returns a fixed-size array of a primitive. In C a
// "returned array" is a pointer (`gint*`) annotated fixed-size="4"; the
// generator must reinterpret that pointer as a pointer to a [4] array.
const fixedSizeReturnFixture = `<?xml version="1.0"?>
<repository version="1.2"
            xmlns="http://www.gtk.org/introspection/core/1.0"
            xmlns:c="http://www.gtk.org/introspection/c/1.0"
            xmlns:glib="http://www.gtk.org/introspection/glib/1.0">
  <namespace name="Test" version="1.0" c:identifier-prefixes="Test" c:symbol-prefixes="test" shared-library="libtest.so">
    <function name="get_quad" c:identifier="test_get_quad">
      <return-value transfer-ownership="none">
        <array fixed-size="4" c:type="gint*">
          <type name="gint" c:type="gint"/>
        </array>
      </return-value>
    </function>
  </namespace>
</repository>`

// TestFixedSizeArrayReturnCToGo pins the fixed-size-array C->Go path where the
// C value is a pointer (a returned `T*`). c-go.go reinterprets the pointer as a
// pointer to the array — `src := (*[4]C.gint)(unsafe.Pointer(_cret))` — then
// copies the 4 elements through a compile-time-bounded loop. This is the
// "pointer" branch of the GTK-4.22 fixed-size-array fix (fixedArrayIsPtr ==
// true); TestFixedSizeArrayOutParamCToGo covers the inline branch.
func TestFixedSizeArrayReturnCToGo(t *testing.T) {
	src := generateNamespace(t, fixedSizeReturnFixture, "Test", "1.0")

	// Sanity: the function and its fixed-size array return type must be emitted
	// so the cast/loop assertions below cannot pass vacuously on empty output.
	if !strings.Contains(src, "func GetQuad() [4]int") {
		t.Fatalf("expected generated func GetQuad returning [4]int; got:\n%s", src)
	}

	// The returned pointer is reinterpreted as a pointer to the [4] array.
	if !strings.Contains(src, "(*[4]C.gint)(unsafe.Pointer(") {
		t.Errorf("expected fixed-size array pointer cast (*[4]...)(unsafe.Pointer(...)); got:\n%s", src)
	}
	// All 4 elements are mapped through a compile-time-bounded loop.
	if !strings.Contains(src, "for i := 0; i < 4; i++") {
		t.Errorf("expected a 4-element copy loop; got:\n%s", src)
	}
	// Boundary: a fixed-size array is reinterpreted in place, never viewed via
	// unsafe.Slice (that is the length-array path, TestLengthArrayCToGo).
	if strings.Contains(src, "unsafe.Slice") {
		t.Errorf("fixed-size array must not use unsafe.Slice; got:\n%s", src)
	}
}

// fixedSizeOutParamFixture takes the same fixed-size array as a
// caller-allocated out parameter. Here the C side writes into a Go-stack array
// (`gint[4]`), so the value is an inline array, not a pointer.
const fixedSizeOutParamFixture = `<?xml version="1.0"?>
<repository version="1.2"
            xmlns="http://www.gtk.org/introspection/core/1.0"
            xmlns:c="http://www.gtk.org/introspection/c/1.0"
            xmlns:glib="http://www.gtk.org/introspection/glib/1.0">
  <namespace name="Test" version="1.0" c:identifier-prefixes="Test" c:symbol-prefixes="test" shared-library="libtest.so">
    <function name="fill_quad" c:identifier="test_fill_quad">
      <return-value transfer-ownership="none">
        <type name="none" c:type="void"/>
      </return-value>
      <parameters>
        <parameter name="quad" direction="out" caller-allocates="1" transfer-ownership="none">
          <array fixed-size="4" c:type="gint*">
            <type name="gint" c:type="gint"/>
          </array>
        </parameter>
      </parameters>
    </function>
  </namespace>
</repository>`

// TestFixedSizeArrayOutParamCToGo pins the inline branch of the same fixed-size
// fix (fixedArrayIsPtr == false). The out array is caller-allocated as a Go
// `[4]C.gint`, so the generator takes its address (`src := &_arg1`) instead of
// casting a pointer to an array — taking the address of a pointer would add a
// level and fail to compile. Together with the return test above, this asserts
// the generator distinguishes the pointer vs inline-array representations.
func TestFixedSizeArrayOutParamCToGo(t *testing.T) {
	src := generateNamespace(t, fixedSizeOutParamFixture, "Test", "1.0")

	// Sanity: the function and its [4]int result must be emitted.
	if !strings.Contains(src, "func FillQuad() [4]int") {
		t.Fatalf("expected generated func FillQuad returning [4]int; got:\n%s", src)
	}

	// The out array is allocated inline as a fixed-size C array...
	if !strings.Contains(src, "var _arg1 [4]C.gint") {
		t.Errorf("expected caller-allocated inline [4]C.gint; got:\n%s", src)
	}
	// ...and its address is taken directly (inline branch), NOT reinterpreted
	// from a pointer.
	if !strings.Contains(src, "src := &_arg1") {
		t.Errorf("expected address-of inline array (src := &_arg1); got:\n%s", src)
	}
	if strings.Contains(src, "(*[4]C.gint)(unsafe.Pointer(") {
		t.Errorf("inline-array out param must not use the pointer-cast branch; got:\n%s", src)
	}
}

// lengthReturnFixture returns a primitive array whose length is reported in a
// companion out parameter (length="0" => parameter index 0). This is the
// non-fixed array shape.
const lengthReturnFixture = `<?xml version="1.0"?>
<repository version="1.2"
            xmlns="http://www.gtk.org/introspection/core/1.0"
            xmlns:c="http://www.gtk.org/introspection/c/1.0"
            xmlns:glib="http://www.gtk.org/introspection/glib/1.0">
  <namespace name="Test" version="1.0" c:identifier-prefixes="Test" c:symbol-prefixes="test" shared-library="libtest.so">
    <function name="get_values" c:identifier="test_get_values">
      <return-value transfer-ownership="container">
        <array length="0" zero-terminated="0" c:type="gint*">
          <type name="gint" c:type="gint"/>
        </array>
      </return-value>
      <parameters>
        <parameter name="n_values" direction="out" transfer-ownership="none">
          <type name="guint" c:type="guint*"/>
        </parameter>
      </parameters>
    </function>
  </namespace>
</repository>`

// TestLengthArrayCToGo pins the length-array C->Go path: a runtime-sized array
// has no compile-time bound, so c-go.go views the C buffer with
// unsafe.Slice((*C.gint)(_cret), _arg1) and copies it into a freshly-made Go
// slice. This is the contrast to the fixed-size path: a dynamic []int and
// unsafe.Slice, never a [N] reinterpretation.
func TestLengthArrayCToGo(t *testing.T) {
	src := generateNamespace(t, lengthReturnFixture, "Test", "1.0")

	// Sanity: the function returns a Go slice (not a fixed array), proving the
	// length path produced a runtime-sized result.
	if !strings.Contains(src, "func GetValues() []int") {
		t.Fatalf("expected generated func GetValues returning []int; got:\n%s", src)
	}

	// The C buffer is viewed with unsafe.Slice over the returned pointer.
	if !strings.Contains(src, "unsafe.Slice((*C.gint)(") {
		t.Errorf("expected unsafe.Slice over the C array pointer; got:\n%s", src)
	}
	// Boundary: this path is length-driven, so no fixed-size [4] array type or
	// bound appears anywhere.
	if strings.Contains(src, "[4]") {
		t.Errorf("length array must not emit a fixed-size [4] array; got:\n%s", src)
	}
}
