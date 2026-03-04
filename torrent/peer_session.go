package torrent

import (
	"errors"
	"sync/atomic"
	"time"

	"amish/peer"
)

// peerWorker manages a single peer connection. It handles:
// - Bitfield tracking (only request pieces the peer has)
// - Choke/unchoke (wait and resume instead of dying)
// - Reconnection on transient errors
// - Endgame mode (race with other peers for remaining pieces)
func (t *Torrent) peerWorker(addr string, ctx *downloadCtx) {
	const maxReconnects = 3

	for attempt := 0; attempt <= maxReconnects; attempt++ {
		select {
		case <-ctx.doneCh:
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

		t.runPeerSession(conn, ctx)
		conn.Close()
	}
}

func (t *Torrent) runPeerSession(conn *peer.Conn, ctx *downloadCtx) {
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
		case <-ctx.doneCh:
			return
		default:
		}

		// Check if endgame mode is active.
		var pw *PieceWork
		inEndgame := false
		select {
		case <-ctx.egCh:
			inEndgame = true
		default:
		}

		if inEndgame {
			hasPiece := func(idx int) bool {
				return bitfield == nil || bitfield.HasPiece(idx)
			}
			pw = ctx.endgame.PickPiece(hasPiece)
			if pw == nil {
				return
			}
		} else {
			select {
			case <-ctx.doneCh:
				return
			case <-ctx.egCh:
				continue
			case p, ok := <-ctx.workCh:
				if !ok {
					return
				}
				pw = p
			}

			if bitfield != nil && !bitfield.HasPiece(pw.Index) {
				ctx.workCh <- pw
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
				ctx.workCh <- pw
			}

			if errors.Is(result.Err, ErrChoked) {
				if t.waitForUnchoke(conn, &bitfield) {
					continue
				}
			}

			return
		}

		ctx.resultCh <- result
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
