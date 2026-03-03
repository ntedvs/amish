package magnet

import (
	"encoding/hex"
	"testing"
)

func TestParseBasic(t *testing.T) {
	uri := "magnet:?xt=urn:btih:d984f67af9917b214cd8b6048ab5624c7df6a07a&dn=Test+File&tr=http%3A%2F%2Ftracker.example.com%2Fannounce"

	m, err := Parse(uri)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantHash := "d984f67af9917b214cd8b6048ab5624c7df6a07a"
	if m.HexHash() != wantHash {
		t.Fatalf("hash = %s, want %s", m.HexHash(), wantHash)
	}
	if m.Name != "Test File" {
		t.Fatalf("name = %q, want %q", m.Name, "Test File")
	}
	if len(m.Trackers) != 1 {
		t.Fatalf("trackers len = %d, want 1", len(m.Trackers))
	}
	if m.Trackers[0] != "http://tracker.example.com/announce" {
		t.Fatalf("tracker = %q", m.Trackers[0])
	}
}

func TestParseMultipleTrackers(t *testing.T) {
	uri := "magnet:?xt=urn:btih:d984f67af9917b214cd8b6048ab5624c7df6a07a&tr=http%3A%2F%2Fone.com&tr=http%3A%2F%2Ftwo.com"

	m, err := Parse(uri)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.Trackers) != 2 {
		t.Fatalf("trackers len = %d, want 2", len(m.Trackers))
	}
}

func TestParseNoName(t *testing.T) {
	uri := "magnet:?xt=urn:btih:d984f67af9917b214cd8b6048ab5624c7df6a07a"

	m, err := Parse(uri)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Name != "" {
		t.Fatalf("name = %q, want empty", m.Name)
	}
}

func TestParseBase32(t *testing.T) {
	// Base32-encode a known hash.
	hashHex := "d984f67af9917b214cd8b6048ab5624c7df6a07a"
	hashBytes, _ := hex.DecodeString(hashHex)

	// Manually base32 encode the hash.
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567"
	var b32 []byte
	var bits uint64
	var nBits int
	for _, b := range hashBytes {
		bits = bits<<8 | uint64(b)
		nBits += 8
		for nBits >= 5 {
			nBits -= 5
			b32 = append(b32, alphabet[bits>>nBits&0x1F])
			bits &= (1 << nBits) - 1
		}
	}

	uri := "magnet:?xt=urn:btih:" + string(b32)
	m, err := Parse(uri)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.HexHash() != hashHex {
		t.Fatalf("hash = %s, want %s", m.HexHash(), hashHex)
	}
}

func TestParseInvalidScheme(t *testing.T) {
	_, err := Parse("http://example.com")
	if err != ErrInvalidScheme {
		t.Fatalf("expected ErrInvalidScheme, got %v", err)
	}
}

func TestParseMissingXT(t *testing.T) {
	_, err := Parse("magnet:?dn=test")
	if err != ErrMissingInfoHash {
		t.Fatalf("expected ErrMissingInfoHash, got %v", err)
	}
}

func TestParseBadHex(t *testing.T) {
	uri := "magnet:?xt=urn:btih:ZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZ"
	_, err := Parse(uri)
	if err == nil {
		t.Fatal("expected error for bad hex")
	}
}

func TestParseWrongLength(t *testing.T) {
	uri := "magnet:?xt=urn:btih:abcdef"
	_, err := Parse(uri)
	if err == nil {
		t.Fatal("expected error for wrong length")
	}
}

func TestParseMissingPrefix(t *testing.T) {
	uri := "magnet:?xt=urn:sha1:d984f67af9917b214cd8b6048ab5624c7df6a07a"
	_, err := Parse(uri)
	if err == nil {
		t.Fatal("expected error for wrong urn prefix")
	}
}

func TestHexHash(t *testing.T) {
	m := Magnet{}
	copy(m.InfoHash[:], []byte{0xde, 0xad, 0xbe, 0xef, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00})
	want := "deadbeef00000000000000000000000000000000"
	if m.HexHash() != want {
		t.Fatalf("got %s, want %s", m.HexHash(), want)
	}
}
