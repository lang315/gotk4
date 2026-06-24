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
	obj   *gtk.ColumnView
	model *gioutil.ListModel[T]
	sel   *gtk.MultiSelection
}

// NewTable creates an empty table. Add columns with Column, rows with Items.
func NewTable[T any]() *TableWidget[T] {
	model := gioutil.NewListModel[T]()
	sortModel := gtk.NewSortListModel(model, nil)
	sel := gtk.NewMultiSelection(sortModel)
	cv := gtk.NewColumnView(sel)
	// Drive sorting from the column headers the user clicks.
	sortModel.SetSorter(cv.Sorter())

	t := &TableWidget[T]{obj: cv, model: model, sel: sel}
	t.init(t, &cv.Widget)
	return t
}

// Column appends a text column. cell maps a row to the string shown in that
// column; the column is also sortable by that string (header click).
func (t *TableWidget[T]) Column(title string, cell func(T) string) *TableWidget[T] {
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

	// Sort by comparing the two rows' cell text. The compare callback receives
	// borrowed item pointers, so wrap with Take (ref + finalizer), not
	// AssumeOwnership (which would steal a ref and free too early).
	sorter := gtk.NewCustomSorter(func(a, b unsafe.Pointer) int {
		va := gioutil.ObjectValue[T](coreglib.Take(a))
		vb := gioutil.ObjectValue[T](coreglib.Take(b))
		return strings.Compare(cell(va), cell(vb))
	})

	col := gtk.NewColumnViewColumn(title, &factory.ListItemFactory)
	col.SetExpand(true)
	col.SetResizable(true)
	col.SetSorter(&sorter.Sorter)
	t.obj.AppendColumn(col)
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

// Set replaces the row at index i with v, firing a change so any realized cell
// for that row re-binds. Out-of-range i is a no-op.
func (t *TableWidget[T]) Set(i int, v T) *TableWidget[T] {
	if i < 0 || i >= t.model.Len() {
		return t
	}
	t.model.Splice(i, 1, v)
	return t
}

// Len returns the current row count.
func (t *TableWidget[T]) Len() int { return t.model.Len() }

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
