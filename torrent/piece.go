// Package torrent orchestrates the download of a torrent.
// This file handles individual piece downloading and verification.
package torrent

import (
	"crypto/sha1"
	"errors"
	"fmt"
	"time"

	"amish/peer"
)

var (
	ErrChoked    = errors.New("choked")
	ErrPieceFail = errors.New("piece download failed")
)

// PieceResult holds the outcome of downloading a single piece.
type PieceResult struct {
	Index int
	Data  []byte
	Err   error
}

// PieceWork describes a piece to be downloaded.
type PieceWork struct {
	Index    int
	Hash     [20]byte
	Length   int64
	NumTries int
}

// MaxPieceTries is the maximum number of download attempts per piece.
const MaxPieceTries = 5

// DownloadPiece fetches all blocks of a piece from a peer, assembles them,
// and verifies the SHA1 hash. Returns ErrChoked if the peer chokes us
// (caller should wait for unchoke and retry, not abandon the connection).
func DownloadPiece(conn *peer.Conn, pw *PieceWork) PieceResult {
	// Per-piece deadline -- 30s or 5s per block, whichever is larger.
	numBlocks := (int(pw.Length) + peer.BlockSize - 1) / peer.BlockSize
	timeout := time.Duration(numBlocks*5) * time.Second
	if timeout < 30*time.Second {
		timeout = 30 * time.Second
	}
	conn.SetDeadline(time.Now().Add(timeout))

	buf := make([]byte, pw.Length)
	downloaded := 0
	requested := 0
	backlog := 0
	maxBacklog := 5 // pipelined requests

	for downloaded < int(pw.Length) {
		// Send requests until we hit the backlog limit.
		for backlog < maxBacklog && requested < int(pw.Length) {
			blockSize := peer.BlockSize
			remaining := int(pw.Length) - requested
			if remaining < blockSize {
				blockSize = remaining
			}

			msg := peer.NewRequest(uint32(pw.Index), uint32(requested), uint32(blockSize))
			if err := conn.SendMessage(msg); err != nil {
				return PieceResult{Index: pw.Index, Err: fmt.Errorf("request: %w", err)}
			}

			requested += blockSize
			backlog++
		}

		// Read a response.
		msg, err := conn.RecvMessage()
		if err != nil {
			return PieceResult{Index: pw.Index, Err: fmt.Errorf("recv: %w", err)}
		}
		if msg == nil {
			continue // keep-alive
		}

		switch msg.ID {
		case peer.MsgPiece:
			idx, begin, block, err := peer.ParsePiece(msg.Payload)
			if err != nil {
				return PieceResult{Index: pw.Index, Err: err}
			}
			if int(idx) != pw.Index {
				continue // wrong piece
			}
			if int(begin)+len(block) > len(buf) {
				continue // out of bounds, skip
			}
			copy(buf[begin:], block)
			downloaded += len(block)
			backlog--

		case peer.MsgChoke:
			// Return a typed error so the worker knows to wait for unchoke.
			return PieceResult{Index: pw.Index, Err: ErrChoked}

		default:
			continue
		}
	}

	// Verify hash.
	hash := sha1.Sum(buf)
	if hash != pw.Hash {
		return PieceResult{Index: pw.Index, Err: fmt.Errorf("hash mismatch for piece %d", pw.Index)}
	}

	return PieceResult{Index: pw.Index, Data: buf}
}

// VerifyPiece checks a piece's data against its expected SHA1 hash.
func VerifyPiece(data []byte, expected [20]byte) bool {
	hash := sha1.Sum(data)
	return hash == expected
}
