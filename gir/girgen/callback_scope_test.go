package girgen_test

import (
	"strings"
	"testing"
)

// This file pins the Go->C callback-scope marshaling in
// gir/girgen/types/typeconv/go-c.go. A Go callback cannot be handed to C as a
// pointer, so the generator registers it in the gbox callback registry and
// passes C an opaque uintptr id (see CLAUDE.md "Memory & cgo model"). The
// "scope" annotation decides the registry lifetime, which is the single
// observable difference asserted here.
//
// The callback has primitive-only params (gint) plus a gpointer user_data, so
// it resolves with no GObject/GLib dependency. callbackScopeFixture has one
// SCOPE placeholder substituted per-variant.
const callbackScopeFixture = `<?xml version="1.0"?>
<repository version="1.2"
            xmlns="http://www.gtk.org/introspection/core/1.0"
            xmlns:c="http://www.gtk.org/introspection/c/1.0"
            xmlns:glib="http://www.gtk.org/introspection/glib/1.0">
  <namespace name="Test" version="1.0" c:identifier-prefixes="Test" c:symbol-prefixes="test" shared-library="libtest.so">
    <callback name="ScanFunc" c:type="ScanFunc">
      <return-value transfer-ownership="none">
        <type name="none" c:type="void"/>
      </return-value>
      <parameters>
        <parameter name="value" transfer-ownership="none">
          <type name="gint" c:type="gint"/>
        </parameter>
        <parameter name="user_data" transfer-ownership="none" closure="1">
          <type name="gpointer" c:type="gpointer"/>
        </parameter>
      </parameters>
    </callback>
    <function name="each_call" c:identifier="test_each_call">
      <return-value transfer-ownership="none">
        <type name="none" c:type="void"/>
      </return-value>
      <parameters>
        <parameter name="scan" transfer-ownership="none" scope="SCOPE" closure="1">
          <type name="ScanFunc" c:type="ScanFunc"/>
        </parameter>
        <parameter name="user_data" transfer-ownership="none">
          <type name="gpointer" c:type="gpointer"/>
        </parameter>
      </parameters>
    </function>
  </namespace>
</repository>`

// TestCallbackScopeGoToC asserts the gbox lifetime chosen per callback scope.
//
// scope="call": C invokes the callback only during the call, so the generator
// registers it with gbox.Assign and defers gbox.Delete to drop it as soon as
// the call returns — it can never be invoked afterward.
//
// scope="async": C invokes the callback once, later, so the generator uses
// gbox.AssignOnce (which pops the entry when first invoked) and must NOT defer
// a Delete, which would free the closure before the deferred call happens.
func TestCallbackScopeGoToC(t *testing.T) {
	call := generateNamespace(t, strings.ReplaceAll(callbackScopeFixture, "SCOPE", "call"), "Test", "1.0")
	async := generateNamespace(t, strings.ReplaceAll(callbackScopeFixture, "SCOPE", "async"), "Test", "1.0")

	// Sanity: the callback type, the wrapper func, and gbox registration must be
	// emitted in both variants so the scope assertions below cannot pass on
	// empty output.
	for name, src := range map[string]string{"call": call, "async": async} {
		if !strings.Contains(src, "type ScanFunc func(value int)") {
			t.Fatalf("[%s] expected generated callback type ScanFunc; got:\n%s", name, src)
		}
		if !strings.Contains(src, "func EachCall(scan ScanFunc)") {
			t.Fatalf("[%s] expected generated func EachCall(scan ScanFunc); got:\n%s", name, src)
		}
		if !strings.Contains(src, "gbox.") {
			t.Fatalf("[%s] expected the closure to be registered in gbox; got:\n%s", name, src)
		}
	}

	// scope=call: persistent Assign + deferred Delete after the synchronous call.
	if !strings.Contains(call, "gbox.Assign(scan)") {
		t.Errorf("scope=call should register with gbox.Assign; got:\n%s", call)
	}
	if !strings.Contains(call, "defer gbox.Delete(uintptr(") {
		t.Errorf("scope=call should defer gbox.Delete after the call; got:\n%s", call)
	}

	// scope=async: AssignOnce, and crucially NO Delete (AssignOnce self-pops).
	if !strings.Contains(async, "gbox.AssignOnce(scan)") {
		t.Errorf("scope=async should register with gbox.AssignOnce; got:\n%s", async)
	}
	if strings.Contains(async, "gbox.Delete") {
		t.Errorf("scope=async must NOT delete the closure (AssignOnce self-pops); got:\n%s", async)
	}
}
