package gota

import "testing"

// Constructing GTK widgets needs a display, so this only covers the pure
// mnemonic-parsing logic. The fluent chaining (each setter returns the concrete
// *XxxWidget) is checked at compile time by `go build ./gota/...`.
func TestParseMnemonic(t *testing.T) {
	cases := []struct {
		in      string
		wantStr string
		wantOK  bool
	}{
		{"{{_Save}}", "_Save", true},
		{"plain", "", false},
		{"", "", false},
		{"{{}}", "", true},
		{"{{", "", false},
		{"{{x}", "", false},
	}
	for _, c := range cases {
		got, ok := parseMnemonic(c.in)
		if got != c.wantStr || ok != c.wantOK {
			t.Errorf("parseMnemonic(%q) = (%q, %v), want (%q, %v)", c.in, got, ok, c.wantStr, c.wantOK)
		}
	}
}
