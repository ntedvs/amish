package netaddr

import (
	"net"
	"testing"
)

func TestPeerAddr(t *testing.T) {
	p := Peer{IP: net.IPv4(192, 168, 1, 1), Port: 6881}
	got := p.Addr()
	want := "192.168.1.1:6881"
	if got != want {
		t.Errorf("Addr() = %q, want %q", got, want)
	}
}
