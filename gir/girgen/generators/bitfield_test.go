package generators

import (
	"testing"

	"github.com/diamondburned/gotk4/gir"
)

// TestBitfieldMemberAliasDedup mirrors TestEnumMemberAliasDedup for the bitfield
// emitter. Bitfields reuse the enum dedupe helpers through the bitfieldData
// wrapper methods (ConstMembers -> DedupeEnumMembersByName, UniqueMembers ->
// UniqueEnumMembers), so the GTK 4.20+ alias collisions (diamondburned/gotk4#174)
// must collapse correctly on the bitfield path too.
func TestBitfieldMemberAliasDedup(t *testing.T) {
	// Sanity: the two aliased identifiers must collapse to the same Go name,
	// otherwise this test isn't exercising the bug.
	formatMember := (&bitfieldData{}).FormatMember
	if a, b := formatMember(member("GDK_MEMORY_G8B8R8_420", "33")), formatMember(member("GDK_MEMORY_G8_B8R8_420", "33")); a != b {
		t.Fatalf("expected aliased members to share a Go name, got %q and %q", a, b)
	}

	cases := []struct {
		name       string
		members    []gir.Member
		wantConst  int // const block: unique Go names only
		wantUnique int // switch cases: unique Go names AND values
	}{
		{
			// True alias: const block and switch both keep one.
			name: "alias same value",
			members: []gir.Member{
				member("GDK_MEMORY_G8B8R8_420", "33"),
				member("GDK_MEMORY_G8_B8R8_420", "33"),
			},
			wantConst:  1,
			wantUnique: 1,
		},
		{
			// Same Go name, different value: const block must keep one (name
			// redeclare), switch must keep one (duplicate case identifier).
			name: "same name different value",
			members: []gir.Member{
				member("GDK_MEMORY_G8B8R8_420", "33"),
				member("GDK_MEMORY_G8_B8R8_420", "34"),
			},
			wantConst:  1,
			wantUnique: 1,
		},
		{
			// Different Go name, same value: const block keeps both (valid Go),
			// switch keeps one (duplicate case value).
			name: "different name same value",
			members: []gir.Member{
				member("GDK_MEMORY_R8G8B8", "0"),
				member("GDK_MEMORY_B8G8R8", "0"),
			},
			wantConst:  2,
			wantUnique: 1,
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			bd := &bitfieldData{Members: tt.members}
			if got := len(bd.ConstMembers()); got != tt.wantConst {
				t.Errorf("ConstMembers(): got %d, want %d", got, tt.wantConst)
			}
			if got := len(bd.UniqueMembers()); got != tt.wantUnique {
				t.Errorf("UniqueMembers(): got %d, want %d", got, tt.wantUnique)
			}
		})
	}
}

// TestBitfieldBits characterizes bitfieldData.Bits, which renders a decimal
// member value as a Go binary literal for the generated const block. Values
// that don't parse as unsigned integers are passed through unchanged.
func TestBitfieldBits(t *testing.T) {
	bd := &bitfieldData{}
	tests := []struct {
		in   string
		want string
	}{
		{"0", "0b0"},
		{"1", "0b1"},
		{"4", "0b100"},
		{"33", "0b100001"},
		{"notanumber", "notanumber"},
		{"-1", "-1"},
	}
	for _, tt := range tests {
		if got := bd.Bits(tt.in); got != tt.want {
			t.Errorf("Bits(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
