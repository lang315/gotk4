// photos_test.go
package tg

import (
	"path/filepath"
	"strings"
	"testing"

	tgapi "github.com/gotd/td/tg"
)

func TestPickSize(t *testing.T) {
	sizes := []tgapi.PhotoSizeClass{
		&tgapi.PhotoStrippedSize{Type: "i"},
		&tgapi.PhotoSize{Type: "m", W: 320, H: 240},
		&tgapi.PhotoSize{Type: "x", W: 800, H: 600},
		&tgapi.PhotoSize{Type: "y", W: 1280, H: 960},
	}
	if got, ok := pickSize(sizes); !ok || got != "x" {
		t.Fatalf("pickSize = %q, %v", got, ok)
	}
	only := []tgapi.PhotoSizeClass{&tgapi.PhotoSize{Type: "w", W: 2560, H: 1920}}
	if got, ok := pickSize(only); !ok || got != "w" {
		t.Fatalf("fallback = %q, %v", got, ok)
	}
	if _, ok := pickSize([]tgapi.PhotoSizeClass{&tgapi.PhotoStrippedSize{Type: "i"}}); ok {
		t.Fatal("no real sizes must be !ok")
	}
}

func TestCachePath(t *testing.T) {
	root := t.TempDir()
	p, err := cachePath(root, 0x1234, "x")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(p) != root || !strings.HasSuffix(p, "1234_x.jpg") {
		t.Fatalf("p = %q", p)
	}
	if _, err := cachePath(root, 1, "../evil"); err == nil {
		t.Fatal("traversal in size type must error")
	}
}
