package dht

import (
	"crypto/rand"
	"crypto/sha1"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"amish/bencode"
	"amish/netaddr"
)

const (
	BEP5Port = 6881
	TokenLen = 4
)

var (
	ErrInvalidPeerID = errors.New("dht: invalid peer id")
	ErrBadResponse   = errors.New("dht: bad response")
	ErrTimeout       = errors.New("dht: timeout")
	ErrNotListening  = errors.New("dht: not listening")
)

type QueryType string

const (
	QueryPing     QueryType = "ping"
	QueryFindNode QueryType = "find_node"
	QueryGetPeers QueryType = "get_peers"
)

type MsgType string

const (
	MsgQuery MsgType = "q"
	MsgResp  MsgType = "r"
	MsgError MsgType = "e"
)

// Peer is a discovered peer address.
type Peer = netaddr.Peer

type Node struct {
	ID   [20]byte
	Addr string
}

// LogFunc is an optional callback for debug logging.
type LogFunc func(format string, args ...any)

type Client struct {
	id        [20]byte
	conn      *net.UDPConn
	secret    [4]byte
	nodes     map[string]Node
	nodesMu   sync.RWMutex
	peers     map[string][]Peer
	peersMu   sync.RWMutex
	queries   map[string]chan map[string]any
	queriesMu sync.Mutex
	Log       LogFunc
	done      chan struct{}
}

func New() *Client {
	var id [20]byte
	rand.Read(id[:])
	id[0] = 'M'

	var secret [4]byte
	rand.Read(secret[:])

	return &Client{
		id:      id,
		nodes:   make(map[string]Node),
		peers:   make(map[string][]Peer),
		queries: make(map[string]chan map[string]any),
		secret:  secret,
		done:    make(chan struct{}),
	}
}

func (c *Client) log(format string, args ...any) {
	if c.Log != nil {
		c.Log(format, args...)
	}
}

func (c *Client) ID() [20]byte {
	return c.id
}

func (c *Client) Listen(addr string) error {
	a, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return err
	}

	conn, err := net.ListenUDP("udp", a)
	if err != nil {
		return err
	}

	c.conn = conn
	go c.recvLoop()
	return nil
}

func (c *Client) Addr() string {
	if c.conn == nil {
		return ""
	}
	return c.conn.LocalAddr().String()
}

// Close shuts down the client, closing the UDP connection and stopping recvLoop.
func (c *Client) Close() error {
	select {
	case <-c.done:
		return nil // already closed
	default:
		close(c.done)
	}
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

func (c *Client) Bootstrap(nodes []string) error {
	var lastErr error
	reachable := 0
	for _, addr := range nodes {
		if err := c.pingNode(addr); err != nil {
			c.log("bootstrap ping %s: %v", addr, err)
			lastErr = err
		} else {
			reachable++
		}
	}
	if reachable == 0 && lastErr != nil {
		return fmt.Errorf("dht: all bootstrap nodes failed: %w", lastErr)
	}
	return nil
}

func (c *Client) pingNode(addr string) error {
	resp, err := c.query(addr, QueryPing, map[string]any{
		"id": c.id[:],
	})
	if err != nil {
		return err
	}
	// Add the responding node to our routing table.
	if idStr, ok := resp["id"].(string); ok && len(idStr) == 20 {
		var nodeID [20]byte
		copy(nodeID[:], idStr)
		c.nodesMu.Lock()
		c.nodes[addr] = Node{ID: nodeID, Addr: addr}
		c.nodesMu.Unlock()
	}
	return nil
}

func (c *Client) GetPeers(infoHash [20]byte) chan Peer {
	ch := make(chan Peer, 64)

	go func() {
		c.peersMu.RLock()
		if peers, ok := c.peers[string(infoHash[:])]; ok && len(peers) > 0 {
			c.peersMu.RUnlock()
			for _, p := range peers {
				ch <- p
			}
			close(ch)
			return
		}
		c.peersMu.RUnlock()

		c.nodesMu.RLock()
		nodes := make([]Node, 0, len(c.nodes))
		for _, n := range c.nodes {
			nodes = append(nodes, n)
		}
		c.nodesMu.RUnlock()

		goodNodes := 0
		for _, n := range nodes {
			if goodNodes >= 8 {
				break
			}
			resp, err := c.query(n.Addr, QueryGetPeers, map[string]any{
				"id":        c.id[:],
				"info_hash": infoHash[:],
			})
			if err != nil || resp == nil {
				continue
			}
			goodNodes++

			if values, ok := resp["values"]; ok {
				peers := c.decodePeers(values)
				c.peersMu.Lock()
				c.peers[string(infoHash[:])] = append(c.peers[string(infoHash[:])], peers...)
				c.peersMu.Unlock()
				for _, p := range peers {
					select {
					case ch <- p:
					default:
					}
				}
			}

			if nodesStr, ok := resp["nodes"].(string); ok && len(nodesStr) >= 26 {
				c.addNodes(nodesStr)
			}
		}

		close(ch)
	}()

	return ch
}

func (c *Client) decodePeers(v any) []Peer {
	var peers []Peer

	switch s := v.(type) {
	case string:
		for i := 0; i+6 <= len(s); i += 6 {
			peers = append(peers, Peer{
				IP:   net.IP([]byte(s[i : i+4])),
				Port: binary.BigEndian.Uint16([]byte(s[i+4 : i+6])),
			})
		}
	case []any:
		for _, m := range s {
			if m, ok := m.(map[string]any); ok {
				if ip, ok := m["ip"].(string); ok {
					if port, ok := m["port"].(int64); ok {
						peers = append(peers, Peer{IP: net.ParseIP(ip), Port: uint16(port)})
					}
				}
			}
		}
	}

	return peers
}

func (c *Client) addNodes(nodesStr string) {
	for i := 0; i+26 <= len(nodesStr); i += 26 {
		var id [20]byte
		copy(id[:], nodesStr[i:i+20])
		ip := net.IP(nodesStr[i+20 : i+24])
		port := binary.BigEndian.Uint16([]byte(nodesStr[i+24 : i+26]))
		addr := fmt.Sprintf("%s:%d", ip.String(), port)

		c.nodesMu.Lock()
		if _, ok := c.nodes[addr]; !ok {
			c.nodes[addr] = Node{ID: id, Addr: addr}
		}
		c.nodesMu.Unlock()
	}
}

func (c *Client) query(addr string, qtype QueryType, args map[string]any) (map[string]any, error) {
	var t [2]byte
	rand.Read(t[:])
	tid := string(t[:])

	respCh := make(chan map[string]any, 1)

	c.queriesMu.Lock()
	c.queries[tid] = respCh
	c.queriesMu.Unlock()

	defer func() {
		c.queriesMu.Lock()
		delete(c.queries, tid)
		c.queriesMu.Unlock()
	}()

	msg := map[string]any{
		"t": tid,
		"y": string(MsgQuery),
		"q": string(qtype),
		"a": args,
	}

	if err := c.send(addr, msg); err != nil {
		return nil, err
	}

	select {
	case resp := <-respCh:
		return resp, nil
	case <-time.After(3 * time.Second):
		return nil, ErrTimeout
	}
}

func (c *Client) send(addr string, msg map[string]any) error {
	if c.conn == nil {
		return ErrNotListening
	}

	data, err := bencode.Encode(msg)
	if err != nil {
		return err
	}

	raddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return err
	}

	_, err = c.conn.WriteToUDP(data, raddr)
	return err
}

func (c *Client) recvLoop() {
	buf := make([]byte, 65536)
	for {
		n, addr, err := c.conn.ReadFromUDP(buf)
		if err != nil {
			select {
			case <-c.done:
				return
			default:
				c.log("recv: %v", err)
				continue
			}
		}

		data, err := bencode.Decode(buf[:n])
		if err != nil {
			c.log("decode from %s: %v", addr, err)
			continue
		}

		msg, ok := data.(map[string]any)
		if !ok {
			continue
		}

		go c.handle(addr.String(), msg)
	}
}

func (c *Client) handle(addr string, msg map[string]any) {
	y, _ := msg["y"].(string)
	t, _ := msg["t"].(string)

	switch y {
	case string(MsgQuery):
		c.handleQuery(addr, t, msg)
	case string(MsgResp):
		c.handleResponse(t, msg)
	}
}

func (c *Client) handleQuery(addr string, t string, msg map[string]any) {
	q, _ := msg["q"].(string)

	resp := map[string]any{
		"t": t,
		"y": string(MsgResp),
		"r": map[string]any{"id": c.id[:]},
	}

	args, _ := msg["a"].(map[string]any)

	switch QueryType(q) {
	case QueryPing:
	case QueryFindNode:
		if _, ok := args["target"].(string); ok {
			closest := c.closestNodes(nil)
			if encoded := encodeCompactNodes(closest); len(encoded) > 0 {
				resp["r"].(map[string]any)["nodes"] = string(encoded)
			}
		}
	case QueryGetPeers:
		if infoHash, ok := args["info_hash"].(string); ok && len(infoHash) == 20 {
			resp["r"].(map[string]any)["token"] = c.genToken(addr)

			// Return known peers for this info hash if we have them.
			c.peersMu.RLock()
			knownPeers := c.peers[infoHash]
			c.peersMu.RUnlock()

			if len(knownPeers) > 0 {
				resp["r"].(map[string]any)["values"] = encodePeerList(knownPeers)
			} else {
				closest := c.closestNodes(nil)
				if encoded := encodeCompactNodes(closest); len(encoded) > 0 {
					resp["r"].(map[string]any)["nodes"] = string(encoded)
				}
			}
		}
	}

	c.send(addr, resp)
}

// closestNodes returns up to 8 nodes from the routing table.
// If filter is non-nil, only nodes passing the filter are included.
func (c *Client) closestNodes(filter func(Node) bool) []Node {
	c.nodesMu.RLock()
	defer c.nodesMu.RUnlock()
	var nodes []Node
	for _, n := range c.nodes {
		if filter != nil && !filter(n) {
			continue
		}
		nodes = append(nodes, n)
		if len(nodes) >= 8 {
			break
		}
	}
	return nodes
}

// encodePeerList encodes peers into BEP 5 compact format (6 bytes each: 4-byte IP + 2-byte port).
func encodePeerList(peers []Peer) string {
	buf := make([]byte, 0, len(peers)*6)
	for _, p := range peers {
		ip4 := p.IP.To4()
		if ip4 == nil {
			continue
		}
		buf = append(buf, ip4...)
		var portBuf [2]byte
		binary.BigEndian.PutUint16(portBuf[:], p.Port)
		buf = append(buf, portBuf[:]...)
	}
	return string(buf)
}

// encodeCompactNodes encodes nodes into BEP 5 compact format (20-byte ID + 4-byte IP + 2-byte port).
func encodeCompactNodes(nodes []Node) []byte {
	buf := make([]byte, 0, len(nodes)*26)
	for _, n := range nodes {
		host, portStr, err := net.SplitHostPort(n.Addr)
		if err != nil {
			continue
		}
		ip := net.ParseIP(host).To4()
		if ip == nil {
			continue
		}
		var port int
		fmt.Sscanf(portStr, "%d", &port)
		buf = append(buf, n.ID[:]...)
		buf = append(buf, ip...)
		var portBuf [2]byte
		binary.BigEndian.PutUint16(portBuf[:], uint16(port))
		buf = append(buf, portBuf[:]...)
	}
	return buf
}

func (c *Client) handleResponse(t string, msg map[string]any) {
	c.queriesMu.Lock()
	ch, ok := c.queries[t]
	c.queriesMu.Unlock()

	if ok {
		r, ok := msg["r"].(map[string]any)
		if !ok {
			return
		}
		select {
		case ch <- r:
		default:
		}
	}
}

func (c *Client) genToken(addr string) []byte {
	var token [TokenLen]byte
	copy(token[:], c.secret[:])
	h := sha1Hash(addr + string(c.secret[:]))
	for i := 0; i < TokenLen; i++ {
		token[i] ^= h[i]
	}
	return token[:]
}

func sha1Hash(data string) [20]byte {
	h := sha1.New()
	h.Write([]byte(data))
	var result [20]byte
	copy(result[:], h.Sum(nil))
	return result
}


var BootstrapNodes = []string{
	"router.bittorrent.com:6881",
	"router.utorrent.com:6881",
	"dht.transmissionbt.com:6881",
}
