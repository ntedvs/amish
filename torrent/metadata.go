package torrent

import (
	"time"

	"amish/metainfo"
	"amish/peer"
	"amish/tracker"
)

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
				resCh <- result{err: peer.ErrNoExtension}
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
		return nil, peer.ErrNoMetadataSize
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
		subID := msg.Payload[0]
		if int64(subID) != 1 {
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

// dedupPeers merges new peers into existing, deduplicating by address.
func dedupPeers(existing, new []tracker.Peer) []tracker.Peer {
	seen := make(map[string]bool, len(existing))
	for _, p := range existing {
		seen[p.Addr()] = true
	}
	result := existing
	for _, p := range new {
		if !seen[p.Addr()] {
			seen[p.Addr()] = true
			result = append(result, p)
		}
	}
	return result
}
