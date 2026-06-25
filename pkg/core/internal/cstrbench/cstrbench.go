// Package cstrbench is a benchmark-only helper that isolates the cost of
// marshaling a Go string into C memory and freeing it again (C.CString +
// C.free), which every string-argument binding call pays.
//
// It lives in its own package because Go does not support import "C" inside
// _test.go files (golang.org/issue/4030), so the cgo call must sit in a regular
// source file. This package is under internal/ and is imported by nothing in
// production; it only needs libc (no GTK/GLib pkg-config), so it builds
// anywhere cgo is enabled.
package cstrbench

// #include <stdlib.h>
import "C"

import "unsafe"

// Roundtrip copies s into a freshly malloc'd C string and immediately frees it.
func Roundtrip(s string) {
	cs := C.CString(s)
	C.free(unsafe.Pointer(cs))
}
