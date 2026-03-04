package dht

import (
	"net"
	"testing"
)

func TestNewClient(t *testing.T) {
	c := New()
	if c == nil {
		t.Fatal("expected non-nil client")
	}
	if c.id[0] != 'M' {
		t.Fatal("expected peer ID to start with 'M'")
	}
	if c.nodes == nil {
		t.Fatal("expected non-nil nodes map")
	}
	if c.peers == nil {
		t.Fatal("expected non-nil peers map")
	}
}

func TestPeerAddr(t *testing.T) {
	p := Peer{IP: "192.168.1.1", Port: 6881}
	if p.Addr() != "192.168.1.1:6881" {
		t.Fatalf("Addr() = %q, want %q", p.Addr(), "192.168.1.1:6881")
	}
}

func TestPeerAddrZeroPort(t *testing.T) {
	p := Peer{IP: "10.0.0.1", Port: 0}
	if p.Addr() != "10.0.0.1:0" {
		t.Fatalf("Addr() = %q, want %q", p.Addr(), "10.0.0.1:0")
	}
}

func TestNodeFields(t *testing.T) {
	var id [20]byte
	id[0] = 0xAA
	n := Node{ID: id, Addr: "192.168.1.1:6881"}
	if n.Addr != "192.168.1.1:6881" {
		t.Fatalf("Addr = %q, want %q", n.Addr, "192.168.1.1:6881")
	}
}

func TestClientID(t *testing.T) {
	c := New()
	id := c.ID()
	if id[0] != 'M' {
		t.Fatal("expected peer ID to start with 'M'")
	}
}

func TestClientAddrNotListening(t *testing.T) {
	c := New()
	if c.Addr() != "" {
		t.Fatal("expected empty Addr when not listening")
	}
}

func TestBootstrapNodes(t *testing.T) {
	if len(BootstrapNodes) == 0 {
		t.Fatal("expected non-empty BootstrapNodes")
	}
	for _, node := range BootstrapNodes {
		host, port, err := net.SplitHostPort(node)
		if err != nil {
			t.Fatalf("invalid bootstrap node %q: %v", node, err)
		}
		if host == "" {
			t.Fatalf("empty host in bootstrap node %q", node)
		}
		if port == "" {
			t.Fatalf("empty port in bootstrap node %q", node)
		}
	}
}

func TestConstants(t *testing.T) {
	if BEP5Port != 6881 {
		t.Fatalf("BEP5Port = %d, want 6881", BEP5Port)
	}
	if TokenLen != 4 {
		t.Fatalf("TokenLen = %d, want 4", TokenLen)
	}
}

func TestQueryTypes(t *testing.T) {
	tests := []struct {
		name  string
		got   QueryType
		wants []QueryType
	}{
		{"ping", QueryPing, []QueryType{"ping"}},
		{"find_node", QueryFindNode, []QueryType{"find_node"}},
		{"get_peers", QueryGetPeers, []QueryType{"get_peers"}},
	}
	for _, tt := range tests {
		found := false
		for _, w := range tt.wants {
			if tt.got == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("QueryType %q not in expected values", tt.name)
		}
	}
}

func TestMsgTypes(t *testing.T) {
	tests := []struct {
		name  string
		got   MsgType
		wants []MsgType
	}{
		{"query", MsgQuery, []MsgType{"q"}},
		{"response", MsgResp, []MsgType{"r"}},
		{"error", MsgError, []MsgType{"e"}},
	}
	for _, tt := range tests {
		found := false
		for _, w := range tt.wants {
			if tt.got == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("MsgType %q not in expected values", tt.name)
		}
	}
}
