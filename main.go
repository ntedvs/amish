package main

import (
	"errors"
	"fmt"
	"os"
	"time"

	"amish/display"
	"amish/magnet"
	"amish/torrent"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

var errUsage = errors.New("usage: amish <magnet-uri>")

func run(args []string) error {
	if len(args) < 1 {
		return errUsage
	}

	m, err := magnet.Parse(args[0])
	if err != nil {
		return fmt.Errorf("bad magnet link: %w", err)
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
		return err
	}

	t := torrent.New(m, cwd)
	t.Log = func(format string, args ...any) {
		fmt.Printf(format+"\n", args...)
	}

	if err := t.DiscoverAndFetchMetadata(); err != nil {
		return err
	}

	fmt.Printf("\n%s (%d pieces, %s)\n\n",
		t.Info.Name, t.Info.NumPieces(), display.FormatBytes(t.Info.TotalLength()))

	t.Log = func(string, ...any) {}

	disp := display.New(&t.Stats, display.TruncateName(name, 30))
	disp.Start(500 * time.Millisecond)

	if err := t.Download(); err != nil {
		disp.Stop()
		return err
	}

	disp.Stop()
	fmt.Printf("done: %s\n", t.Info.Name)
	return nil
}
