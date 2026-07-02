// chat.go
package ui

import (
	"fmt"
	"strconv"
	"time"

	"github.com/diamondburned/gotk4/pkg/core/gioutil"
	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gota"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/diamondburned/gotk4/pkg/pango"

	"github.com/diamondburned/gotk4/examples/telegram/internal/model"
)

type ChatActions struct {
	LoadHistory func(d model.Dialog)
	Send        func(d model.Dialog, text string)
	Logout      func()
	LoadPhoto   PhotoLoader
}

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
	msgList   *gtk.ListView
	msgScroll *gtk.ScrolledWindow
	fab       *gtk.Button

	current    model.Dialog
	hasCurrent bool
}

// setAvatar fills an avatar label: initials text plus the peer's palette
// gradient class (exactly one avatar-N class at a time).
func setAvatar(lb *gtk.Label, title string, id int64) {
	lb.SetText(initials(title))
	for i := 0; i < paletteSize; i++ {
		lb.RemoveCSSClass(fmt.Sprintf("avatar-%d", i))
	}
	lb.AddCSSClass(fmt.Sprintf("avatar-%d", colorIndex(id)))
}

func NewChatView(actions ChatActions) *ChatView {
	v := &ChatView{
		dialogs:  gioutil.NewListModel[model.Dialog](),
		messages: gioutil.NewListModel[model.Message](),
	}
	v.actions = actions

	// banner
	v.bannerLb = gtk.NewLabel("")
	v.banner = gtk.NewRevealer()
	v.banner.SetChild(v.bannerLb)
	v.banner.SetRevealChild(false)

	// sidebar
	dlgFactory := gtk.NewSignalListItemFactory()
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
	dlgSel := gtk.NewSingleSelection(v.dialogs.ListModel)
	dlgSel.SetAutoselect(false)
	dlgList := gtk.NewListView(dlgSel, &dlgFactory.ListItemFactory)

	// message pane
	msgFactory := gtk.NewSignalListItemFactory()
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
	msgSel := gtk.NewNoSelection(v.messages.ListModel)
	msgList := gtk.NewListView(msgSel, &msgFactory.ListItemFactory)
	msgList.AddCSSClass("msg-list")

	// composer (gota Entry is self-contained → ToWidget into the raw box)
	composer := gota.Entry().SetPlaceholder("Message")
	composer.OnEnter(func(e *gota.EntryWidget) {
		text := e.GetText()
		if text == "" || !v.hasCurrent || !v.current.CanSend {
			return
		}
		e.SetText("")
		actions.Send(v.current, text)
	})

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

	logoutBtn := gota.Button("Logout").OnClick(func(*gota.ButtonWidget) { actions.Logout() })
	logoutW := logoutBtn.ToWidget()
	logoutW.AddCSSClass("flat")

	// layout: raw gtk (gota containers only accept gota.IWidget)
	msgScroll := gtk.NewScrolledWindow()
	msgScroll.SetChild(msgList)
	msgScroll.SetVExpand(true)
	v.msgList = msgList
	v.msgScroll = msgScroll

	msgEmpty := gtk.NewLabel("Select a chat")
	v.msgStack = gtk.NewStack()
	v.msgStack.SetVExpand(true)
	v.msgStack.AddNamed(msgScroll, "messages")
	v.msgStack.AddNamed(msgEmpty, "empty")
	v.msgStack.SetVisibleChildName("empty")

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

	v.msgScroll.VAdjustment().ConnectValueChanged(func() {
		v.fab.SetVisible(v.messages.Len() > 0 && !v.atBottom())
	})

	right := gtk.NewBox(gtk.OrientationVertical, 4)
	right.Append(header)
	right.Append(overlay)
	right.Append(composer.ToWidget())

	dlgScroll := gtk.NewScrolledWindow()
	dlgScroll.SetChild(dlgList)
	dlgScroll.SetVExpand(true)

	sideEmpty := gtk.NewLabel("No chats yet")
	v.sideStack = gtk.NewStack()
	v.sideStack.SetVExpand(true)
	v.sideStack.AddNamed(dlgScroll, "list")
	v.sideStack.AddNamed(sideEmpty, "empty")
	v.sideStack.SetVisibleChildName("empty")

	left := gtk.NewBox(gtk.OrientationVertical, 4)
	left.Append(v.sideStack)
	left.Append(logoutW)

	// ordering: v.msgStack and v.sideStack are both assigned above, so this
	// handler can safely reference them.
	dlgSel.ConnectSelectionChanged(func(_, _ uint) {
		i := dlgSel.Selected()
		if i == gtk.InvalidListPosition {
			return
		}
		v.showDialog(v.dialogs.At(int(i)))
	})

	pane := gtk.NewPaned(gtk.OrientationHorizontal)
	pane.SetStartChild(left)
	pane.SetEndChild(right)
	pane.SetPosition(300)
	pane.SetVExpand(true)

	v.root = gtk.NewBox(gtk.OrientationVertical, 0)
	v.root.Append(v.banner)
	v.root.Append(pane)
	return v
}

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

func (v *ChatView) Widget() gtk.Widgetter { return v.root }

func (v *ChatView) SetBanner(text string) {
	v.bannerLb.SetText(text)
	v.banner.SetRevealChild(text != "")
}

func (v *ChatView) SetDialogs(ds []model.Dialog) {
	v.dialogs.Splice(0, v.dialogs.Len(), ds...)
	if len(ds) == 0 {
		v.sideStack.SetVisibleChildName("empty")
	} else {
		v.sideStack.SetVisibleChildName("list")
	}
}

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

// Reset clears the open conversation and returns to empty placeholders.
// Call on the UI thread (e.g. after logout).
func (v *ChatView) Reset() {
	v.current = model.Dialog{}
	v.hasCurrent = false
	v.messages.Splice(0, v.messages.Len())
	v.dialogs.Splice(0, v.dialogs.Len())
	v.msgStack.SetVisibleChildName("empty")
	v.sideStack.SetVisibleChildName("empty")
	v.fab.SetVisible(false)
	v.SetBanner("")
	v.headerTitle.SetText("")
	v.headerSub.SetText("")
	v.headerSub.SetVisible(false)
}
