package torrent

import (
	"crypto/rand"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"amish/dht"
	"amish/magnet"
	"amish/metainfo"
	"amish/peer"
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

	allPeers := peers
	for _, p := range dhtPeers {
		found := false
		for _, ep := range peers {
			if p.Addr() == ep.Addr() {
				found = true
				break
			}
		}
		if !found {
			allPeers = append(allPeers, p)
		}
	}

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
			dhtPeers = append(dhtPeers, tracker.Peer{IP: net.ParseIP(p.IP), Port: uint16(p.Port)})
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

	// Re-announce in the background to find more peers.
	newPeerCh := make(chan tracker.Peer, 100)
	go t.reannounceLoop(newPeerCh)

	return t.downloadPieces(t.peers, newPeerCh)
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

	// Deduplicate.
	seen := make(map[string]bool)
	var unique []tracker.Peer
	for _, p := range allPeers {
		addr := p.Addr()
		if !seen[addr] {
			seen[addr] = true
			unique = append(unique, p)
		}
	}

	if len(unique) == 0 {
		return nil, ErrNoPeers
	}
	return unique, nil
}

// metadataPieceSize is the standard size of a BEP 9 metadata piece.
const metadataPieceSize = 16384

func (t *Torrent) fetchMetadata(peers []tracker.Peer) (*metainfo.Info, error) {
	type result struct {
		info *metainfo.Info
		err  error
	}

	ctx := make(chan struct{}) // closed when we have a winner
	resCh := make(chan result, len(peers))

	// Try up to 10 peers concurrently.
	sem := make(chan struct{}, 10)

	for _, p := range peers {
		go func(addr string) {
			sem <- struct{}{}
			defer func() { <-sem }()

			// Bail early if another goroutine already succeeded.
			select {
			case <-ctx:
				return
			default:
			}

			t.Log("  trying peer %s for metadata...", addr)

			conn, err := peer.Dial(addr, t.Magnet.InfoHash, t.PeerID, 5*time.Second)
			if err != nil {
				resCh <- result{err: err}
				return
			}
			defer conn.Close()

			if !conn.SupportsExtension {
				resCh <- result{err: errors.New("no extension support")}
				return
			}

			if err := conn.SendExtensionHandshake(); err != nil {
				resCh <- result{err: err}
				return
			}

			info, err := t.readMetadataFromPeer(conn)
			resCh <- result{info: info, err: err}
		}(p.Addr())
	}

	for i := 0; i < len(peers); i++ {
		r := <-resCh
		if r.err == nil && r.info != nil {
			close(ctx) // signal others to stop
			return r.info, nil
		}
	}

	return nil, ErrNoMetadata
}

func (t *Torrent) readMetadataFromPeer(conn *peer.Conn) (*metainfo.Info, error) {
	conn.SetDeadline(time.Now().Add(10 * time.Second))

	// Wait for extension handshake.
	for {
		msg, err := conn.RecvMessage()
		if err != nil {
			return nil, err
		}
		if msg == nil {
			continue
		}
		if msg.ID == peer.MsgExtended && len(msg.Payload) > 0 && msg.Payload[0] == 0 {
			if err := conn.HandleExtensionHandshake(msg.Payload[1:]); err != nil {
				return nil, err
			}
			break
		}
	}

	if conn.MetadataSize == 0 {
		return nil, errors.New("peer did not report metadata_size")
	}

	// Request all metadata pieces.
	numPieces := (int(conn.MetadataSize) + metadataPieceSize - 1) / metadataPieceSize
	metadata := make([]byte, conn.MetadataSize)
	received := make([]bool, numPieces)

	for i := 0; i < numPieces; i++ {
		if err := conn.RequestMetadataPiece(i); err != nil {
			return nil, err
		}
	}

	// Collect responses.
	got := 0
	for got < numPieces {
		msg, err := conn.RecvMessage()
		if err != nil {
			return nil, err
		}
		if msg == nil {
			continue
		}
		if msg.ID != peer.MsgExtended {
			continue
		}
		if len(msg.Payload) < 2 {
			continue
		}
		// Check if this is a ut_metadata message (sub-ID from our extension handshake).
		subID := msg.Payload[0]
		if int64(subID) != 1 { // our ut_metadata ID
			continue
		}

		piece, data, err := peer.ParseMetadataPiece(msg.Payload[1:])
		if err != nil {
			continue
		}

		if piece < 0 || piece >= numPieces || received[piece] {
			continue
		}

		offset := piece * metadataPieceSize
		copy(metadata[offset:], data)
		received[piece] = true
		got++
	}

	return metainfo.Parse(metadata, t.Magnet.InfoHash)
}

func (t *Torrent) reannounceLoop(newPeerCh chan<- tracker.Peer) {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
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
					return
				}
				for _, p := range peers {
					select {
					case newPeerCh <- p:
					default: // channel full, skip
					}
				}
			}(tr)
		}
	}
}

func (t *Torrent) downloadPieces(peers []tracker.Peer, newPeerCh <-chan tracker.Peer) error {
	workCh := make(chan *PieceWork, t.Info.NumPieces())
	resultCh := make(chan PieceResult)
	doneCh := make(chan struct{}) // closed when download is complete

	// Endgame: when few pieces remain, all peers race for the same pieces.
	eg := newEndgameTracker()
	egCh := make(chan struct{})

	// Fill work queue.
	for i := 0; i < t.Info.NumPieces(); i++ {
		workCh <- &PieceWork{
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
			t.peerWorker(addr, workCh, resultCh, doneCh, eg, egCh)
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
			case <-doneCh:
				return
			}
		}
	}()

	// Close results channel when all workers done.
	go func() {
		wg.Wait()
		close(resultCh)
	}()

	// Collect results.
	writer := NewWriter(t.Info, t.OutputDir)
	defer writer.Close()
	done := 0
	completed := make(map[int]bool)

	for result := range resultCh {
		if result.Err != nil {
			continue
		}

		// Discard duplicate results from endgame.
		if completed[result.Index] {
			continue
		}
		completed[result.Index] = true
		if eg.IsActive() {
			eg.Complete(result.Index)
		}

		if err := writer.WritePiece(result.Index, result.Data); err != nil {
			return fmt.Errorf("write piece %d: %w", result.Index, err)
		}

		done++
		atomic.AddInt32(&t.Stats.Downloaded, 1)
		atomic.AddInt64(&t.Stats.DownloadedBytes, int64(len(result.Data)))

		if done >= t.Info.NumPieces() {
			close(doneCh)
			break
		}

		// Enter endgame when few pieces remain.
		remaining := t.Info.NumPieces() - done
		if remaining <= EndgameThreshold && !eg.IsActive() {
			// Add all uncompleted pieces to the endgame tracker.
			for i := 0; i < t.Info.NumPieces(); i++ {
				if !completed[i] {
					eg.AddPiece(&PieceWork{
						Index:  i,
						Hash:   t.Info.Pieces[i],
						Length: t.Info.PieceSize(i),
					})
				}
			}
			eg.Activate()
			// Drain workCh so normal-mode workers unblock and switch.
		drainLoop:
			for {
				select {
				case <-workCh:
				default:
					break drainLoop
				}
			}
			close(egCh)
		}
	}

	if done < t.Info.NumPieces() {
		return fmt.Errorf("%w: got %d/%d pieces", ErrAllFailed, done, t.Info.NumPieces())
	}

	return nil
}

// peerWorker manages a single peer connection. It handles:
// - Bitfield tracking (only request pieces the peer has)
// - Choke/unchoke (wait and resume instead of dying)
// - Reconnection on transient errors
// - Endgame mode (race with other peers for remaining pieces)
func (t *Torrent) peerWorker(addr string, workCh chan *PieceWork, resultCh chan<- PieceResult, doneCh <-chan struct{}, eg *endgameTracker, egCh <-chan struct{}) {
	const maxReconnects = 3

	for attempt := 0; attempt <= maxReconnects; attempt++ {
		select {
		case <-doneCh:
			return
		default:
		}

		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * 2 * time.Second)
		}

		conn, err := peer.Dial(addr, t.Magnet.InfoHash, t.PeerID, 10*time.Second)
		if err != nil {
			continue
		}

		t.runPeerSession(conn, workCh, resultCh, doneCh, eg, egCh)
		conn.Close()
	}
}

func (t *Torrent) runPeerSession(conn *peer.Conn, workCh chan *PieceWork, resultCh chan<- PieceResult, doneCh <-chan struct{}, eg *endgameTracker, egCh <-chan struct{}) {
	// Initial handshake phase -- 30s to get through bitfield + unchoke.
	conn.SetDeadline(time.Now().Add(30 * time.Second))

	// Send interested.
	if err := conn.SendMessage(peer.NewInterested()); err != nil {
		return
	}

	// Read initial messages (bitfield, unchoke).
	var bitfield peer.Bitfield
	choked := true

	for {
		msg, err := conn.RecvMessage()
		if err != nil {
			return
		}
		if msg == nil {
			continue
		}
		switch msg.ID {
		case peer.MsgBitfield:
			bitfield = peer.Bitfield(msg.Payload)
		case peer.MsgUnchoke:
			choked = false
		case peer.MsgHave:
			if idx, err := peer.ParseHave(msg.Payload); err == nil {
				if bitfield == nil {
					bitfield = peer.NewBitfield(t.Info.NumPieces())
				}
				bitfield.SetPiece(int(idx))
			}
		}
		if !choked {
			break
		}
	}

	atomic.AddInt32(&t.Stats.ActivePeers, 1)
	defer atomic.AddInt32(&t.Stats.ActivePeers, -1)

	consecutiveMisses := 0
	for {
		select {
		case <-doneCh:
			return
		default:
		}

		// Check if endgame mode is active.
		var pw *PieceWork
		inEndgame := false
		select {
		case <-egCh:
			inEndgame = true
		default:
		}

		if inEndgame {
			hasPiece := func(idx int) bool {
				return bitfield == nil || bitfield.HasPiece(idx)
			}
			pw = eg.PickPiece(hasPiece)
			if pw == nil {
				return // can't help with remaining pieces
			}
		} else {
			select {
			case <-doneCh:
				return
			case <-egCh:
				continue // endgame just activated, re-enter loop
			case p, ok := <-workCh:
				if !ok {
					return
				}
				pw = p
			}

			// Check if this peer has the piece.
			if bitfield != nil && !bitfield.HasPiece(pw.Index) {
				workCh <- pw
				consecutiveMisses++
				if consecutiveMisses >= 15 {
					return
				}
				time.Sleep(time.Duration(consecutiveMisses) * 20 * time.Millisecond)
				continue
			}
			consecutiveMisses = 0
		}

		result := DownloadPiece(conn, pw)

		if result.Err != nil {
			if !inEndgame {
				// Re-queue in normal mode. In endgame, other peers are already trying.
				workCh <- pw
			}

			if errors.Is(result.Err, ErrChoked) {
				if t.waitForUnchoke(conn, &bitfield) {
					continue
				}
			}

			return
		}

		resultCh <- result
	}
}

// waitForUnchoke blocks until the peer unchokes us or the connection fails.
// Also processes Have messages to keep the bitfield up to date.
func (t *Torrent) waitForUnchoke(conn *peer.Conn, bitfield *peer.Bitfield) bool {
	conn.SetDeadline(time.Now().Add(60 * time.Second))

	for {
		msg, err := conn.RecvMessage()
		if err != nil {
			return false
		}
		if msg == nil {
			continue
		}
		switch msg.ID {
		case peer.MsgUnchoke:
			return true
		case peer.MsgHave:
			if idx, err := peer.ParseHave(msg.Payload); err == nil && *bitfield != nil {
				bitfield.SetPiece(int(idx))
			}
		}
	}
}

// FormatBytes formats a byte count as a human-readable string.
func FormatBytes(b int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)
	switch {
	case b >= GB:
		return fmt.Sprintf("%.2f GB", float64(b)/float64(GB))
	case b >= MB:
		return fmt.Sprintf("%.2f MB", float64(b)/float64(MB))
	case b >= KB:
		return fmt.Sprintf("%.2f KB", float64(b)/float64(KB))
	default:
		return fmt.Sprintf("%d B", b)
	}
}
