// format_test.go
package ui

import (
	"testing"
	"time"

	"github.com/diamondburned/gotk4/examples/telegram/internal/model"
)

var now = time.Date(2026, 7, 2, 22, 0, 0, 0, time.Local)

func TestInitials(t *testing.T) {
	for in, want := range map[string]string{
		"Alice":               "A",
		"News Channel":        "NC",
		"Mang Tiền Về Cho Vợ": "MT",
		"  ":                  "?",
		"42 coins":            "4C",
	} {
		if got := initials(in); got != want {
			t.Errorf("initials(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestColorIndex(t *testing.T) {
	if colorIndex(-9) != 2 || colorIndex(7) != 0 || colorIndex(13) != 6 {
		t.Fatalf("colorIndex wrong: %d %d %d", colorIndex(-9), colorIndex(7), colorIndex(13))
	}
}

func TestSidebarTime(t *testing.T) {
	for _, tc := range []struct {
		t    time.Time
		want string
	}{
		{time.Time{}, ""},
		{now.Add(-2 * time.Hour), "20:00"},
		{now.AddDate(0, 0, -3), now.AddDate(0, 0, -3).Format("Mon")},
		{now.AddDate(0, 0, -30), now.AddDate(0, 0, -30).Format("02.01.06")},
	} {
		if got := sidebarTime(tc.t, now); got != tc.want {
			t.Errorf("sidebarTime(%v) = %q, want %q", tc.t, got, tc.want)
		}
	}
}

func TestDateSeparator(t *testing.T) {
	if got := dateSeparator(now, now); got != "Today" {
		t.Errorf("today = %q", got)
	}
	if got := dateSeparator(now.AddDate(0, 0, -1), now); got != "Yesterday" {
		t.Errorf("yesterday = %q", got)
	}
	if got := dateSeparator(time.Date(2026, 3, 5, 0, 0, 0, 0, time.Local), now); got != "March 5" {
		t.Errorf("same year = %q", got)
	}
	if got := dateSeparator(time.Date(2025, 3, 5, 0, 0, 0, 0, time.Local), now); got != "March 5, 2025" {
		t.Errorf("other year = %q", got)
	}
}

func TestGrouped(t *testing.T) {
	a := model.Message{SenderID: 1, Date: now}
	b := model.Message{SenderID: 1, Date: now.Add(time.Minute)}
	c := model.Message{SenderID: 2, Date: now}
	d := model.Message{SenderID: 1, Out: true, Date: now}
	e := model.Message{SenderID: 1, Date: now.AddDate(0, 0, 1)}
	if !grouped(a, b) || grouped(a, c) || grouped(a, d) || grouped(a, e) {
		t.Fatal("grouped rules wrong")
	}
}

func TestSubtitleText(t *testing.T) {
	if got := subtitleText(model.Dialog{Members: 6660}); got != "6660 members" {
		t.Errorf("members = %q", got)
	}
	if got := subtitleText(model.Dialog{Kind: model.PeerChannel}); got != "channel" {
		t.Errorf("channel = %q", got)
	}
	if got := subtitleText(model.Dialog{Kind: model.PeerChat}); got != "group" {
		t.Errorf("group = %q", got)
	}
	if got := subtitleText(model.Dialog{Kind: model.PeerUser}); got != "" {
		t.Errorf("user = %q", got)
	}
}
