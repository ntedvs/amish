package tracker

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"amish/bencode"
)

func TestPeerAddr(t *testing.T) {
	p := Peer{IP: net.IP{192, 168, 1, 1}, Port: 6881}
	want := "192.168.1.1:6881"
	if p.Addr() != want {
		t.Fatalf("got %q, want %q", p.Addr(), want)
	}
}

func TestParseCompactPeers(t *testing.T) {
	// Two peers: 192.168.1.1:6881 and 10.0.0.1:8080
	data := []byte{
		192, 168, 1, 1, 0x1A, 0xE1, // 6881
		10, 0, 0, 1, 0x1F, 0x90, // 8080
	}

	peers, err := ParseCompactPeers(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(peers) != 2 {
		t.Fatalf("got %d peers, want 2", len(peers))
	}
	if peers[0].IP.String() != "192.168.1.1" {
		t.Fatalf("peer[0] ip = %s", peers[0].IP)
	}
	if peers[0].Port != 6881 {
		t.Fatalf("peer[0] port = %d", peers[0].Port)
	}
	if peers[1].IP.String() != "10.0.0.1" {
		t.Fatalf("peer[1] ip = %s", peers[1].IP)
	}
	if peers[1].Port != 8080 {
		t.Fatalf("peer[1] port = %d", peers[1].Port)
	}
}

func TestParseCompactPeersBadLength(t *testing.T) {
	_, err := ParseCompactPeers([]byte{1, 2, 3, 4, 5}) // 5 bytes, not multiple of 6
	if err == nil {
		t.Fatal("expected error for bad length")
	}
}

func TestParseCompactPeersEmpty(t *testing.T) {
	peers, err := ParseCompactPeers([]byte{})
	if err != nil {
		t.Fatal(err)
	}
	if len(peers) != 0 {
		t.Fatalf("got %d peers, want 0", len(peers))
	}
}

func TestParseHTTPResponseCompact(t *testing.T) {
	peerBytes := string([]byte{192, 168, 1, 1, 0x1A, 0xE1})
	resp := map[string]any{
		"interval": int64(1800),
		"peers":    peerBytes,
	}
	body, err := bencode.Encode(resp)
	if err != nil {
		t.Fatal(err)
	}

	peers, err := parseHTTPResponse(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(peers) != 1 {
		t.Fatalf("got %d peers, want 1", len(peers))
	}
	if peers[0].Port != 6881 {
		t.Fatalf("port = %d, want 6881", peers[0].Port)
	}
}

func TestParseHTTPResponseDictModel(t *testing.T) {
	resp := map[string]any{
		"interval": int64(1800),
		"peers": []any{
			map[string]any{"ip": "10.0.0.1", "port": int64(6881)},
			map[string]any{"ip": "10.0.0.2", "port": int64(6882)},
		},
	}
	body, err := bencode.Encode(resp)
	if err != nil {
		t.Fatal(err)
	}

	peers, err := parseHTTPResponse(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(peers) != 2 {
		t.Fatalf("got %d peers, want 2", len(peers))
	}
}

func TestParseHTTPResponseFailure(t *testing.T) {
	resp := map[string]any{
		"failure reason": "info_hash not found",
	}
	body, _ := bencode.Encode(resp)

	_, err := parseHTTPResponse(body)
	if err == nil {
		t.Fatal("expected error for failure response")
	}
}

func TestParseHTTPResponseBadBencode(t *testing.T) {
	_, err := parseHTTPResponse([]byte("not bencode"))
	if err == nil {
		t.Fatal("expected error for bad bencode")
	}
}

func TestAnnounceHTTP(t *testing.T) {
	peerBytes := []byte{127, 0, 0, 1, 0x1A, 0xE1}
	resp := map[string]any{
		"interval": int64(900),
		"peers":    string(peerBytes),
	}
	body, _ := bencode.Encode(resp)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("compact") != "1" {
			t.Error("expected compact=1")
		}
		if q.Get("info_hash") == "" {
			t.Error("expected info_hash param")
		}
		w.Write(body)
	}))
	defer ts.Close()

	params := AnnounceParams{
		Port: 6881,
		Left: 1000,
	}

	peers, err := Announce(ts.URL, params)
	if err != nil {
		t.Fatalf("Announce: %v", err)
	}
	if len(peers) != 1 {
		t.Fatalf("got %d peers", len(peers))
	}
	if peers[0].Addr() != "127.0.0.1:6881" {
		t.Fatalf("peer addr = %s", peers[0].Addr())
	}
}

func TestAnnounceUnsupportedScheme(t *testing.T) {
	_, err := Announce("ftp://tracker.example.com/announce", AnnounceParams{})
	if err == nil {
		t.Fatal("expected error for unsupported scheme")
	}
}
