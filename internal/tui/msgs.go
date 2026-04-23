package tui

import (
	"sniffer-tui/internal/capture"
	"sniffer-tui/internal/watch"
)

// Cross-cutting messages sent from capture / watcher / decode goroutines.

type BatchMessagesMsg []capture.CapturedMessage

type ReloadMsg struct{ Kind watch.Kind }

// RegistryKind indicates which schema was reloaded.
type RegistryKind uint8

const (
	RegConnection RegistryKind = iota
	RegGame
)

func (k RegistryKind) String() string {
	if k == RegConnection {
		return "connection"
	}
	return "game"
}

type RegistryReloadedMsg struct {
	Which   RegistryKind
	Err     error
	Count   int
	HasEnv  bool
	Version uint64
}

type MappingsReloadedMsg struct {
	Err error
}

type DecodedMsg struct {
	Seq     uint64
	Version uint64
	Text    string
	Err     error
}

type StatusLevel uint8

const (
	StatusInfo StatusLevel = iota
	StatusWarn
	StatusErr
)

type StatusMsg struct {
	Level StatusLevel
	Text  string
}

type TickDropMsg struct{ Dropped uint64 }

// NewGameIPMsg is sent by the capture classifier when it observes a remote IP
// that isn't a known connection server. The TUI persists the IP in config.
type NewGameIPMsg struct{ IP string }
