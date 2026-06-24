package gota

import (
	"testing"

	"github.com/diamondburned/gotk4/pkg/core/gioutil"
)

// TableWidget construction needs a GTK display, so it cannot be built headless.
// The table's correctness rests on gioutil.ListModel[T] boxing a Go value and
// handing the same value back in the bind/sort callbacks. This guards that
// round-trip without a display; the generic factory/sorter wiring itself is
// type-checked by `go build ./gota/...`.
func TestListModelRoundTrip(t *testing.T) {
	type row struct {
		name string
		n    int
	}
	m := gioutil.NewListModel[row]()
	want := []row{{"alice", 1}, {"bob", 2}, {"carol", 3}}
	for _, r := range want {
		m.Append(r)
	}
	if m.Len() != len(want) {
		t.Fatalf("Len = %d, want %d", m.Len(), len(want))
	}
	for i, w := range want {
		if got := m.At(i); got != w {
			t.Errorf("At(%d) = %+v, want %+v", i, got, w)
		}
	}
}
