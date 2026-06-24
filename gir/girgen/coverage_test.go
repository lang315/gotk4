package girgen_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/diamondburned/gotk4/gir"
	"github.com/diamondburned/gotk4/gir/girgen/strcases"
)

// TestGTK4Coverage asserts that every public, bindable type in the system
// Gtk/Gdk/Gsk-4.0 GIR has a corresponding generated Go type in the committed
// pkg/ tree, or is listed in intentionalSkips. It is a completeness guard:
// when GTK is bumped and introduces new API, this fails unless the new type is
// bound (or explicitly recorded as an intentional skip), so a feature can never
// be silently dropped.
//
// It needs the GTK4 .gir files installed and is skipped otherwise (the same
// environmental dependency as pkgconfig.TestGIRDirs).

// covTarget maps a GIR namespace to its .gir basename and the committed
// package directory (relative to this test's directory, gir/girgen).
type covTarget struct {
	namespace string
	girFile   string
	pkgDir    string
}

var covTargets = []covTarget{
	{"Gtk", "Gtk-4.0.gir", "../../pkg/gtk/v4"},
	{"Gdk", "Gdk-4.0.gir", "../../pkg/gdk/v4"},
	{"Gsk", "Gsk-4.0.gir", "../../pkg/gsk/v4"},
}

// renames mirrors the TypeRenamer entries in gendata.go: GIR types the
// generator emits under a different Go name. Keyed by "Namespace.Name".
var renames = map[string]string{
	"Gtk.Native":   "NativeSurface",      // TypeRenamer("Gtk-4.Native", ...)
	"Gtk.Editable": "EditableTextWidget", // TypeRenamer("Gtk-4.Editable", ...)
}

// intentionalSkips are public GIR types that are deliberately NOT bound, keyed
// by "Namespace.Name". Each must have a reason. Categories:
//   - FileFilter in gendata.go (Unix print dialogs, GPU renderers)
//   - closure-less callback: GIR declares no user_data/closure parameter, so a
//     Go closure id cannot be threaded through C safely (see core/gbox).
//   - represented by other bound API.
var intentionalSkips = map[string]string{
	// gendata.go FileFilter: gtkprintunixdialog/pagesetupunixdialog/printer/printjob
	"Gtk.PrintUnixDialog":      "FileFilter gtkprintunixdialog",
	"Gtk.PageSetupUnixDialog":  "FileFilter gtkpagesetupunixdialog",
	"Gtk.Printer":              "FileFilter gtkprinter",
	"Gtk.PrintJob":             "FileFilter gtkprintjob",
	"Gtk.PrinterFunc":          "FileFilter gtkprinter",
	"Gtk.PrintJobCompleteFunc": "FileFilter gtkprintjob",
	"Gtk.PrintCapabilities":    "AbsoluteFilter C.gtk_print_capabilities_get_type",
	// gobject ParamSpec subclass; the Expression machinery itself is bound.
	"Gtk.ParamSpecExpression": "represented by bound Expression types",
	// closure-less callbacks (no user_data param in GIR).
	"Gtk.WidgetActionActivateFunc": "closure-less callback",
	"Gtk.CustomAllocateFunc":       "closure-less callback",
	"Gtk.CustomMeasureFunc":        "closure-less callback",
	"Gtk.CustomRequestModeFunc":    "closure-less callback",
	"Gdk.ContentSerializeFunc":     "closure-less callback (use ContentSerializer)",
	"Gdk.ContentDeserializeFunc":   "closure-less callback (use ContentDeserializer)",
	"Gdk.CursorGetTextureCallback": "closure-less callback",
	// gendata.go FileFilter: gskglrenderer/gsknglrenderer/gskvulkanrenderer
	"Gsk.GLRenderer":                "FileFilter gskglrenderer",
	"Gsk.NglRenderer":               "FileFilter gsknglrenderer",
	"Gsk.VulkanRenderer":            "FileFilter gskvulkanrenderer",
	"Gsk.RenderReplayNodeFilter":    "closure-less callback",
	"Gsk.RenderReplayTextureFilter": "closure-less callback",
	"Gsk.RenderReplayFontFilter":    "closure-less callback",
}

func findGIR(t *testing.T, basename string) string {
	files, err := gir.FindGIRFiles("gtk4")
	if err != nil || len(files) == 0 {
		t.Skipf("GTK4 .gir files not available (%v); skipping coverage guard", err)
	}
	for _, f := range files {
		if filepath.Base(f) == basename {
			return f
		}
	}
	t.Skipf("%s not found in gir dir; skipping coverage guard", basename)
	return ""
}

func TestGTK4Coverage(t *testing.T) {
	var totalPublic, totalCovered int

	for _, tgt := range covTargets {
		girPath := findGIR(t, tgt.girFile)
		repo, err := gir.ParseRepository(girPath)
		if err != nil {
			t.Fatalf("parse %s: %v", girPath, err)
		}

		declared := declaredTypes(t, tgt.pkgDir)
		var missing []string
		public := 0

		for i := range repo.Namespaces {
			ns := &repo.Namespaces[i]
			if ns.Name != tgt.namespace {
				continue
			}

			check := func(name string) {
				public++
				goName := strcases.PascalToGo(name)
				if r, ok := renames[tgt.namespace+"."+name]; ok {
					goName = r
				}
				if declared[goName] {
					return
				}
				if _, ok := intentionalSkips[tgt.namespace+"."+name]; ok {
					return
				}
				missing = append(missing, name+" (Go: "+goName+")")
			}

			for _, c := range ns.Classes {
				if c.IsIntrospectable() {
					check(c.Name)
				}
			}
			for _, c := range ns.Interfaces {
				if c.IsIntrospectable() {
					check(c.Name)
				}
			}
			for _, c := range ns.Enums {
				if c.IsIntrospectable() {
					check(c.Name)
				}
			}
			for _, c := range ns.Bitfields {
				if c.IsIntrospectable() {
					check(c.Name)
				}
			}
			for _, c := range ns.Callbacks {
				if c.IsIntrospectable() {
					check(c.Name)
				}
			}
			for _, r := range ns.Records {
				// Skip private/opaque and gtype-struct records: these are
				// implementation details the generator intentionally omits.
				if !r.IsIntrospectable() || r.Disguised || r.GLibIsGTypeStructFor != "" {
					continue
				}
				if strings.HasSuffix(r.Name, "Private") || strings.HasSuffix(r.Name, "Class") || strings.HasSuffix(r.Name, "Iface") {
					continue
				}
				check(r.Name)
			}
		}

		covered := public - len(missing)
		totalPublic += public
		totalCovered += covered
		t.Logf("%s: %d/%d public types bound (%.1f%%), %d intentional skips",
			tgt.namespace, covered, public, pct(covered, public), countSkips(tgt.namespace))

		if len(missing) > 0 {
			t.Errorf("%s: %d public GTK4 type(s) neither bound nor in intentionalSkips:\n  %s",
				tgt.namespace, len(missing), strings.Join(missing, "\n  "))
		}
	}

	t.Logf("TOTAL: %d/%d bindable GTK4 types covered (%.2f%%)",
		totalCovered, totalPublic, pct(totalCovered, totalPublic))
}

var typeDeclRe = regexp.MustCompile(`(?m)^type (\w+) `)

// declaredTypes returns the set of top-level Go type names declared across all
// .go files in dir.
func declaredTypes(t *testing.T, dir string) map[string]bool {
	matches, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil || len(matches) == 0 {
		t.Fatalf("no Go sources in %s: %v", dir, err)
	}
	set := make(map[string]bool)
	for _, m := range matches {
		data, err := os.ReadFile(m)
		if err != nil {
			t.Fatalf("read %s: %v", m, err)
		}
		for _, sub := range typeDeclRe.FindAllStringSubmatch(string(data), -1) {
			set[sub[1]] = true
		}
	}
	return set
}

func pct(n, d int) float64 {
	if d == 0 {
		return 100
	}
	return 100 * float64(n) / float64(d)
}

func countSkips(ns string) int {
	n := 0
	for k := range intentionalSkips {
		if strings.HasPrefix(k, ns+".") {
			n++
		}
	}
	return n
}
