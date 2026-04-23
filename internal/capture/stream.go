package capture

import (
	"encoding/binary"
	"net"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/reassembly"
)

// Envelope abstracts the per-frame unwrap so the capture layer doesn't have a
// hard dependency on the protoreg package. protoreg.Registry satisfies it.
//
// HasEnvelope reports whether descriptors are actually loaded; when false,
// the capture layer skips framing validation (user hasn't configured protos
// yet) so we still surface raw frames to the list.
type Envelope interface {
	ExtractEnvelope(*CapturedMessage)
	HasEnvelope() bool
}

// MaxFrameSize bounds the VarInt-declared frame length. Any candidate frame
// claiming to be larger is treated as a resync signal rather than buffered.
// 256 KiB is an order of magnitude above real Dofus frames and small enough
// to reject pathological garbage within a few bytes.
const MaxFrameSize = 256 * 1024

// tcpStream handles one TCP connection (both directions). gopacket/reassembly
// creates a single Stream per connection and reuses it for both halves,
// signalling direction via sg.Info(). Each direction gets its own fragment
// buffer so client and server framing never interleave.
type tcpStream struct {
	out        chan<- CapturedMessage
	env        Envelope
	bufC2S     []byte // buffer for the "internal c2s" half (reassembly's own labeling)
	bufS2C     []byte
	net        gopacket.Flow
	tp         gopacket.Flow
	firstSrc   uint16 // src port of the first packet — lets us map c2s/s2c to real direction
	serverPort uint16
	kind       Kind
	remote     net.IP
	onDrop     func()
}

func (s *tcpStream) Accept(_ *layers.TCP, _ gopacket.CaptureInfo, _ reassembly.TCPFlowDirection, _ reassembly.Sequence, _ *bool, _ reassembly.AssemblerContext) bool {
	return true
}

func (s *tcpStream) ReassembledSG(sg reassembly.ScatterGather, _ reassembly.AssemblerContext) {
	length, _ := sg.Lengths()
	if length == 0 {
		return
	}
	dirInternal, _, _, _ := sg.Info()

	// Map reassembly's internal c2s/s2c to the real server/client direction.
	// dirInternal == TCPDirClientToServer means "same direction as the first
	// packet seen on this connection". So:
	//   isFirstPacketDir == (firstSrc == serverPort) → this half is Server→Client.
	isFirstPacketDir := dirInternal == reassembly.TCPDirClientToServer
	firstWasFromServer := s.firstSrc == s.serverPort
	isFromServer := isFirstPacketDir == firstWasFromServer

	dir := ClientToServer
	if isFromServer {
		dir = ServerToClient
	}
	buf := &s.bufC2S
	if !isFirstPacketDir {
		buf = &s.bufS2C
	}

	data := sg.Fetch(length)
	*buf = append(*buf, data...)

	validate := s.env != nil && s.env.HasEnvelope()

	for {
		if len(*buf) == 0 {
			return
		}
		size, n := binary.Uvarint(*buf)
		if n == 0 {
			// Incomplete varint — wait for more bytes, unless the buffer is
			// already longer than a varint can be (malformed).
			if len(*buf) >= binary.MaxVarintLen64 {
				*buf = (*buf)[1:]
				continue
			}
			return
		}
		if n < 0 || size == 0 || size > MaxFrameSize {
			// Overflow or unreasonable size — slide one byte forward and
			// try again. This is the resync path: when the sniffer joins an
			// existing TCP stream, the first bytes mid-frame decode as
			// garbage, and this loop advances until a real VarInt boundary.
			*buf = (*buf)[1:]
			continue
		}
		if uint64(len(*buf)-n) < size {
			return
		}
		end := uint64(n) + size
		payload := make([]byte, size)
		copy(payload, (*buf)[n:end])

		cm := CapturedMessage{
			Time:   time.Now(),
			Dir:    dir,
			Kind:   s.kind,
			Remote: s.remote,
			Raw:    payload,
			Size:   int(size),
		}
		if s.env != nil {
			s.env.ExtractEnvelope(&cm)
			if validate && cm.Err != nil {
				// Envelope is configured but this frame didn't decode as the
				// configured top-level message — we're out of sync. Drop the
				// candidate and slide forward one byte.
				*buf = (*buf)[1:]
				continue
			}
		}

		select {
		case s.out <- cm:
		default:
			if s.onDrop != nil {
				s.onDrop()
			}
		}
		*buf = (*buf)[end:]
	}
}

func (s *tcpStream) ReassemblyComplete(_ reassembly.AssemblerContext) bool {
	return true
}
