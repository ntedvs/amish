// Package display renders live download progress in the terminal.
package display

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// StatsProvider is the interface that the display reads progress from.
type StatsProvider interface {
	Progress() float64
	BytesPerSecond() float64
	GetStats() (downloaded, total int, bytes int64, peers int)
}

// Display manages terminal output for download progress.
type Display struct {
	provider StatsProvider
	name     string
	done     chan struct{}
	wg       sync.WaitGroup
}

// New creates a new Display that reads from the given stats provider.
func New(provider StatsProvider, torrentName string) *Display {
	return &Display{
		provider: provider,
		name:     torrentName,
		done:     make(chan struct{}),
	}
}

// Start begins rendering progress to the terminal at the given interval.
func (d *Display) Start(interval time.Duration) {
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				d.render()
			case <-d.done:
				d.render() // final render
				fmt.Println()
				return
			}
		}
	}()
}

// Stop signals the display to stop updating and waits for the final render.
func (d *Display) Stop() {
	close(d.done)
	d.wg.Wait()
}

// termWidth returns the terminal width, defaulting to 80.
func termWidth() int {
	type winsize struct {
		Row, Col, Xpixel, Ypixel uint16
	}
	var ws winsize
	_, _, err := syscall.Syscall(
		syscall.SYS_IOCTL,
		uintptr(syscall.Stdout),
		uintptr(syscall.TIOCGWINSZ),
		uintptr(unsafe.Pointer(&ws)),
	)
	if err != 0 || ws.Col == 0 {
		return 80
	}
	return int(ws.Col)
}

func (d *Display) render() {
	progress := d.provider.Progress()
	bps := d.provider.BytesPerSecond()
	downloaded, total, totalBytes, peers := d.provider.GetStats()

	pct := progress * 100
	speed := FormatSpeed(bps)
	eta := FormatETA(progress, bps, totalBytes)

	width := termWidth()

	// Truncate name to fit.
	name := TruncateName(d.name, 30)

	// Build the stats suffix first to know remaining space for the bar.
	stats := fmt.Sprintf(" %.1f%% %d/%d %s %dp %s",
		pct, downloaded, total, speed, peers, eta)

	// Bar gets whatever space is left.
	barWidth := width - len(name) - len(stats) - 3 // 3 for " [] "
	if barWidth < 10 {
		barWidth = 10
	}
	bar := progressBar(progress, barWidth)

	// \033[2K clears the entire line, \r returns to start.
	fmt.Fprintf(os.Stdout, "\033[2K\r%s %s%s", name, bar, stats)
}

// progressBar renders an ASCII progress bar of the given width.
func progressBar(progress float64, width int) string {
	filled := int(progress * float64(width))
	if filled > width {
		filled = width
	}
	if filled < 0 {
		filled = 0
	}
	empty := width - filled
	return "[" + strings.Repeat("#", filled) + strings.Repeat("-", empty) + "]"
}

// TruncateName shortens a name to maxLen, adding "..." if truncated.
func TruncateName(name string, maxLen int) string {
	if len(name) <= maxLen {
		return name
	}
	if maxLen <= 3 {
		return name[:maxLen]
	}
	return name[:maxLen-3] + "..."
}

// FormatBytes formats a byte count as a human-readable string.
func FormatBytes(b int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)
	switch {
	case b >= GB:
		return fmt.Sprintf("%.2f GB", float64(b)/float64(GB))
	case b >= MB:
		return fmt.Sprintf("%.2f MB", float64(b)/float64(MB))
	case b >= KB:
		return fmt.Sprintf("%.2f KB", float64(b)/float64(KB))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

// FormatSpeed formats bytes/sec as a human-readable speed string.
func FormatSpeed(bps float64) string {
	switch {
	case bps >= 1024*1024:
		return fmt.Sprintf("%.1f MB/s", bps/(1024*1024))
	case bps >= 1024:
		return fmt.Sprintf("%.1f KB/s", bps/1024)
	default:
		return fmt.Sprintf("%.0f B/s", bps)
	}
}

// FormatETA estimates the remaining download time.
func FormatETA(progress, bps float64, totalBytes int64) string {
	if bps <= 0 || progress <= 0 {
		return "--:--"
	}
	if progress >= 1.0 {
		return "done"
	}

	downloaded := progress * float64(totalBytes)
	remaining := float64(totalBytes) - downloaded
	seconds := remaining / bps

	if seconds < 60 {
		return fmt.Sprintf("%ds", int(seconds))
	}
	if seconds < 3600 {
		return fmt.Sprintf("%dm%ds", int(seconds)/60, int(seconds)%60)
	}
	return fmt.Sprintf("%dh%dm", int(seconds)/3600, (int(seconds)%3600)/60)
}
