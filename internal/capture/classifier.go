package capture

import (
	"net"
	"sync"
)

// Classifier decides which schema applies to a TCP flow based on its remote
// IP. It is shared across all streams in a Session so that game-server IPs
// discovered at runtime are picked up by subsequent new connections.
type Classifier struct {
	connIPs []net.IP
	connEnv Envelope
	gameEnv Envelope

	mu         sync.Mutex
	gameSet    map[string]struct{} // remote IP → known game server
	gameNotify func(net.IP)
}

func NewClassifier(connIPs []net.IP, knownGameIPs []net.IP, connEnv, gameEnv Envelope, onNewGameIP func(net.IP)) *Classifier {
	c := &Classifier{
		connIPs:    append([]net.IP(nil), connIPs...),
		connEnv:    connEnv,
		gameEnv:    gameEnv,
		gameSet:    map[string]struct{}{},
		gameNotify: onNewGameIP,
	}
	for _, ip := range knownGameIPs {
		c.gameSet[ip.String()] = struct{}{}
	}
	return c
}

// Classify inspects a TCP flow's src/dst IPs and returns the kind, envelope,
// and the remote IP (the side that isn't us). If the remote IP is a freshly-
// observed game server, the notify callback is fired exactly once per IP.
func (c *Classifier) Classify(srcIP, dstIP net.IP) (Kind, Envelope, net.IP) {
	remote := srcIP
	if IsLocalIP(srcIP) {
		remote = dstIP
	} else if !IsLocalIP(dstIP) {
		// Neither side is local — likely sniffing on a SPAN or running in a VM
		// where interface addrs don't match. Fall back to "the one that's not
		// the connection server"; if both match, keep src.
		if c.isConnIP(srcIP) {
			remote = dstIP
		}
	}

	if c.isConnIP(remote) {
		return KindConnection, c.connEnv, remote
	}

	// Everything else is a game server candidate. Record it so it survives a
	// restart (handled by the notify callback, usually persist-to-config).
	key := remote.String()
	c.mu.Lock()
	if _, seen := c.gameSet[key]; !seen {
		c.gameSet[key] = struct{}{}
		notify := c.gameNotify
		c.mu.Unlock()
		if notify != nil {
			notify(remote)
		}
	} else {
		c.mu.Unlock()
	}
	return KindGame, c.gameEnv, remote
}

func (c *Classifier) isConnIP(ip net.IP) bool {
	for _, c := range c.connIPs {
		if c.Equal(ip) {
			return true
		}
	}
	return false
}
