package torrent

import (
	"crypto/sha1"
	"encoding/binary"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"amish/magnet"
	"amish/metainfo"
	"amish/peer"
	"amish/tracker"
)

// mockPeer simulates a BitTorrent peer that serves pre-defined piece data.
type mockPeer struct {
	listener net.Listener
	infoHash [20]byte
	pieces   [][]byte
}

func newMockPeer(t *testing.T, infoHash [20]byte, pieces [][]byte) *mockPeer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	mp := &mockPeer{listener: ln, infoHash: infoHash, pieces: pieces}
	go mp.serve(t)
	return mp
}

func (mp *mockPeer) addr() string { return mp.listener.Addr().String() }
func (mp *mockPeer) close()       { mp.listener.Close() }

func (mp *mockPeer) serve(t *testing.T) {
	t.Helper()
	for {
		conn, err := mp.listener.Accept()
		if err != nil {
			return
		}
		go mp.handleConn(conn)
	}
}

func (mp *mockPeer) handleConn(conn net.Conn) {
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(30 * time.Second))

	// Receive handshake.
	var pstrLen [1]byte
	io.ReadFull(conn, pstrLen[:])
	pstr := make([]byte, pstrLen[0])
	io.ReadFull(conn, pstr)
	var reserved [8]byte
	io.ReadFull(conn, reserved[:])
	var rxInfoHash [20]byte
	io.ReadFull(conn, rxInfoHash[:])
	var rxPeerID [20]byte
	io.ReadFull(conn, rxPeerID[:])

	// Send handshake back.
	var myPeerID [20]byte
	copy(myPeerID[:], "mock-peer-00000000")
	var myReserved [8]byte
	buf := make([]byte, 0, 68)
	buf = append(buf, byte(len(peer.ProtocolID)))
	buf = append(buf, []byte(peer.ProtocolID)...)
	buf = append(buf, myReserved[:]...)
	buf = append(buf, mp.infoHash[:]...)
	buf = append(buf, myPeerID[:]...)
	conn.Write(buf)

	// Send bitfield (all pieces available).
	numPieces := len(mp.pieces)
	bfLen := (numPieces + 7) / 8
	bf := make([]byte, bfLen)
	for i := 0; i < numPieces; i++ {
		bf[i/8] |= 1 << (7 - uint(i%8))
	}
	peer.WriteMessage(conn, &peer.Message{ID: peer.MsgBitfield, Payload: bf})

	// Send unchoke.
	peer.WriteMessage(conn, &peer.Message{ID: peer.MsgUnchoke})

	// Handle requests.
	for {
		msg, err := peer.ReadMessage(conn)
		if err != nil {
			return
		}
		if msg == nil {
			continue
		}
		if msg.ID == peer.MsgInterested {
			continue
		}
		if msg.ID != peer.MsgRequest {
			continue
		}

		idx, begin, length, err := peer.ParseRequest(msg.Payload)
		if err != nil || int(idx) >= len(mp.pieces) {
			return
		}

		data := mp.pieces[idx]
		end := int(begin) + int(length)
		if end > len(data) {
			end = len(data)
		}
		block := data[begin:end]

		// Build piece response: index(4) + begin(4) + block.
		payload := make([]byte, 8+len(block))
		binary.BigEndian.PutUint32(payload[0:4], idx)
		binary.BigEndian.PutUint32(payload[4:8], begin)
		copy(payload[8:], block)
		peer.WriteMessage(conn, &peer.Message{ID: peer.MsgPiece, Payload: payload})
	}
}

func TestDownloadPipeline(t *testing.T) {
	// Create test data: 3 pieces of 32 KiB each.
	pieceLen := int64(32768)
	numPieces := 3
	pieces := make([][]byte, numPieces)
	hashes := make([][20]byte, numPieces)
	for i := 0; i < numPieces; i++ {
		pieces[i] = make([]byte, pieceLen)
		// Fill with recognizable pattern.
		for j := range pieces[i] {
			pieces[i][j] = byte(i*37 + j%251)
		}
		hashes[i] = sha1.Sum(pieces[i])
	}

	// Build concatenated hash string.
	var piecesConcat []byte
	for _, h := range hashes {
		piecesConcat = append(piecesConcat, h[:]...)
	}

	var infoHash [20]byte
	copy(infoHash[:], "test-info-hash-12345")

	// Start mock peer.
	mp := newMockPeer(t, infoHash, pieces)
	defer mp.close()

	// Create output directory.
	tmpDir := t.TempDir()

	// Build torrent with pre-set metadata.
	m := magnet.Magnet{InfoHash: infoHash}
	tor := New(m, tmpDir)
	tor.Info = &metainfo.Info{
		Name:        "test-download",
		PieceLength: pieceLen,
		Pieces:      hashes,
		Length:      pieceLen * int64(numPieces),
	}
	tor.Stats.TotalPieces = int32(numPieces)
	tor.Stats.TotalBytes = tor.Info.TotalLength()
	tor.Stats.StartTime = time.Now()

	host, portStr, _ := net.SplitHostPort(mp.addr())
	ip := net.ParseIP(host)
	port := 0
	for _, c := range portStr {
		port = port*10 + int(c-'0')
	}

	peers := []tracker.Peer{{IP: ip, Port: uint16(port)}}
	err := tor.downloadPieces(peers)
	if err != nil {
		t.Fatalf("downloadPieces failed: %v", err)
	}

	// Verify the output file was written correctly.
	data, err := os.ReadFile(filepath.Join(tmpDir, "test-download"))
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if len(data) != int(pieceLen)*numPieces {
		t.Fatalf("output size = %d, want %d", len(data), int(pieceLen)*numPieces)
	}

	// Verify each piece hash.
	for i := 0; i < numPieces; i++ {
		start := int64(i) * pieceLen
		end := start + pieceLen
		got := sha1.Sum(data[start:end])
		if got != hashes[i] {
			t.Errorf("piece %d hash mismatch", i)
		}
	}
}
