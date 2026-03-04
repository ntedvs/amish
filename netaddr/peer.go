// Package netaddr provides shared network address types used across packages.
package netaddr

import (
	"net"
	"strconv"
)

// Peer represents a discovered peer address (IP + port).
type Peer struct {
	IP   net.IP
	Port uint16
}

// Addr returns the peer address as "ip:port".
func (p Peer) Addr() string {
	return net.JoinHostPort(p.IP.String(), strconv.Itoa(int(p.Port)))
}
