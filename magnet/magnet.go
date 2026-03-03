// Package magnet parses magnet URIs into their component parts.
// A BitTorrent magnet URI contains an info hash, optional display name,
// and optional tracker URLs.
package magnet

import (
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

var (
	ErrInvalidScheme   = errors.New("magnet: not a magnet URI")
	ErrMissingInfoHash = errors.New("magnet: missing info hash (xt parameter)")
	ErrInvalidInfoHash = errors.New("magnet: invalid info hash")
)

// Magnet holds the parsed components of a magnet URI.
type Magnet struct {
	InfoHash [20]byte // SHA1 info hash
	Name     string   // suggested display name (dn parameter)
	Trackers []string // tracker announce URLs (tr parameters)
}

// Parse extracts the info hash, display name, and tracker URLs from a magnet URI.
// Expected format: magnet:?xt=urn:btih:<40-hex-chars>&dn=...&tr=...
func Parse(raw string) (Magnet, error) {
	if !strings.HasPrefix(raw, "magnet:?") {
		return Magnet{}, ErrInvalidScheme
	}

	// Parse everything after "magnet:?"
	query := raw[len("magnet:?"):]
	params, err := url.ParseQuery(query)
	if err != nil {
		return Magnet{}, fmt.Errorf("magnet: invalid query: %w", err)
	}

	xt := params.Get("xt")
	if xt == "" {
		return Magnet{}, ErrMissingInfoHash
	}

	infoHash, err := parseInfoHash(xt)
	if err != nil {
		return Magnet{}, err
	}

	m := Magnet{
		InfoHash: infoHash,
		Name:     params.Get("dn"),
		Trackers: params["tr"],
	}

	return m, nil
}

// HexHash returns the info hash as a lowercase hex string.
func (m Magnet) HexHash() string {
	return hex.EncodeToString(m.InfoHash[:])
}

// parseInfoHash extracts the 20-byte info hash from the xt parameter.
// Supports hex-encoded (40 chars) info hashes.
func parseInfoHash(xt string) ([20]byte, error) {
	var hash [20]byte

	const prefix = "urn:btih:"
	if !strings.HasPrefix(xt, prefix) {
		return hash, fmt.Errorf("%w: missing urn:btih: prefix", ErrInvalidInfoHash)
	}

	raw := xt[len(prefix):]

	switch len(raw) {
	case 40:
		// Hex-encoded
		decoded, err := hex.DecodeString(raw)
		if err != nil {
			return hash, fmt.Errorf("%w: bad hex: %v", ErrInvalidInfoHash, err)
		}
		copy(hash[:], decoded)
	case 32:
		// Base32-encoded
		decoded, err := decodeBase32(raw)
		if err != nil {
			return hash, fmt.Errorf("%w: bad base32: %v", ErrInvalidInfoHash, err)
		}
		copy(hash[:], decoded)
	default:
		return hash, fmt.Errorf("%w: expected 40 hex or 32 base32 chars, got %d", ErrInvalidInfoHash, len(raw))
	}

	return hash, nil
}

// decodeBase32 decodes a base32-encoded string (RFC 4648, no padding) to bytes.
func decodeBase32(s string) ([]byte, error) {
	s = strings.ToUpper(s)
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567"

	var bits uint64
	var nBits int
	var out []byte

	for _, c := range s {
		idx := strings.IndexRune(alphabet, c)
		if idx < 0 {
			return nil, fmt.Errorf("invalid base32 character: %c", c)
		}
		bits = bits<<5 | uint64(idx)
		nBits += 5
		if nBits >= 8 {
			nBits -= 8
			out = append(out, byte(bits>>nBits))
			bits &= (1 << nBits) - 1
		}
	}

	return out, nil
}
