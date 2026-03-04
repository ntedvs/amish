package torrent

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"crypto/sha1"
	"encoding/binary"
	"io"
	"net"
	"runtime"
	"sync"
	"testing"
	"time"

	"amish/peer"
)

// --- Test helpers ---

// noopConn implements net.Conn with no-ops. Used so DownloadPiece can call
// SetDeadline without panicking while reads/writes go through separate buffers.
type noopConn struct{}

func (noopConn) Read([]byte) (int, error)         { return 0, io.EOF }
func (noopConn) Write(b []byte) (int, error)       { return len(b), nil }
func (noopConn) Close() error                      { return nil }
func (noopConn) LocalAddr() net.Addr               { return nil }
func (noopConn) RemoteAddr() net.Addr              { return nil }
func (noopConn) SetDeadline(time.Time) error       { return nil }
func (noopConn) SetReadDeadline(time.Time) error   { return nil }
func (noopConn) SetWriteDeadline(time.Time) error  { return nil }

// buildPieceMsg builds a MsgPiece message with the given index, begin, and data.
func buildPieceMsg(index, begin uint32, data []byte) *peer.Message {
	payload := make([]byte, 8+len(data))
	binary.BigEndian.PutUint32(payload[0:4], index)
	binary.BigEndian.PutUint32(payload[4:8], begin)
	copy(payload[8:], data)
	return &peer.Message{ID: peer.MsgPiece, Payload: payload}
}

// pieceResponses builds the sequence of MsgPiece responses for a full piece download.
func pieceResponses(index int, data []byte) []*peer.Message {
	var msgs []*peer.Message
	offset := 0
	for offset < len(data) {
		end := offset + peer.BlockSize
		if end > len(data) {
			end = len(data)
		}
		msgs = append(msgs, buildPieceMsg(uint32(index), uint32(offset), data[offset:end]))
		offset = end
	}
	return msgs
}

// newMockConn creates a peer.Conn that reads from pre-built response messages
// and discards all writes. Deadlines are no-ops.
func newMockConn(msgs []*peer.Message) *peer.Conn {
	var buf bytes.Buffer
	for _, msg := range msgs {
		peer.WriteMessage(&buf, msg)
	}
	return &peer.Conn{
		Conn:   noopConn{},
		Reader: bufio.NewReaderSize(&buf, 32*1024),
		Writer: bufio.NewWriter(io.Discard),
	}
}

// --- Existing tests ---

func TestVerifyPieceValid(t *testing.T) {
	data := []byte("hello world this is piece data")
	hash := sha1.Sum(data)
	if !VerifyPiece(data, hash) {
		t.Fatal("expected valid piece")
	}
}

func TestVerifyPieceInvalid(t *testing.T) {
	data := []byte("hello world")
	badHash := [20]byte{0xFF}
	if VerifyPiece(data, badHash) {
		t.Fatal("expected invalid piece")
	}
}

func TestVerifyPieceEmpty(t *testing.T) {
	data := []byte{}
	hash := sha1.Sum(data)
	if !VerifyPiece(data, hash) {
		t.Fatal("empty data should verify against its own hash")
	}
}

func TestPieceResultSuccess(t *testing.T) {
	pr := PieceResult{
		Index: 3,
		Data:  []byte("piece data"),
	}
	if pr.Err != nil {
		t.Fatal("expected no error")
	}
	if pr.Index != 3 {
		t.Fatal("index mismatch")
	}
}

func TestPieceResultError(t *testing.T) {
	pr := PieceResult{
		Index: 7,
		Err:   ErrNoPeers,
	}
	if pr.Err == nil {
		t.Fatal("expected error")
	}
	if pr.Data != nil {
		t.Fatal("expected nil data on error")
	}
}

func TestPieceBufferPool(t *testing.T) {
	initialStats := runtime.NumGoroutine()

	buf := make([]byte, 16384)
	pieceBufferPool.Put(buf)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				b := make([]byte, 16384)
				pieceBufferPool.Put(b[:0])
			}
		}()
	}
	wg.Wait()
	runtime.GC()

	if runtime.NumGoroutine()-initialStats > 5 {
		t.Fatal("goroutine leak detected")
	}
}

func TestPieceWorkFields(t *testing.T) {
	pw := PieceWork{
		Index:    5,
		Hash:     [20]byte{0xAA},
		Length:   262144,
		NumTries: 2,
	}
	if pw.Index != 5 {
		t.Fatal("index mismatch")
	}
	if pw.Length != 262144 {
		t.Fatal("length mismatch")
	}
	if pw.NumTries != 2 {
		t.Fatal("numtries mismatch")
	}
}

func TestMaxPieceTries(t *testing.T) {
	if MaxPieceTries != 5 {
		t.Fatalf("MaxPieceTries = %d, want 5", MaxPieceTries)
	}
}

// --- DownloadPiece tests ---

func TestDownloadPieceMultiBlock(t *testing.T) {
	// 2 full blocks + partial block = 3 blocks total.
	data := make([]byte, peer.BlockSize*2+100)
	rand.Read(data)
	hash := sha1.Sum(data)

	conn := newMockConn(pieceResponses(0, data))
	pw := &PieceWork{Index: 0, Hash: hash, Length: int64(len(data))}

	result := DownloadPiece(conn, pw)
	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}
	if !bytes.Equal(result.Data, data) {
		t.Fatal("data mismatch")
	}
}

func TestDownloadPieceSmallLastPiece(t *testing.T) {
	// Simulate a last piece smaller than BlockSize (common in torrents).
	data := make([]byte, 50)
	rand.Read(data)
	hash := sha1.Sum(data)

	conn := newMockConn(pieceResponses(42, data))
	pw := &PieceWork{Index: 42, Hash: hash, Length: 50}

	result := DownloadPiece(conn, pw)
	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}
	if result.Index != 42 {
		t.Fatalf("index = %d, want 42", result.Index)
	}
	if !bytes.Equal(result.Data, data) {
		t.Fatal("data mismatch")
	}
}

func TestDownloadPieceSingleByte(t *testing.T) {
	data := []byte{0x42}
	hash := sha1.Sum(data)

	conn := newMockConn(pieceResponses(0, data))
	pw := &PieceWork{Index: 0, Hash: hash, Length: 1}

	result := DownloadPiece(conn, pw)
	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}
	if !bytes.Equal(result.Data, data) {
		t.Fatal("data mismatch")
	}
}

func TestDownloadPieceExactBlockSize(t *testing.T) {
	data := make([]byte, peer.BlockSize)
	rand.Read(data)
	hash := sha1.Sum(data)

	conn := newMockConn(pieceResponses(0, data))
	pw := &PieceWork{Index: 0, Hash: hash, Length: int64(peer.BlockSize)}

	result := DownloadPiece(conn, pw)
	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}
	if !bytes.Equal(result.Data, data) {
		t.Fatal("data mismatch")
	}
}

func TestDownloadPieceThreeFullBlocks(t *testing.T) {
	data := make([]byte, peer.BlockSize*3)
	rand.Read(data)
	hash := sha1.Sum(data)

	conn := newMockConn(pieceResponses(5, data))
	pw := &PieceWork{Index: 5, Hash: hash, Length: int64(len(data))}

	result := DownloadPiece(conn, pw)
	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}
	if !bytes.Equal(result.Data, data) {
		t.Fatal("data mismatch")
	}
}

func TestDownloadPieceChoke(t *testing.T) {
	// Peer sends one block then chokes.
	data := make([]byte, peer.BlockSize*2)
	rand.Read(data)
	hash := sha1.Sum(data)

	msgs := []*peer.Message{
		buildPieceMsg(0, 0, data[:peer.BlockSize]),
		{ID: peer.MsgChoke},
	}
	conn := newMockConn(msgs)
	pw := &PieceWork{Index: 0, Hash: hash, Length: int64(len(data))}

	result := DownloadPiece(conn, pw)
	if result.Err == nil {
		t.Fatal("expected choke error")
	}
	if result.Err != ErrChoked {
		t.Fatalf("expected ErrChoked, got: %v", result.Err)
	}
}

func TestDownloadPieceHashMismatch(t *testing.T) {
	data := make([]byte, 100)
	rand.Read(data)
	badHash := [20]byte{0xDE, 0xAD} // doesn't match data

	conn := newMockConn(pieceResponses(0, data))
	pw := &PieceWork{Index: 0, Hash: badHash, Length: 100}

	result := DownloadPiece(conn, pw)
	if result.Err == nil {
		t.Fatal("expected hash mismatch error")
	}
	if result.Data != nil {
		t.Fatal("expected nil data on hash mismatch")
	}
}

func TestDownloadPieceWrongIndexIgnored(t *testing.T) {
	// Peer sends a response for the wrong piece index, then the correct one.
	data := make([]byte, 100)
	rand.Read(data)
	hash := sha1.Sum(data)

	wrongData := make([]byte, 100)
	rand.Read(wrongData)

	msgs := []*peer.Message{
		buildPieceMsg(99, 0, wrongData), // wrong piece index
		buildPieceMsg(0, 0, data),       // correct
	}
	conn := newMockConn(msgs)
	pw := &PieceWork{Index: 0, Hash: hash, Length: 100}

	result := DownloadPiece(conn, pw)
	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}
	if !bytes.Equal(result.Data, data) {
		t.Fatal("data mismatch")
	}
}

func TestDownloadPieceKeepAlive(t *testing.T) {
	data := make([]byte, 100)
	rand.Read(data)
	hash := sha1.Sum(data)

	// Build stream: keep-alive, piece response, keep-alive (trailing is harmless).
	var buf bytes.Buffer
	peer.WriteMessage(&buf, nil)                        // keep-alive
	peer.WriteMessage(&buf, buildPieceMsg(0, 0, data))  // piece

	conn := &peer.Conn{
		Conn:   noopConn{},
		Reader: bufio.NewReaderSize(&buf, 32*1024),
		Writer: bufio.NewWriter(io.Discard),
	}
	pw := &PieceWork{Index: 0, Hash: hash, Length: 100}

	result := DownloadPiece(conn, pw)
	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}
	if !bytes.Equal(result.Data, data) {
		t.Fatal("data mismatch")
	}
}

func TestDownloadPieceHaveMsgIgnored(t *testing.T) {
	// Peer sends a MsgHave in the middle of the download. Should be ignored.
	data := make([]byte, 100)
	rand.Read(data)
	hash := sha1.Sum(data)

	msgs := []*peer.Message{
		peer.NewHave(55),          // spurious Have
		buildPieceMsg(0, 0, data), // actual data
	}
	conn := newMockConn(msgs)
	pw := &PieceWork{Index: 0, Hash: hash, Length: 100}

	result := DownloadPiece(conn, pw)
	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}
	if !bytes.Equal(result.Data, data) {
		t.Fatal("data mismatch")
	}
}

func TestDownloadPieceOutOfBoundsIgnored(t *testing.T) {
	// Peer sends a block whose begin+len exceeds buffer. Should be skipped.
	data := make([]byte, 50)
	rand.Read(data)
	hash := sha1.Sum(data)

	oobData := make([]byte, 100)
	msgs := []*peer.Message{
		buildPieceMsg(0, 0, oobData), // 100 bytes into 50-byte buffer = OOB
		buildPieceMsg(0, 0, data),    // correct
	}
	conn := newMockConn(msgs)
	pw := &PieceWork{Index: 0, Hash: hash, Length: 50}

	result := DownloadPiece(conn, pw)
	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}
	if !bytes.Equal(result.Data, data) {
		t.Fatal("data mismatch")
	}
}

func TestDownloadPieceRecvError(t *testing.T) {
	// Empty reader = immediate EOF on first RecvMessage.
	conn := &peer.Conn{
		Conn:   noopConn{},
		Reader: bufio.NewReader(bytes.NewReader(nil)),
		Writer: bufio.NewWriter(io.Discard),
	}
	pw := &PieceWork{Index: 0, Hash: [20]byte{}, Length: 100}

	result := DownloadPiece(conn, pw)
	if result.Err == nil {
		t.Fatal("expected recv error")
	}
}

// TestBufferPoolNoCorruption verifies that a successful DownloadPiece result
// is not corrupted when a subsequent download reuses the pool.
func TestBufferPoolNoCorruption(t *testing.T) {
	data1 := make([]byte, 50)
	rand.Read(data1)
	hash1 := sha1.Sum(data1)

	conn1 := newMockConn(pieceResponses(0, data1))
	result1 := DownloadPiece(conn1, &PieceWork{Index: 0, Hash: hash1, Length: 50})
	if result1.Err != nil {
		t.Fatal(result1.Err)
	}

	// Save expected data.
	expected := make([]byte, len(result1.Data))
	copy(expected, result1.Data)

	// Second download may reuse pool buffer.
	data2 := make([]byte, 50)
	for i := range data2 {
		data2[i] = 0xFF
	}
	hash2 := sha1.Sum(data2)

	conn2 := newMockConn(pieceResponses(1, data2))
	result2 := DownloadPiece(conn2, &PieceWork{Index: 1, Hash: hash2, Length: 50})
	if result2.Err != nil {
		t.Fatal(result2.Err)
	}

	// First result must still be intact.
	if !bytes.Equal(result1.Data, expected) {
		t.Fatal("result1 data corrupted by subsequent pool reuse")
	}
}

// TestBufferPoolConcurrentNoCorruption runs many DownloadPiece calls in
// parallel to stress-test that the pool fix prevents data races.
func TestBufferPoolConcurrentNoCorruption(t *testing.T) {
	const n = 20
	const size = 50

	var wg sync.WaitGroup
	results := make([]PieceResult, n)
	expected := make([][]byte, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			data := make([]byte, size)
			rand.Read(data)
			hash := sha1.Sum(data)

			expected[idx] = make([]byte, size)
			copy(expected[idx], data)

			conn := newMockConn(pieceResponses(idx, data))
			results[idx] = DownloadPiece(conn, &PieceWork{
				Index: idx, Hash: hash, Length: size,
			})
		}(i)
	}
	wg.Wait()

	for i := 0; i < n; i++ {
		if results[i].Err != nil {
			t.Fatalf("goroutine %d: %v", i, results[i].Err)
		}
		if !bytes.Equal(results[i].Data, expected[i]) {
			t.Fatalf("goroutine %d: data corrupted", i)
		}
	}
}

// TestBufferPoolReturnedOnFailure verifies the buffer goes back to the pool
// on hash mismatch (so it can be reused), but not on success.
func TestBufferPoolReturnedOnFailure(t *testing.T) {
	// Drain the pool.
	for pieceBufferPool.Get() != nil {
	}

	// Download with bad hash = failure. Buffer should go back to pool.
	data := make([]byte, 64)
	rand.Read(data)

	conn := newMockConn(pieceResponses(0, data))
	result := DownloadPiece(conn, &PieceWork{
		Index: 0, Hash: [20]byte{0xFF}, Length: 64,
	})
	if result.Err == nil {
		t.Fatal("expected hash mismatch error")
	}

	// Pool should have a buffer now.
	got := pieceBufferPool.Get()
	if got == nil {
		t.Fatal("expected buffer returned to pool on failure")
	}
}
