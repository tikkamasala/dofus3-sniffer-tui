package capture

import (
	"encoding/binary"
	"net"
	"sync/atomic"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/reassembly"
)

type streamFactory struct {
	out        chan CapturedMessage
	classifier *Classifier
	serverPort uint16
	dropped    atomic.Uint64
}

func newStreamFactory(out chan CapturedMessage, classifier *Classifier, serverPort uint16) *streamFactory {
	return &streamFactory{out: out, classifier: classifier, serverPort: serverPort}
}

func (f *streamFactory) New(netFlow, tpFlow gopacket.Flow, _ *layers.TCP, _ reassembly.AssemblerContext) reassembly.Stream {
	srcIP := net.IP(netFlow.Src().Raw())
	dstIP := net.IP(netFlow.Dst().Raw())
	kind, env, remote := f.classifier.Classify(srcIP, dstIP)
	return &tcpStream{
		out:        f.out,
		env:        env,
		net:        netFlow,
		tp:         tpFlow,
		firstSrc:   portFromFlow(tpFlow.Src()),
		serverPort: f.serverPort,
		kind:       kind,
		remote:     remote,
		onDrop:     func() { f.dropped.Add(1) },
	}
}

func (f *streamFactory) Dropped() uint64 {
	return f.dropped.Load()
}

func portFromFlow(e gopacket.Endpoint) uint16 {
	raw := e.Raw()
	if len(raw) != 2 {
		return 0
	}
	return binary.BigEndian.Uint16(raw)
}
