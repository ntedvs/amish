# amish

A minimal, zero-dependency BitTorrent client written in pure Go.

## Usage

```bash
go build -o amish .
./amish "magnet:?xt=urn:btih:..."
```

Files are saved to the current working directory.

## Features

- **Magnet link support** -- downloads from magnet URIs (hex and base32 info hashes)
- **Metadata fetching** -- retrieves torrent metadata from peers via BEP 9/10 extension protocol
- **DHT peer discovery** -- finds peers via the distributed hash table (BEP 5)
- **HTTP & UDP trackers** -- announces to both tracker types
- **Endgame mode** -- races peers for the last pieces to avoid tail latency
- **Multi-file torrents** -- handles torrents with multiple files and nested directories
- **Pipelined downloads** -- requests up to 16 blocks per peer concurrently
- **Piece verification** -- SHA1 hash check on every downloaded piece
- **Progress display** -- terminal progress bar with download speed

## Architecture

```
main.go          Entry point, two-phase download (metadata then pieces)
torrent/         Download orchestration, piece management, endgame, file writing
peer/            BitTorrent wire protocol, handshake, message serialization
tracker/         HTTP and UDP tracker announce
dht/             DHT client for trackerless peer discovery
magnet/          Magnet URI parsing
metainfo/        Torrent metadata (info dict) parsing
bencode/         Bencode encoding/decoding
display/         Terminal progress bar
```

## Testing

```bash
go test ./...
```

## License

MIT
