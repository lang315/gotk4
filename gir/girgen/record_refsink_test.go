package girgen_test

import (
	"strings"
	"testing"
)

// recordRefSinkFixture declares a refcounted record that supports floating
// references (ref + ref_sink + unref, like GVariant) and a function returning
// it with transfer-ownership="none" — the shape of every g_variant_new_*
// constructor, whose fresh return is FLOATING.
const recordRefSinkFixture = `<?xml version="1.0"?>
<repository version="1.2"
            xmlns="http://www.gtk.org/introspection/core/1.0"
            xmlns:c="http://www.gtk.org/introspection/c/1.0"
            xmlns:glib="http://www.gtk.org/introspection/glib/1.0">
  <namespace name="Test"
             version="1.0"
             c:identifier-prefixes="Test"
             c:symbol-prefixes="test"
             shared-library="libtest.so">
    <record name="Box"
            c:type="TestBox"
            glib:type-name="TestBox"
            glib:get-type="test_box_get_type">
      <method name="ref" c:identifier="test_box_ref">
        <return-value transfer-ownership="none">
          <type name="Test.Box" c:type="TestBox*"/>
        </return-value>
        <parameters>
          <instance-parameter name="box" transfer-ownership="none">
            <type name="Test.Box" c:type="TestBox*"/>
          </instance-parameter>
        </parameters>
      </method>
      <method name="ref_sink" c:identifier="test_box_ref_sink">
        <return-value transfer-ownership="none">
          <type name="Test.Box" c:type="TestBox*"/>
        </return-value>
        <parameters>
          <instance-parameter name="box" transfer-ownership="none">
            <type name="Test.Box" c:type="TestBox*"/>
          </instance-parameter>
        </parameters>
      </method>
      <method name="unref" c:identifier="test_box_unref">
        <return-value transfer-ownership="none">
          <type name="none" c:type="void"/>
        </return-value>
        <parameters>
          <instance-parameter name="box" transfer-ownership="none">
            <type name="Test.Box" c:type="TestBox*"/>
          </instance-parameter>
        </parameters>
      </method>
    </record>
    <function name="new_box" c:identifier="test_new_box">
      <return-value transfer-ownership="none">
        <type name="Test.Box" c:type="TestBox*"/>
      </return-value>
    </function>
  </namespace>
</repository>`

// TestRecordTakeRefPrefersRefSink asserts that when the binding takes its own
// reference on a received refcounted record, it uses ref_sink when available:
// on a non-floating instance ref_sink is identical to ref, and on a floating
// one (a fresh GVariant) it claims the floating reference — a plain ref would
// leave the floating flag set, so a later C consumer's ref_sink would claim
// the same reference twice and unbalance the count.
func TestRecordTakeRefPrefersRefSink(t *testing.T) {
	src := generateNamespace(t, recordRefSinkFixture, "Test", "1.0")

	// Sanity: the constructor and the finalizer-based ownership must be
	// emitted, so the assertions below can't pass vacuously.
	if !strings.Contains(src, "func NewBox() *Box") {
		t.Fatalf("expected generated func NewBox; got:\n%s", src)
	}
	if !strings.Contains(src, "runtime.SetFinalizer") {
		t.Fatalf("expected the record wrap to attach a finalizer; got:\n%s", src)
	}

	if !strings.Contains(src, "C.test_box_ref_sink(") {
		t.Errorf("taking a reference on a floatable record must use ref_sink; got:\n%s", src)
	}
	if strings.Contains(src, "C.test_box_ref(") {
		t.Errorf("plain ref used instead of ref_sink on a floatable record:\n%s", src)
	}
}
