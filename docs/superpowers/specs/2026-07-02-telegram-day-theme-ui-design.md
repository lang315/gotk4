# Telegram Day Theme Chat UI — Design Spec

Redesign the chat screen of `examples/telegram` to match Telegram Desktop's
Day (light) theme as closely as GTK4 CSS allows. "Match" means: same palette,
same layout metrics, same message-grouping behavior, same visual hierarchy.

## Goals

- Sidebar, header, message bubbles, composer, and scroll behavior visually
  faithful to Telegram Desktop Day theme (reference: user's screenshot of
  tdesktop on macOS).
- Fix all defects visible in the current UI: empty-sender `": text"` rows,
  multi-line preview overflow, no in/out distinction, no timestamps, no chat
  header, no auto-scroll, oversized Logout button, missing Send button.

## Non-goals (v2+, listed so nobody "helpfully" adds them)

Topics bar (forum supergroups), pinned-message bar, sticker rendering
(show `[sticker]` via existing `[media]` fallback), attachment/emoji/voice
controls, search, collapsed avatar-only rail, custom-drawn bubble tails,
unread badge on the FAB, mute state (all badges render green).

## Fidelity contract (approved by user)

Exact: palette, metrics, layout, grouping. Approximated: bubble tails →
asymmetric corner radius (4px near-edge corner); wallpaper → user-supplied
image, else flat Day gradient; font rendering is the OS's. Omitted: the
non-goal features above.

## Palette (pinned; all values are THE spec — do not "improve")

| Token | Hex | Use |
|---|---|---|
| `bgWindow` | `#FFFFFF` | window, sidebar, header, composer |
| `bgChatFrom` | `#DBE7F0` | chat pane gradient top |
| `bgChatTo` | `#E9EFF4` | chat pane gradient bottom |
| `bubbleIn` | `#FFFFFF` | incoming bubble |
| `bubbleOut` | `#EFFDDE` | outgoing bubble |
| `textPrimary` | `#000000` | message text, titles |
| `textSecondary` | `#8E9AA4` | previews, subtitles, FAB chevron |
| `timeGray` | `#A0ACB6` | timestamps (in-bubble and sidebar) |
| `timeOutGreen` | `#62AC55` | timestamp inside outgoing bubble |
| `accent` | `#168ACD` | links, focus |
| `selBlue` | `#419FD9` | selected dialog row bg (text turns white) |
| `hoverRow` | `#F1F4F7` | dialog row hover |
| `unreadGreen` | `#4DCD5E` | unread pill bg (text white) |
| `border` | `#E4E9EE` | header/composer hairlines, FAB border |
| `datePillBg` | `rgba(255,255,255,0.9)` | date separator pill (text `#70777B`) |

Sender-name colors (index = `abs(senderID) % 7`):
`#CC5049 #D67722 #955CDB #40A920 #309EBA #368AD1 #C7508B`

Avatar gradient pairs, same index rule (top→bottom):
`#FF885E→#FF516A, #FFCD6A→#FFA85C, #82B1FF→#665FFF, #A0DE7E→#54CB68,
#53EDD6→#28C9B7, #72D5FD→#2A9EF1, #E0A2F3→#D669ED`

## Metrics (pinned)

Sidebar width 300px (Paned position). Dialog row: 8px vertical / 10px
horizontal padding; avatar 46px circle; title 14px bold; preview 13px
`textSecondary`, single line, ellipsized, newlines flattened to spaces; time
12px `timeGray` top-right; unread pill bottom-right, 12px white text, 10px
corner radius, min-width 20px.

Message list: bubble max width 430px (`SetMaxWidthChars` ≈ 55 + wrap);
padding 8px×12px; corner radius 18px, except the near-edge corner (left for
incoming, right for outgoing): 4px when the message continues a group, and
bottom-near always 4px (tail substitute); vertical gap 2px inside a group,
8px between groups; sender name 13px bold colored, FIRST message of group
only; avatar 33px beside LAST message of group only (incoming only); time
12px inside bubble bottom-right (`timeGray` in / `timeOutGreen` out).

Grouping rule: same `SenderID`, same `Out`, same calendar day as previous
message. Date separator: centered pill when calendar day differs from
previous message.

Header: 56px tall, title 14px bold, subtitle 13px `textSecondary` — for
groups/channels with known member count: `N members`; else `channel` /
`group` / the user's `@username` / empty. Composer: white, top hairline,
frameless entry (placeholder "Message"), `Send` button `suggested-action`;
both disabled with placeholder "Read-only channel" when `!CanSend`.
Logout: small flat button pinned to sidebar bottom. FAB: 54px white circle,
`border` hairline, chevron-down, bottom-right of message pane (16px
margins), visible only when scrolled up from bottom.

## Behaviors

- Opening a chat scrolls to the newest message. A new message auto-scrolls
  only if the view is already at (or within ~1 row of) the bottom; otherwise
  the FAB appears. FAB click scrolls to bottom.
- `AppendMessage` re-binds the previous row (Set same value) so its
  group-position rendering (avatar/corners) updates when it stops being the
  last of its group.
- Sidebar time format (tdesktop rule): today `15:04`; past 7 days `Mon`;
  older `02.01.06`.
- Wallpaper: if `<config dir>/wallpaper.jpg` or `.png` exists at startup, it
  becomes the chat pane `background-image` (cover). Else the Day gradient.

## Data & architecture changes

- `model.Message` += `Date time.Time`, `SenderID int64`. `model.Dialog` +=
  `LastDate time.Time`.
- `mapMessage`: fill Date (`time.Unix(int64(m.Date), 0)`), SenderID; when a
  channel post has no `FromID`, Sender = the channel's title (kills the
  `": text"` bug). `mapDialogs`: fill LastDate from the top message.
- `peerStore` += `Members(kind, id) int` (0 = unknown) from
  `Chat.ParticipantsCount` / `Channel.ParticipantsCount` (flag-guarded).
- `previewText`: flatten newlines to spaces.
- New `internal/ui/style.go`: the full CSS string (all classes above),
  `LoadCSS(wallpaperPath string)` installing one `gtk.CssProvider` for the
  default display; called once from `activate`.
- New `internal/ui/format.go`: pure helpers — `initials(title)`,
  `colorIndex(id)`, `sidebarTime(t, now)`, `sameDay(a,b)`,
  `dateSeparator(t, now)`, `bubbleClasses(prev, cur, next)` — all unit-tested
  without GTK.
- `internal/ui/chat.go`: reworked rows (dialog + message cells), header,
  FAB overlay, composer states. `login.go`/`setup.go` untouched except any
  shared CSS classes.

## Verified-API risk list (plan must verify each with `go doc`/scratch run before use — precedent: `Widget.Activate()` was a documented-looking no-op)

`gtk.CssProvider` load method name (4.22 deprecates LoadFromData),
`gtk.StyleContextAddProviderForDisplay`, `gdk.DisplayGetDefault`,
`ListView.ScrollTo` (4.12+), `gtk.Overlay.AddOverlay`, `ScrolledWindow.VAdjustment`
change notifications, `Widget.AddCSSClass/RemoveCSSClass`,
`Label.SetMaxWidthChars` + `SetWrap` interaction, `gioutil.ListModel.Set`
rebind semantics.

## Testing

Pure helpers: table-driven unit tests (no GTK). Widget behavior: GTK tests
behind the existing single-shot `gtkOK`/`TestMain` guard — bubble class
assignment in/out/grouped, unread pill visibility, composer disabled state,
sidebar row text flattening. Visual fidelity: user's manual screenshot
comparison against tdesktop is the acceptance gate.
