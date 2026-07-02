// chat.go
package ui

import (
	"strconv"

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
}

type ChatView struct {
	root     *gtk.Box
	banner   *gtk.Revealer
	bannerLb *gtk.Label

	dialogs  *gioutil.ListModel[model.Dialog]
	messages *gioutil.ListModel[model.Message]

	current    model.Dialog
	hasCurrent bool
}

func NewChatView(actions ChatActions) *ChatView {
	v := &ChatView{
		dialogs:  gioutil.NewListModel[model.Dialog](),
		messages: gioutil.NewListModel[model.Message](),
	}

	// banner
	v.bannerLb = gtk.NewLabel("")
	v.banner = gtk.NewRevealer()
	v.banner.SetChild(v.bannerLb)
	v.banner.SetRevealChild(false)

	// sidebar
	dlgFactory := gtk.NewSignalListItemFactory()
	dlgFactory.ConnectSetup(func(obj *coreglib.Object) {
		item := obj.Cast().(*gtk.ListItem)
		lb := gtk.NewLabel("")
		lb.SetXAlign(0)
		lb.SetEllipsize(pango.EllipsizeEnd)
		item.SetChild(lb)
	})
	dlgFactory.ConnectBind(func(obj *coreglib.Object) {
		item := obj.Cast().(*gtk.ListItem)
		d := gioutil.ObjectValue[model.Dialog](item.Item())
		text := d.Title
		if d.Unread > 0 {
			text += " (" + strconv.Itoa(d.Unread) + ")"
		}
		if d.Preview != "" {
			text += "\n" + d.Preview
		}
		item.Child().(*gtk.Label).SetText(text) // plain text only
	})
	dlgSel := gtk.NewSingleSelection(v.dialogs.ListModel)
	dlgSel.SetAutoselect(false)
	dlgSel.ConnectSelectionChanged(func(_, _ uint) {
		i := dlgSel.Selected()
		if i == gtk.InvalidListPosition {
			return
		}
		d := v.dialogs.At(int(i))
		v.current, v.hasCurrent = d, true
		actions.LoadHistory(d)
	})
	dlgList := gtk.NewListView(dlgSel, &dlgFactory.ListItemFactory)

	// message pane
	msgFactory := gtk.NewSignalListItemFactory()
	msgFactory.ConnectSetup(func(obj *coreglib.Object) {
		item := obj.Cast().(*gtk.ListItem)
		item.SetChild(newMessageCell().root)
	})
	msgFactory.ConnectBind(func(obj *coreglib.Object) {
		item := obj.Cast().(*gtk.ListItem)
		m := gioutil.ObjectValue[model.Message](item.Item())
		bindMessageCell(item.Child().(*gtk.Box), m, nil) // photo loader arrives in Task 10
	})
	msgSel := gtk.NewNoSelection(v.messages.ListModel)
	msgList := gtk.NewListView(msgSel, &msgFactory.ListItemFactory)

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

	logoutBtn := gota.Button("Logout").OnClick(func(*gota.ButtonWidget) { actions.Logout() })

	// layout: raw gtk (gota containers only accept gota.IWidget)
	msgScroll := gtk.NewScrolledWindow()
	msgScroll.SetChild(msgList)
	msgScroll.SetVExpand(true)

	right := gtk.NewBox(gtk.OrientationVertical, 4)
	right.Append(msgScroll)
	right.Append(composer.ToWidget())

	dlgScroll := gtk.NewScrolledWindow()
	dlgScroll.SetChild(dlgList)
	dlgScroll.SetVExpand(true)

	left := gtk.NewBox(gtk.OrientationVertical, 4)
	left.Append(logoutBtn.ToWidget())
	left.Append(dlgScroll)

	pane := gtk.NewPaned(gtk.OrientationHorizontal)
	pane.SetStartChild(left)
	pane.SetEndChild(right)
	pane.SetPosition(280)
	pane.SetVExpand(true)

	v.root = gtk.NewBox(gtk.OrientationVertical, 0)
	v.root.Append(v.banner)
	v.root.Append(pane)
	return v
}

// newMessageCell / bindMessageCell live in photo.go from Task 10 on; until
// then define them here minimally:
type messageCell struct{ root *gtk.Box }

func newMessageCell() *messageCell {
	box := gtk.NewBox(gtk.OrientationVertical, 4)
	lb := gtk.NewLabel("")
	lb.SetXAlign(0)
	lb.SetWrap(true)
	box.Append(lb)
	return &messageCell{root: box}
}

func bindMessageCell(box *gtk.Box, m model.Message, _ interface{}) {
	lb := box.FirstChild().(*gtk.Label)
	prefix := m.Sender
	if m.Out {
		prefix = "me"
	}
	lb.SetText(prefix + ": " + m.Text)
}

func (v *ChatView) Widget() gtk.Widgetter { return v.root }

func (v *ChatView) SetBanner(text string) {
	v.bannerLb.SetText(text)
	v.banner.SetRevealChild(text != "")
}

func (v *ChatView) SetDialogs(ds []model.Dialog) {
	v.dialogs.Splice(0, v.dialogs.Len(), ds...)
}

func (v *ChatView) SetMessages(d model.Dialog, ms []model.Message) {
	if !v.hasCurrent || v.current.PeerID != d.PeerID {
		return
	}
	v.messages.Splice(0, v.messages.Len(), ms...)
}

func (v *ChatView) AppendMessage(m model.Message) {
	if v.hasCurrent && v.current.PeerID == m.PeerID {
		v.messages.Append(m)
	}
	for i := 0; i < v.dialogs.Len(); i++ {
		d := v.dialogs.At(i)
		if d.PeerID == m.PeerID {
			d.Preview = m.Text
			v.dialogs.Set(i, d)
			break
		}
	}
}
