package gota

import "github.com/diamondburned/gotk4/pkg/gtk/v4"

// base carries the chainable setters every widget shares. The type parameter T
// is the concrete wrapper, so each chained call returns *T (the concrete type),
// not *base — keeping the fluent chain typed without per-widget boilerplate.
//
// The original gota repeated these helpers across every widget via a generated
// ~170KB file; Go generics collapse that into one definition here.
type base[T any] struct {
	self *T
	w    *gtk.Widget
}

// init wires the embedded base to its concrete wrapper and underlying widget.
// Every constructor must call it.
func (b *base[T]) init(self *T, w *gtk.Widget) {
	b.self = self
	b.w = w
}

// ToWidget returns the underlying GTK widget, satisfying IWidget.
func (b *base[T]) ToWidget() *gtk.Widget { return b.w }

// Class adds one or more CSS classes.
func (b *base[T]) Class(classes ...string) *T {
	for _, c := range classes {
		b.w.AddCSSClass(c)
	}
	return b.self
}

// RemoveClass removes a CSS class.
func (b *base[T]) RemoveClass(class string) *T {
	b.w.RemoveCSSClass(class)
	return b.self
}

// Tooltip sets the hover tooltip text.
func (b *base[T]) Tooltip(text string) *T {
	b.w.SetTooltipText(text)
	return b.self
}

// Name sets the widget name (used for CSS selectors).
func (b *base[T]) Name(name string) *T {
	b.w.SetName(name)
	return b.self
}

// Visible shows or hides the widget.
func (b *base[T]) Visible(v bool) *T {
	b.w.SetVisible(v)
	return b.self
}

// Size requests a minimum width/height; -1 leaves a dimension unconstrained.
func (b *base[T]) Size(width, height int) *T {
	b.w.SetSizeRequest(width, height)
	return b.self
}

// HExpand sets whether the widget expands horizontally to fill space.
func (b *base[T]) HExpand(v bool) *T {
	b.w.SetHExpand(v)
	return b.self
}

// VExpand sets whether the widget expands vertically to fill space.
func (b *base[T]) VExpand(v bool) *T {
	b.w.SetVExpand(v)
	return b.self
}

// HAlign sets horizontal alignment within the allocated space.
func (b *base[T]) HAlign(align gtk.Align) *T {
	b.w.SetHAlign(align)
	return b.self
}

// VAlign sets vertical alignment within the allocated space.
func (b *base[T]) VAlign(align gtk.Align) *T {
	b.w.SetVAlign(align)
	return b.self
}
