package peer

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"
)

func TestBitfieldHasPiece(t *testing.T) {
	bf := NewBitfield(16)

	if bf.HasPiece(0) {
		t.Fatal("expected piece 0 not set")
	}

	bf.SetPiece(0)
	if !bf.HasPiece(0) {
		t.Fatal("expected piece 0 set")
	}

	bf.SetPiece(7)
	if !bf.HasPiece(7) {
		t.Fatal("expected piece 7 set")
	}

	bf.SetPiece(15)
	if !bf.HasPiece(15) {
		t.Fatal("expected piece 15 set")
	}

	if bf.HasPiece(1) {
		t.Fatal("expected piece 1 not set")
	}
}

func TestBitfieldOutOfRange(t *testing.T) {
	bf := NewBitfield(8)
	if bf.HasPiece(100) {
		t.Fatal("out of range should return false")
	}
	if bf.HasPiece(-1) {
		t.Fatal("negative should return false")
	}
	// SetPiece on out of range should not panic.
	bf.SetPiece(100)
}

func TestNewBitfield(t *testing.T) {
	tests := []struct {
		pieces  int
		wantLen int
	}{
		{0, 0},
		{1, 1},
		{8, 1},
		{9, 2},
		{16, 2},
		{17, 3},
	}
	for _, tt := range tests {
		bf := NewBitfield(tt.pieces)
		if len(bf) != tt.wantLen {
			t.Errorf("NewBitfield(%d) len = %d, want %d", tt.pieces, len(bf), tt.wantLen)
		}
	}
}

func TestHandshakeRoundTrip(t *testing.T) {
	infoHash := [20]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20}
	clientID := [20]byte{20, 19, 18, 17, 16, 15, 14, 13, 12, 11, 10, 9, 8, 7, 6, 5, 4, 3, 2, 1}
	serverID := [20]byte{0xA, 0xB, 0xC, 0xD, 0xE, 0xF, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}

	// Create a pipe to simulate network.
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	errCh := make(chan error, 1)

	// Server side: respond with handshake.
	go func() {
		// Read client handshake.
		buf := make([]byte, 68)
		if _, err := io.ReadFull(serverConn, buf); err != nil {
			errCh <- err
			return
		}

		// Write server handshake.
		resp := BuildHandshakeBytes(infoHash, serverID, true)
		if _, err := serverConn.Write(resp); err != nil {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	// Client side.
	c := &Conn{Conn: clientConn, Reader: bufio.NewReader(clientConn), Writer: bufio.NewWriter(clientConn), InfoHash: infoHash}

	if err := c.sendHandshake(infoHash, clientID); err != nil {
		t.Fatalf("sendHandshake: %v", err)
	}
	if err := c.recvHandshake(infoHash); err != nil {
		t.Fatalf("recvHandshake: %v", err)
	}

	if !c.SupportsExtension {
		t.Fatal("expected extension support")
	}
	if c.PeerID != serverID {
		t.Fatalf("peer ID mismatch")
	}

	if err := <-errCh; err != nil {
		t.Fatalf("server error: %v", err)
	}
}

func TestHandshakeInfoHashMismatch(t *testing.T) {
	expected := [20]byte{1, 2, 3}
	wrong := [20]byte{4, 5, 6}
	peerID := [20]byte{}

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	go func() {
		buf := make([]byte, 68)
		io.ReadFull(serverConn, buf)
		resp := BuildHandshakeBytes(wrong, peerID, false)
		serverConn.Write(resp)
	}()

	c := &Conn{Conn: clientConn, Reader: bufio.NewReader(clientConn), Writer: bufio.NewWriter(clientConn)}
	c.sendHandshake(expected, peerID)
	err := c.recvHandshake(expected)
	if err != ErrBadInfoHash {
		t.Fatalf("expected ErrBadInfoHash, got %v", err)
	}
}

func TestHandshakeBadProtocol(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	go func() {
		// Drain the client's handshake.
		buf := make([]byte, 68)
		io.ReadFull(serverConn, buf)
		// Send garbage response.
		serverConn.Write([]byte{5, 'h', 'e', 'l', 'l', 'o'})
		serverConn.Write(make([]byte, 62))
	}()

	c := &Conn{Conn: clientConn, Reader: bufio.NewReader(clientConn), Writer: bufio.NewWriter(clientConn)}
	c.sendHandshake([20]byte{}, [20]byte{})
	err := c.recvHandshake([20]byte{})
	if err == nil {
		t.Fatal("expected error for bad protocol")
	}
}

func TestDial(t *testing.T) {
	infoHash := [20]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20}
	peerID := [20]byte{0xAA}
	serverPeerID := [20]byte{0xBB}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		buf := make([]byte, 68)
		io.ReadFull(conn, buf)
		resp := BuildHandshakeBytes(infoHash, serverPeerID, true)
		conn.Write(resp)
	}()

	c, err := Dial(ln.Addr().String(), infoHash, peerID, 2*time.Second)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()

	if c.PeerID != serverPeerID {
		t.Fatal("peer ID mismatch")
	}
	if !c.SupportsExtension {
		t.Fatal("expected extension support")
	}
}

func TestDialRefused(t *testing.T) {
	// Connect to a port that's not listening — should fail immediately.
	_, err := Dial("127.0.0.1:1", [20]byte{}, [20]byte{}, 500*time.Millisecond)
	if err == nil {
		t.Fatal("expected connection error")
	}
}

func TestExtensionHandshake(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	c := &Conn{Conn: clientConn, Reader: bufio.NewReader(clientConn), Writer: bufio.NewWriter(clientConn)}

	// Read on the server side.
	go func() {
		// Read the extension message.
		var length uint32
		binary.Read(serverConn, binary.BigEndian, &length)
		buf := make([]byte, length)
		io.ReadFull(serverConn, buf)
	}()

	if err := c.SendExtensionHandshake(); err != nil {
		t.Fatalf("SendExtensionHandshake: %v", err)
	}
}

func TestHandleExtensionHandshake(t *testing.T) {
	c := &Conn{}

	// Simulate a peer extension handshake payload.
	// d1:md11:ut_metadatai2ee13:metadata_sizei31235ee
	payload := []byte("d1:md11:ut_metadatai2ee13:metadata_sizei31235ee")

	if err := c.HandleExtensionHandshake(payload); err != nil {
		t.Fatalf("HandleExtensionHandshake: %v", err)
	}

	if c.ExtensionIDs["ut_metadata"] != 2 {
		t.Fatalf("ut_metadata = %d, want 2", c.ExtensionIDs["ut_metadata"])
	}
	if c.MetadataSize != 31235 {
		t.Fatalf("metadata_size = %d, want 31235", c.MetadataSize)
	}
}

func TestParseMetadataPiece(t *testing.T) {
	// Construct: bencoded dict + raw data.
	dict := "d8:msg_typei1e5:piecei0ee"
	raw := []byte("metadata bytes here")
	payload := append([]byte(dict), raw...)

	piece, data, err := ParseMetadataPiece(payload)
	if err != nil {
		t.Fatalf("ParseMetadataPiece: %v", err)
	}
	if piece != 0 {
		t.Fatalf("piece = %d, want 0", piece)
	}
	if !bytes.Equal(data, raw) {
		t.Fatalf("data = %q, want %q", data, raw)
	}
}

func TestParseMetadataPieceReject(t *testing.T) {
	// msg_type=2 is reject.
	payload := []byte("d8:msg_typei2e5:piecei0ee")
	_, _, err := ParseMetadataPiece(payload)
	if err == nil {
		t.Fatal("expected error for reject message")
	}
}

func TestBuildHandshakeBytes(t *testing.T) {
	ih := [20]byte{0xAA}
	pid := [20]byte{0xBB}
	b := BuildHandshakeBytes(ih, pid, true)
	if len(b) != 68 {
		t.Fatalf("len = %d, want 68", len(b))
	}
	if b[0] != 19 {
		t.Fatalf("pstrlen = %d, want 19", b[0])
	}
	if string(b[1:20]) != ProtocolID {
		t.Fatal("protocol mismatch")
	}
	// Check extension bit.
	if b[25]&ExtensionBit == 0 {
		t.Fatal("expected extension bit set")
	}
}

func TestEncodeUint32(t *testing.T) {
	b := EncodeUint32(256)
	v := binary.BigEndian.Uint32(b)
	if v != 256 {
		t.Fatalf("got %d, want 256", v)
	}
}
