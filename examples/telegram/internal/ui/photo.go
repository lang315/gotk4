// photo.go
package ui

import (
	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/diamondburned/gotk4/examples/telegram/internal/model"
)

type PhotoLoader func(ref model.PhotoRef) (string, error)

// asyncPic: goroutine fetch → idle → SetFilename. Kept app-local for v1;
// upstream to pkg/gota (with a real consumer as evidence) after v1 ships.
type asyncPic struct {
	*gtk.Picture
	idle func(func()) // test seam; defaults to coreglib.IdleAdd
}

func newAsyncPic() *asyncPic {
	p := gtk.NewPicture()
	p.SetSizeRequest(320, 240)
	p.SetCanShrink(true)
	return &asyncPic{Picture: p, idle: func(f func()) { coreglib.IdleAdd(f) }}
}

func (a *asyncPic) load(fetch func() (string, error)) {
	go func() {
		path, err := fetch()
		a.idle(func() {
			if err == nil && path != "" {
				a.SetFilename(path)
			}
		})
	}()
}
