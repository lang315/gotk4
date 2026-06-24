package gota

import "github.com/diamondburned/gotk4/pkg/gtk/v4"

// ButtonWidget wraps *gtk.Button.
type ButtonWidget struct {
	base[ButtonWidget]
	obj *gtk.Button
}

// Button creates a button. An empty text yields a bare button; text wrapped in
// {{...}} creates a mnemonic (underline-accelerator) label; otherwise it is a
// plain label.
func Button(text string) *ButtonWidget {
	var btn *gtk.Button
	switch {
	case text == "":
		btn = gtk.NewButton()
	default:
		if m, ok := parseMnemonic(text); ok {
			btn = gtk.NewButtonWithMnemonic(m)
		} else {
			btn = gtk.NewButtonWithLabel(text)
		}
	}
	w := &ButtonWidget{obj: btn}
	w.init(w, &btn.Widget)
	return w
}

// OnClick connects the clicked signal.
func (b *ButtonWidget) OnClick(fn func(sender *ButtonWidget)) *ButtonWidget {
	b.obj.ConnectClicked(func() { fn(b) })
	return b
}

// SetText sets the button label.
func (b *ButtonWidget) SetText(s string) *ButtonWidget {
	b.obj.SetLabel(s)
	return b
}

// GetText returns the button label.
func (b *ButtonWidget) GetText() string { return b.obj.Label() }

// EntryWidget wraps *gtk.Entry.
type EntryWidget struct {
	base[EntryWidget]
	obj *gtk.Entry
}

// Entry creates a single-line text entry.
func Entry() *EntryWidget {
	ent := gtk.NewEntry()
	w := &EntryWidget{obj: ent}
	w.init(w, &ent.Widget)
	return w
}

// SetText sets the entry contents.
func (e *EntryWidget) SetText(s string) *EntryWidget {
	e.obj.SetText(s)
	return e
}

// GetText returns the entry contents.
func (e *EntryWidget) GetText() string { return e.obj.Text() }

// SetPlaceholder sets the placeholder shown when empty.
func (e *EntryWidget) SetPlaceholder(s string) *EntryWidget {
	e.obj.SetPlaceholderText(s)
	return e
}

// PasswordMode hides the typed characters when v is true.
func (e *EntryWidget) PasswordMode(v bool) *EntryWidget {
	e.obj.SetVisibility(!v)
	return e
}

// OnChange connects the changed signal.
func (e *EntryWidget) OnChange(fn func(sender *EntryWidget)) *EntryWidget {
	e.obj.ConnectChanged(func() { fn(e) })
	return e
}

// OnEnter connects the activate signal (Enter pressed).
func (e *EntryWidget) OnEnter(fn func(sender *EntryWidget)) *EntryWidget {
	e.obj.ConnectActivate(func() { fn(e) })
	return e
}

// LabelWidget wraps *gtk.Label.
type LabelWidget struct {
	base[LabelWidget]
	obj *gtk.Label
}

// Label creates a text label.
func Label(text string) *LabelWidget {
	lbl := gtk.NewLabel(text)
	w := &LabelWidget{obj: lbl}
	w.init(w, &lbl.Widget)
	return w
}

// SetText sets the label text.
func (l *LabelWidget) SetText(s string) *LabelWidget {
	l.obj.SetText(s)
	return l
}

// SetMarkup sets Pango-markup text.
func (l *LabelWidget) SetMarkup(s string) *LabelWidget {
	l.obj.SetMarkup(s)
	return l
}

// GetText returns the label text.
func (l *LabelWidget) GetText() string { return l.obj.Text() }

// BoxWidget wraps *gtk.Box.
type BoxWidget struct {
	base[BoxWidget]
	obj *gtk.Box
}

// VBox creates a vertical box with the given inter-child spacing.
func VBox(spacing int) *BoxWidget { return wrapBox(gtk.NewBox(gtk.OrientationVertical, spacing)) }

// HBox creates a horizontal box with the given inter-child spacing.
func HBox(spacing int) *BoxWidget { return wrapBox(gtk.NewBox(gtk.OrientationHorizontal, spacing)) }

func wrapBox(box *gtk.Box) *BoxWidget {
	w := &BoxWidget{obj: box}
	w.init(w, &box.Widget)
	return w
}

// Append adds children to the end of the box, in order.
func (b *BoxWidget) Append(children ...IWidget) *BoxWidget {
	for _, c := range children {
		b.obj.Append(c.ToWidget())
	}
	return b
}

// Prepend adds a child to the start of the box.
func (b *BoxWidget) Prepend(child IWidget) *BoxWidget {
	b.obj.Prepend(child.ToWidget())
	return b
}

// Remove removes a child from the box.
func (b *BoxWidget) Remove(child IWidget) *BoxWidget {
	b.obj.Remove(child.ToWidget())
	return b
}

// WindowWidget wraps *gtk.ApplicationWindow.
type WindowWidget struct {
	base[WindowWidget]
	obj *gtk.ApplicationWindow
}

func wrapWindow(win *gtk.ApplicationWindow) *WindowWidget {
	w := &WindowWidget{obj: win}
	w.init(w, &win.Widget)
	return w
}

// Title sets the window title.
func (w *WindowWidget) Title(s string) *WindowWidget {
	w.obj.SetTitle(s)
	return w
}

// DefaultSize sets the initial window size.
func (w *WindowWidget) DefaultSize(width, height int) *WindowWidget {
	w.obj.SetDefaultSize(width, height)
	return w
}

// Child sets the window's single child widget.
func (w *WindowWidget) Child(child IWidget) *WindowWidget {
	w.obj.SetChild(child.ToWidget())
	return w
}
