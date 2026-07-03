// Package interncheck is a test-only helper for verifying the reference-count
// accounting of pkg/core/intern against real GObjects.
//
// It lives in its own package because Go does not support import "C" inside
// _test.go files (golang.org/issue/4030), so the cgo helpers must sit in a
// regular source file. This package is under internal/ and is imported by
// nothing in production.
package interncheck

// #cgo pkg-config: gobject-2.0
// #include <glib-object.h>
import "C"

import "unsafe"

// NewObject creates a bare GObject carrying a single strong reference.
// g_object_new is variadic (not callable from cgo), so this uses
// g_object_new_with_properties with zero properties.
func NewObject() unsafe.Pointer {
	return unsafe.Pointer(C.g_object_new_with_properties(C.G_TYPE_OBJECT, 0, nil, nil))
}

// RefCount reads the object's current reference count.
func RefCount(ptr unsafe.Pointer) int {
	return int((*C.GObject)(ptr).ref_count)
}

// Ref adds one strong reference, simulating the reference a transfer-full C
// function hands to its caller.
func Ref(ptr unsafe.Pointer) {
	C.g_object_ref(C.gpointer(ptr))
}
