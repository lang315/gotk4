// setup.go
package ui

import (
	"strconv"

	"github.com/diamondburned/gotk4/pkg/gota"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

// NewSetupPage renders the api_id/api_hash form. onSave gets valid values.
func NewSetupPage(onSave func(id int, hash string)) gtk.Widgetter {
	idEntry := gota.Entry().SetPlaceholder("api_id (digits)")
	hashEntry := gota.Entry().SetPlaceholder("api_hash")
	errLabel := gota.Label("")

	submit := func() {
		id, err := strconv.Atoi(idEntry.GetText())
		if err != nil || id <= 0 || hashEntry.GetText() == "" {
			errLabel.SetText("api_id must be a positive number; api_hash must be non-empty")
			return
		}
		onSave(id, hashEntry.GetText())
	}

	box := gota.VBox(8).Append(
		gota.Label("Telegram API credentials").Class("title-2"),
		gota.Label("Create them at my.telegram.org → API development tools"),
		idEntry,
		hashEntry,
		gota.Button("Save & connect").OnClick(func(*gota.ButtonWidget) { submit() }),
		errLabel,
	)
	w := box.ToWidget()
	w.SetMarginTop(24)
	w.SetMarginBottom(24)
	w.SetMarginStart(24)
	w.SetMarginEnd(24)
	return w
}
