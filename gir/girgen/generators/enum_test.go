package generators

import (
	"testing"

	"github.com/diamondburned/gotk4/gir"
)

func member(cident, value string) gir.Member {
	return gir.Member{CIdentifier: cident, Value: value}
}

// TestEnumMemberAliasDedup reproduces the GTK 4.20 GdkMemoryFormat regression
// (diamondburned/gotk4#174) where aliased C identifiers normalize to the same
// Go name, producing "redeclared in this block" and "duplicate case" errors.
func TestEnumMemberAliasDedup(t *testing.T) {
	// Sanity: the two aliased identifiers must collapse to the same Go name,
	// otherwise this test isn't exercising the bug.
	a := FormatEnumMember(member("GDK_MEMORY_G8B8R8_420", "33"))
	b := FormatEnumMember(member("GDK_MEMORY_G8_B8R8_420", "33"))
	if a != b {
		t.Fatalf("expected aliased members to share a Go name, got %q and %q", a, b)
	}

	// Same name, same value (true alias): both const block and switch keep one.
	alias := []gir.Member{
		member("GDK_MEMORY_G8B8R8_420", "33"),
		member("GDK_MEMORY_G8_B8R8_420", "33"),
	}
	if got := len(DedupeEnumMembersByName(alias)); got != 1 {
		t.Errorf("DedupeEnumMembersByName(alias): got %d, want 1", got)
	}
	if got := len(UniqueEnumMembers(alias)); got != 1 {
		t.Errorf("UniqueEnumMembers(alias): got %d, want 1", got)
	}

	// Same name, different value: const block must keep one (name redeclare),
	// switch must keep one (duplicate case identifier).
	sameName := []gir.Member{
		member("GDK_MEMORY_G8B8R8_420", "33"),
		member("GDK_MEMORY_G8_B8R8_420", "34"),
	}
	if got := len(DedupeEnumMembersByName(sameName)); got != 1 {
		t.Errorf("DedupeEnumMembersByName(sameName): got %d, want 1", got)
	}
	if got := len(UniqueEnumMembers(sameName)); got != 1 {
		t.Errorf("UniqueEnumMembers(sameName): got %d, want 1", got)
	}

	// Different name, same value: const block keeps both (valid Go), switch
	// keeps one (duplicate case value).
	sameValue := []gir.Member{
		member("GDK_MEMORY_R8G8B8", "0"),
		member("GDK_MEMORY_B8G8R8", "0"),
	}
	if got := len(DedupeEnumMembersByName(sameValue)); got != 2 {
		t.Errorf("DedupeEnumMembersByName(sameValue): got %d, want 2", got)
	}
	if got := len(UniqueEnumMembers(sameValue)); got != 1 {
		t.Errorf("UniqueEnumMembers(sameValue): got %d, want 1", got)
	}
}
