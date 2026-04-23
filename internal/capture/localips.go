package capture

import (
	"net"
	"sync"
)

var (
	localIPsOnce sync.Once
	localIPs     map[string]struct{}
)

// IsLocalIP returns true if ip belongs to a local interface on this host.
// Result is cached for the process lifetime (interface changes during a
// session are ignored).
func IsLocalIP(ip net.IP) bool {
	localIPsOnce.Do(func() {
		localIPs = map[string]struct{}{}
		addrs, err := net.InterfaceAddrs()
		if err != nil {
			return
		}
		for _, a := range addrs {
			var addr net.IP
			switch v := a.(type) {
			case *net.IPNet:
				addr = v.IP
			case *net.IPAddr:
				addr = v.IP
			}
			if addr != nil {
				localIPs[addr.String()] = struct{}{}
			}
		}
	})
	_, ok := localIPs[ip.String()]
	return ok
}
