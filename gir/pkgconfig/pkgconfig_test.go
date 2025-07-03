package pkgconfig

import (
	"fmt"
	"testing"

	"github.com/alecthomas/assert/v2"
)

func TestParseValues(t *testing.T) {
	tests := map[string]struct {
		inPkgs   []string
		inStdout string
		expVals  map[string]string
	}{
		"missing-leading-fdo": {
			[]string{"gtk4", "guile-3.0", "ruby-3.0"},
			"/usr/share/guile/site/3.0 /usr/lib/ruby/site_ruby\n",
			map[string]string{
				"gtk4":      "",
				"guile-3.0": "/usr/share/guile/site/3.0",
				"ruby-3.0":  "/usr/lib/ruby/site_ruby",
			},
		},
		"missing-leading-pkgconf1.8": {
			[]string{"gtk4", "guile-3.0", "ruby-3.0"},
			" /usr/share/guile/site/3.0 /usr/lib/ruby/site_ruby\n",
			map[string]string{
				"gtk4":      "",
				"guile-3.0": "/usr/share/guile/site/3.0",
				"ruby-3.0":  "/usr/lib/ruby/site_ruby",
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			out, err := parseValues(test.inPkgs, nil, test.inStdout)
			assert.NoError(t, err)

			assert.Equal(t, test.expVals, out)
		})
	}
}

func TestGIRDirs(t *testing.T) {
	tests := []struct {
		inPkgs         []string
		expGIRDirPaths map[string]string
	}{
		{
			[]string{"gtk4"},
			map[string]string{
				"gtk4": "/share/gir-1.0",
			},
		},
		{
			[]string{"gtk4", "pango", "cairo", "glib-2.0", "gdk-3.0"},
			map[string]string{
				"gtk4":     "/share/gir-1.0",
				"pango":    "/share/gir-1.0",
				"cairo":    "/share/gir-1.0",
				"glib-2.0": "/share/gir-1.0",
				"gdk-3.0":  "/share/gir-1.0",
			},
		},
	}

	for i, test := range tests {
		t.Run(fmt.Sprintf("case_%d", i), func(t *testing.T) {
			out, err := GIRDirs(test.inPkgs...)
			assert.NoError(t, err)

			for pkg, dir := range out {
				assert.Contains(t, dir, test.expGIRDirPaths[pkg], "unexpected directory for package %q", pkg)
			}
		})
	}
}
