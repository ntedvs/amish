// Package metainfo handles parsing of BitTorrent metadata (the "info" dictionary).
// This metadata is obtained either from a .torrent file or fetched from peers via BEP 9.
package metainfo

import (
	"crypto/sha1"
	"errors"
	"fmt"

	"amish/bencode"
)

var (
	ErrInvalidMetadata = errors.New("metainfo: invalid metadata")
	ErrHashMismatch    = errors.New("metainfo: info hash mismatch")
)

// Info holds the parsed torrent metadata.
type Info struct {
	Name        string     // suggested file/directory name
	PieceLength int64      // bytes per piece
	Pieces      [][20]byte // SHA1 hash for each piece
	Length      int64      // total length for single-file torrents (0 if multi-file)
	Files       []File     // file list for multi-file torrents
}

// File represents a single file in a multi-file torrent.
type File struct {
	Length int64
	Path   []string // path components relative to the torrent name
}

// TotalLength returns the total size of all files in the torrent.
func (info *Info) TotalLength() int64 {
	if info.Length > 0 {
		return info.Length
	}
	var total int64
	for _, f := range info.Files {
		total += f.Length
	}
	return total
}

// NumPieces returns the total number of pieces.
func (info *Info) NumPieces() int {
	return len(info.Pieces)
}

// PieceSize returns the size of the given piece index.
// The last piece may be smaller than PieceLength.
func (info *Info) PieceSize(index int) int64 {
	total := info.TotalLength()
	start := int64(index) * info.PieceLength
	remaining := total - start
	if remaining < info.PieceLength {
		return remaining
	}
	return info.PieceLength
}

// Parse decodes raw bencoded metadata bytes into an Info struct.
// It also verifies the SHA1 hash of the raw bytes against expectedHash.
func Parse(raw []byte, expectedHash [20]byte) (*Info, error) {
	hash := sha1.Sum(raw)
	if hash != expectedHash {
		return nil, ErrHashMismatch
	}

	return ParseUnverified(raw)
}

// ParseUnverified decodes raw bencoded metadata without hash verification.
// Used internally and for testing.
func ParseUnverified(raw []byte) (*Info, error) {
	decoded, err := bencode.Decode(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidMetadata, err)
	}

	dict, ok := decoded.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%w: expected dict", ErrInvalidMetadata)
	}

	info := &Info{}

	// Name (required).
	name, ok := dict["name"].(string)
	if !ok || name == "" {
		return nil, fmt.Errorf("%w: missing name", ErrInvalidMetadata)
	}
	info.Name = name

	// Piece length (required).
	pl, ok := dict["piece length"].(int64)
	if !ok || pl <= 0 {
		return nil, fmt.Errorf("%w: invalid piece length", ErrInvalidMetadata)
	}
	info.PieceLength = pl

	// Pieces (required) — concatenated 20-byte SHA1 hashes.
	piecesStr, ok := dict["pieces"].(string)
	if !ok {
		return nil, fmt.Errorf("%w: missing pieces", ErrInvalidMetadata)
	}
	piecesRaw := []byte(piecesStr)
	if len(piecesRaw)%20 != 0 {
		return nil, fmt.Errorf("%w: pieces length %d not multiple of 20", ErrInvalidMetadata, len(piecesRaw))
	}
	info.Pieces = make([][20]byte, len(piecesRaw)/20)
	for i := range info.Pieces {
		copy(info.Pieces[i][:], piecesRaw[i*20:(i+1)*20])
	}

	// Single-file or multi-file?
	if length, ok := dict["length"].(int64); ok {
		info.Length = length
	} else if files, ok := dict["files"].([]any); ok {
		for i, f := range files {
			fd, ok := f.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("%w: file entry %d is not a dict", ErrInvalidMetadata, i)
			}
			fl, ok := fd["length"].(int64)
			if !ok {
				return nil, fmt.Errorf("%w: file entry %d missing length", ErrInvalidMetadata, i)
			}
			pathList, ok := fd["path"].([]any)
			if !ok || len(pathList) == 0 {
				return nil, fmt.Errorf("%w: file entry %d missing path", ErrInvalidMetadata, i)
			}
			var path []string
			for _, p := range pathList {
				if s, ok := p.(string); ok {
					path = append(path, s)
				}
			}
			if len(path) == 0 {
				return nil, fmt.Errorf("%w: file entry %d has empty path", ErrInvalidMetadata, i)
			}
			info.Files = append(info.Files, File{Length: fl, Path: path})
		}
		if len(info.Files) == 0 {
			return nil, fmt.Errorf("%w: no files", ErrInvalidMetadata)
		}
	} else {
		return nil, fmt.Errorf("%w: missing length or files", ErrInvalidMetadata)
	}

	return info, nil
}
