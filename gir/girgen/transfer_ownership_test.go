package girgen_test

import (
	"strings"
	"testing"

	"github.com/diamondburned/gotk4/gir"
	"github.com/diamondburned/gotk4/gir/girgen"
)

// This file drives the real generator on tiny, hand-written .gir fixtures held
// entirely in memory. It needs no system GIR files and no pkg-config: gir.
// Repositories is just []gir.PkgRepository, so it can be assembled directly
// from a parsed *gir.Repository.
//
// The fixtures exercise the hardest part of typeconv — the transfer-ownership
// rules that decide whether generated marshaling frees C memory. A function
// returning/taking a "utf8" (Go string) is ideal: "utf8" resolves to the Go
// "string" builtin via a static map in types/resolve.go, so it pulls in no
// GObject/GLib type resolution, yet transfer-full vs transfer-none flips a
// single, observable "C.free(...)" line in the emitted Go.

// generateNamespace parses the given .gir XML, assembles a Repositories from it
// (no pkg-config), runs the real namespace generator, and returns the
// concatenated source of every emitted Go file.
func generateNamespace(t *testing.T, girXML, namespace, version string) string {
	t.Helper()

	repo, err := gir.ParseRepositoryFromReader(strings.NewReader(girXML))
	if err != nil {
		t.Fatalf("parse fixture .gir: %v", err)
	}

	// Repositories is a plain []PkgRepository; build it directly instead of
	// going through Add/AddSelected, which shell out to pkg-config.
	repos := gir.Repositories{
		gir.PkgRepository{
			Repository: *repo,
			Pkg:        "test",
			Path:       gir.VersionedName(namespace, version) + ".gir",
		},
	}

	g := girgen.NewGenerator(repos, func(n *gir.Namespace) string {
		return "example.com/test/" + n.Name
	})

	nsgen := g.UseNamespace(namespace, version)
	if nsgen == nil {
		t.Fatalf("UseNamespace(%q, %q) returned nil", namespace, version)
	}

	files, err := nsgen.Generate()
	if err != nil {
		// Generate gofmt's each file; a non-nil error means emitted Go didn't
		// parse, which is itself a generator bug worth surfacing.
		t.Fatalf("generate namespace: %v", err)
	}

	var b strings.Builder
	for _, content := range files {
		b.Write(content)
		b.WriteByte('\n')
	}
	return b.String()
}

// returnStringFixture is a one-function namespace whose function returns a
// transfer-sensitive C string (utf8 -> Go string). %[1]s is the return value's
// transfer-ownership. The c:type is identical in both variants so the ONLY
// independent variable is the transfer-ownership attribute.
const returnStringFixture = `<?xml version="1.0"?>
<repository version="1.2"
            xmlns="http://www.gtk.org/introspection/core/1.0"
            xmlns:c="http://www.gtk.org/introspection/c/1.0"
            xmlns:glib="http://www.gtk.org/introspection/glib/1.0">
  <namespace name="Test"
             version="1.0"
             c:identifier-prefixes="Test"
             c:symbol-prefixes="test"
             shared-library="libtest.so">
    <function name="dup_string" c:identifier="test_dup_string">
      <return-value transfer-ownership="%[1]s">
        <type name="utf8" c:type="gchar*"/>
      </return-value>
    </function>
  </namespace>
</repository>`

// TestTransferOwnershipReturnString asserts the C->Go marshaling direction.
//
// On a returned string, Go receives the value: transfer-full means C handed
// ownership to Go, so the generated code must free the C string after copying
// it; transfer-none means C keeps ownership, so it must NOT be freed.
func TestTransferOwnershipReturnString(t *testing.T) {
	full := generateNamespace(t, format1(returnStringFixture, "full"), "Test", "1.0")
	none := generateNamespace(t, format1(returnStringFixture, "none"), "Test", "1.0")

	// Sanity: the function and its string conversion must actually be emitted,
	// so the free assertions below can't pass vacuously on empty output.
	for name, src := range map[string]string{"full": full, "none": none} {
		if !strings.Contains(src, "func DupString() string") {
			t.Fatalf("[%s] expected generated func DupString; got:\n%s", name, src)
		}
		if !strings.Contains(src, "C.GoString(") {
			t.Fatalf("[%s] expected a C.GoString conversion; got:\n%s", name, src)
		}
	}

	// transfer-full: ownership taken, so the C string is freed.
	if !strings.Contains(full, "defer C.free(unsafe.Pointer(_cret))") {
		t.Errorf("transfer-full return should free the C string, but no C.free found:\n%s", full)
	}

	// transfer-none: ownership retained by C, so nothing is freed.
	if strings.Contains(none, "C.free") {
		t.Errorf("transfer-none return must NOT free the C string, but C.free was emitted:\n%s", none)
	}
}

// paramStringFixture is a one-function namespace whose function takes a
// transfer-sensitive C string (utf8 -> Go string). %[1]s is the parameter's
// transfer-ownership.
const paramStringFixture = `<?xml version="1.0"?>
<repository version="1.2"
            xmlns="http://www.gtk.org/introspection/core/1.0"
            xmlns:c="http://www.gtk.org/introspection/c/1.0"
            xmlns:glib="http://www.gtk.org/introspection/glib/1.0">
  <namespace name="Test"
             version="1.0"
             c:identifier-prefixes="Test"
             c:symbol-prefixes="test"
             shared-library="libtest.so">
    <function name="take_string" c:identifier="test_take_string">
      <return-value transfer-ownership="none">
        <type name="none" c:type="void"/>
      </return-value>
      <parameters>
        <parameter name="s" transfer-ownership="%[1]s">
          <type name="utf8" c:type="gchar*"/>
        </parameter>
      </parameters>
    </function>
  </namespace>
</repository>`

// TestTransferOwnershipParamString asserts the Go->C marshaling direction,
// which is the inverse of the return case.
//
// On an input string, Go gives the value: transfer-full means Go handed the
// freshly-allocated C string to C, so Go must NOT free it; transfer-none means
// C only borrows it, so Go must free its copy after the call returns.
func TestTransferOwnershipParamString(t *testing.T) {
	full := generateNamespace(t, format1(paramStringFixture, "full"), "Test", "1.0")
	none := generateNamespace(t, format1(paramStringFixture, "none"), "Test", "1.0")

	for name, src := range map[string]string{"full": full, "none": none} {
		if !strings.Contains(src, "func TakeString(s string)") {
			t.Fatalf("[%s] expected generated func TakeString; got:\n%s", name, src)
		}
		if !strings.Contains(src, "C.CString(s)") {
			t.Fatalf("[%s] expected a C.CString conversion; got:\n%s", name, src)
		}
	}

	// transfer-full: ownership given to C, so Go must NOT free the C string.
	if strings.Contains(full, "C.free") {
		t.Errorf("transfer-full param gives ownership to C and must NOT free, but C.free was emitted:\n%s", full)
	}

	// transfer-none: C borrows the copy, so Go frees it after the call.
	if !strings.Contains(none, "defer C.free(unsafe.Pointer(_arg1))") {
		t.Errorf("transfer-none param should free the C copy, but no C.free found:\n%s", none)
	}
}

// format1 substitutes the single %[1]s verb in a fixture template. It is a thin
// wrapper to keep the intent ("only the transfer-ownership varies") obvious at
// the call sites.
func format1(tmpl, transfer string) string {
	return strings.ReplaceAll(tmpl, "%[1]s", transfer)
}
