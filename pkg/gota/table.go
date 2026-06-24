package gota

import (
	"strings"
	"unsafe"

	"github.com/diamondburned/gotk4/pkg/core/gioutil"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
)

// TableWidget is a fluent, type-safe table over GtkColumnView. T is the row
// type; rows are boxed into GObjects by gioutil.ListModel, so any Go value
// works. The view is natively virtualized — only visible rows get widgets.
//
// Pipeline: gioutil.ListModel[T] -> SortListModel -> MultiSelection -> ColumnView.
type TableWidget[T any] struct {
	base[TableWidget[T]]
	obj    *gtk.ColumnView
	model  *gioutil.ListModel[T]
	sort   *gtk.SortListModel
	sel    *gtk.MultiSelection
	cols   []*gtk.ColumnViewColumn
	sorter *gtk.ColumnViewSorter // cv.Sorter(); used to detect an active sort
}

// NewTable creates an empty table. Add columns with Column, rows with Items.
func NewTable[T any]() *TableWidget[T] {
	model := gioutil.NewListModel[T]()
	sortModel := gtk.NewSortListModel(model, nil)
	sel := gtk.NewMultiSelection(sortModel)
	cv := gtk.NewColumnView(sel)
	// Drive sorting from the column headers the user clicks.
	sortModel.SetSorter(cv.Sorter())

	t := &TableWidget[T]{obj: cv, model: model, sort: sortModel, sel: sel}
	t.sorter, _ = cv.Sorter().Cast().(*gtk.ColumnViewSorter)
	t.init(t, &cv.Widget)
	return t
}

// sortActive reports whether a column sort is currently applied.
func (t *TableWidget[T]) sortActive() bool {
	return t.sorter != nil && t.sorter.NSortColumns() > 0
}

// Column appends a text column. cell maps a row to the string shown in that
// column; the column is sortable by that string (header click). For numeric or
// otherwise non-lexicographic ordering use ColumnCmp — sorting by the rendered
// string orders "10" before "2".
func (t *TableWidget[T]) Column(title string, cell func(T) string) *TableWidget[T] {
	return t.addColumn(title, cell, func(a, b T) int { return strings.Compare(cell(a), cell(b)) })
}

// ColumnCmp appends a column rendered by cell but sorted by cmp, a comparator
// over the row type (negative if a<b, 0 if equal, positive if a>b). Use it for
// numeric columns, e.g. cmp = func(a, b Row) int { return a.Age - b.Age }.
func (t *TableWidget[T]) ColumnCmp(title string, cell func(T) string, cmp func(a, b T) int) *TableWidget[T] {
	return t.addColumn(title, cell, cmp)
}

func (t *TableWidget[T]) addColumn(title string, cell func(T) string, cmp func(a, b T) int) *TableWidget[T] {
	factory := gtk.NewSignalListItemFactory()
	// ColumnView hands the factory a *gtk.ColumnViewCell (ListView/GridView use
	// *gtk.ListItem instead).
	factory.ConnectSetup(func(obj *coreglib.Object) {
		cv := obj.Cast().(*gtk.ColumnViewCell)
		cv.SetChild(gtk.NewLabel(""))
	})
	factory.ConnectBind(func(obj *coreglib.Object) {
		cv := obj.Cast().(*gtk.ColumnViewCell)
		label := cv.Child().(*gtk.Label)
		label.SetText(cell(gioutil.ObjectValue[T](cv.Item())))
	})

	// The compare callback receives borrowed item pointers, so wrap with Take
	// (ref + finalizer), not AssumeOwnership (which would steal a ref and free
	// too early).
	sorter := gtk.NewCustomSorter(func(a, b unsafe.Pointer) int {
		va := gioutil.ObjectValue[T](coreglib.Take(a))
		vb := gioutil.ObjectValue[T](coreglib.Take(b))
		return cmp(va, vb)
	})

	col := gtk.NewColumnViewColumn(title, &factory.ListItemFactory)
	col.SetExpand(true)
	col.SetResizable(true)
	col.SetSorter(&sorter.Sorter)
	t.obj.AppendColumn(col)
	t.cols = append(t.cols, col)
	return t
}

// SortByColumn activates sorting by the i-th column (as if its header was
// clicked). Out-of-range i is a no-op.
func (t *TableWidget[T]) SortByColumn(i int, ascending bool) *TableWidget[T] {
	if i < 0 || i >= len(t.cols) {
		return t
	}
	dir := gtk.SortDescending
	if ascending {
		dir = gtk.SortAscending
	}
	t.obj.SortByColumn(t.cols[i], dir)
	return t
}

// IncrementalSort spreads sorting across multiple frames instead of sorting in
// one blocking pass. Enable it when rows change while a sort is active on a
// large model: a blocking re-sort on every change freezes the UI, whereas an
// incremental sort keeps the main loop responsive (the order catches up over a
// few frames). See SortListModel.SetIncremental.
func (t *TableWidget[T]) IncrementalSort(v bool) *TableWidget[T] {
	t.sort.SetIncremental(v)
	return t
}

// Items replaces all rows.
func (t *TableWidget[T]) Items(items []T) *TableWidget[T] {
	t.model.Splice(0, t.model.Len(), items...)
	return t
}

// Append adds rows to the end.
func (t *TableWidget[T]) Append(items ...T) *TableWidget[T] {
	for _, it := range items {
		t.model.Append(it)
	}
	return t
}

// Set replaces the row at index i with v in place, firing a change so any
// realized cell for that row re-binds. It reuses the row's backing object (no
// remove+insert), so high-frequency updates do not churn GObjects. Out-of-range
// i is a no-op.
func (t *TableWidget[T]) Set(i int, v T) *TableWidget[T] {
	t.model.Set(i, v)
	return t
}

// Len returns the current row count.
func (t *TableWidget[T]) Len() int { return t.model.Len() }

// Batch applies many row updates and emits a SINGLE change notification
// covering the affected span, instead of one per row. Updating a realized
// ColumnView with one items-changed per row at high frequency floods the view
// faster than it can redraw, so the changes back up; coalescing keeps it flat.
// Call set(i, v) for each row inside apply; out-of-range indices are ignored.
func (t *TableWidget[T]) Batch(apply func(set func(i int, v T))) *TableWidget[T] {
	n := t.model.Len()

	// With an active sort, a single wide-span change forces the SortListModel to
	// re-sort the whole span on every batch, which stalls the UI. Per-row changes
	// instead let it do cheap targeted reinserts, so emit them individually.
	if t.sortActive() {
		apply(func(i int, v T) {
			if i >= 0 && i < n {
				t.model.Set(i, v)
			}
		})
		return t
	}

	// No sort: coalesce into one change spanning the touched range — far cheaper
	// for a realized view than one signal per row.
	lo, hi := -1, -1
	apply(func(i int, v T) {
		if i < 0 || i >= n {
			return
		}
		t.model.SetSilent(i, v)
		if lo == -1 || i < lo {
			lo = i
		}
		if i > hi {
			hi = i
		}
	})
	if lo != -1 {
		span := hi - lo + 1
		t.model.EmitChanged(lo, span, span)
	}
	return t
}

// Selected returns the currently selected rows in view (sorted) order.
func (t *TableWidget[T]) Selected() []T {
	bitset := t.sel.Selection()
	n := int(bitset.Size())
	rows := make([]T, 0, n)
	for i := 0; i < n; i++ {
		pos := bitset.Nth(uint(i))
		rows = append(rows, gioutil.ObjectValue[T](t.sel.Item(pos)))
	}
	return rows
}

// OnActivate connects row activation (double-click or Enter).
func (t *TableWidget[T]) OnActivate(fn func(row T)) *TableWidget[T] {
	t.obj.ConnectActivate(func(position uint) {
		fn(gioutil.ObjectValue[T](t.sel.Item(position)))
	})
	return t
}

// OnSelectionChanged fires with the selected rows whenever the selection changes.
func (t *TableWidget[T]) OnSelectionChanged(fn func(rows []T)) *TableWidget[T] {
	t.sel.ConnectSelectionChanged(func(position, nItems uint) {
		fn(t.Selected())
	})
	return t
}

// Separators toggles row and column separator lines.
func (t *TableWidget[T]) Separators(v bool) *TableWidget[T] {
	t.obj.SetShowColumnSeparators(v)
	t.obj.SetShowRowSeparators(v)
	return t
}
