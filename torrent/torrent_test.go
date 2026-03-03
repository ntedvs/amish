package torrent

import (
	"testing"
	"time"

	"amish/magnet"
)

func TestNewTorrent(t *testing.T) {
	m := magnet.Magnet{
		InfoHash: [20]byte{0xAA},
		Name:     "test",
	}

	tor := New(m, "/tmp")
	if tor.Magnet.InfoHash != m.InfoHash {
		t.Fatal("info hash mismatch")
	}
	if tor.OutputDir != "/tmp" {
		t.Fatal("output dir mismatch")
	}
	// PeerID should start with -AT0001-.
	prefix := string(tor.PeerID[:8])
	if prefix != "-AM0001-" {
		t.Fatalf("peer ID prefix = %q, want -AM0001-", prefix)
	}
	// Rest should be random (non-zero, most likely).
	allZero := true
	for _, b := range tor.PeerID[8:] {
		if b != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		t.Fatal("random portion of peer ID is all zeros")
	}
}

func TestStatsProgress(t *testing.T) {
	s := &Stats{TotalPieces: 100, Downloaded: 50}
	p := s.Progress()
	if p != 0.5 {
		t.Fatalf("progress = %f, want 0.5", p)
	}
}

func TestStatsProgressZero(t *testing.T) {
	s := &Stats{TotalPieces: 0}
	if s.Progress() != 0 {
		t.Fatal("expected 0 progress for 0 total pieces")
	}
}

func TestStatsBytesPerSecond(t *testing.T) {
	s := &Stats{
		DownloadedBytes: 1000,
		StartTime:       time.Now().Add(-1 * time.Second),
	}
	bps := s.BytesPerSecond()
	// Should be roughly 1000 bytes/sec (within margin for timing).
	if bps < 500 || bps > 2000 {
		t.Fatalf("bps = %f, expected ~1000", bps)
	}
}

func TestStatsBytesPerSecondNoBytes(t *testing.T) {
	s := &Stats{
		DownloadedBytes: 0,
		StartTime:       time.Now().Add(-1 * time.Second),
	}
	if s.BytesPerSecond() != 0 {
		t.Fatal("expected 0 bps with no bytes downloaded")
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		input int64
		want  string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.00 KB"},
		{1048576, "1.00 MB"},
		{1073741824, "1.00 GB"},
		{1536, "1.50 KB"},
	}

	for _, tt := range tests {
		got := FormatBytes(tt.input)
		if got != tt.want {
			t.Errorf("FormatBytes(%d) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
