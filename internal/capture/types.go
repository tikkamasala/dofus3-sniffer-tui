package capture

import (
	"net"
	"time"
)

type Direction uint8

const (
	ClientToServer Direction = iota
	ServerToClient
)

func (d Direction) String() string {
	switch d {
	case ServerToClient:
		return "Server"
	default:
		return "Client"
	}
}

// Kind identifies which schema was used to decode a message. One TCP
// connection maps to one Kind for its entire lifetime.
type Kind uint8

const (
	KindUnknown Kind = iota
	KindConnection
	KindGame
)

func (k Kind) String() string {
	switch k {
	case KindConnection:
		return "Conn"
	case KindGame:
		return "Game"
	default:
		return "?"
	}
}

// CapturedMessage is one framed protobuf payload pulled off the wire.
//
// The capture pipeline fills Raw/Size/Time/Dir/Kind/Remote and attempts
// cheap envelope extraction (TypeUrl + InnerRaw). Full decoding of InnerRaw
// happens lazily in the TUI when the message becomes the selected item.
type CapturedMessage struct {
	Time     time.Time
	Dir      Direction
	Kind     Kind
	Remote   net.IP
	Raw      []byte
	Size     int
	TypeUrl  string
	InnerRaw []byte
	Err      error
}
