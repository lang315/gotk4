package girgen_test

import (
	"log"
	"os"
	"strings"
	"testing"

	"github.com/diamondburned/gotk4/gir"
	"github.com/diamondburned/gotk4/gir/girgen"
)

// This file pins the Go->C marshaling of values built INTO a C container
// (GHashTable) inside a callback/vfunc export, in
// gir/girgen/types/typeconv/go-c.go.
//
// The generated builder creates the table with
// g_hash_table_new_full(..., C.free, <value destroy>), so the CONTAINER owns
// every inserted key and value: they are freed exactly once, by the table's
// destroy functions. The inner conversions must therefore never emit their own
// "defer C.free(...)" — doing so double-frees the keys/values as soon as the
// export returns, while the C caller still holds the table.
//
// The regression this pins: for an OUT-PARAMETER container in a callback
// (conv.Callback == true), ShouldFree() used to resolve true for the inner
// values, emitting per-element defer frees (and skipping g_object_ref for
// object values). The same container built as a plain in-parameter or as a
// callback RETURN value was already correct — the out-param path was the odd
// one out. Manifested in pkg/gio/v2 gio_export.go MenuModel get_item_links /
// get_item_attributes.
//
// GLib.HashTable is not a builtin in types/resolve.go — it resolves through
// the GLib namespace like any record — so the fixtures carry a minimal GLib
// repository alongside the Test one. Keys and values are utf8 (Go string),
// which needs no further resolution, mirroring transfer_ownership_test.go.
const miniGLibFixture = `<?xml version="1.0"?>
<repository version="1.2"
            xmlns="http://www.gtk.org/introspection/core/1.0"
            xmlns:c="http://www.gtk.org/introspection/c/1.0"
            xmlns:glib="http://www.gtk.org/introspection/glib/1.0">
  <namespace name="GLib"
             version="2.0"
             c:identifier-prefixes="G"
             c:symbol-prefixes="g,glib"
             shared-library="libglib-2.0.so.0">
    <record name="HashTable"
            c:type="GHashTable"
            glib:type-name="GHashTable"
            glib:get-type="g_hash_table_get_type"
            c:symbol-prefix="hash_table">
    </record>
  </namespace>
</repository>`

const callbackOutHashTableFixture = `<?xml version="1.0"?>
<repository version="1.2"
            xmlns="http://www.gtk.org/introspection/core/1.0"
            xmlns:c="http://www.gtk.org/introspection/c/1.0"
            xmlns:glib="http://www.gtk.org/introspection/glib/1.0">
  <include name="GLib" version="2.0"/>
  <namespace name="Test"
             version="1.0"
             c:identifier-prefixes="Test"
             c:symbol-prefixes="test"
             shared-library="libtest.so">
    <callback name="TableFunc" c:type="TestTableFunc">
      <return-value transfer-ownership="none">
        <type name="none" c:type="void"/>
      </return-value>
      <parameters>
        <parameter name="table"
                   direction="out"
                   caller-allocates="0"
                   transfer-ownership="full">
          <type name="GLib.HashTable" c:type="GHashTable**">
            <type name="utf8" c:type="gchar*"/>
            <type name="utf8" c:type="gchar*"/>
          </type>
        </parameter>
        <parameter name="user_data"
                   transfer-ownership="none"
                   closure="1">
          <type name="gpointer" c:type="gpointer"/>
        </parameter>
      </parameters>
    </callback>
  </namespace>
</repository>`

// generateNamespaceDeps is generateNamespace with extra dependency
// repositories that types in the target namespace may resolve against.
func generateNamespaceDeps(t *testing.T, girXML, namespace, version string, depXMLs ...string) string {
	t.Helper()

	var repos gir.Repositories
	for _, depXML := range depXMLs {
		dep, err := gir.ParseRepositoryFromReader(strings.NewReader(depXML))
		if err != nil {
			t.Fatalf("parse dependency fixture .gir: %v", err)
		}
		ns := dep.Namespaces[0]
		repos = append(repos, gir.PkgRepository{
			Repository: *dep,
			Pkg:        "test",
			Path:       gir.VersionedName(ns.Name, ns.Version) + ".gir",
		})
	}

	repo, err := gir.ParseRepositoryFromReader(strings.NewReader(girXML))
	if err != nil {
		t.Fatalf("parse fixture .gir: %v", err)
	}
	repos = append(repos, gir.PkgRepository{
		Repository: *repo,
		Pkg:        "test",
		Path:       gir.VersionedName(namespace, version) + ".gir",
	})

	g := girgen.NewGenerator(repos, func(n *gir.Namespace) string {
		return "example.com/test/" + n.Name
	})
	if os.Getenv("GIR_VERBOSE") == "1" {
		// Mirror gir-generate: surface skip/debug reasons when diagnosing a
		// fixture that silently generates nothing.
		g.Logger = log.New(os.Stderr, "", 0)
	}

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

// TestCallbackOutParamHashTableOwnership asserts that a hash table written to
// a callback out-parameter hands full ownership of its keys and values to the
// table's destroy functions: the builder loop must not free them itself.
func TestCallbackOutParamHashTableOwnership(t *testing.T) {
	src := generateNamespaceDeps(t, callbackOutHashTableFixture, "Test", "1.0", miniGLibFixture)

	// Sanity: the callback and the table builder must actually be emitted, so
	// the free assertion below can't pass vacuously.
	if !strings.Contains(src, "TableFunc") {
		t.Fatalf("expected generated callback TableFunc; got:\n%s", src)
	}
	if !strings.Contains(src, "C.g_hash_table_new_full") {
		t.Fatalf("expected a g_hash_table_new_full builder; got:\n%s", src)
	}

	// The table is created with destroy functions (C.free for both utf8 keys
	// and utf8 values), so the table owns every inserted pointer. A defer
	// C.free on kdst/vdst would free the same pointer a second time when the
	// C caller destroys the table.
	if strings.Contains(src, "defer C.free") {
		t.Errorf("container-owned keys/values must not be freed by the builder "+
			"(the table's destroy funcs own them), but a defer C.free was emitted:\n%s", src)
	}
}
