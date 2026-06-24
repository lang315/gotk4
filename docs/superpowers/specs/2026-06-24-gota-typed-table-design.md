# gota typed Table over GtkColumnView — design

Date: 2026-06-24
Status: approved

## Goal

Add a fluent, generic `TableWidget[T]` to `pkg/gota`, mirroring the table DX of
the original gota framework (built for gotk3) but adapted to GTK4. v1 supports
read-only typed rows, multi-selection, and header-click sort.

## Why it is small (vs gota's gotk3 table)

gota's table is ~600 lines + 6 helper files because gotk3's `GtkTreeView` +
`GtkListStore` load all rows into memory with no recycling, so gota hand-rolled
a virtual `GtkTreeModel` (`GotaListModel` + `SetNRows`), a `TreeModelFilter` →
`TreeModelSort` chain rebuilt on every bulk change, per-column GType/getter
parallel slices, and careful finalizer/unref handling to avoid GC double-free.

GTK4 replaced that stack. `GtkColumnView` is natively virtualized (only visible
rows get widgets; widgets are recycled), `GtkSortListModel`/`GtkFilterListModel`
are composable, and `core/gioutil.ListModel[T]` boxes arbitrary Go values into
GObjects via `gbox` with correct ownership. So the wrapper is thin.

## Architecture

Model pipeline (each stage a GListModel):

```
gioutil.ListModel[T]  →  gtk.SortListModel  →  gtk.MultiSelection  →  gtk.ColumnView
   (boxed Go values)      (header sorting)       (multi-select)         (virtualized view)
```

Construction order resolves the sorter chicken-and-egg:
1. `model := gioutil.NewListModel[T]()`
2. `sortModel := gtk.NewSortListModel(model, nil)`
3. `sel := gtk.NewMultiSelection(sortModel)`
4. `cv := gtk.NewColumnView(sel)`
5. `sortModel.SetSorter(cv.Sorter())` — now header clicks drive the sort.

## Public API

```go
type TableWidget[T any] struct {
    base[TableWidget[T]] // inherits Class/Tooltip/Size/HExpand/...
    obj   *gtk.ColumnView
    model *gioutil.ListModel[T]
    sort  *gtk.SortListModel
    sel   *gtk.MultiSelection
}

func NewTable[T any]() *TableWidget[T]
func (t *TableWidget[T]) Column(title string, cell func(T) string) *TableWidget[T] // sortable by cell text
func (t *TableWidget[T]) Items(items []T) *TableWidget[T]   // replace all via Splice(0, len, items...)
func (t *TableWidget[T]) Append(items ...T) *TableWidget[T]
func (t *TableWidget[T]) Selected() []T                     // selected rows, view order
func (t *TableWidget[T]) OnActivate(fn func(row T)) *TableWidget[T]       // double-click / Enter
func (t *TableWidget[T]) OnSelectionChanged(fn func(rows []T)) *TableWidget[T]
func (t *TableWidget[T]) Separators(v bool) *TableWidget[T]
```

## Per-column wiring

Each `Column(title, cell)` builds:
- a `SignalListItemFactory`: `setup` casts the arg `*coreglib.Object` →
  `*gtk.ListItem` and sets a `gtk.Label` child; `bind` reads the row value with
  `gioutil.ObjectValue[T](li.Item())` and sets the label text to `cell(value)`.
- a `gtk.CustomSorter` whose `CompareDataFunc(a, b unsafe.Pointer) int` wraps
  each borrowed pointer with `coreglib.Take(ptr)` (safe borrow + ref; finalizer
  unrefs), extracts T via `gioutil.ObjectValue[T]`, and returns
  `strings.Compare(cell(va), cell(vb))`.
- a `gtk.ColumnViewColumn` (expand + resizable) with that factory and sorter,
  appended via `cv.AppendColumn`.

`coreglib.Take` (not `AssumeOwnership`) is correct: the compare callback's
pointers are borrowed, so we add a temporary ref rather than steal ownership.

## Selection

`sel.Selection()` returns a `*gtk.Bitset`. Iterate `i in [0, Size())`,
`pos := bitset.Nth(uint(i))`, `obj := sel.Item(pos)` (view order, promoted from
the embedded `gio.ListModel`), `gioutil.ObjectValue[T](obj)`.

`OnActivate` maps the activate `position uint` the same way.

## Data flow

`Items` replaces the whole backing list (`model.Splice(0, model.Len(), items...)`).
`Append` adds one or more. The view, sort, and selection layers react via GListModel
change signals automatically.

## Edge cases

- Empty model: 0 rows, valid.
- `Selected()` on no selection: empty slice.
- Sort default is lexicographic on the column's cell string; numeric/custom
  comparators are out of scope for v1.

## Out of scope (YAGNI — add later)

Filtering, custom per-column comparators, editable cells, lazy/DB-windowed data
(`SliceListModel` + data-loader callback), checkbox column, index column, bottom
total panel, stable-string-ID selection.

## Testing

GTK widget construction requires a display, so headless CI cannot build widgets.
Coverage:
- `go build ./gota/...` type-checks the generic factory/sorter wiring (the only
  place generics + closures could go wrong at compile time).
- A `gioutil.ListModel[T]` round-trip unit test (`Append` then `At`/`All`
  returns the same values) — no display needed — guards the boxing assumption
  the table relies on.
- Existing `TestParseMnemonic` stays.

## Files

- `pkg/gota/table.go` — new, the wrapper.
- `pkg/gota/table_test.go` — round-trip + compile coverage notes.
