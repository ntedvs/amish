package display

import (
	"strings"
	"testing"
	"time"
)

func TestProgressBar(t *testing.T) {
	tests := []struct {
		progress float64
		width    int
		filled   int
	}{
		{0, 10, 0},
		{0.5, 10, 5},
		{1.0, 10, 10},
		{0.33, 10, 3},
		{1.5, 10, 10}, // clamp
	}

	for _, tt := range tests {
		bar := progressBar(tt.progress, tt.width)
		if !strings.HasPrefix(bar, "[") || !strings.HasSuffix(bar, "]") {
			t.Fatalf("bar = %q, missing brackets", bar)
		}
		inner := bar[1 : len(bar)-1]
		if len(inner) != tt.width {
			t.Fatalf("inner len = %d, want %d", len(inner), tt.width)
		}
		hashCount := strings.Count(inner, "#")
		if hashCount != tt.filled {
			t.Fatalf("progress=%.2f: filled=%d, want %d", tt.progress, hashCount, tt.filled)
		}
	}
}

func TestFormatSpeed(t *testing.T) {
	tests := []struct {
		bps  float64
		want string
	}{
		{0, "0 B/s"},
		{500, "500 B/s"},
		{1024, "1.0 KB/s"},
		{1536, "1.5 KB/s"},
		{1048576, "1.0 MB/s"},
		{5242880, "5.0 MB/s"},
	}

	for _, tt := range tests {
		got := FormatSpeed(tt.bps)
		if got != tt.want {
			t.Errorf("FormatSpeed(%f) = %q, want %q", tt.bps, got, tt.want)
		}
	}
}

func TestFormatETA(t *testing.T) {
	tests := []struct {
		progress   float64
		bps        float64
		totalBytes int64
		want       string
	}{
		{0, 0, 1000, "--:--"},
		{0.5, 0, 1000, "--:--"},
		{1.0, 100, 1000, "done"},
		{0.5, 100, 1000, "5s"},
		{0.5, 10, 10000, "8m20s"},
		{0.1, 1, 36000, "9h0m"},
	}

	for _, tt := range tests {
		got := FormatETA(tt.progress, tt.bps, tt.totalBytes)
		if got != tt.want {
			t.Errorf("FormatETA(%.1f, %.0f, %d) = %q, want %q",
				tt.progress, tt.bps, tt.totalBytes, got, tt.want)
		}
	}
}

func TestTruncateName(t *testing.T) {
	tests := []struct {
		name   string
		max    int
		want   string
	}{
		{"short", 10, "short"},
		{"exactly ten", 11, "exactly ten"},
		{"this is a really long torrent name", 15, "this is a re..."},
		{"abc", 3, "abc"},
		{"abcd", 3, "abc"},
	}

	for _, tt := range tests {
		got := TruncateName(tt.name, tt.max)
		if got != tt.want {
			t.Errorf("TruncateName(%q, %d) = %q, want %q", tt.name, tt.max, got, tt.want)
		}
	}
}

type mockStats struct {
	progress   float64
	bps        float64
	downloaded int
	total      int
	bytes      int64
	peers      int
}

func (m *mockStats) Progress() float64      { return m.progress }
func (m *mockStats) BytesPerSecond() float64 { return m.bps }
func (m *mockStats) GetStats() (int, int, int64, int) {
	return m.downloaded, m.total, m.bytes, m.peers
}

func TestNewDisplay(t *testing.T) {
	stats := &mockStats{}
	d := New(stats, "test.torrent")
	if d.name != "test.torrent" {
		t.Fatalf("name = %q", d.name)
	}
	if d.provider == nil {
		t.Fatal("provider is nil")
	}
	if d.done == nil {
		t.Fatal("done channel is nil")
	}
}

func TestDisplayStartStop(t *testing.T) {
	stats := &mockStats{progress: 0.5, total: 10, downloaded: 5}
	d := New(stats, "test")
	d.Start(50 * time.Millisecond)
	time.Sleep(100 * time.Millisecond)
	d.Stop()
}
