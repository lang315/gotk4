# Telegram Day Theme Chat UI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the examples/telegram chat screen visually faithful to Telegram Desktop's Day theme: avatar+preview sidebar rows with unread pills, chat header, grouped message bubbles with timestamps and date separators, auto-scroll with a floating scroll-to-bottom button, and a proper composer.

**Architecture:** Data additions land in `internal/model` + `internal/tg` (dates, sender IDs, member counts). All styling is one CSS string (`internal/ui/style.go`) installed once via `gtk.CssProvider`; pure formatting/grouping helpers live in `internal/ui/format.go` (unit-tested without GTK); `internal/ui/chat.go` is reworked cell-by-cell. Message cells read neighbor items at bind time (`item.Position()` + `messages.At`) to decide grouping/date separators — no mixed list models.

**Tech Stack:** Go 1.26, gotk4 GTK4 bindings (GTK 4.22.4 brew), gotd v0.159.0.

## Global Constraints

- Spec (palette hex, metrics, grouping rules) is `docs/superpowers/specs/2026-07-02-telegram-day-theme-ui-design.md` — values there are final; do not "improve" colors or sizes.
- Before ANY go command: `cd examples/telegram && export PKG_CONFIG_PATH="$(brew --prefix)/lib/pkgconfig:$(brew --prefix)/share/pkgconfig:$PKG_CONFIG_PATH"`.
- `ld: warning: ignoring duplicate libraries` is environmental noise. LSP/IDE diagnostics are chronically stale — trust real `go build`/`go test` output only.
- goimports -w, never plain gofmt. Commit per task on branch `feat/telegram-app`.
- GTK-touching tests go in package `ui`, guarded by the existing `gtkOK` (single-shot `TestMain` in `login_test.go`) — NEVER call `gtk.InitCheck()` anywhere else; a second init attempt after a failed first segfaults.
- APIs pre-verified against the installed bindings: `CSSProvider.LoadFromString(str string)`, `gtk.StyleContextAddProviderForDisplay(display *gdk.Display, provider StyleProviderer, priority uint)`, `gdk.DisplayGetDefault() *Display`, `(*ListView).ScrollTo(pos uint, flags ListScrollFlags, scroll *ScrollInfo)` (+`gtk.ListScrollNone`), `(*Overlay).AddOverlay(Widgetter)`, `(*ScrolledWindow).VAdjustment() *Adjustment` (+`ConnectValueChanged/Value/Upper/PageSize`), `(*Label).SetMaxWidthChars`, `gioutil.ListModel.Set` (rebinds realized rows; out-of-range = no-op).
- NOT pre-verified (check with `go doc` at first use; fallbacks given inline): `gtk.Widget.HasCSSClass`, `gtk.NewButtonFromIconName`, gotd `(*tgapi.Channel).GetParticipantsCount`, the exported name of GTK's style-provider priority constant (fallback: local `const styleProviderPriorityApplication = 600`).

---

### Task 1: Data layer — dates, sender IDs, member counts

**Files:**
- Modify: `examples/telegram/internal/model/model.go`
- Modify: `examples/telegram/internal/tg/peers.go`
- Modify: `examples/telegram/internal/tg/messages.go`
- Modify: `examples/telegram/internal/tg/dialogs.go`
- Test: `examples/telegram/internal/tg/uidata_test.go` (new)

**Interfaces:**
- Consumes: existing `peerStore`, `mapMessage`, `mapDialogs`, `previewText`, `consumeSent`.
- Produces (Tasks 2–4 rely on these exact shapes): `model.Message{..., SenderID int64, Date time.Time}`, `model.Dialog{..., Members int, LastDate time.Time}`, `(*peerStore).Members(kind model.PeerKind, id int64) int`.

- [ ] **Step 1: Extend the model types**

In `internal/model/model.go`, add `"time"` to imports (create an import block) and change the two structs:

```go
type Dialog struct {
	PeerID   int64
	Kind     PeerKind
	Title    string
	Preview  string
	Unread   int
	CanSend  bool // false for broadcast channels
	Members  int  // participant count; 0 = unknown
	LastDate time.Time
}

type Message struct {
	ID       int
	PeerID   int64
	Kind     PeerKind
	Sender   string
	SenderID int64
	Text     string // plain text; "[photo]"/"[file]"/"[media]" for media
	Out      bool
	Date     time.Time
	Photo    *PhotoRef
}
```

- [ ] **Step 2: Member counts in the peer store**

In `internal/tg/peers.go`: add `members int` to `peerInfo`; fill it in `Add` (`*tgapi.Chat` → `ch.ParticipantsCount`; `*tgapi.Channel` → flag-guarded getter — verify the exact name with `go doc github.com/gotd/td/tg.Channel | grep -i participants`, expected `GetParticipantsCount() (int, bool)`); add the accessor:

```go
		case *tgapi.Channel:
			members := 0
			if n, ok := ch.GetParticipantsCount(); ok {
				members = n
			}
			s.m[model.PeerChannel][ch.ID] = peerInfo{
				input:   &tgapi.InputPeerChannel{ChannelID: ch.ID, AccessHash: ch.AccessHash},
				title:   ch.Title,
				canSend: !ch.Broadcast,
				members: members,
			}
```

(and for `*tgapi.Chat`, set `members: ch.ParticipantsCount`)

```go
// Members returns the participant count for a peer, 0 if unknown.
func (s *peerStore) Members(kind model.PeerKind, id int64) int {
	p, _ := s.get(kind, id)
	return p.members
}
```

- [ ] **Step 3: Dates and sender identity in mapMessage**

In `internal/tg/messages.go` (add `"time"` import), replace the top of `mapMessage`:

```go
	kind, peerID := peerKey(m.PeerID)
	out := model.Message{
		ID: m.ID, PeerID: peerID, Kind: kind,
		Text: m.Message, Out: m.Out,
		Date: time.Unix(int64(m.Date), 0),
	}
	switch from := m.FromID.(type) {
	case *tgapi.PeerUser:
		out.Sender = s.Title(model.PeerUser, from.UserID)
		out.SenderID = from.UserID
		if from.UserID == selfID {
			out.Out = true
		}
	case *tgapi.PeerChannel: // anonymous admin / linked channel
		out.Sender = s.Title(model.PeerChannel, from.ChannelID)
		out.SenderID = from.ChannelID
	default: // channel post with no FromID: attribute to the channel itself
		if kind == model.PeerChannel {
			out.Sender = s.Title(kind, peerID)
			out.SenderID = peerID
		}
	}
```

(The old `if m.FromID != nil { if fu, ok := ... }` block is fully replaced; the media block below it stays.)

In `consumeSent`, give the synthesized echo a date and sender ID:

```go
	case *tgapi.UpdateShortSentMessage:
		if !c.chats.Note(kind, peerID, u.ID) {
			return
		}
		selfID := int64(0)
		if me := c.self(); me != nil {
			selfID = me.ID
		}
		c.emit(model.Event{Kind: model.EventNewMessage, Msg: model.Message{
			ID: u.ID, PeerID: peerID, Kind: kind, Sender: "me", SenderID: selfID,
			Text: text, Out: true, Date: time.Unix(int64(u.Date), 0),
		}})
```

- [ ] **Step 4: Preview flattening, LastDate, Members in dialogs**

In `internal/tg/dialogs.go` (add `"strings"` and `"time"` imports): make the `top` map typed and flatten previews:

```go
func previewText(m tgapi.MessageClass) string {
	msg, ok := m.(*tgapi.Message)
	if !ok {
		return ""
	}
	if msg.Media != nil {
		return mediaLabel(msg.Media)
	}
	return strings.Join(strings.Fields(msg.Message), " ")
}
```

```go
	top := make(map[int]*tgapi.Message, len(msgs))
	for _, m := range msgs {
		if mm, ok := m.(*tgapi.Message); ok {
			top[mm.ID] = mm
		}
	}
	out := make([]model.Dialog, 0, len(dialogs))
	for _, dc := range dialogs {
		d, ok := dc.(*tgapi.Dialog)
		if !ok {
			continue
		}
		kind, id := peerKey(d.Peer)
		md := model.Dialog{
			PeerID:  id,
			Kind:    kind,
			Title:   s.Title(kind, id),
			Unread:  d.UnreadCount,
			CanSend: s.CanSend(kind, id),
			Members: s.Members(kind, id),
		}
		if tm, ok := top[d.TopMessage]; ok {
			md.Preview = previewText(tm)
			md.LastDate = time.Unix(int64(tm.Date), 0)
		}
		out = append(out, md)
	}
```

- [ ] **Step 5: Write the tests**

Create `internal/tg/uidata_test.go`:

```go
// uidata_test.go
package tg

import (
	"testing"
	"time"

	tgapi "github.com/gotd/td/tg"

	"github.com/diamondburned/gotk4/examples/telegram/internal/model"
)

func storeWith(t *testing.T) *peerStore {
	t.Helper()
	s := newPeerStore()
	ch := &tgapi.Channel{ID: 77, Title: "News Channel", AccessHash: 1}
	ch.SetParticipantsCount(6660) // verify setter name via go doc if compile fails
	s.Add(
		[]tgapi.UserClass{&tgapi.User{ID: 5, FirstName: "Alice", AccessHash: 2}},
		[]tgapi.ChatClass{
			&tgapi.Chat{ID: 42, Title: "Group", ParticipantsCount: 12},
			ch,
		},
	)
	return s
}

func TestMapMessageChannelSenderFallback(t *testing.T) {
	s := storeWith(t)
	m := &tgapi.Message{
		ID: 1, Message: "post", Date: 1_800_000_000,
		PeerID: &tgapi.PeerChannel{ChannelID: 77}, // FromID nil: channel post
	}
	got, ok := mapMessage(s, 0, m)
	if !ok {
		t.Fatal("mapMessage returned !ok")
	}
	if got.Sender != "News Channel" || got.SenderID != 77 {
		t.Fatalf("sender = %q/%d, want News Channel/77", got.Sender, got.SenderID)
	}
	if got.Date != time.Unix(1_800_000_000, 0) {
		t.Fatalf("date = %v", got.Date)
	}
}

func TestMapMessageUserSender(t *testing.T) {
	s := storeWith(t)
	m := &tgapi.Message{
		ID: 2, Message: "hi", Date: 1_800_000_000,
		PeerID: &tgapi.PeerChat{ChatID: 42},
		FromID: &tgapi.PeerUser{UserID: 5},
	}
	got, _ := mapMessage(s, 0, m)
	if got.Sender != "Alice" || got.SenderID != 5 {
		t.Fatalf("sender = %q/%d, want Alice/5", got.Sender, got.SenderID)
	}
}

func TestPreviewTextFlattensNewlines(t *testing.T) {
	m := &tgapi.Message{Message: "line one\nline two\n\nline three"}
	if got := previewText(m); got != "line one line two line three" {
		t.Fatalf("preview = %q", got)
	}
}

func TestMapDialogsMembersAndLastDate(t *testing.T) {
	s := storeWith(t)
	ds := mapDialogs(s,
		[]tgapi.DialogClass{&tgapi.Dialog{
			Peer: &tgapi.PeerChannel{ChannelID: 77}, TopMessage: 9, UnreadCount: 3,
		}},
		[]tgapi.MessageClass{&tgapi.Message{
			ID: 9, Message: "last", Date: 1_800_000_000,
			PeerID: &tgapi.PeerChannel{ChannelID: 77},
		}},
		nil, nil,
	)
	if len(ds) != 1 {
		t.Fatalf("dialogs = %d", len(ds))
	}
	d := ds[0]
	if d.Members != 6660 {
		t.Fatalf("members = %d, want 6660", d.Members)
	}
	if d.LastDate != time.Unix(1_800_000_000, 0) {
		t.Fatalf("lastDate = %v", d.LastDate)
	}
}

func TestPeerStoreMembers(t *testing.T) {
	s := storeWith(t)
	if got := s.Members(model.PeerChat, 42); got != 12 {
		t.Fatalf("chat members = %d, want 12", got)
	}
	if got := s.Members(model.PeerUser, 5); got != 0 {
		t.Fatalf("user members = %d, want 0", got)
	}
}
```

- [ ] **Step 6: Run tests — expect compile failures first, then green**

```sh
go test ./internal/tg/ -run 'TestMapMessage|TestPreviewText|TestMapDialogs|TestPeerStore' -v
```

Before the implementation steps compile: FAIL (missing fields). After: all PASS. Then the whole module: `go build ./... && go test ./...` — note `internal/ui` still compiles because struct fields were only ADDED.

- [ ] **Step 7: Format and commit**

```sh
goimports -w internal/
git add internal/model/model.go internal/tg/
git commit -m "feat(telegram): dates, sender IDs, and member counts in the data layer

Message gains Date/SenderID (channel posts attribute to the channel,
killing the empty-sender rows); Dialog gains Members/LastDate; previews
flatten newlines. Groundwork for the Telegram Day theme UI."
```

---

### Task 2: Theme CSS, pure formatting helpers, wallpaper wiring

**Files:**
- Create: `examples/telegram/internal/ui/style.go`
- Create: `examples/telegram/internal/ui/format.go`
- Test: `examples/telegram/internal/ui/format_test.go` (new)
- Modify: `examples/telegram/main.go` (activate: LoadCSS + wallpaper detection)

**Interfaces:**
- Consumes: `model.Dialog`/`model.Message` fields from Task 1.
- Produces (Tasks 3–5 rely on these): `LoadCSS(wallpaperPath string)`, `paletteSize = 7`, `colorIndex(id int64) int`, `initials(title string) string`, `sameDay(a, b time.Time) bool`, `sidebarTime(t, now time.Time) string`, `dateSeparator(t, now time.Time) string`, `grouped(a, b model.Message) bool`, `subtitleText(d model.Dialog) string`; CSS classes `.dialog-title .dialog-preview .dialog-time .unread-pill .avatar .avatar-0..6 .chat-header .chat-title .chat-subtitle .chat-pane .msg-list .bubble .bubble-in .bubble-out .r-tl4 .r-bl4 .r-tr4 .r-br4 .sender .sender-0..6 .msg-time .msg-time-out .date-pill .fab .composer`.

- [ ] **Step 1: Write the failing tests**

Create `internal/ui/format_test.go` (pure — no GTK, no gtkOK guard needed):

```go
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
```

- [ ] **Step 2: Run to verify failure**

`go test ./internal/ui/ -run 'TestInitials|TestColorIndex|TestSidebarTime|TestDateSeparator|TestGrouped|TestSubtitleText' -v` → compile error (undefined functions).

- [ ] **Step 3: Implement format.go**

```go
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
```

- [ ] **Step 4: Run tests to verify pass**

Same command as Step 2 → all PASS.

- [ ] **Step 5: Implement style.go**

First verify the priority constant: `go doc github.com/diamondburned/gotk4/pkg/gtk/v4 2>/dev/null | grep -i 'STYLE_PROVIDER_PRIORITY'`. If an exported constant exists, use it; otherwise keep the local fallback shown here.

```go
// style.go
package ui

import (
	"fmt"
	"os"

	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

// styleProviderPriorityApplication mirrors GTK_STYLE_PROVIDER_PRIORITY_APPLICATION.
const styleProviderPriorityApplication = 600

// themeCSS is the Telegram Day palette; every hex value is spec-pinned
// (docs/superpowers/specs/2026-07-02-telegram-day-theme-ui-design.md).
const themeCSS = `
listview > row:selected, listview > child:selected { background: #419FD9; border-radius: 6px; }
listview > row:selected label, listview > child:selected label { color: #FFFFFF; }
listview > row:hover:not(:selected), listview > child:hover:not(:selected) { background: #F1F4F7; }

.dialog-title { font-size: 14px; font-weight: bold; color: #000000; }
.dialog-preview { font-size: 13px; color: #8E9AA4; }
.dialog-time { font-size: 12px; color: #A0ACB6; }
.unread-pill { background: #4DCD5E; color: #FFFFFF; font-size: 12px; font-weight: bold;
  border-radius: 10px; padding: 1px 6px; min-width: 20px; }

.avatar { border-radius: 9999px; color: #FFFFFF; font-weight: bold; }
.avatar-0 { background: linear-gradient(to bottom, #FF885E, #FF516A); }
.avatar-1 { background: linear-gradient(to bottom, #FFCD6A, #FFA85C); }
.avatar-2 { background: linear-gradient(to bottom, #82B1FF, #665FFF); }
.avatar-3 { background: linear-gradient(to bottom, #A0DE7E, #54CB68); }
.avatar-4 { background: linear-gradient(to bottom, #53EDD6, #28C9B7); }
.avatar-5 { background: linear-gradient(to bottom, #72D5FD, #2A9EF1); }
.avatar-6 { background: linear-gradient(to bottom, #E0A2F3, #D669ED); }

.chat-header { background: #FFFFFF; border-bottom: 1px solid #E4E9EE; }
.chat-title { font-size: 14px; font-weight: bold; color: #000000; }
.chat-subtitle { font-size: 13px; color: #8E9AA4; }

.chat-pane { background: linear-gradient(to bottom, #DBE7F0, #E9EFF4); }
listview.msg-list, listview.msg-list > row, listview.msg-list > child { background: transparent; }

.bubble { padding: 8px 12px; border-radius: 18px; }
.bubble-in { background: #FFFFFF; color: #000000; }
.bubble-out { background: #EFFDDE; color: #000000; }
.r-tl4 { border-top-left-radius: 4px; }
.r-bl4 { border-bottom-left-radius: 4px; }
.r-tr4 { border-top-right-radius: 4px; }
.r-br4 { border-bottom-right-radius: 4px; }

.sender { font-size: 13px; font-weight: bold; }
.sender-0 { color: #CC5049; } .sender-1 { color: #D67722; } .sender-2 { color: #955CDB; }
.sender-3 { color: #40A920; } .sender-4 { color: #309EBA; } .sender-5 { color: #368AD1; }
.sender-6 { color: #C7508B; }

.msg-time { font-size: 12px; color: #A0ACB6; }
.msg-time-out { font-size: 12px; color: #62AC55; }

.date-pill { background: rgba(255,255,255,0.9); color: #70777B; font-size: 12px;
  font-weight: bold; border-radius: 10px; padding: 2px 10px; }

.fab { background: #FFFFFF; border: 1px solid #E4E9EE; border-radius: 9999px;
  min-width: 54px; min-height: 54px; color: #8E9AA4; }

.composer { background: #FFFFFF; border-top: 1px solid #E4E9EE; }
.composer entry, .composer text { border: none; box-shadow: none; background: transparent; }
`

// LoadCSS installs the app stylesheet on the default display. A non-empty,
// existing wallpaperPath replaces the chat pane's Day gradient (cover).
// Call once, on the UI thread, after GTK initialized.
func LoadCSS(wallpaperPath string) {
	css := themeCSS
	if wallpaperPath != "" {
		if _, err := os.Stat(wallpaperPath); err == nil {
			css += fmt.Sprintf(
				".chat-pane { background-image: url(%q); background-size: cover; }\n",
				"file://"+wallpaperPath)
		}
	}
	p := gtk.NewCSSProvider()
	p.LoadFromString(css)
	gtk.StyleContextAddProviderForDisplay(gdk.DisplayGetDefault(), p, styleProviderPriorityApplication)
}
```

- [ ] **Step 6: Wire into main.go**

In `activate`, right after `cacheDir, _ := config.DefaultCacheDir()` and before any widget is created, add (imports: `"path/filepath"`; `os` already imported):

```go
	wallpaper := ""
	for _, n := range []string{"wallpaper.jpg", "wallpaper.png"} {
		if p := filepath.Join(dir, n); fileExists(p) {
			wallpaper = p
			break
		}
	}
	ui.LoadCSS(wallpaper)
```

And the tiny helper at file scope in `main.go`:

```go
func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
```

- [ ] **Step 7: Build, test, format, commit**

```sh
go build ./... && go test ./...
goimports -w internal/ui/ main.go
git add internal/ui/format.go internal/ui/format_test.go internal/ui/style.go main.go
git commit -m "feat(telegram): Telegram Day theme CSS and pure formatting helpers

One CssProvider carries the spec-pinned palette; format.go holds the
tested time/grouping/initials rules; optional wallpaper.{jpg,png} in the
config dir replaces the Day gradient."
```

---

### Task 3: Sidebar rows, chat header, logout placement

**Files:**
- Modify: `examples/telegram/internal/ui/chat.go`
- Test: `examples/telegram/internal/ui/chat_test.go` (new)

**Interfaces:**
- Consumes: Task 1 Dialog fields; Task 2 helpers + CSS classes.
- Produces (Task 4–5 rely on these): `ChatView.actions ChatActions` field; `ChatView.headerTitle/headerSub *gtk.Label`; `(*ChatView).showDialog(d model.Dialog)` (selection handler body); `setAvatar(lb *gtk.Label, title string, id int64)`.

- [ ] **Step 1: Store actions, add header fields**

In `chat.go`, extend `ChatView`:

```go
type ChatView struct {
	root     *gtk.Box
	banner   *gtk.Revealer
	bannerLb *gtk.Label

	actions ChatActions

	dialogs  *gioutil.ListModel[model.Dialog]
	messages *gioutil.ListModel[model.Message]

	headerTitle *gtk.Label
	headerSub   *gtk.Label

	sideStack *gtk.Stack
	msgStack  *gtk.Stack

	current    model.Dialog
	hasCurrent bool
}
```

First line of `NewChatView` body: `v.actions = actions` (keep the parameter; other closures keep using `actions` where already written).

- [ ] **Step 2: The avatar helper**

Add to `chat.go` (imports gain `"fmt"`, `"time"`):

```go
// setAvatar fills an avatar label: initials text plus the peer's palette
// gradient class (exactly one avatar-N class at a time).
func setAvatar(lb *gtk.Label, title string, id int64) {
	lb.SetText(initials(title))
	for i := 0; i < paletteSize; i++ {
		lb.RemoveCSSClass(fmt.Sprintf("avatar-%d", i))
	}
	lb.AddCSSClass(fmt.Sprintf("avatar-%d", colorIndex(id)))
}
```

- [ ] **Step 3: Rework the dialog factory**

Replace the existing `dlgFactory.ConnectSetup`/`ConnectBind` bodies entirely:

```go
	dlgFactory.ConnectSetup(func(obj *coreglib.Object) {
		item := obj.Cast().(*gtk.ListItem)
		row := gtk.NewBox(gtk.OrientationHorizontal, 10)
		row.SetMarginTop(8)
		row.SetMarginBottom(8)
		row.SetMarginStart(10)
		row.SetMarginEnd(10)

		av := gtk.NewLabel("")
		av.AddCSSClass("avatar")
		av.SetSizeRequest(46, 46)

		mid := gtk.NewBox(gtk.OrientationVertical, 2)
		mid.SetHExpand(true)
		title := gtk.NewLabel("")
		title.SetXAlign(0)
		title.SetEllipsize(pango.EllipsizeEnd)
		title.AddCSSClass("dialog-title")
		preview := gtk.NewLabel("")
		preview.SetXAlign(0)
		preview.SetEllipsize(pango.EllipsizeEnd)
		preview.AddCSSClass("dialog-preview")
		mid.Append(title)
		mid.Append(preview)

		right := gtk.NewBox(gtk.OrientationVertical, 2)
		tm := gtk.NewLabel("")
		tm.SetXAlign(1)
		tm.AddCSSClass("dialog-time")
		pill := gtk.NewLabel("")
		pill.AddCSSClass("unread-pill")
		pill.SetHAlign(gtk.AlignEnd)
		pill.SetVisible(false)
		right.Append(tm)
		right.Append(pill)

		row.Append(av)
		row.Append(mid)
		row.Append(right)
		item.SetChild(row)
	})
	dlgFactory.ConnectBind(func(obj *coreglib.Object) {
		item := obj.Cast().(*gtk.ListItem)
		d := gioutil.ObjectValue[model.Dialog](item.Item())
		row := item.Child().(*gtk.Box)
		av := row.FirstChild().(*gtk.Label)
		mid := av.NextSibling().(*gtk.Box)
		right := mid.NextSibling().(*gtk.Box)
		title := mid.FirstChild().(*gtk.Label)
		preview := title.NextSibling().(*gtk.Label)
		tm := right.FirstChild().(*gtk.Label)
		pill := tm.NextSibling().(*gtk.Label)

		setAvatar(av, d.Title, d.PeerID)
		title.SetText(d.Title)
		preview.SetText(d.Preview)
		tm.SetText(sidebarTime(d.LastDate, time.Now()))
		pill.SetText(strconv.Itoa(d.Unread))
		pill.SetVisible(d.Unread > 0)
	})
```

(`NextSibling()` returns `gtk.Widgetter` whose dynamic type is the concrete wrapper — the same pattern the old code used for `FirstChild()`.)

- [ ] **Step 4: Header + showDialog + logout placement**

In `NewChatView`, build the header and insert it at the top of the right pane, and replace the selection-changed body with a method call:

```go
	// header
	v.headerTitle = gtk.NewLabel("")
	v.headerTitle.SetXAlign(0)
	v.headerTitle.AddCSSClass("chat-title")
	v.headerSub = gtk.NewLabel("")
	v.headerSub.SetXAlign(0)
	v.headerSub.AddCSSClass("chat-subtitle")
	header := gtk.NewBox(gtk.OrientationVertical, 1)
	header.AddCSSClass("chat-header")
	header.SetMarginTop(8)
	header.SetMarginBottom(8)
	header.SetMarginStart(12)
	header.SetMarginEnd(12)
	header.Append(v.headerTitle)
	header.Append(v.headerSub)
```

`right` box order becomes: `right.Append(header)`, then the message area, then the composer.

```go
	dlgSel.ConnectSelectionChanged(func(_, _ uint) {
		i := dlgSel.Selected()
		if i == gtk.InvalidListPosition {
			return
		}
		v.showDialog(v.dialogs.At(int(i)))
	})
```

New method:

```go
// showDialog opens a conversation: header, message stack, history load.
// UI thread only.
func (v *ChatView) showDialog(d model.Dialog) {
	v.current, v.hasCurrent = d, true
	v.headerTitle.SetText(d.Title)
	sub := subtitleText(d)
	v.headerSub.SetText(sub)
	v.headerSub.SetVisible(sub != "")
	v.msgStack.SetVisibleChildName("messages")
	v.actions.LoadHistory(d)
}
```

Logout moves to the bottom of the left pane and gets the flat style:

```go
	logoutBtn := gota.Button("Logout").OnClick(func(*gota.ButtonWidget) { actions.Logout() })
	logoutW := logoutBtn.ToWidget()
	logoutW.AddCSSClass("flat")

	left := gtk.NewBox(gtk.OrientationVertical, 4)
	left.Append(v.sideStack)
	left.Append(logoutW)
```

The paned divider moves to the spec width — find `pane.SetPosition(280)` and change it:

```go
	pane.SetPosition(300)
```

`Reset()` additionally clears the header:

```go
	v.headerTitle.SetText("")
	v.headerSub.SetText("")
	v.headerSub.SetVisible(false)
```

- [ ] **Step 5: GTK tests**

Create `internal/ui/chat_test.go` (package `ui`; the `gtkOK`/`TestMain` guard already exists in `login_test.go`):

```go
// chat_test.go
package ui

import (
	"slices"
	"testing"
	"time"

	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/diamondburned/gotk4/examples/telegram/internal/model"
)

func TestSetAvatarSingleColorClass(t *testing.T) {
	if !gtkOK {
		t.Skip("no display; GTK cannot init")
	}
	lb := gtk.NewLabel("")
	lb.AddCSSClass("avatar")
	setAvatar(lb, "News Channel", 8) // 8 % 7 = 1
	setAvatar(lb, "Alice", 0)        // must swap, not accumulate
	classes := lb.CSSClasses()
	if !slices.Contains(classes, "avatar-0") || slices.Contains(classes, "avatar-1") {
		t.Fatalf("classes = %v, want avatar-0 only", classes)
	}
	if lb.Text() != "A" {
		t.Fatalf("initials = %q", lb.Text())
	}
}

func TestShowDialogHeader(t *testing.T) {
	if !gtkOK {
		t.Skip("no display; GTK cannot init")
	}
	loaded := 0
	v := NewChatView(ChatActions{LoadHistory: func(model.Dialog) { loaded++ }})
	v.showDialog(model.Dialog{Title: "Group X", Kind: model.PeerChat, Members: 12,
		PeerID: 1, CanSend: true, LastDate: time.Now()})
	if v.headerTitle.Text() != "Group X" {
		t.Fatalf("header title = %q", v.headerTitle.Text())
	}
	if v.headerSub.Text() != "12 members" {
		t.Fatalf("header sub = %q", v.headerSub.Text())
	}
	if loaded != 1 {
		t.Fatalf("LoadHistory calls = %d", loaded)
	}
	v.Reset()
	if v.headerTitle.Text() != "" {
		t.Fatal("Reset must clear header")
	}
}
```

(Verify `(*gtk.Widget).CSSClasses() []string` exists via `go doc`; it is the generated getter for `gtk_widget_get_css_classes`.)

- [ ] **Step 6: Build, run all ui tests, format, commit**

```sh
go build ./... && go test ./internal/ui/ -v -count=1 && go test ./...
goimports -w internal/ui/
git add internal/ui/chat.go internal/ui/chat_test.go
git commit -m "feat(telegram): Telegram-style sidebar rows, chat header, bottom logout

Dialog rows gain avatar/title/preview/time/unread-pill layout; the right
pane gains a header driven by showDialog; Logout moves to the sidebar
bottom as a flat button."
```

---

### Task 4: Message bubbles, grouping, date separators, auto-scroll, FAB

**Files:**
- Modify: `examples/telegram/internal/ui/chat.go`
- Delete: `examples/telegram/internal/ui/zz_recycle_check_test.go` (tests the old flat-cell recycle path this task removes; superseded by TestBindMessagePhotoSlot below)
- Test: append to `examples/telegram/internal/ui/chat_test.go`

**Interfaces:**
- Consumes: Tasks 1–3 (Message.Date/SenderID, grouped/dateSeparator/setAvatar/paletteSize, CSS classes, `v.actions`).
- Produces (Task 5 relies on): `ChatView.msgList *gtk.ListView`, `ChatView.msgScroll *gtk.ScrolledWindow`, `ChatView.fab *gtk.Button`, `(*ChatView).bindMessage(c *msgCell, m model.Message, prev, next *model.Message)`, `(*ChatView).atBottom() bool`, `(*ChatView).scrollToBottom()`.

- [ ] **Step 1: Replace the message cell**

Delete the old `messageCell` type, `newMessageCell`, and `bindMessageCell` from `chat.go`. Add:

```go
// msgCell is the recycled widget tree of one message row:
// root(V)[ sepBox(H center)[sep], row(H)[avatar, bubble(V)[sender, text, photos, timeLb]] ]
type msgCell struct {
	root   *gtk.Box
	sepBox *gtk.Box
	sep    *gtk.Label
	row    *gtk.Box
	avatar *gtk.Label
	bubble *gtk.Box
	sender *gtk.Label
	text   *gtk.Label
	photos *gtk.Box
	timeLb *gtk.Label
}

func newMsgCell() *msgCell {
	c := &msgCell{}

	c.sep = gtk.NewLabel("")
	c.sep.AddCSSClass("date-pill")
	c.sepBox = gtk.NewBox(gtk.OrientationHorizontal, 0)
	c.sepBox.SetHAlign(gtk.AlignCenter)
	c.sepBox.SetMarginTop(8)
	c.sepBox.SetMarginBottom(8)
	c.sepBox.Append(c.sep)
	c.sepBox.SetVisible(false)

	c.avatar = gtk.NewLabel("")
	c.avatar.AddCSSClass("avatar")
	c.avatar.SetSizeRequest(33, 33)
	c.avatar.SetVAlign(gtk.AlignEnd)

	c.sender = gtk.NewLabel("")
	c.sender.SetXAlign(0)
	c.sender.AddCSSClass("sender")
	c.text = gtk.NewLabel("")
	c.text.SetXAlign(0)
	c.text.SetWrap(true)
	c.text.SetMaxWidthChars(55)
	c.photos = gtk.NewBox(gtk.OrientationVertical, 4)
	c.timeLb = gtk.NewLabel("")
	c.timeLb.SetXAlign(1)

	c.bubble = gtk.NewBox(gtk.OrientationVertical, 2)
	c.bubble.AddCSSClass("bubble")
	c.bubble.Append(c.sender)
	c.bubble.Append(c.text)
	c.bubble.Append(c.photos)
	c.bubble.Append(c.timeLb)

	c.row = gtk.NewBox(gtk.OrientationHorizontal, 8)
	c.row.SetMarginStart(12)
	c.row.SetMarginEnd(12)
	c.row.Append(c.avatar)
	c.row.Append(c.bubble)

	c.root = gtk.NewBox(gtk.OrientationVertical, 0)
	c.root.Append(c.sepBox)
	c.root.Append(c.row)
	return c
}

// cellOf rebuilds the typed cell view from a recycled root (fixed tree walk).
func cellOf(root *gtk.Box) *msgCell {
	c := &msgCell{root: root}
	c.sepBox = root.FirstChild().(*gtk.Box)
	c.sep = c.sepBox.FirstChild().(*gtk.Label)
	c.row = c.sepBox.NextSibling().(*gtk.Box)
	c.avatar = c.row.FirstChild().(*gtk.Label)
	c.bubble = c.avatar.NextSibling().(*gtk.Box)
	c.sender = c.bubble.FirstChild().(*gtk.Label)
	c.text = c.sender.NextSibling().(*gtk.Label)
	c.photos = c.text.NextSibling().(*gtk.Box)
	c.timeLb = c.photos.NextSibling().(*gtk.Label)
	return c
}
```

- [ ] **Step 2: The bind logic**

```go
// bindMessage renders one message into a (possibly recycled) cell.
// prev/next are the neighboring messages in display order, nil at the edges.
func (v *ChatView) bindMessage(c *msgCell, m model.Message, prev, next *model.Message) {
	first := prev == nil || !grouped(*prev, m)
	last := next == nil || !grouped(m, *next)

	showSep := prev == nil || !sameDay(prev.Date, m.Date)
	if showSep {
		c.sep.SetText(dateSeparator(m.Date, time.Now()))
	}
	c.sepBox.SetVisible(showSep)

	if m.Out {
		c.row.SetHAlign(gtk.AlignEnd)
	} else {
		c.row.SetHAlign(gtk.AlignStart)
	}
	if first {
		c.root.SetMarginTop(8) // gap between groups
	} else {
		c.root.SetMarginTop(2) // gap inside a group
	}

	// Incoming rows always allocate the avatar column so grouped bubbles stay
	// aligned; only the last message of a group actually shows it.
	c.avatar.SetVisible(!m.Out)
	if !m.Out {
		if last {
			setAvatar(c.avatar, m.Sender, m.SenderID)
			c.avatar.SetOpacity(1)
		} else {
			c.avatar.SetOpacity(0)
		}
	}

	showSender := first && !m.Out && m.Sender != ""
	c.sender.SetVisible(showSender)
	if showSender {
		c.sender.SetText(m.Sender)
		for i := 0; i < paletteSize; i++ {
			c.sender.RemoveCSSClass(fmt.Sprintf("sender-%d", i))
		}
		c.sender.AddCSSClass(fmt.Sprintf("sender-%d", colorIndex(m.SenderID)))
	}

	c.text.SetText(m.Text)
	c.text.SetVisible(m.Text != "")

	for _, cl := range []string{"bubble-in", "bubble-out", "r-tl4", "r-bl4", "r-tr4", "r-br4"} {
		c.bubble.RemoveCSSClass(cl)
	}
	if m.Out {
		c.bubble.AddCSSClass("bubble-out")
		if !first {
			c.bubble.AddCSSClass("r-tr4")
		}
		c.bubble.AddCSSClass("r-br4") // tail substitute
	} else {
		c.bubble.AddCSSClass("bubble-in")
		if !first {
			c.bubble.AddCSSClass("r-tl4")
		}
		c.bubble.AddCSSClass("r-bl4") // tail substitute
	}

	c.timeLb.SetText(m.Date.Format("15:04"))
	c.timeLb.RemoveCSSClass("msg-time")
	c.timeLb.RemoveCSSClass("msg-time-out")
	if m.Out {
		c.timeLb.AddCSSClass("msg-time-out")
	} else {
		c.timeLb.AddCSSClass("msg-time")
	}

	for ch := c.photos.FirstChild(); ch != nil; ch = c.photos.FirstChild() {
		c.photos.Remove(ch)
	}
	if m.Photo != nil && m.Photo.SizeType != "" && v.actions.LoadPhoto != nil {
		ref := *m.Photo
		pic := newAsyncPic()
		c.photos.Append(pic)
		pic.load(func() (string, error) { return v.actions.LoadPhoto(ref) })
	}
}
```

Factory hookup replaces the old `msgFactory` bodies:

```go
	msgFactory.ConnectSetup(func(obj *coreglib.Object) {
		item := obj.Cast().(*gtk.ListItem)
		item.SetChild(newMsgCell().root)
	})
	msgFactory.ConnectBind(func(obj *coreglib.Object) {
		item := obj.Cast().(*gtk.ListItem)
		pos := int(item.Position())
		m := gioutil.ObjectValue[model.Message](item.Item())
		var prev, next *model.Message
		if pos > 0 {
			p := v.messages.At(pos - 1)
			prev = &p
		}
		if pos+1 < v.messages.Len() {
			n := v.messages.At(pos + 1)
			next = &n
		}
		v.bindMessage(cellOf(item.Child().(*gtk.Box)), m, prev, next)
	})
```

If `coreglib.ObjectEq` is now unused in chat.go, drop its import (goimports confirms).

- [ ] **Step 3: Scroll machinery + FAB**

`ChatView` gains fields `msgList *gtk.ListView`, `msgScroll *gtk.ScrolledWindow`, `fab *gtk.Button`. In `NewChatView`, keep references (`v.msgList = msgList`, `v.msgScroll = msgScroll`), add `msgList.AddCSSClass("msg-list")`, and wrap the message stack in an overlay:

```go
	overlay := gtk.NewOverlay()
	overlay.SetChild(v.msgStack)
	overlay.SetVExpand(true)
	overlay.AddCSSClass("chat-pane")

	v.fab = gtk.NewButtonFromIconName("go-down-symbolic")
	v.fab.AddCSSClass("fab")
	v.fab.SetHAlign(gtk.AlignEnd)
	v.fab.SetVAlign(gtk.AlignEnd)
	v.fab.SetMarginEnd(16)
	v.fab.SetMarginBottom(16)
	v.fab.SetVisible(false)
	v.fab.ConnectClicked(func() {
		v.scrollToBottom()
		v.fab.SetVisible(false)
	})
	overlay.AddOverlay(v.fab)
```

`right` order becomes: header, overlay, composer. FAB visibility follows the scroll position:

```go
	v.msgScroll.VAdjustment().ConnectValueChanged(func() {
		v.fab.SetVisible(v.messages.Len() > 0 && !v.atBottom())
	})
```

Methods:

```go
// atBottom reports whether the message view is scrolled to (near) the end.
func (v *ChatView) atBottom() bool {
	adj := v.msgScroll.VAdjustment()
	return adj.Value()+adj.PageSize() >= adj.Upper()-60
}

func (v *ChatView) scrollToBottom() {
	if n := v.messages.Len(); n > 0 {
		v.msgList.ScrollTo(uint(n-1), gtk.ListScrollNone, nil)
	}
}
```

`SetMessages` scrolls after the splice settles; `AppendMessage` keeps the view pinned only when already at bottom and refreshes the previous row's group rendering:

```go
func (v *ChatView) SetMessages(d model.Dialog, ms []model.Message) {
	if !v.hasCurrent || v.current.PeerID != d.PeerID {
		return
	}
	v.messages.Splice(0, v.messages.Len(), ms...)
	v.fab.SetVisible(false)
	coreglib.IdleAdd(func() { v.scrollToBottom() })
}

func (v *ChatView) AppendMessage(m model.Message) {
	if v.hasCurrent && v.current.PeerID == m.PeerID {
		wasBottom := v.atBottom()
		v.messages.Append(m)
		if n := v.messages.Len(); n >= 2 {
			v.messages.Set(n-2, v.messages.At(n-2)) // re-render: it may no longer be last-of-group
		}
		if wasBottom {
			coreglib.IdleAdd(func() { v.scrollToBottom() })
		} else {
			v.fab.SetVisible(true)
		}
	}
	for i := 0; i < v.dialogs.Len(); i++ {
		d := v.dialogs.At(i)
		if d.PeerID == m.PeerID {
			d.Preview = m.Text
			d.LastDate = m.Date
			v.dialogs.Set(i, d)
			break
		}
	}
}
```

`Reset()` also hides the FAB: `v.fab.SetVisible(false)`.

- [ ] **Step 4: Delete the obsolete recycle test, add bind tests**

`git rm examples/telegram/internal/ui/zz_recycle_check_test.go`. Append to `chat_test.go`:

```go
func TestBindMessageInOutAndGrouping(t *testing.T) {
	if !gtkOK {
		t.Skip("no display; GTK cannot init")
	}
	v := NewChatView(ChatActions{})
	c := newMsgCell()
	day := time.Date(2026, 7, 2, 10, 0, 0, 0, time.Local)
	alice1 := model.Message{SenderID: 1, Sender: "Alice", Text: "one", Date: day}
	alice2 := model.Message{SenderID: 1, Sender: "Alice", Text: "two", Date: day.Add(time.Minute)}
	mine := model.Message{SenderID: 9, Out: true, Text: "yo", Date: day.Add(2 * time.Minute)}

	// First of a 2-message group: sender shown, no top-near-4 radius, ghost avatar.
	v.bindMessage(c, alice1, nil, &alice2)
	cl := c.bubble.CSSClasses()
	if !slices.Contains(cl, "bubble-in") || slices.Contains(cl, "r-tl4") {
		t.Fatalf("first-of-group classes = %v", cl)
	}
	if !c.sender.Visible() || c.sender.Text() != "Alice" {
		t.Fatal("sender must show on first of group")
	}
	if c.avatar.Opacity() != 0 {
		t.Fatal("non-last incoming message must ghost the avatar")
	}
	if !c.sepBox.Visible() {
		t.Fatal("date separator must show at the top edge")
	}

	// Last of the group: r-tl4 present, avatar visible, sender hidden, no separator.
	v.bindMessage(c, alice2, &alice1, &mine)
	cl = c.bubble.CSSClasses()
	if !slices.Contains(cl, "r-tl4") {
		t.Fatalf("continuation classes = %v, want r-tl4", cl)
	}
	if c.sender.Visible() {
		t.Fatal("sender must hide inside a group")
	}
	if c.avatar.Opacity() != 1 {
		t.Fatal("last-of-group must show the avatar")
	}
	if c.sepBox.Visible() {
		t.Fatal("no separator inside the same day")
	}

	// Outgoing: right-aligned, out classes, avatar hidden entirely.
	v.bindMessage(c, mine, &alice2, nil)
	cl = c.bubble.CSSClasses()
	if !slices.Contains(cl, "bubble-out") || slices.Contains(cl, "bubble-in") {
		t.Fatalf("out classes = %v", cl)
	}
	if c.row.HAlign() != gtk.AlignEnd {
		t.Fatal("outgoing row must right-align")
	}
	if c.avatar.Visible() {
		t.Fatal("outgoing rows have no avatar")
	}
}

func TestBindMessagePhotoSlot(t *testing.T) {
	if !gtkOK {
		t.Skip("no display; GTK cannot init")
	}
	v := NewChatView(ChatActions{LoadPhoto: func(model.PhotoRef) (string, error) { return "", nil }})
	c := newMsgCell()
	day := time.Now()
	withPhoto := model.Message{SenderID: 1, Text: "[photo]", Date: day,
		Photo: &model.PhotoRef{PhotoID: 1, SizeType: "x"}}
	plain := model.Message{SenderID: 1, Text: "no photo", Date: day}

	v.bindMessage(c, withPhoto, nil, nil)
	if c.photos.FirstChild() == nil {
		t.Fatal("photo message must populate the photo slot")
	}
	v.bindMessage(c, plain, nil, nil) // recycled: slot must be cleared
	if c.photos.FirstChild() != nil {
		t.Fatal("recycled cell must clear the photo slot")
	}
}
```

(Verify `(*gtk.Widget).Opacity() float64`, `.Visible() bool`, `.HAlign() gtk.Align` with `go doc` — all are standard generated getters.)

- [ ] **Step 5: Build, test, format, commit**

```sh
go build ./... && go test ./internal/ui/ -v -count=1 && go test ./...
goimports -w internal/ui/
git add internal/ui/chat.go internal/ui/chat_test.go
git rm -q internal/ui/zz_recycle_check_test.go 2>/dev/null || true
git commit -m "feat(telegram): grouped message bubbles, date separators, autoscroll, FAB

Telegram-style bubbles (in white / out green, 4px tail-substitute
corners, sender colors, in-bubble timestamps), day separators from
neighbor peeking at bind time, scroll-to-bottom on open, pinned-follow
on append, and a floating jump-to-bottom button."
```

---

### Task 5: Composer states, rebuild, manual E2E handoff

**Files:**
- Modify: `examples/telegram/internal/ui/chat.go`
- Test: append to `examples/telegram/internal/ui/chat_test.go`

**Interfaces:**
- Consumes: everything above.
- Produces: `ChatView.composer *gota.EntryWidget`, `ChatView.sendBtn *gtk.Button`, `(*ChatView).composerFor(d model.Dialog)`.

- [ ] **Step 1: Rework the composer**

`ChatView` gains `composer *gota.EntryWidget` and `sendBtn *gtk.Button`. In `NewChatView`, replace the current composer block:

```go
	v.composer = gota.Entry().SetPlaceholder("Message")
	v.composer.OnEnter(func(*gota.EntryWidget) { v.submitComposer() })
	entryW := v.composer.ToWidget()
	entryW.SetHExpand(true)

	v.sendBtn = gtk.NewButtonWithLabel("Send")
	v.sendBtn.AddCSSClass("suggested-action")
	v.sendBtn.ConnectClicked(func() { v.submitComposer() })

	comp := gtk.NewBox(gtk.OrientationHorizontal, 8)
	comp.AddCSSClass("composer")
	comp.SetMarginTop(8)
	comp.SetMarginBottom(8)
	comp.SetMarginStart(8)
	comp.SetMarginEnd(8)
	comp.Append(entryW)
	comp.Append(v.sendBtn)

	// initial state: nothing selected yet
	entryW.SetSensitive(false)
	v.sendBtn.SetSensitive(false)
```

`right` order: header, overlay, `comp`. New methods:

```go
func (v *ChatView) submitComposer() {
	text := v.composer.GetText()
	if text == "" || !v.hasCurrent || !v.current.CanSend {
		return
	}
	v.composer.SetText("")
	v.actions.Send(v.current, text)
}

// composerFor enables/disables input to match the open conversation.
func (v *ChatView) composerFor(d model.Dialog) {
	if d.CanSend {
		v.composer.SetPlaceholder("Message")
	} else {
		v.composer.SetPlaceholder("Read-only channel")
	}
	v.composer.ToWidget().SetSensitive(d.CanSend)
	v.sendBtn.SetSensitive(d.CanSend)
}
```

`showDialog` calls `v.composerFor(d)` (after setting the header). `Reset()` disables both again:

```go
	v.composer.ToWidget().SetSensitive(false)
	v.sendBtn.SetSensitive(false)
	v.composer.SetPlaceholder("Message")
```

(Verify `gtk.NewButtonWithLabel` exists — standard constructor.)

- [ ] **Step 2: Tests**

Append to `chat_test.go`:

```go
func TestComposerFollowsCanSend(t *testing.T) {
	if !gtkOK {
		t.Skip("no display; GTK cannot init")
	}
	sent := 0
	v := NewChatView(ChatActions{
		LoadHistory: func(model.Dialog) {},
		Send:        func(model.Dialog, string) { sent++ },
	})
	if v.sendBtn.Sensitive() {
		t.Fatal("composer must start disabled")
	}
	v.showDialog(model.Dialog{Title: "RO", Kind: model.PeerChannel, PeerID: 1, CanSend: false})
	if v.sendBtn.Sensitive() || v.composer.ToWidget().Sensitive() {
		t.Fatal("read-only channel must disable the composer")
	}
	v.showDialog(model.Dialog{Title: "G", Kind: model.PeerChat, PeerID: 2, CanSend: true})
	if !v.sendBtn.Sensitive() {
		t.Fatal("writable chat must enable the composer")
	}
	v.composer.SetText("hi")
	v.submitComposer()
	if sent != 1 || v.composer.GetText() != "" {
		t.Fatalf("submit: sent=%d text=%q", sent, v.composer.GetText())
	}
}
```

- [ ] **Step 3: Full suite + rebuild**

```sh
go build ./... && go test ./... -count=1
go build -o telegram .
git status --porcelain | grep -v '.DS_Store\|.memsearch\|CLAUDE-FABLE'   # binary is gitignored
```

Expected: builds exit 0, all packages ok, tree clean apart from noise.

- [ ] **Step 4: Format and commit**

```sh
goimports -w internal/ui/
git add internal/ui/chat.go internal/ui/chat_test.go
git commit -m "feat(telegram): composer with Send button and read-only state

Entry + suggested-action Send button in a white composer bar; disabled
with a Read-only channel placeholder for broadcast channels and before
any chat is selected."
```

- [ ] **Step 5: Manual E2E handoff (user runs; no credentials in automation)**

Report to the user: run `./telegram`, verify against tdesktop side-by-side — sidebar rows (avatar/pill/time), header, white/green grouped bubbles with 4px tail corners, date pills, sender colors, auto-scroll + FAB, read-only composer on broadcast channels, optional `wallpaper.jpg` in the config dir. Screenshots feed the README and the PR evidence.
