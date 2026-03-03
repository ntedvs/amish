// Package tracker implements HTTP and UDP BitTorrent tracker announce.
package tracker

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"amish/bencode"
)

var (
	ErrBadResponse     = errors.New("tracker: bad response")
	ErrTrackerFailure  = errors.New("tracker: failure")
	ErrUnsupportedProto = errors.New("tracker: unsupported protocol")
)

// Peer represents a discovered peer address.
type Peer struct {
	IP   net.IP
	Port uint16
}

// Addr returns the peer address as "ip:port".
func (p Peer) Addr() string {
	return net.JoinHostPort(p.IP.String(), strconv.Itoa(int(p.Port)))
}

// AnnounceParams holds the parameters for a tracker announce.
type AnnounceParams struct {
	InfoHash [20]byte
	PeerID   [20]byte
	Port     uint16
	Left     int64
}

// Announce contacts a tracker and returns a list of peers.
// Supports both HTTP and UDP trackers.
func Announce(trackerURL string, params AnnounceParams) ([]Peer, error) {
	u, err := url.Parse(trackerURL)
	if err != nil {
		return nil, err
	}

	switch u.Scheme {
	case "http", "https":
		return announceHTTP(trackerURL, params)
	case "udp":
		return announceUDP(u.Host, params)
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedProto, u.Scheme)
	}
}

func announceHTTP(base string, params AnnounceParams) ([]Peer, error) {
	u, err := url.Parse(base)
	if err != nil {
		return nil, err
	}

	q := u.Query()
	q.Set("info_hash", string(params.InfoHash[:]))
	q.Set("peer_id", string(params.PeerID[:]))
	q.Set("port", strconv.Itoa(int(params.Port)))
	q.Set("uploaded", "0")
	q.Set("downloaded", "0")
	q.Set("left", strconv.FormatInt(params.Left, 10))
	q.Set("compact", "1")
	q.Set("event", "started")
	u.RawQuery = q.Encode()

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(u.String())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return parseHTTPResponse(body)
}

func parseHTTPResponse(body []byte) ([]Peer, error) {
	decoded, err := bencode.Decode(body)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBadResponse, err)
	}

	dict, ok := decoded.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%w: expected dict", ErrBadResponse)
	}

	if failure, ok := dict["failure reason"].(string); ok {
		return nil, fmt.Errorf("%w: %s", ErrTrackerFailure, failure)
	}

	return parsePeers(dict)
}

func parsePeers(dict map[string]any) ([]Peer, error) {
	peersVal, ok := dict["peers"]
	if !ok {
		return nil, fmt.Errorf("%w: missing peers", ErrBadResponse)
	}

	switch p := peersVal.(type) {
	case string:
		// Compact format: 6 bytes per peer (4 IP + 2 port).
		return ParseCompactPeers([]byte(p))
	case []any:
		// Dictionary model.
		return parseDictPeers(p)
	default:
		return nil, fmt.Errorf("%w: unexpected peers type %T", ErrBadResponse, peersVal)
	}
}

// ParseCompactPeers parses the compact peer format (6 bytes per peer).
func ParseCompactPeers(data []byte) ([]Peer, error) {
	if len(data)%6 != 0 {
		return nil, fmt.Errorf("%w: compact peers length %d not multiple of 6", ErrBadResponse, len(data))
	}

	peers := make([]Peer, 0, len(data)/6)
	for i := 0; i < len(data); i += 6 {
		peers = append(peers, Peer{
			IP:   net.IP(data[i : i+4]),
			Port: binary.BigEndian.Uint16(data[i+4 : i+6]),
		})
	}
	return peers, nil
}

func parseDictPeers(list []any) ([]Peer, error) {
	var peers []Peer
	for _, item := range list {
		d, ok := item.(map[string]any)
		if !ok {
			continue
		}
		ip, _ := d["ip"].(string)
		port, _ := d["port"].(int64)
		if ip == "" || port == 0 {
			continue
		}
		peers = append(peers, Peer{
			IP:   net.ParseIP(ip),
			Port: uint16(port),
		})
	}
	return peers, nil
}

// UDP tracker protocol (BEP 15).

const (
	actionConnect  = 0
	actionAnnounce = 1
)

func announceUDP(host string, params AnnounceParams) ([]Peer, error) {
	conn, err := net.DialTimeout("udp", host, 5*time.Second)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(15 * time.Second))

	connectionID, err := udpConnectHandshake(conn)
	if err != nil {
		return nil, err
	}

	return udpAnnounceRequest(conn, connectionID, params)
}

func udpConnectHandshake(conn net.Conn) (uint64, error) {
	transactionID := rand.Uint32()

	// Connect request: connection_id(8) + action(4) + transaction_id(4)
	req := make([]byte, 16)
	binary.BigEndian.PutUint64(req[0:8], 0x41727101980) // magic constant
	binary.BigEndian.PutUint32(req[8:12], 0)            // action: connect
	binary.BigEndian.PutUint32(req[12:16], transactionID)

	if _, err := conn.Write(req); err != nil {
		return 0, err
	}

	// Connect response: action(4) + transaction_id(4) + connection_id(8)
	resp := make([]byte, 16)
	if _, err := io.ReadFull(conn, resp); err != nil {
		return 0, fmt.Errorf("tracker: udp connect response: %w", err)
	}

	action := binary.BigEndian.Uint32(resp[0:4])
	if action != 0 {
		return 0, fmt.Errorf("tracker: udp connect action = %d", action)
	}

	rxTxID := binary.BigEndian.Uint32(resp[4:8])
	if rxTxID != transactionID {
		return 0, fmt.Errorf("tracker: udp transaction ID mismatch")
	}

	return binary.BigEndian.Uint64(resp[8:16]), nil
}

func udpAnnounceRequest(conn net.Conn, connectionID uint64, params AnnounceParams) ([]Peer, error) {
	transactionID := rand.Uint32()

	// Announce request: 98 bytes.
	req := make([]byte, 98)
	binary.BigEndian.PutUint64(req[0:8], connectionID)
	binary.BigEndian.PutUint32(req[8:12], 1) // action: announce
	binary.BigEndian.PutUint32(req[12:16], transactionID)
	copy(req[16:36], params.InfoHash[:])
	copy(req[36:56], params.PeerID[:])
	// downloaded(8), left(8), uploaded(8)
	binary.BigEndian.PutUint64(req[64:72], uint64(params.Left))
	// event(4): 0=none (we use 2=started for first announce)
	binary.BigEndian.PutUint32(req[80:84], 2)
	// IP, key, num_want
	binary.BigEndian.PutUint32(req[92:96], 0xFFFFFFFF) // num_want: -1 (default)
	binary.BigEndian.PutUint16(req[96:98], params.Port)

	if _, err := conn.Write(req); err != nil {
		return nil, err
	}

	// Response: action(4) + transaction_id(4) + interval(4) + leechers(4) + seeders(4) + peers(6*N)
	resp := make([]byte, 2048)
	n, err := conn.Read(resp)
	if err != nil {
		return nil, fmt.Errorf("tracker: udp announce response: %w", err)
	}
	resp = resp[:n]

	if len(resp) < 20 {
		return nil, fmt.Errorf("%w: udp response too short", ErrBadResponse)
	}

	action := binary.BigEndian.Uint32(resp[0:4])
	if action != 1 {
		return nil, fmt.Errorf("tracker: udp announce action = %d", action)
	}

	return ParseCompactPeers(resp[20:])
}
