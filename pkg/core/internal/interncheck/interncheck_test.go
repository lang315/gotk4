package interncheck

import (
	"runtime"
	"testing"

	"github.com/diamondburned/gotk4/pkg/core/intern"
)

// TestGetTransferFullAlreadyInterned pins the reference accounting of
// intern.Get on an object that is ALREADY interned.
//
// take=false (glib.AssumeOwnership, i.e. every transfer-full return) means the
// C side handed us one reference. The box's toggle reference already embodies
// our ownership, so Get must drop the transferred reference. The new-box path
// always did this; the already-interned path used to return early and leak it,
// pinning the object strong forever (one leaked reference per repeated
// transfer-full get, e.g. every gio.ListModel.Item call on the same object).
func TestGetTransferFullAlreadyInterned(t *testing.T) {
	ptr := NewObject() // refcount 1, not floating

	// First intern, transfer none (glib.Take): the toggle ref adds one.
	box := intern.Get(ptr, true)
	if rc := RefCount(ptr); rc != 2 {
		t.Fatalf("after first Get(take=true): refcount = %d, want 2", rc)
	}

	// Simulate a transfer-full return of the same object: C hands us one
	// extra reference on top.
	Ref(ptr)
	if rc := RefCount(ptr); rc != 3 {
		t.Fatalf("after simulated transfer-full ref: refcount = %d, want 3", rc)
	}

	box2 := intern.Get(ptr, false)
	if box2 != box {
		t.Fatalf("Get returned a different box for the same object")
	}
	if rc := RefCount(ptr); rc != 2 {
		t.Errorf("transferred ref leaked on already-interned Get(take=false): refcount = %d, want 2", rc)
	}

	runtime.KeepAlive(box)
	runtime.KeepAlive(box2)
}

// TestGetTransferNoneAlreadyInterned pins the symmetric case: take=true
// (glib.Take, transfer-none) on an already-interned object must not change the
// reference count at all — no reference was handed to us, and the toggle
// reference already exists.
func TestGetTransferNoneAlreadyInterned(t *testing.T) {
	ptr := NewObject()

	box := intern.Get(ptr, true)
	if rc := RefCount(ptr); rc != 2 {
		t.Fatalf("after first Get(take=true): refcount = %d, want 2", rc)
	}

	box2 := intern.Get(ptr, true)
	if box2 != box {
		t.Fatalf("Get returned a different box for the same object")
	}
	if rc := RefCount(ptr); rc != 2 {
		t.Errorf("Get(take=true) on interned object changed refcount = %d, want 2", rc)
	}

	runtime.KeepAlive(box)
	runtime.KeepAlive(box2)
}
