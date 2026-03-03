package metainfo

import (
	"crypto/sha1"
	"strings"
	"testing"

	"amish/bencode"
)

func makeTestMetadata(t *testing.T, dict map[string]any) []byte {
	t.Helper()
	raw, err := bencode.Encode(dict)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return raw
}

func TestParseSingleFile(t *testing.T) {
	pieces := strings.Repeat("A", 20) // one piece hash
	dict := map[string]any{
		"name":         "test.txt",
		"piece length": int64(262144),
		"pieces":       pieces,
		"length":       int64(100000),
	}
	raw := makeTestMetadata(t, dict)

	info, err := ParseUnverified(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if info.Name != "test.txt" {
		t.Fatalf("name = %q", info.Name)
	}
	if info.PieceLength != 262144 {
		t.Fatalf("piece length = %d", info.PieceLength)
	}
	if info.NumPieces() != 1 {
		t.Fatalf("num pieces = %d", info.NumPieces())
	}
	if info.Length != 100000 {
		t.Fatalf("length = %d", info.Length)
	}
	if info.TotalLength() != 100000 {
		t.Fatalf("total length = %d", info.TotalLength())
	}
}

func TestParseMultiFile(t *testing.T) {
	pieces := strings.Repeat("B", 40) // two piece hashes
	dict := map[string]any{
		"name":         "mydir",
		"piece length": int64(262144),
		"pieces":       pieces,
		"files": []any{
			map[string]any{
				"length": int64(50000),
				"path":   []any{"subdir", "file1.txt"},
			},
			map[string]any{
				"length": int64(30000),
				"path":   []any{"file2.txt"},
			},
		},
	}
	raw := makeTestMetadata(t, dict)

	info, err := ParseUnverified(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if info.Name != "mydir" {
		t.Fatalf("name = %q", info.Name)
	}
	if len(info.Files) != 2 {
		t.Fatalf("files = %d", len(info.Files))
	}
	if info.Files[0].Length != 50000 {
		t.Fatalf("file0 length = %d", info.Files[0].Length)
	}
	if info.Files[0].Path[0] != "subdir" || info.Files[0].Path[1] != "file1.txt" {
		t.Fatalf("file0 path = %v", info.Files[0].Path)
	}
	if info.TotalLength() != 80000 {
		t.Fatalf("total = %d", info.TotalLength())
	}
	if info.NumPieces() != 2 {
		t.Fatalf("pieces = %d", info.NumPieces())
	}
}

func TestParseWithHashVerification(t *testing.T) {
	pieces := strings.Repeat("C", 20)
	dict := map[string]any{
		"name":         "verified.bin",
		"piece length": int64(65536),
		"pieces":       pieces,
		"length":       int64(65536),
	}
	raw := makeTestMetadata(t, dict)
	hash := sha1.Sum(raw)

	info, err := Parse(raw, hash)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if info.Name != "verified.bin" {
		t.Fatalf("name = %q", info.Name)
	}
}

func TestParseHashMismatch(t *testing.T) {
	pieces := strings.Repeat("D", 20)
	dict := map[string]any{
		"name":         "bad.bin",
		"piece length": int64(65536),
		"pieces":       pieces,
		"length":       int64(65536),
	}
	raw := makeTestMetadata(t, dict)
	wrongHash := [20]byte{0xFF}

	_, err := Parse(raw, wrongHash)
	if err != ErrHashMismatch {
		t.Fatalf("expected ErrHashMismatch, got %v", err)
	}
}

func TestParseMissingName(t *testing.T) {
	raw := makeTestMetadata(t, map[string]any{
		"piece length": int64(256),
		"pieces":       strings.Repeat("E", 20),
		"length":       int64(100),
	})
	_, err := ParseUnverified(raw)
	if err == nil {
		t.Fatal("expected error for missing name")
	}
}

func TestParseMissingPieceLength(t *testing.T) {
	raw := makeTestMetadata(t, map[string]any{
		"name":   "test",
		"pieces": strings.Repeat("F", 20),
		"length": int64(100),
	})
	_, err := ParseUnverified(raw)
	if err == nil {
		t.Fatal("expected error for missing piece length")
	}
}

func TestParseMissingPieces(t *testing.T) {
	raw := makeTestMetadata(t, map[string]any{
		"name":         "test",
		"piece length": int64(256),
		"length":       int64(100),
	})
	_, err := ParseUnverified(raw)
	if err == nil {
		t.Fatal("expected error for missing pieces")
	}
}

func TestParseBadPiecesLength(t *testing.T) {
	raw := makeTestMetadata(t, map[string]any{
		"name":         "test",
		"piece length": int64(256),
		"pieces":       "short",
		"length":       int64(100),
	})
	_, err := ParseUnverified(raw)
	if err == nil {
		t.Fatal("expected error for bad pieces length")
	}
}

func TestParseMissingLengthAndFiles(t *testing.T) {
	raw := makeTestMetadata(t, map[string]any{
		"name":         "test",
		"piece length": int64(256),
		"pieces":       strings.Repeat("G", 20),
	})
	_, err := ParseUnverified(raw)
	if err == nil {
		t.Fatal("expected error for missing length and files")
	}
}

func TestPieceSize(t *testing.T) {
	info := &Info{
		PieceLength: 100,
		Length:       250,
		Pieces:      make([][20]byte, 3),
	}

	if info.PieceSize(0) != 100 {
		t.Fatalf("piece 0 size = %d", info.PieceSize(0))
	}
	if info.PieceSize(1) != 100 {
		t.Fatalf("piece 1 size = %d", info.PieceSize(1))
	}
	if info.PieceSize(2) != 50 {
		t.Fatalf("piece 2 size = %d (last piece)", info.PieceSize(2))
	}
}

func TestParseBadBencode(t *testing.T) {
	_, err := ParseUnverified([]byte("not bencode"))
	if err == nil {
		t.Fatal("expected error")
	}
}
