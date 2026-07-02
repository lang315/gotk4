// model.go
package model

import (
	"strings"
	"time"
)

// OneLine collapses every whitespace run to a single space — the one rule for
// rendering message text as a single-line sidebar preview (initial dialog
// load and live updates must agree).
func OneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

type PeerKind int

const (
	PeerUser    PeerKind = iota
	PeerChat             // basic group
	PeerChannel          // supergroup or broadcast channel
)

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

type PhotoRef struct {
	PhotoID    int64
	AccessHash int64
	FileRef    []byte
	SizeType   string   // Telegram size letter, e.g. "x"
	MsgID      int      // for FILE_REFERENCE_EXPIRED refetch
	PeerID     int64    // refetch routing:
	Kind       PeerKind // channels need ChannelsGetMessages
}

type PromptKind int

const (
	PromptPhone PromptKind = iota
	PromptCode
	PromptPassword
)

type EventKind int

const (
	EventReady EventKind = iota
	EventConnecting
	EventAuthNeeded // session absent/invalid → show login
	EventNewMessage
)

type Event struct {
	Kind EventKind
	Msg  Message // EventNewMessage
}
