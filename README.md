# amish

A BitTorrent client written from scratch in Go with zero dependencies.

Downloads torrents from magnet links using the BitTorrent protocol, including peer discovery via HTTP and UDP trackers, metadata exchange (BEP 9/10), pipelined piece downloading, and SHA1 verification.

## Usage

```
go build -o amish .
./amish <magnet-uri>
```

## Features

- Magnet URI parsing (hex and base32 info hashes)
- HTTP and UDP tracker announce (BEP 15)
- BEP 10 extension protocol / BEP 9 metadata exchange
- Pipelined piece downloads with SHA1 hash verification
- Choke/unchoke handling with automatic retry
- Peer reconnection with backoff
- Background re-announce for continuous peer discovery
- Multi-file torrent support
- Live terminal progress bar with speed and ETA

## License

MIT
