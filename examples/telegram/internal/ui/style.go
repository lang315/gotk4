// style.go
package ui

import (
	"fmt"
	"os"

	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

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
/* exactly paletteSize (format.go) slots each: avatar-0..6, sender-0..6 */
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
	gtk.StyleContextAddProviderForDisplay(gdk.DisplayGetDefault(), p, gtk.STYLE_PROVIDER_PRIORITY_APPLICATION)
}
