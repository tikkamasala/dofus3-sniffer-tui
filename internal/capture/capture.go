package capture

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcap"
	"github.com/google/gopacket/reassembly"
)

type Options struct {
	Device     string
	ServerPort uint16
	BufferSize int
	Classifier *Classifier
	// KnownIPs are added to the BPF filter as "host <ip>" clauses so the
	// sniffer captures traffic to/from these peers regardless of port. This
	// matters when game servers run on ports other than ServerPort.
	KnownIPs []net.IP
}

// Session owns the pcap handle and reassembler for one capture run. Messages
// flow out on Messages(); call Stop() to tear everything down.
type Session struct {
	opts      Options
	handle    *pcap.Handle
	out       chan CapturedMessage
	factory   *streamFactory
	cancel    context.CancelFunc
	done      chan struct{}
	bytesRcvd atomic.Uint64
}

func Start(ctx context.Context, opts Options) (*Session, error) {
	if opts.Device == "" {
		return nil, fmt.Errorf("capture: Device is empty")
	}
	if opts.ServerPort == 0 {
		return nil, fmt.Errorf("capture: ServerPort is zero")
	}
	if opts.BufferSize <= 0 {
		opts.BufferSize = 4096
	}

	handle, err := pcap.OpenLive(opts.Device, 1600, true, pcap.BlockForever)
	if err != nil {
		return nil, fmt.Errorf("pcap.OpenLive: %w", err)
	}
	bpf := buildBPF(opts.ServerPort, opts.KnownIPs)
	if err := handle.SetBPFFilter(bpf); err != nil {
		handle.Close()
		return nil, fmt.Errorf("SetBPFFilter(%q): %w", bpf, err)
	}

	out := make(chan CapturedMessage, opts.BufferSize)
	factory := newStreamFactory(out, opts.Classifier, opts.ServerPort)
	pool := reassembly.NewStreamPool(factory)
	assembler := reassembly.NewAssembler(pool)

	runCtx, cancel := context.WithCancel(ctx)
	s := &Session{
		opts:    opts,
		handle:  handle,
		out:     out,
		factory: factory,
		cancel:  cancel,
		done:    make(chan struct{}),
	}

	go s.run(runCtx, assembler)
	return s, nil
}

func (s *Session) Messages() <-chan CapturedMessage { return s.out }

func (s *Session) Dropped() uint64 { return s.factory.Dropped() }

// BytesReceived is the running total of TCP payload bytes passed to the
// reassembler. Useful for diagnosing "pcap sees nothing" vs "pcap sees bytes
// but framing never locks" — if this is zero while traffic is flowing, the
// BPF filter is wrong; if it climbs but no messages appear, look at framing.
func (s *Session) BytesReceived() uint64 { return s.bytesRcvd.Load() }

// BPF returns the BPF filter string this session was built with. Exposed for
// the status line so the user can sanity-check the filter.
func (s *Session) BPF() string { return buildBPF(s.opts.ServerPort, s.opts.KnownIPs) }

func (s *Session) Stop() {
	s.cancel()
	s.handle.Close()
	<-s.done
}

func (s *Session) run(ctx context.Context, assembler *reassembly.Assembler) {
	defer close(s.done)
	defer close(s.out)

	source := gopacket.NewPacketSource(s.handle, s.handle.LinkType())
	packets := source.Packets()
	flushTick := time.NewTicker(30 * time.Second)
	defer flushTick.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-flushTick.C:
			assembler.FlushCloseOlderThan(time.Now().Add(-2 * time.Minute))
		case pkt, ok := <-packets:
			if !ok {
				return
			}
			if pkt.NetworkLayer() == nil {
				continue
			}
			tcpLayer := pkt.Layer(layers.LayerTypeTCP)
			if tcpLayer == nil {
				continue
			}
			tcp, _ := tcpLayer.(*layers.TCP)
			if tcp == nil {
				continue
			}
			s.bytesRcvd.Add(uint64(len(tcp.Payload)))
			assembler.AssembleWithContext(
				pkt.NetworkLayer().NetworkFlow(),
				tcp,
				&assemblerCtx{ci: pkt.Metadata().CaptureInfo},
			)
		}
	}
}

type assemblerCtx struct {
	ci gopacket.CaptureInfo
}

func (c *assemblerCtx) GetCaptureInfo() gopacket.CaptureInfo { return c.ci }

// buildBPF constructs the BPF filter. It always includes `tcp and port X` and
// additionally matches any known server IP regardless of port, which is what
// lets the sniffer catch traffic to a game server announced on a non-standard
// port.
func buildBPF(serverPort uint16, knownIPs []net.IP) string {
	clauses := []string{fmt.Sprintf("port %d", serverPort)}
	seen := map[string]struct{}{}
	for _, ip := range knownIPs {
		if ip == nil {
			continue
		}
		s := ip.String()
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		clauses = append(clauses, "host "+s)
	}
	return "tcp and (" + strings.Join(clauses, " or ") + ")"
}
