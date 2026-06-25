package strcases

import "testing"

// TestSnakeToGo characterizes the C-identifier -> Go-name mapping that
// FormatEnumMember (and most of the generator) relies on. These pin current
// behavior; if the mangling changes, the generated API names change with it.
func TestSnakeToGo(t *testing.T) {
	tests := []struct {
		pascal bool
		in     string
		want   string
	}{
		// Exported (pascal) names: each segment is capitalized.
		{true, "foo_bar", "FooBar"},
		{true, "format", "Format"},
		{true, "interface_thing", "InterfaceThing"},
		{true, "a_b_c", "ABC"},
		// Acronyms in the pascal-specials list are fully upper-cased.
		{true, "gtk_window", "GTKWindow"},
		// Leading-digit segments are kept as-is by strcases (the numberMap
		// remap to a word happens later, in generators.FormatEnumMember).
		{true, "2d_offset", "2DOffset"},
		// On the pascal path a Go keyword is NOT mangled: it routes through
		// PascalToGo, not SnakeNoGo.
		{true, "type", "Type"},

		// Unexported (camel) names keep the first segment lower-cased.
		{false, "foo_bar", "fooBar"},
		{false, "normal_word", "normalWord"},
		// On the non-pascal path SnakeNoGo rewrites Go keywords / builtins.
		{false, "type", "typ"},
		{false, "interface", "iface"},
		{false, "go", "_go"},
		{false, "string", "str"},
	}

	for _, tt := range tests {
		if got := SnakeToGo(tt.pascal, tt.in); got != tt.want {
			t.Errorf("SnakeToGo(%v, %q) = %q, want %q", tt.pascal, tt.in, got, tt.want)
		}
	}
}

// TestSnakeToGoUnderscoreCollapse pins the underscore-collapsing behavior that
// is the root of the GTK 4.20 enum/bitfield alias bug (diamondburned/gotk4#174).
// The snakeRegex `[_0-9]+\w` consumes underscores that sit between digits, so
// two distinct C identifiers that differ only by such an underscore normalize
// to the exact same Go name. The generator must then dedupe them (see
// generators.DedupeEnumMembersByName); this test guards the collapse that makes
// the dedupe necessary.
func TestSnakeToGoUnderscoreCollapse(t *testing.T) {
	collisions := []struct {
		a, b string
		want string
	}{
		// GdkMemoryFormat aliases from #174.
		{"memory_g8b8r8_420", "memory_g8_b8r8_420", "MemoryG8B8R8420"},
		// The same collapse without the namespace prefix.
		{"g8b8r8_420", "g8_b8r8_420", "G8B8R8420"},
	}

	for _, c := range collisions {
		gotA := SnakeToGo(true, c.a)
		gotB := SnakeToGo(true, c.b)
		if gotA != gotB {
			t.Errorf("expected %q and %q to collide, got %q and %q", c.a, c.b, gotA, gotB)
		}
		if gotA != c.want {
			t.Errorf("SnakeToGo(true, %q) = %q, want %q", c.a, gotA, c.want)
		}
	}
}

// TestPascalToGo characterizes the Pascal-case path, including the acronym
// handling and the constructor New-suffix relocation.
func TestPascalToGo(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"FooBar", "FooBar"},
		{"Window", "Window"},
		{"GLArea", "GLArea"},
		// A trailing "New" is moved to the front (constructor convention).
		{"WindowNew", "NewWindow"},
	}

	for _, tt := range tests {
		if got := PascalToGo(tt.in); got != tt.want {
			t.Errorf("PascalToGo(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// TestKebabToGo characterizes the kebab-case path (used for signal names),
// which simply maps dashes to underscores and delegates to SnakeToGo.
func TestKebabToGo(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"foo-bar", "FooBar"},
		{"drag-begin", "DragBegin"},
	}

	for _, tt := range tests {
		if got := KebabToGo(true, tt.in); got != tt.want {
			t.Errorf("KebabToGo(true, %q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
