package torrent

import (
	"sync"
	"sync/atomic"
)

// EndgameThreshold is the number of remaining pieces at which endgame
// mode activates. In endgame, all peers race for the same pieces and
// the first to complete wins.
const EndgameThreshold = 20

// endgameTracker manages the endgame phase where multiple peers
// download the same pieces simultaneously.
type endgameTracker struct {
	active int32 // atomic: 1 when active

	mu        sync.Mutex
	pieces    []*PieceWork
	completed map[int]bool
	offset    int // round-robin offset for PickPiece
}

func newEndgameTracker() *endgameTracker {
	return &endgameTracker{
		completed: make(map[int]bool),
	}
}

// Activate enables endgame mode.
func (e *endgameTracker) Activate() {
	atomic.StoreInt32(&e.active, 1)
}

// IsActive reports whether endgame mode is enabled.
func (e *endgameTracker) IsActive() bool {
	return atomic.LoadInt32(&e.active) == 1
}

// AddPiece adds a piece to the endgame set. No-op if already completed.
func (e *endgameTracker) AddPiece(pw *PieceWork) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.completed[pw.Index] {
		e.pieces = append(e.pieces, pw)
	}
}

// PickPiece returns a piece for a worker to attempt. hasPiece checks
// whether the peer has the piece. Multiple workers can pick the same piece.
// Uses round-robin to spread workers across pieces.
func (e *endgameTracker) PickPiece(hasPiece func(int) bool) *PieceWork {
	e.mu.Lock()
	defer e.mu.Unlock()
	n := len(e.pieces)
	if n == 0 {
		return nil
	}
	e.offset++
	start := e.offset % n
	for i := 0; i < n; i++ {
		pw := e.pieces[(start+i)%n]
		if hasPiece(pw.Index) {
			return pw
		}
	}
	return nil
}

// Complete marks a piece as downloaded. Returns true if this was the
// first completion (caller should write the data). Returns false for
// duplicate completions from other workers.
func (e *endgameTracker) Complete(index int) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.completed[index] {
		return false
	}
	e.completed[index] = true
	for i, pw := range e.pieces {
		if pw.Index == index {
			e.pieces[i] = e.pieces[len(e.pieces)-1]
			e.pieces = e.pieces[:len(e.pieces)-1]
			break
		}
	}
	return true
}

// Remaining returns the count of uncompleted pieces.
func (e *endgameTracker) Remaining() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.pieces)
}
