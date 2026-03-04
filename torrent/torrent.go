package torrent

import (
	"crypto/rand"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"amish/dht"
	"amish/magnet"
	"amish/metainfo"
	"amish/tracker"
)

var (
	ErrNoPeers    = errors.New("torrent: no peers found")
	ErrNoMetadata = errors.New("torrent: failed to fetch metadata from any peer")
	ErrAllFailed  = errors.New("torrent: all piece downloads failed")
)

// LogFunc is a callback for status messages.
type LogFunc func(format string, args ...any)

// Stats tracks download progress.
type Stats struct {
	TotalPieces     int32
	Downloaded      int32
	TotalBytes      int64
	DownloadedBytes int64
	StartTime       time.Time
	ActivePeers     int32
}

// Progress returns the download progress as a fraction [0, 1].
func (s *Stats) Progress() float64 {
	total := atomic.LoadInt32(&s.TotalPieces)
	if total == 0 {
		return 0
	}
	downloaded := atomic.LoadInt32(&s.Downloaded)
	return float64(downloaded) / float64(total)
}

// GetStats returns a snapshot of current download stats.
func (s *Stats) GetStats() (downloaded, total int, bytes int64, peers int) {
	downloaded = int(atomic.LoadInt32(&s.Downloaded))
	total = int(atomic.LoadInt32(&s.TotalPieces))
	bytes = atomic.LoadInt64(&s.TotalBytes)
	peers = int(atomic.LoadInt32(&s.ActivePeers))
	return
}

// BytesPerSecond returns the current download speed.
func (s *Stats) BytesPerSecond() float64 {
	elapsed := time.Since(s.StartTime).Seconds()
	if elapsed == 0 {
		return 0
	}
	return float64(atomic.LoadInt64(&s.DownloadedBytes)) / elapsed
}

// Torrent manages the full download lifecycle.
type Torrent struct {
	Magnet    magnet.Magnet
	Info      *metainfo.Info
	PeerID    [20]byte
	Stats     Stats
	OutputDir string
	Log       LogFunc
	peers     []tracker.Peer
}

// New creates a new Torrent from a parsed magnet link.
func New(m magnet.Magnet, outputDir string) *Torrent {
	var peerID [20]byte
	copy(peerID[:], "-AM0001-") // AM = Amish
	rand.Read(peerID[8:])

	return &Torrent{
		Magnet:    m,
		PeerID:    peerID,
		OutputDir: outputDir,
		Log:       func(string, ...any) {}, // no-op default
	}
}

// DiscoverAndFetchMetadata contacts trackers, finds peers, and fetches torrent metadata.
// Call this first, then call Download to start the piece download.
func (t *Torrent) DiscoverAndFetchMetadata() error {
	t.Log("contacting %d trackers...", len(t.Magnet.Trackers))
	peers, err := t.discoverPeers()
	if err != nil {
		return err
	}
	t.Log("found %d peers from trackers", len(peers))

	t.Log("starting DHT peer discovery...")
	dhtPeers := t.discoverDHTPeers()
	t.Log("found %d peers from DHT", len(dhtPeers))

	allPeers := dedupPeers(peers, dhtPeers)

	t.peers = allPeers
	t.Log("total peers: %d", len(allPeers))

	t.Log("fetching torrent metadata...")
	info, err := t.fetchMetadata(allPeers)
	if err != nil {
		return err
	}
	t.Info = info
	return nil
}

func (t *Torrent) discoverDHTPeers() []tracker.Peer {
	dhtClient := dht.New()
	if err := dhtClient.Listen(":0"); err != nil {
		t.Log("DHT listen failed: %v", err)
		return nil
	}
	defer dhtClient.Close()
	dhtClient.Bootstrap(dht.BootstrapNodes)

	peerCh := dhtClient.GetPeers(t.Magnet.InfoHash)

	var dhtPeers []tracker.Peer
	timeout := time.After(3 * time.Second)
	for {
		select {
		case p, ok := <-peerCh:
			if !ok {
				return dhtPeers
			}
			dhtPeers = append(dhtPeers, p)
		case <-timeout:
			return dhtPeers
		}
	}
}

// Download starts downloading all pieces. Call after DiscoverAndFetchMetadata.
// Periodically re-announces to trackers to discover new peers.
func (t *Torrent) Download() error {
	t.Stats = Stats{
		TotalPieces: int32(t.Info.NumPieces()),
		TotalBytes:  t.Info.TotalLength(),
		StartTime:   time.Now(),
	}

	return t.downloadPieces(t.peers)
}

func (t *Torrent) discoverPeers() ([]tracker.Peer, error) {
	params := tracker.AnnounceParams{
		InfoHash: t.Magnet.InfoHash,
		PeerID:   t.PeerID,
		Port:     6881,
		Left:     0, // we don't know the size yet
	}

	type result struct {
		peers []tracker.Peer
		url   string
		err   error
	}

	results := make(chan result, len(t.Magnet.Trackers))

	for _, tr := range t.Magnet.Trackers {
		go func(url string) {
			peers, err := tracker.Announce(url, params)
			results <- result{peers: peers, url: url, err: err}
		}(tr)
	}

	var allPeers []tracker.Peer
	for i := 0; i < len(t.Magnet.Trackers); i++ {
		r := <-results
		if r.err != nil {
			t.Log("  tracker %s: %v", r.url, r.err)
			continue
		}
		t.Log("  tracker %s: %d peers", r.url, len(r.peers))
		allPeers = append(allPeers, r.peers...)
	}

	unique := dedupPeers(nil, allPeers)
	if len(unique) == 0 {
		return nil, ErrNoPeers
	}
	return unique, nil
}

func (t *Torrent) reannounceLoop(newPeerCh chan<- tracker.Peer, doneCh <-chan struct{}) {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-doneCh:
			return
		case <-ticker.C:
		}

		params := tracker.AnnounceParams{
			InfoHash: t.Magnet.InfoHash,
			PeerID:   t.PeerID,
			Port:     6881,
			Left:     t.Info.TotalLength() - t.Stats.DownloadedBytes,
		}

		for _, tr := range t.Magnet.Trackers {
			go func(url string) {
				peers, err := tracker.Announce(url, params)
				if err != nil {
					t.Log("reannounce %s: %v", url, err)
					return
				}
				for _, p := range peers {
					select {
					case newPeerCh <- p:
					default:
					}
				}
			}(tr)
		}
	}
}

// downloadCtx bundles the channels and trackers shared by all peer workers.
type downloadCtx struct {
	workCh   chan *PieceWork
	resultCh chan PieceResult
	doneCh   chan struct{} // closed when download is complete
	endgame  *endgameTracker
	egCh     chan struct{} // closed when endgame mode activates
}

func (t *Torrent) downloadPieces(peers []tracker.Peer) error {
	ctx := &downloadCtx{
		workCh:   make(chan *PieceWork, t.Info.NumPieces()),
		resultCh: make(chan PieceResult),
		doneCh:   make(chan struct{}),
		endgame:  newEndgameTracker(),
		egCh:     make(chan struct{}),
	}

	// Re-announce in the background to find more peers.
	newPeerCh := make(chan tracker.Peer, 100)
	go t.reannounceLoop(newPeerCh, ctx.doneCh)

	// Fill work queue.
	for i := 0; i < t.Info.NumPieces(); i++ {
		ctx.workCh <- &PieceWork{
			Index:  i,
			Hash:   t.Info.Pieces[i],
			Length: t.Info.PieceSize(i),
		}
	}

	// Track which peer addresses have active workers (max 2 per address).
	const maxWorkersPerPeer = 2
	var workerMu sync.Mutex
	activeWorkers := make(map[string]int)

	startWorker := func(addr string, wg *sync.WaitGroup) {
		workerMu.Lock()
		if activeWorkers[addr] >= maxWorkersPerPeer {
			workerMu.Unlock()
			return
		}
		activeWorkers[addr]++
		workerMu.Unlock()

		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				workerMu.Lock()
				activeWorkers[addr]--
				if activeWorkers[addr] == 0 {
					delete(activeWorkers, addr)
				}
				workerMu.Unlock()
			}()
			t.peerWorker(addr, ctx)
		}()
	}

	// Start initial workers.
	var wg sync.WaitGroup
	for _, p := range peers {
		startWorker(p.Addr(), &wg)
	}

	// Spawn workers for newly discovered peers.
	go func() {
		for {
			select {
			case p := <-newPeerCh:
				startWorker(p.Addr(), &wg)
			case <-ctx.doneCh:
				return
			}
		}
	}()

	// Close results channel when all workers done.
	go func() {
		wg.Wait()
		close(ctx.resultCh)
	}()

	// Collect results.
	writer := NewWriter(t.Info, t.OutputDir)
	defer writer.Close()
	done := 0
	completed := make(map[int]bool)

	for result := range ctx.resultCh {
		if result.Err != nil {
			continue
		}

		// Discard duplicate results from endgame.
		if completed[result.Index] {
			continue
		}
		completed[result.Index] = true
		if ctx.endgame.IsActive() {
			ctx.endgame.Complete(result.Index)
		}

		if err := writer.WritePiece(result.Index, result.Data); err != nil {
			return fmt.Errorf("write piece %d: %w", result.Index, err)
		}

		done++
		atomic.AddInt32(&t.Stats.Downloaded, 1)
		atomic.AddInt64(&t.Stats.DownloadedBytes, int64(len(result.Data)))

		if done >= t.Info.NumPieces() {
			close(ctx.doneCh)
			break
		}

		// Enter endgame when few pieces remain.
		remaining := t.Info.NumPieces() - done
		if remaining <= EndgameThreshold && !ctx.endgame.IsActive() {
			t.transitionToEndgame(ctx, completed)
		}
	}

	if done < t.Info.NumPieces() {
		return fmt.Errorf("%w: got %d/%d pieces", ErrAllFailed, done, t.Info.NumPieces())
	}

	return nil
}

// transitionToEndgame activates endgame mode, adding all uncompleted pieces
// and draining the normal work channel so workers switch to endgame picks.
func (t *Torrent) transitionToEndgame(ctx *downloadCtx, completed map[int]bool) {
	for i := 0; i < t.Info.NumPieces(); i++ {
		if !completed[i] {
			ctx.endgame.AddPiece(&PieceWork{
				Index:  i,
				Hash:   t.Info.Pieces[i],
				Length: t.Info.PieceSize(i),
			})
		}
	}
	ctx.endgame.Activate()
	for {
		select {
		case <-ctx.workCh:
		default:
			close(ctx.egCh)
			return
		}
	}
}


