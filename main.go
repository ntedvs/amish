package main

import (
	"fmt"
	"os"
	"time"

	"amish/display"
	"amish/magnet"
	"amish/torrent"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "usage: amish <magnet-uri>\n")
		os.Exit(1)
	}

	uri := os.Args[1]

	m, err := magnet.Parse(uri)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bad magnet link: %v\n", err)
		os.Exit(1)
	}

	name := m.Name
	if name == "" {
		name = m.HexHash()[:12]
	}

	fmt.Printf("amish - %s\n", display.TruncateName(name, 50))
	fmt.Printf("hash:     %s\n", m.HexHash())
	fmt.Printf("trackers: %d\n\n", len(m.Trackers))

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	t := torrent.New(m, cwd)
	t.Log = func(format string, args ...any) {
		fmt.Printf(format+"\n", args...)
	}

	// Phase 1: discover peers + fetch metadata (with log output).
	if err := t.DiscoverAndFetchMetadata(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\n%s (%d pieces, %s)\n\n",
		t.Info.Name, t.Info.NumPieces(), torrent.FormatBytes(t.Info.TotalLength()))

	// Phase 2: download with progress display.
	// Silence logs so they don't interfere with the progress bar.
	t.Log = func(string, ...any) {}

	disp := display.New(&t.Stats, display.TruncateName(name, 30))
	disp.Start(500 * time.Millisecond)

	if err := t.Download(); err != nil {
		disp.Stop()
		fmt.Fprintf(os.Stderr, "\nerror: %v\n", err)
		os.Exit(1)
	}

	disp.Stop()
	fmt.Printf("done: %s\n", t.Info.Name)
}
