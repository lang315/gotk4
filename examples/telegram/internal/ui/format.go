// format.go
package ui

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/diamondburned/gotk4/examples/telegram/internal/model"
)

// paletteSize is the number of sender/avatar color slots (spec-pinned: 7).
const paletteSize = 7

func colorIndex(id int64) int {
	if id < 0 {
		id = -id
	}
	return int(id % paletteSize)
}

// initials returns up to two uppercased leading letters of the title's words.
func initials(title string) string {
	out := make([]rune, 0, 2)
	for _, f := range strings.Fields(title) {
		r := []rune(f)[0]
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			out = append(out, unicode.ToUpper(r))
		}
		if len(out) == 2 {
			break
		}
	}
	if len(out) == 0 {
		return "?"
	}
	return string(out)
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

// sidebarTime renders a dialog's last-message time the way tdesktop does:
// today → clock, within the last week → weekday, older → dotted date.
func sidebarTime(t, now time.Time) string {
	switch {
	case t.IsZero():
		return ""
	case sameDay(t, now):
		return t.Format("15:04")
	case now.Sub(t) < 7*24*time.Hour:
		return t.Format("Mon")
	default:
		return t.Format("02.01.06")
	}
}

func dateSeparator(t, now time.Time) string {
	switch {
	case sameDay(t, now):
		return "Today"
	case sameDay(t, now.AddDate(0, 0, -1)):
		return "Yesterday"
	case t.Year() == now.Year():
		return t.Format("January 2")
	default:
		return t.Format("January 2, 2006")
	}
}

// grouped reports whether b visually continues a's message group.
func grouped(a, b model.Message) bool {
	return a.SenderID == b.SenderID && a.Out == b.Out && sameDay(a.Date, b.Date)
}

func subtitleText(d model.Dialog) string {
	switch {
	case d.Members > 0:
		return fmt.Sprintf("%d members", d.Members)
	case d.Kind == model.PeerChannel:
		return "channel"
	case d.Kind == model.PeerChat:
		return "group"
	default:
		return ""
	}
}
