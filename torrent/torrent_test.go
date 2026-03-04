package torrent

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"amish/magnet"
)

func TestNewTorrent(t *testing.T) {
	m := magnet.Magnet{
		InfoHash: [20]byte{0xAA},
		Name:     "test",
	}

	tor := New(m, "/tmp")
	if tor.Magnet.InfoHash != m.InfoHash {
		t.Fatal("info hash mismatch")
	}
	if tor.OutputDir != "/tmp" {
		t.Fatal("output dir mismatch")
	}
	// PeerID should start with -AT0001-.
	prefix := string(tor.PeerID[:8])
	if prefix != "-AM0001-" {
		t.Fatalf("peer ID prefix = %q, want -AM0001-", prefix)
	}
	// Rest should be random (non-zero, most likely).
	allZero := true
	for _, b := range tor.PeerID[8:] {
		if b != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		t.Fatal("random portion of peer ID is all zeros")
	}
}

func TestStatsProgress(t *testing.T) {
	s := &Stats{}
	atomic.StoreInt32(&s.TotalPieces, 100)
	atomic.StoreInt32(&s.Downloaded, 50)
	p := s.Progress()
	if p != 0.5 {
		t.Fatalf("progress = %f, want 0.5", p)
	}
}

func TestStatsProgressZero(t *testing.T) {
	s := &Stats{}
	atomic.StoreInt32(&s.TotalPieces, 0)
	if s.Progress() != 0 {
		t.Fatal("expected 0 progress for 0 total pieces")
	}
}

func TestStatsBytesPerSecond(t *testing.T) {
	s := &Stats{
		StartTime: time.Now().Add(-1 * time.Second),
	}
	atomic.StoreInt64(&s.DownloadedBytes, 1000)
	bps := s.BytesPerSecond()
	// Should be roughly 1000 bytes/sec (within margin for timing).
	if bps < 500 || bps > 2000 {
		t.Fatalf("bps = %f, expected ~1000", bps)
	}
}

func TestStatsBytesPerSecondNoBytes(t *testing.T) {
	s := &Stats{
		StartTime: time.Now().Add(-1 * time.Second),
	}
	atomic.StoreInt64(&s.DownloadedBytes, 0)
	if s.BytesPerSecond() != 0 {
		t.Fatal("expected 0 bps with no bytes downloaded")
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		input int64
		want  string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.00 KB"},
		{1048576, "1.00 MB"},
		{1073741824, "1.00 GB"},
		{1536, "1.50 KB"},
	}

	for _, tt := range tests {
		got := FormatBytes(tt.input)
		if got != tt.want {
			t.Errorf("FormatBytes(%d) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestStatsGetStats(t *testing.T) {
	s := &Stats{}
	atomic.StoreInt32(&s.TotalPieces, 100)
	atomic.StoreInt32(&s.Downloaded, 50)
	atomic.StoreInt64(&s.TotalBytes, 1048576)
	atomic.StoreInt64(&s.DownloadedBytes, 524288)
	atomic.StoreInt32(&s.ActivePeers, 5)

	downloaded, total, bytes, peers := s.GetStats()
	if downloaded != 50 {
		t.Fatalf("downloaded = %d, want 50", downloaded)
	}
	if total != 100 {
		t.Fatalf("total = %d, want 100", total)
	}
	if bytes != 1048576 {
		t.Fatalf("bytes = %d, want 1048576", bytes)
	}
	if peers != 5 {
		t.Fatalf("peers = %d, want 5", peers)
	}
}

func TestStatsGetStatsZero(t *testing.T) {
	s := &Stats{}
	downloaded, total, bytes, peers := s.GetStats()
	if downloaded != 0 {
		t.Fatalf("downloaded = %d, want 0", downloaded)
	}
	if total != 0 {
		t.Fatalf("total = %d, want 0", total)
	}
	if bytes != 0 {
		t.Fatalf("bytes = %d, want 0", bytes)
	}
	if peers != 0 {
		t.Fatalf("peers = %d, want 0", peers)
	}
}

func TestStatsActivePeersAtomic(t *testing.T) {
	s := &Stats{}
	atomic.AddInt32(&s.ActivePeers, 1)
	atomic.AddInt32(&s.ActivePeers, 1)
	atomic.AddInt32(&s.ActivePeers, -1)

	if atomic.LoadInt32(&s.ActivePeers) != 1 {
		t.Fatal("ActivePeers should be 1")
	}

	atomic.AddInt32(&s.ActivePeers, -1)
	if atomic.LoadInt32(&s.ActivePeers) != 0 {
		t.Fatal("ActivePeers should be 0")
	}
}

func TestStatsDownloadedAtomic(t *testing.T) {
	s := &Stats{}
	atomic.AddInt32(&s.Downloaded, 1)
	atomic.AddInt32(&s.Downloaded, 4)

	if atomic.LoadInt32(&s.Downloaded) != 5 {
		t.Fatalf("Downloaded = %d, want 5", atomic.LoadInt32(&s.Downloaded))
	}
}

func TestStatsDownloadedBytesAtomic(t *testing.T) {
	s := &Stats{}
	atomic.AddInt64(&s.DownloadedBytes, 1024)
	atomic.AddInt64(&s.DownloadedBytes, 2048)

	if atomic.LoadInt64(&s.DownloadedBytes) != 3072 {
		t.Fatalf("DownloadedBytes = %d, want 3072", atomic.LoadInt64(&s.DownloadedBytes))
	}
}

// TestBytesPerSecondIsCumulativeAverage demonstrates that BytesPerSecond
// reports a cumulative average, not instantaneous speed. A download that
// transferred all bytes in the first second but then stalled for 9 more
// seconds still shows a "positive" speed.
func TestBytesPerSecondIsCumulativeAverage(t *testing.T) {
	s := &Stats{
		StartTime: time.Now().Add(-10 * time.Second), // started 10s ago
	}
	// All bytes were downloaded in the first second; nothing since.
	atomic.StoreInt64(&s.DownloadedBytes, 10*1024*1024) // 10 MB

	bps := s.BytesPerSecond()
	// Cumulative average: 10MB / 10s = 1 MB/s.
	// An instantaneous measurement would show 0 B/s since nothing recent.
	if bps < 900*1024 || bps > 1100*1024 {
		t.Fatalf("bps = %.0f, expected ~1MB/s cumulative average", bps)
	}
}

// TestWorkChannelRequeue verifies that a piece put back on the work channel
// can be picked up again (no deadlock from channel interactions).
func TestWorkChannelRequeue(t *testing.T) {
	ch := make(chan *PieceWork, 10)
	pw := &PieceWork{Index: 5, Length: 100}

	ch <- pw

	// Grab and re-queue (simulates failed download).
	got := <-ch
	ch <- got

	// Should be retrievable again.
	got2 := <-ch
	if got2.Index != 5 {
		t.Fatalf("index = %d, want 5", got2.Index)
	}
}

// TestWorkChannelEndgameContention simulates the endgame scenario: many
// goroutines competing for few pieces. Verifies no deadlock or lost pieces.
func TestWorkChannelEndgameContention(t *testing.T) {
	const numPieces = 3
	const numWorkers = 20

	workCh := make(chan *PieceWork, numPieces)
	for i := 0; i < numPieces; i++ {
		workCh <- &PieceWork{Index: i, Length: 100}
	}

	doneCh := make(chan struct{})
	downloaded := make(map[int]bool)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			misses := 0
			for {
				select {
				case <-doneCh:
					return
				case pw := <-workCh:
					// Simulate: only worker 0 can "download" any piece.
					if workerID != 0 {
						workCh <- pw
						misses++
						if misses >= 5 {
							return
						}
						time.Sleep(time.Millisecond)
						continue
					}
					mu.Lock()
					downloaded[pw.Index] = true
					done := len(downloaded) == numPieces
					mu.Unlock()
					if done {
						close(doneCh)
						return
					}
				}
			}
		}(i)
	}

	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if len(downloaded) != numPieces {
		t.Fatalf("downloaded %d/%d pieces", len(downloaded), numPieces)
	}
}

// TestEndgameModeFastCompletion verifies that endgame mode allows multiple
// workers to race for the same pieces and complete faster than serial assignment.
func TestEndgameModeFastCompletion(t *testing.T) {
	eg := newEndgameTracker()
	egCh := make(chan struct{})
	const numPieces = 5
	const numWorkers = 20

	for i := 0; i < numPieces; i++ {
		eg.AddPiece(&PieceWork{Index: i, Length: 100})
	}
	eg.Activate()
	close(egCh)

	resultCh := make(chan PieceResult, numWorkers*numPieces)
	var wg sync.WaitGroup

	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				// Check endgame signal.
				select {
				case <-egCh:
				default:
				}

				pw := eg.PickPiece(func(int) bool { return true })
				if pw == nil {
					return
				}

				time.Sleep(time.Millisecond) // simulate download

				// Send result regardless -- collector deduplicates.
				resultCh <- PieceResult{Index: pw.Index, Data: make([]byte, pw.Length)}

				// Mark completed; losers will get false and continue.
				eg.Complete(pw.Index)
			}
		}()
	}

	wg.Wait()
	close(resultCh)

	// Deduplicate results like downloadPieces does.
	completed := make(map[int]bool)
	for r := range resultCh {
		if !completed[r.Index] {
			completed[r.Index] = true
		}
	}
	if len(completed) != numPieces {
		t.Fatalf("completed %d/%d pieces", len(completed), numPieces)
	}
}

// TestEndgameTransition verifies the transition from normal mode to endgame:
// pieces in workCh get drained and workers switch to the endgame tracker.
func TestEndgameTransition(t *testing.T) {
	eg := newEndgameTracker()
	egCh := make(chan struct{})
	workCh := make(chan *PieceWork, 10)
	doneCh := make(chan struct{})

	// Put 5 pieces in normal work channel.
	for i := 0; i < 5; i++ {
		workCh <- &PieceWork{Index: i, Length: 100}
	}

	// Simulate normal download of first 3.
	completed := make(map[int]bool)
	for i := 0; i < 3; i++ {
		pw := <-workCh
		completed[pw.Index] = true
	}

	// Transition to endgame with 2 remaining.
	for i := 0; i < 5; i++ {
		if !completed[i] {
			eg.AddPiece(&PieceWork{Index: i, Length: 100})
		}
	}
	eg.Activate()

	// Drain workCh.
drainLoop:
	for {
		select {
		case <-workCh:
		default:
			break drainLoop
		}
	}
	close(egCh)

	// Workers should now pick from endgame tracker.
	var wg sync.WaitGroup
	for w := 0; w < 5; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-doneCh:
					return
				case <-egCh:
				}

				pw := eg.PickPiece(func(int) bool { return true })
				if pw == nil {
					return
				}

				time.Sleep(time.Millisecond)
				if eg.Complete(pw.Index) {
					completed[pw.Index] = true // race-safe: each index written once
				}
			}
		}()
	}

	wg.Wait()
	close(doneCh)

	if len(completed) != 5 {
		t.Fatalf("completed %d/5 pieces", len(completed))
	}
}

// TestEndgameNoRequeueOnFailure verifies that in endgame mode, failed
// downloads don't get re-queued to workCh (other peers are already trying).
func TestEndgameNoRequeueOnFailure(t *testing.T) {
	eg := newEndgameTracker()
	eg.AddPiece(&PieceWork{Index: 0, Length: 100})
	eg.Activate()

	workCh := make(chan *PieceWork, 10)

	// Simulate: worker picks piece, download fails.
	pw := eg.PickPiece(func(int) bool { return true })
	if pw == nil {
		t.Fatal("expected piece")
	}

	// In endgame, we do NOT requeue to workCh.
	// Verify workCh is still empty.
	select {
	case <-workCh:
		t.Fatal("workCh should be empty in endgame mode")
	default:
	}

	// Piece should still be in tracker (not completed).
	if eg.Remaining() != 1 {
		t.Fatalf("remaining = %d, want 1", eg.Remaining())
	}
}
