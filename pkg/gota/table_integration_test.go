package gota

import (
	"math/rand"
	"strconv"
	"testing"

	"github.com/diamondburned/gotk4/pkg/core/gioutil"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

type person struct {
	name string
	age  int
}

// randomPeople builds n pseudo-random rows with a fixed seed (reproducible).
func randomPeople(n int) []person {
	r := rand.New(rand.NewSource(42))
	out := make([]person, n)
	for i := range out {
		out[i] = person{
			name: "user-" + strconv.Itoa(r.Intn(1_000_000)),
			age:  18 + r.Intn(63),
		}
	}
	return out
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

// TestTableLargeRandom loads a >5000-row random dataset and checks the model
// pipeline scales: all rows box into gbox, the count propagates through
// sort+selection, and individual + bulk selection round-trip the boxed values.
// (The view is never realized, so this exercises the data layer, not row
// widgets — virtualization at render time is for the manual demo.)
func TestTableLargeRandom(t *testing.T) {
	gtkReady(t)

	const n = 5321 // > 5000
	rows := randomPeople(n)

	tbl := NewTable[person]().
		Column("Name", func(p person) string { return p.name }).
		Column("Age", func(p person) string { return strconv.Itoa(p.age) }).
		Items(rows)

	if got := int(tbl.sel.NItems()); got != n {
		t.Fatalf("NItems = %d, want %d", got, n)
	}

	// A random single row round-trips through the boxed model.
	r := rand.New(rand.NewSource(7))
	idx := uint(r.Intn(n))
	tbl.sel.SelectItem(idx, true)
	if sel := tbl.Selected(); len(sel) != 1 || sel[0] != rows[idx] {
		t.Fatalf("Selected after SelectItem(%d) = %+v, want [%+v]", idx, sel, rows[idx])
	}

	// Select-all returns every one of the 5000+ rows.
	tbl.sel.SelectAll()
	if got := len(tbl.Selected()); got != n {
		t.Fatalf("SelectAll -> %d rows, want %d", got, n)
	}
}

// TestTableSetReusesIdentity is the regression guard for the live-update leak:
// updating a row must reuse the SAME backing GObject (and only change its value),
// not remove+insert a fresh one. Object churn is what made high-frequency updates
// pile up un-finalized GObjects (Go heap + RSS climb, GC thrash). Identity stable
// + value updated == no churn.
func TestTableSetReusesIdentity(t *testing.T) {
	gtkReady(t)

	tbl := NewTable[person]().
		Column("Name", func(p person) string { return p.name }).
		Items([]person{{"alice", 1}, {"bob", 2}, {"carol", 3}})

	before := tbl.model.Item(1).Native()
	tbl.Set(1, person{"bob2", 22})
	after := tbl.model.Item(1).Native()

	if before != after {
		t.Errorf("Set replaced the backing object (0x%x -> 0x%x); want same object (no churn)", before, after)
	}
	if got := tbl.model.At(1); got != (person{"bob2", 22}) {
		t.Errorf("At(1) = %+v, want {bob2 22}", got)
	}
}

// TestTableBatchInPlace checks Batch updates many rows in place (values change,
// backing objects reused) and emits a single coalesced change.
func TestTableBatchInPlace(t *testing.T) {
	gtkReady(t)

	tbl := NewTable[person]().
		Column("Name", func(p person) string { return p.name }).
		Items([]person{{"a", 1}, {"b", 2}, {"c", 3}})

	before0 := tbl.model.Item(0).Native()
	before2 := tbl.model.Item(2).Native()

	tbl.Batch(func(set func(i int, v person)) {
		set(0, person{"a2", 10})
		set(2, person{"c2", 30})
		set(99, person{"oob", 0}) // out of range: ignored
	})

	if tbl.model.Item(0).Native() != before0 || tbl.model.Item(2).Native() != before2 {
		t.Error("Batch replaced backing objects; want in-place (no churn)")
	}
	if got := tbl.model.At(0); got != (person{"a2", 10}) {
		t.Errorf("At(0) = %+v, want {a2 10}", got)
	}
	if got := tbl.model.At(2); got != (person{"c2", 30}) {
		t.Errorf("At(2) = %+v, want {c2 30}", got)
	}
	if got := tbl.model.At(1); got != (person{"b", 2}) {
		t.Errorf("At(1) = %+v, want {b 2} (untouched)", got)
	}
}

// TestTableColumnCmpSorts checks ColumnCmp sorts by the typed comparator
// (numeric), not by the rendered string (which would order "10" before "2"),
// and that the sorted order is readable through the selection model.
func TestTableColumnCmpSorts(t *testing.T) {
	gtkReady(t)

	tbl := NewTable[person]().
		Column("Name", func(p person) string { return p.name }).
		ColumnCmp("Age", func(p person) string { return strconv.Itoa(p.age) },
			func(a, b person) int { return a.age - b.age }).
		Items([]person{{"a", 2}, {"b", 10}, {"c", 1}, {"d", 21}, {"e", 3}})

	tbl.SortByColumn(1, true) // Age ascending

	got := make([]int, tbl.Len())
	for i := range got {
		got[i] = gioutil.ObjectValue[person](tbl.sel.Item(uint(i))).age
	}
	want := []int{1, 2, 3, 10, 21}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sorted ages = %v, want %v (numeric, not lexicographic)", got, want)
		}
	}
}

// TestTableBatchUnderSort checks Batch still updates values correctly when a
// sort is active (it takes the per-row path in that case), and the result
// stays sorted by the active column.
func TestTableBatchUnderSort(t *testing.T) {
	gtkReady(t)

	tbl := NewTable[person]().
		Column("Name", func(p person) string { return p.name }).
		ColumnCmp("Age", func(p person) string { return strconv.Itoa(p.age) },
			func(a, b person) int { return a.age - b.age }).
		Items([]person{{"a", 5}, {"b", 3}, {"c", 9}, {"d", 1}})
	tbl.SortByColumn(1, true)

	// Push everyone's age into a new order via Batch.
	tbl.Batch(func(set func(i int, v person)) {
		set(0, person{"a", 40})
		set(2, person{"c", 2})
	})

	got := make([]int, tbl.Len())
	for i := range got {
		got[i] = gioutil.ObjectValue[person](tbl.sel.Item(uint(i))).age
	}
	want := []int{1, 2, 3, 40} // d=1, c=2, b=3, a=40
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ages after Batch under sort = %v, want %v", got, want)
		}
	}
}

// TestTableAutoIncremental checks incremental sort auto-enables for large
// models and stays off for small ones, and that an explicit IncrementalSort
// pins the choice.
func TestTableAutoIncremental(t *testing.T) {
	gtkReady(t)

	small := NewTable[person]().Column("Name", func(p person) string { return p.name }).
		Items(randomPeople(100))
	if small.sort.Incremental() {
		t.Error("small model: incremental should be off")
	}

	large := NewTable[person]().Column("Name", func(p person) string { return p.name }).
		Items(randomPeople(autoIncrementalSortRows))
	if !large.sort.Incremental() {
		t.Errorf("large model (>=%d rows): incremental should auto-enable", autoIncrementalSortRows)
	}

	pinned := NewTable[person]().Column("Name", func(p person) string { return p.name }).
		IncrementalSort(false).
		Items(randomPeople(autoIncrementalSortRows))
	if pinned.sort.Incremental() {
		t.Error("explicit IncrementalSort(false) must override auto-enable")
	}
}

// BenchmarkTableSet measures the model-layer throughput of per-row updates
// (the data path the live perf demo exercises). The view is not realized, so
// this is the floor cost — boxing + items-changed — without cell re-render.
func BenchmarkTableSet(b *testing.B) {
	if !gtk.InitCheck() {
		b.Skip("no GTK display available")
	}
	const n = 5321
	rows := randomPeople(n)
	tbl := NewTable[person]().
		Column("Name", func(p person) string { return p.name }).
		Column("Age", func(p person) string { return strconv.Itoa(p.age) }).
		Items(rows)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx := i % n
		r := rows[idx]
		r.age = i & 0xff
		tbl.Set(idx, r)
	}
}
