package torrent

import (
	"sync"
	"testing"
	"time"
)

func TestEndgameTrackerAddAndPick(t *testing.T) {
	eg := newEndgameTracker()
	eg.AddPiece(&PieceWork{Index: 0, Length: 100})
	eg.AddPiece(&PieceWork{Index: 1, Length: 200})

	pw := eg.PickPiece(func(int) bool { return true })
	if pw == nil {
		t.Fatal("expected a piece")
	}
	if pw.Index != 0 && pw.Index != 1 {
		t.Fatalf("unexpected index: %d", pw.Index)
	}
}

func TestEndgameTrackerPickFiltered(t *testing.T) {
	eg := newEndgameTracker()
	eg.AddPiece(&PieceWork{Index: 0, Length: 100})
	eg.AddPiece(&PieceWork{Index: 1, Length: 200})

	pw := eg.PickPiece(func(idx int) bool { return idx == 1 })
	if pw == nil {
		t.Fatal("expected piece 1")
	}
	if pw.Index != 1 {
		t.Fatalf("got index %d, want 1", pw.Index)
	}
}

func TestEndgameTrackerPickNone(t *testing.T) {
	eg := newEndgameTracker()
	eg.AddPiece(&PieceWork{Index: 0, Length: 100})

	pw := eg.PickPiece(func(int) bool { return false })
	if pw != nil {
		t.Fatal("expected nil when no pieces match")
	}
}

func TestEndgameTrackerPickEmpty(t *testing.T) {
	eg := newEndgameTracker()
	pw := eg.PickPiece(func(int) bool { return true })
	if pw != nil {
		t.Fatal("expected nil from empty tracker")
	}
}

func TestEndgameTrackerPickRoundRobin(t *testing.T) {
	eg := newEndgameTracker()
	eg.AddPiece(&PieceWork{Index: 0, Length: 100})
	eg.AddPiece(&PieceWork{Index: 1, Length: 100})
	eg.AddPiece(&PieceWork{Index: 2, Length: 100})

	seen := make(map[int]int)
	for i := 0; i < 30; i++ {
		pw := eg.PickPiece(func(int) bool { return true })
		seen[pw.Index]++
	}
	// Round-robin should hit all three pieces.
	if len(seen) != 3 {
		t.Fatalf("expected 3 distinct pieces, got %d: %v", len(seen), seen)
	}
	// Each should get 10 picks.
	for idx, count := range seen {
		if count != 10 {
			t.Fatalf("piece %d picked %d times, want 10", idx, count)
		}
	}
}

func TestEndgameTrackerComplete(t *testing.T) {
	eg := newEndgameTracker()
	eg.AddPiece(&PieceWork{Index: 0, Length: 100})
	eg.AddPiece(&PieceWork{Index: 1, Length: 200})

	if !eg.Complete(0) {
		t.Fatal("first completion should return true")
	}
	if eg.Complete(0) {
		t.Fatal("duplicate completion should return false")
	}
	if eg.Remaining() != 1 {
		t.Fatalf("remaining = %d, want 1", eg.Remaining())
	}
}

func TestEndgameTrackerCompleteUnknown(t *testing.T) {
	eg := newEndgameTracker()
	eg.AddPiece(&PieceWork{Index: 0, Length: 100})

	// Completing an index not in the tracker.
	if !eg.Complete(99) {
		t.Fatal("completing unknown index should return true (first time)")
	}
	if eg.Complete(99) {
		t.Fatal("duplicate should return false")
	}
	// Original piece still there.
	if eg.Remaining() != 1 {
		t.Fatalf("remaining = %d, want 1", eg.Remaining())
	}
}

func TestEndgameTrackerCompletedPieceNotReadded(t *testing.T) {
	eg := newEndgameTracker()
	eg.AddPiece(&PieceWork{Index: 0, Length: 100})
	eg.Complete(0)

	eg.AddPiece(&PieceWork{Index: 0, Length: 100})
	if eg.Remaining() != 0 {
		t.Fatalf("remaining = %d, want 0", eg.Remaining())
	}
}

func TestEndgameTrackerActivate(t *testing.T) {
	eg := newEndgameTracker()
	if eg.IsActive() {
		t.Fatal("should not be active initially")
	}
	eg.Activate()
	if !eg.IsActive() {
		t.Fatal("should be active after Activate")
	}
}

func TestEndgameTrackerConcurrentComplete(t *testing.T) {
	eg := newEndgameTracker()
	const numPieces = 50
	for i := 0; i < numPieces; i++ {
		eg.AddPiece(&PieceWork{Index: i, Length: 100})
	}

	var wg sync.WaitGroup
	wins := make([]int32, numPieces)
	var mu sync.Mutex
	_ = mu

	for g := 0; g < 10; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < numPieces; i++ {
				if eg.Complete(i) {
					// atomic isn't needed since Complete serializes,
					// but belt-and-suspenders
					wins[i]++
				}
			}
		}()
	}
	wg.Wait()

	for i, w := range wins {
		if w != 1 {
			t.Fatalf("piece %d had %d winners, want exactly 1", i, w)
		}
	}
	if eg.Remaining() != 0 {
		t.Fatalf("remaining = %d, want 0", eg.Remaining())
	}
}

func TestEndgameTrackerConcurrentPickAndComplete(t *testing.T) {
	eg := newEndgameTracker()
	const numPieces = 10
	for i := 0; i < numPieces; i++ {
		eg.AddPiece(&PieceWork{Index: i, Length: 100})
	}

	var wg sync.WaitGroup
	completed := make(map[int]bool)
	var mu sync.Mutex

	for g := 0; g < 20; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				pw := eg.PickPiece(func(int) bool { return true })
				if pw == nil {
					return
				}
				time.Sleep(time.Millisecond)
				if eg.Complete(pw.Index) {
					mu.Lock()
					completed[pw.Index] = true
					mu.Unlock()
				}
			}
		}()
	}
	wg.Wait()

	if len(completed) != numPieces {
		t.Fatalf("completed %d/%d pieces", len(completed), numPieces)
	}
}

func TestEndgameThresholdPositive(t *testing.T) {
	if EndgameThreshold <= 0 {
		t.Fatal("EndgameThreshold must be positive")
	}
}

// TestEndgameRaceSimulation simulates the full endgame flow: multiple
// workers pick pieces, download (with varying latency), and complete.
// First completion wins, duplicates discarded.
func TestEndgameRaceSimulation(t *testing.T) {
	eg := newEndgameTracker()
	const numPieces = 5
	for i := 0; i < numPieces; i++ {
		eg.AddPiece(&PieceWork{Index: i, Length: 100})
	}
	eg.Activate()

	resultCh := make(chan PieceResult, 100)
	var wg sync.WaitGroup

	for w := 0; w < 10; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				pw := eg.PickPiece(func(int) bool { return true })
				if pw == nil {
					return
				}
				time.Sleep(time.Millisecond)
				if eg.Complete(pw.Index) {
					resultCh <- PieceResult{Index: pw.Index, Data: make([]byte, pw.Length)}
				}
			}
		}()
	}

	wg.Wait()
	close(resultCh)

	got := make(map[int]bool)
	for r := range resultCh {
		if got[r.Index] {
			t.Fatalf("duplicate result for piece %d", r.Index)
		}
		got[r.Index] = true
	}
	if len(got) != numPieces {
		t.Fatalf("got %d pieces, want %d", len(got), numPieces)
	}
}
