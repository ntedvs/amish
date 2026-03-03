package torrent

import (
	"os"
	"path/filepath"
	"testing"

	"amish/metainfo"
)

func TestWriterSingleFile(t *testing.T) {
	dir := t.TempDir()

	info := &metainfo.Info{
		Name:        "test.bin",
		PieceLength: 10,
		Length:      25,
		Pieces:      make([][20]byte, 3),
	}

	w := NewWriter(info, dir)
	defer w.Close()

	// Write three pieces.
	if err := w.WritePiece(0, []byte("0123456789")); err != nil {
		t.Fatal(err)
	}
	if err := w.WritePiece(1, []byte("abcdefghij")); err != nil {
		t.Fatal(err)
	}
	if err := w.WritePiece(2, []byte("KLMNO")); err != nil {
		t.Fatal(err)
	}

	// Read back.
	data, err := os.ReadFile(filepath.Join(dir, "test.bin"))
	if err != nil {
		t.Fatal(err)
	}
	want := "0123456789abcdefghijKLMNO"
	if string(data) != want {
		t.Fatalf("got %q, want %q", string(data), want)
	}
}

func TestWriterMultiFile(t *testing.T) {
	dir := t.TempDir()

	info := &metainfo.Info{
		Name:        "mydir",
		PieceLength: 10,
		Pieces:      make([][20]byte, 2),
		Files: []metainfo.File{
			{Length: 7, Path: []string{"a.txt"}},
			{Length: 8, Path: []string{"sub", "b.txt"}},
		},
	}

	w := NewWriter(info, dir)
	defer w.Close()

	// Piece 0 spans: a.txt[0:7] + b.txt[0:3]
	if err := w.WritePiece(0, []byte("1234567890")); err != nil {
		t.Fatal(err)
	}
	// Piece 1 spans: b.txt[3:8]
	if err := w.WritePiece(1, []byte("ABCDE")); err != nil {
		t.Fatal(err)
	}

	// Verify a.txt.
	dataA, err := os.ReadFile(filepath.Join(dir, "mydir", "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(dataA[:7]) != "1234567" {
		t.Fatalf("a.txt = %q", string(dataA[:7]))
	}

	// Verify b.txt.
	dataB, err := os.ReadFile(filepath.Join(dir, "mydir", "sub", "b.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(dataB[:3]) != "890" {
		t.Fatalf("b.txt first 3 = %q", string(dataB[:3]))
	}
	if string(dataB[3:8]) != "ABCDE" {
		t.Fatalf("b.txt last 5 = %q", string(dataB[3:8]))
	}
}

func TestPieceSegmentsSingleFile(t *testing.T) {
	info := &metainfo.Info{
		Name:        "test.bin",
		PieceLength: 100,
		Length:      250,
		Pieces:      make([][20]byte, 3),
	}

	w := NewWriter(info, "/tmp")
	segs := w.pieceSegments(0)
	if len(segs) != 1 {
		t.Fatalf("segments = %d, want 1", len(segs))
	}
	if segs[0].Offset != 0 {
		t.Fatalf("offset = %d", segs[0].Offset)
	}
	if segs[0].Length != 100 {
		t.Fatalf("length = %d", segs[0].Length)
	}

	// Last piece (partial).
	segs = w.pieceSegments(2)
	if segs[0].Length != 50 {
		t.Fatalf("last piece length = %d, want 50", segs[0].Length)
	}
}

func TestPieceSegmentsMultiFile(t *testing.T) {
	info := &metainfo.Info{
		Name:        "dir",
		PieceLength: 10,
		Pieces:      make([][20]byte, 2),
		Files: []metainfo.File{
			{Length: 7, Path: []string{"a.txt"}},
			{Length: 8, Path: []string{"b.txt"}},
		},
	}

	w := NewWriter(info, "/tmp")

	// Piece 0 should span both files.
	segs := w.pieceSegments(0)
	if len(segs) != 2 {
		t.Fatalf("segments = %d, want 2", len(segs))
	}
	if segs[0].Length != 7 {
		t.Fatalf("seg[0] length = %d, want 7", segs[0].Length)
	}
	if segs[1].Length != 3 {
		t.Fatalf("seg[1] length = %d, want 3", segs[1].Length)
	}
}

func TestMax64(t *testing.T) {
	if max64(3, 5) != 5 {
		t.Fatal("max64(3,5)")
	}
	if max64(10, 2) != 10 {
		t.Fatal("max64(10,2)")
	}
}

func TestMin64(t *testing.T) {
	if min64(3, 5) != 3 {
		t.Fatal("min64(3,5)")
	}
	if min64(10, 2) != 2 {
		t.Fatal("min64(10,2)")
	}
}
