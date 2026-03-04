package dht

import (
	"crypto/rand"
	"crypto/sha1"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"net"
	"sync"
	"time"

	"amish/bencode"
)

const (
	BEP5Port = 6881
	TokenLen = 4
)

var (
	ErrInvalidPeerID = errors.New("dht: invalid peer id")
	ErrBadResponse   = errors.New("dht: bad response")
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

type Peer struct {
	IP   string
	Port int
}

func (p Peer) Addr() string {
	return fmt.Sprintf("%s:%d", p.IP, p.Port)
}

type Node struct {
	ID   [20]byte
	Addr string
}

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

func (c *Client) Bootstrap(nodes []string) error {
	for _, addr := range nodes {
		c.pingNode(addr)
	}
	time.Sleep(500 * time.Millisecond)
	return nil
}

func (c *Client) pingNode(addr string) error {
	id := c.id
	_, err := c.query(addr, QueryPing, map[string]any{
		"id": id[:],
	})
	return err
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
				IP:   net.IP(s[i : i+4]).String(),
				Port: int(binary.BigEndian.Uint16([]byte(s[i+4 : i+6]))),
			})
		}
	case []any:
		for _, m := range s {
			if m, ok := m.(map[string]any); ok {
				if ip, ok := m["ip"].(string); ok {
					if port, ok := m["port"].(int64); ok {
						peers = append(peers, Peer{IP: ip, Port: int(port)})
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
		return nil, errors.New("dht: timeout")
	}
}

func (c *Client) send(addr string, msg map[string]any) error {
	if c.conn == nil {
		return errors.New("dht: not listening")
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
			continue
		}

		data, err := bencode.Decode(buf[:n])
		if err != nil {
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
		if target, ok := args["target"].(string); ok && len(target) == 20 {
			var tid [20]byte
			copy(tid[:], target)
			c.nodesMu.RLock()
			var closest []Node
			for _, n := range c.nodes {
				if distance(n.ID, tid) < math.MaxInt {
					closest = append(closest, n)
					if len(closest) >= 8 {
						break
					}
				}
			}
			c.nodesMu.RUnlock()
			if len(closest) > 0 {
				buf := make([]byte, 0, len(closest)*26)
				for _, n := range closest {
					buf = append(buf, n.ID[:]...)
					ip := net.ParseIP(n.Addr).To4()
					var port int
					fmt.Sscanf(n.Addr, "%*s:%d", &port)
					buf = append(buf, ip...)
					binary.BigEndian.PutUint16(buf[len(buf)-2:], uint16(port))
				}
				resp["r"].(map[string]any)["nodes"] = string(buf)
			}
		}
	case QueryGetPeers:
		if infoHash, ok := args["info_hash"].(string); ok && len(infoHash) == 20 {
			var ih [20]byte
			copy(ih[:], infoHash)
			resp["r"].(map[string]any)["token"] = c.genToken(addr)
			c.nodesMu.RLock()
			var nodes []Node
			for _, n := range c.nodes {
				if len(nodes) >= 8 {
					break
				}
				nodes = append(nodes, n)
			}
			c.nodesMu.RUnlock()
			if len(nodes) > 0 {
				buf := make([]byte, 0, len(nodes)*26)
				for _, n := range nodes {
					buf = append(buf, n.ID[:]...)
				}
				resp["r"].(map[string]any)["nodes"] = string(buf)
			}
		}
	}

	c.send(addr, resp)
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

func distance(a, b [20]byte) int {
	var d int
	for i := 0; i < 20; i++ {
		x := a[i] ^ b[i]
		if x == 0 {
			continue
		}
		d += 20 - i
		d += 8 - int(math.Log2(float64(x)))
	}
	return d
}

var BootstrapNodes = []string{
	"router.bittorrent.com:6881",
	"router.utorrent.com:6881",
	"dht.transmissionbt.com:6881",
}
