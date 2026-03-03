// Package peer implements the BitTorrent peer wire protocol.
// This file defines message types and serialization.
package peer

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// Message IDs as defined by the BitTorrent protocol.
const (
	MsgChoke         uint8 = 0
	MsgUnchoke       uint8 = 1
	MsgInterested    uint8 = 2
	MsgNotInterested uint8 = 3
	MsgHave          uint8 = 4
	MsgBitfield      uint8 = 5
	MsgRequest       uint8 = 6
	MsgPiece         uint8 = 7
	MsgCancel        uint8 = 8
	MsgExtended      uint8 = 20 // BEP 10
)

// BlockSize is the standard block size for piece requests.
const BlockSize = 16384 // 16 KiB

var (
	ErrMessageTooLarge = errors.New("peer: message too large")
	ErrUnexpectedEOF   = errors.New("peer: unexpected end of connection")
)

// MaxMessageLen is the maximum allowed message length (16 MiB).
const MaxMessageLen = 1 << 24

// Message represents a single peer protocol message.
type Message struct {
	ID      uint8
	Payload []byte
}

// ReadMessage reads a length-prefixed message from the connection.
// A keep-alive (length 0) returns a nil message with no error.
func ReadMessage(r io.Reader) (*Message, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnexpectedEOF, err)
	}
	length := binary.BigEndian.Uint32(hdr[:])

	// Keep-alive.
	if length == 0 {
		return nil, nil
	}

	if length > MaxMessageLen {
		return nil, fmt.Errorf("%w: length %d", ErrMessageTooLarge, length)
	}

	buf := make([]byte, length)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnexpectedEOF, err)
	}

	return &Message{
		ID:      buf[0],
		Payload: buf[1:],
	}, nil
}

// WriteMessage writes a length-prefixed message to the connection.
func WriteMessage(w io.Writer, msg *Message) error {
	if msg == nil {
		_, err := w.Write([]byte{0, 0, 0, 0})
		return err
	}

	length := uint32(1 + len(msg.Payload))
	buf := make([]byte, 4+1+len(msg.Payload))
	binary.BigEndian.PutUint32(buf[0:4], length)
	buf[4] = msg.ID
	copy(buf[5:], msg.Payload)
	_, err := w.Write(buf)
	return err
}

// NewRequest creates a MsgRequest message for a block.
func NewRequest(index, begin, length uint32) *Message {
	payload := make([]byte, 12)
	binary.BigEndian.PutUint32(payload[0:4], index)
	binary.BigEndian.PutUint32(payload[4:8], begin)
	binary.BigEndian.PutUint32(payload[8:12], length)
	return &Message{ID: MsgRequest, Payload: payload}
}

// NewHave creates a MsgHave message for a piece index.
func NewHave(index uint32) *Message {
	payload := make([]byte, 4)
	binary.BigEndian.PutUint32(payload, index)
	return &Message{ID: MsgHave, Payload: payload}
}

// NewInterested creates a MsgInterested message.
func NewInterested() *Message {
	return &Message{ID: MsgInterested}
}

// NewNotInterested creates a MsgNotInterested message.
func NewNotInterested() *Message {
	return &Message{ID: MsgNotInterested}
}

// ParsePiece extracts index, begin offset, and block data from a piece message payload.
func ParsePiece(payload []byte) (index, begin uint32, block []byte, err error) {
	if len(payload) < 8 {
		return 0, 0, nil, errors.New("peer: piece payload too short")
	}
	index = binary.BigEndian.Uint32(payload[0:4])
	begin = binary.BigEndian.Uint32(payload[4:8])
	block = payload[8:]
	return
}

// ParseHave extracts the piece index from a have message payload.
func ParseHave(payload []byte) (uint32, error) {
	if len(payload) != 4 {
		return 0, errors.New("peer: have payload must be 4 bytes")
	}
	return binary.BigEndian.Uint32(payload), nil
}

// ParseRequest extracts index, begin, and length from a request message payload.
func ParseRequest(payload []byte) (index, begin, length uint32, err error) {
	if len(payload) != 12 {
		return 0, 0, 0, errors.New("peer: request payload must be 12 bytes")
	}
	index = binary.BigEndian.Uint32(payload[0:4])
	begin = binary.BigEndian.Uint32(payload[4:8])
	length = binary.BigEndian.Uint32(payload[8:12])
	return
}
