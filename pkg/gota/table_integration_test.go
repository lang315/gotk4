package gota

import (
	"testing"

	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

type person struct {
	name string
	age  int
}

// gtkReady initializes GTK once and reports whether a display is available.
// Widget construction needs the GTK type system + a backend display, so the
// integration tests skip cleanly on headless machines (CI without X/Wayland,
// macOS without a window-server session).
func gtkReady(t *testing.T) {
	t.Helper()
	if !gtk.InitCheck() {
		t.Skip("no GTK display available; skipping widget integration test")
	}
}

// TestTableIntegration drives the real GTK pipeline
// (gioutil.ListModel -> SortListModel -> MultiSelection -> ColumnView): it
// boxes Go rows, propagates list-change signals through the sort and selection
// layers, and reads selected rows back out via the live selection-changed
// signal. No main loop runs, so cell-render (bind) text is not asserted — that
// needs a realized view.
func TestTableIntegration(t *testing.T) {
	gtkReady(t)

	tbl := NewTable[person]().
		Column("Name", func(p person) string { return p.name }).
		Column("Age", func(p person) string { return string(rune('0' + p.age)) })

	// Capture rows reported by the selection-changed signal.
	var lastSelection []person
	tbl.OnSelectionChanged(func(rows []person) { lastSelection = rows })

	rows := []person{{"alice", 1}, {"bob", 2}, {"carol", 3}}
	tbl.Items(rows)

	// The change signal must propagate all the way to the selection model.
	if got := int(tbl.sel.NItems()); got != len(rows) {
		t.Fatalf("after Items: sel.NItems = %d, want %d", got, len(rows))
	}

	// Append extends the live model.
	tbl.Append(person{"dave", 4})
	if got := int(tbl.sel.NItems()); got != 4 {
		t.Fatalf("after Append: sel.NItems = %d, want 4", got)
	}

	// Selecting one row round-trips the boxed value back out (view order ==
	// insertion order, no sort active).
	tbl.sel.SelectItem(1, true)
	sel := tbl.Selected()
	if len(sel) != 1 || sel[0] != rows[1] {
		t.Fatalf("Selected after SelectItem(1) = %+v, want [%+v]", sel, rows[1])
	}
	// The selection-changed handler saw the same row.
	if len(lastSelection) != 1 || lastSelection[0] != rows[1] {
		t.Fatalf("OnSelectionChanged got %+v, want [%+v]", lastSelection, rows[1])
	}

	// Select-all yields every row.
	tbl.sel.SelectAll()
	if got := len(tbl.Selected()); got != 4 {
		t.Fatalf("Selected after SelectAll = %d rows, want 4", got)
	}

	// Items replaces the whole backing list.
	tbl.Items([]person{{"eve", 5}})
	if got := int(tbl.sel.NItems()); got != 1 {
		t.Fatalf("after replace Items: sel.NItems = %d, want 1", got)
	}
}
