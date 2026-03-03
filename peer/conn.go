package peer

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"amish/bencode"
)

const (
	ProtocolID = "BitTorrent protocol"
	// Extension bit for BEP 10 (extension protocol).
	ExtensionBit = 0x10
)

var (
	ErrBadProtocol = errors.New("peer: invalid protocol string in handshake")
	ErrBadInfoHash = errors.New("peer: info hash mismatch in handshake")
)

// Handshake represents the initial BitTorrent handshake.
type Handshake struct {
	InfoHash [20]byte
	PeerID   [20]byte
	Reserved [8]byte
}

// Conn wraps a TCP connection to a single peer.
type Conn struct {
	net.Conn
	Reader   *bufio.Reader
	Writer   *bufio.Writer
	InfoHash [20]byte
	PeerID   [20]byte
	// Whether the peer supports BEP 10 extensions.
	SupportsExtension bool
	// Extension message IDs negotiated with this peer.
	ExtensionIDs map[string]int64
	// Metadata size reported by the peer.
	MetadataSize int64
	// Our extension handshake ID mapping.
	ourExtIDs map[string]int64
}

// Dial connects to a peer, performs the handshake, and returns a Conn.
func Dial(addr string, infoHash, peerID [20]byte, timeout time.Duration) (*Conn, error) {
	tcpConn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return nil, err
	}
	tcpConn.SetDeadline(time.Now().Add(timeout))

	c := &Conn{
		Conn:     tcpConn,
		Reader:   bufio.NewReaderSize(tcpConn, 32*1024),
		Writer:   bufio.NewWriter(tcpConn),
		InfoHash: infoHash,
	}

	if err := c.sendHandshake(infoHash, peerID); err != nil {
		tcpConn.Close()
		return nil, err
	}
	if err := c.recvHandshake(infoHash); err != nil {
		tcpConn.Close()
		return nil, err
	}

	return c, nil
}

func (c *Conn) sendHandshake(infoHash, peerID [20]byte) error {
	var reserved [8]byte
	// Signal BEP 10 extension support.
	reserved[5] = ExtensionBit

	buf := make([]byte, 0, 68)
	buf = append(buf, byte(len(ProtocolID)))
	buf = append(buf, []byte(ProtocolID)...)
	buf = append(buf, reserved[:]...)
	buf = append(buf, infoHash[:]...)
	buf = append(buf, peerID[:]...)

	if _, err := c.Writer.Write(buf); err != nil {
		return err
	}
	return c.Writer.Flush()
}

func (c *Conn) recvHandshake(expectedHash [20]byte) error {
	var pstrLen [1]byte
	if _, err := io.ReadFull(c.Reader, pstrLen[:]); err != nil {
		return fmt.Errorf("%w: %v", ErrBadProtocol, err)
	}
	if pstrLen[0] != 19 {
		return fmt.Errorf("%w: pstrlen = %d", ErrBadProtocol, pstrLen[0])
	}

	buf := make([]byte, 48+19) // pstr(19) + reserved(8) + info_hash(20) + peer_id(20)
	if _, err := io.ReadFull(c.Reader, buf); err != nil {
		return fmt.Errorf("%w: %v", ErrBadProtocol, err)
	}

	pstr := string(buf[:19])
	if pstr != ProtocolID {
		return fmt.Errorf("%w: %q", ErrBadProtocol, pstr)
	}

	var reserved [8]byte
	copy(reserved[:], buf[19:27])
	c.SupportsExtension = (reserved[5] & ExtensionBit) != 0

	var infoHash [20]byte
	copy(infoHash[:], buf[27:47])
	if infoHash != expectedHash {
		return ErrBadInfoHash
	}

	copy(c.PeerID[:], buf[47:67])
	return nil
}

// SendExtensionHandshake sends a BEP 10 extension handshake,
// advertising our support for ut_metadata (BEP 9).
func (c *Conn) SendExtensionHandshake() error {
	c.ourExtIDs = map[string]int64{
		"ut_metadata": 1,
	}

	handshake := map[string]any{
		"m": map[string]any{
			"ut_metadata": int64(1),
		},
	}
	payload, err := bencode.Encode(handshake)
	if err != nil {
		return err
	}

	// Extension message: ID=20, sub-ID=0 (handshake).
	msg := &Message{
		ID:      MsgExtended,
		Payload: append([]byte{0}, payload...),
	}
	if err = WriteMessage(c.Writer, msg); err != nil {
		return err
	}
	return c.Writer.Flush()
}

// HandleExtensionHandshake parses the peer's extension handshake response.
func (c *Conn) HandleExtensionHandshake(payload []byte) error {
	decoded, err := bencode.Decode(payload)
	if err != nil {
		return fmt.Errorf("peer: bad extension handshake: %w", err)
	}

	dict, ok := decoded.(map[string]any)
	if !ok {
		return errors.New("peer: extension handshake not a dict")
	}

	if m, ok := dict["m"].(map[string]any); ok {
		c.ExtensionIDs = make(map[string]int64)
		for k, v := range m {
			if id, ok := v.(int64); ok {
				c.ExtensionIDs[k] = id
			}
		}
	}

	if size, ok := dict["metadata_size"].(int64); ok {
		c.MetadataSize = size
	}

	return nil
}

// RequestMetadataPiece sends a BEP 9 metadata request for the given piece.
func (c *Conn) RequestMetadataPiece(piece int) error {
	utMetadata, ok := c.ExtensionIDs["ut_metadata"]
	if !ok {
		return errors.New("peer: remote does not support ut_metadata")
	}

	req := map[string]any{
		"msg_type": int64(0), // request
		"piece":    int64(piece),
	}
	payload, err := bencode.Encode(req)
	if err != nil {
		return err
	}

	msg := &Message{
		ID:      MsgExtended,
		Payload: append([]byte{byte(utMetadata)}, payload...),
	}
	if err = WriteMessage(c.Writer, msg); err != nil {
		return err
	}
	return c.Writer.Flush()
}

// ParseMetadataPiece parses a BEP 9 metadata data response.
// Returns the piece index and the raw metadata bytes for that piece.
func ParseMetadataPiece(payload []byte) (piece int, data []byte, err error) {
	decoded, consumed, err := bencode.DecodeBytes(payload)
	if err != nil {
		return 0, nil, err
	}

	dict, ok := decoded.(map[string]any)
	if !ok {
		return 0, nil, errors.New("peer: metadata response not a dict")
	}

	msgType, ok := dict["msg_type"].(int64)
	if !ok || msgType != 1 { // 1 = data
		return 0, nil, fmt.Errorf("peer: metadata msg_type = %v, want 1 (data)", dict["msg_type"])
	}

	pieceVal, ok := dict["piece"].(int64)
	if !ok {
		return 0, nil, errors.New("peer: missing piece index")
	}

	return int(pieceVal), payload[consumed:], nil
}

// Bitfield represents which pieces a peer has.
type Bitfield []byte

// HasPiece reports whether the bitfield indicates the peer has the given piece.
func (bf Bitfield) HasPiece(index int) bool {
	byteIdx := index / 8
	bitIdx := uint(7 - index%8)
	if byteIdx < 0 || byteIdx >= len(bf) {
		return false
	}
	return bf[byteIdx]&(1<<bitIdx) != 0
}

// SetPiece marks a piece as present in the bitfield.
func (bf Bitfield) SetPiece(index int) {
	byteIdx := index / 8
	bitIdx := uint(7 - index%8)
	if byteIdx >= 0 && byteIdx < len(bf) {
		bf[byteIdx] |= 1 << bitIdx
	}
}

// NewBitfield creates a bitfield for the given number of pieces.
func NewBitfield(numPieces int) Bitfield {
	return make(Bitfield, (numPieces+7)/8)
}

// SendMessage sends a pre-built message on this connection.
func (c *Conn) SendMessage(msg *Message) error {
	if err := WriteMessage(c.Writer, msg); err != nil {
		return err
	}
	return c.Writer.Flush()
}

// RecvMessage reads the next message from this connection.
func (c *Conn) RecvMessage() (*Message, error) {
	return ReadMessage(c.Reader)
}

// ReadN reads exactly n bytes from the connection.
func ReadN(r io.Reader, n int) ([]byte, error) {
	buf := make([]byte, n)
	_, err := io.ReadFull(r, buf)
	return buf, err
}

// BuildHandshakeBytes builds raw handshake bytes for testing.
func BuildHandshakeBytes(infoHash, peerID [20]byte, supportExt bool) []byte {
	var buf bytes.Buffer
	buf.WriteByte(19)
	buf.WriteString(ProtocolID)
	var reserved [8]byte
	if supportExt {
		reserved[5] = ExtensionBit
	}
	buf.Write(reserved[:])
	buf.Write(infoHash[:])
	buf.Write(peerID[:])
	return buf.Bytes()
}

// EncodeUint32 encodes a uint32 to big-endian bytes (helper for tests).
func EncodeUint32(v uint32) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, v)
	return b
}
