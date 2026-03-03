package peer

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestReadWriteMessage(t *testing.T) {
	original := &Message{
		ID:      MsgPiece,
		Payload: []byte{0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0xDE, 0xAD},
	}

	var buf bytes.Buffer
	if err := WriteMessage(&buf, original); err != nil {
		t.Fatalf("write error: %v", err)
	}

	got, err := ReadMessage(&buf)
	if err != nil {
		t.Fatalf("read error: %v", err)
	}

	if got.ID != original.ID {
		t.Fatalf("id = %d, want %d", got.ID, original.ID)
	}
	if !bytes.Equal(got.Payload, original.Payload) {
		t.Fatalf("payload mismatch")
	}
}

func TestReadWriteKeepAlive(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteMessage(&buf, nil); err != nil {
		t.Fatalf("write error: %v", err)
	}

	got, err := ReadMessage(&buf)
	if err != nil {
		t.Fatalf("read error: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil for keep-alive, got %+v", got)
	}
}

func TestReadMessageTooLarge(t *testing.T) {
	var buf bytes.Buffer
	binary.Write(&buf, binary.BigEndian, uint32(MaxMessageLen+1))
	buf.Write(make([]byte, 100)) // doesn't matter, should fail on length check

	_, err := ReadMessage(&buf)
	if err == nil {
		t.Fatal("expected error for oversized message")
	}
}

func TestReadMessageTruncated(t *testing.T) {
	var buf bytes.Buffer
	// Claim 100 bytes but only provide 5.
	binary.Write(&buf, binary.BigEndian, uint32(100))
	buf.Write([]byte{0x01, 0x02, 0x03, 0x04, 0x05})

	_, err := ReadMessage(&buf)
	if err == nil {
		t.Fatal("expected error for truncated message")
	}
}

func TestWriteMessageNoPayload(t *testing.T) {
	msg := &Message{ID: MsgInterested}
	var buf bytes.Buffer
	if err := WriteMessage(&buf, msg); err != nil {
		t.Fatalf("write error: %v", err)
	}

	// Length should be 1 (just the ID byte).
	var length uint32
	binary.Read(&buf, binary.BigEndian, &length)
	if length != 1 {
		t.Fatalf("length = %d, want 1", length)
	}
}

func TestNewRequest(t *testing.T) {
	msg := NewRequest(5, 16384, 16384)
	if msg.ID != MsgRequest {
		t.Fatalf("id = %d, want %d", msg.ID, MsgRequest)
	}
	idx, begin, length, err := ParseRequest(msg.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if idx != 5 || begin != 16384 || length != 16384 {
		t.Fatalf("got %d/%d/%d", idx, begin, length)
	}
}

func TestNewHave(t *testing.T) {
	msg := NewHave(42)
	if msg.ID != MsgHave {
		t.Fatalf("id = %d, want %d", msg.ID, MsgHave)
	}
	idx, err := ParseHave(msg.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if idx != 42 {
		t.Fatalf("got %d, want 42", idx)
	}
}

func TestNewInterested(t *testing.T) {
	msg := NewInterested()
	if msg.ID != MsgInterested {
		t.Fatalf("id = %d", msg.ID)
	}
	if len(msg.Payload) != 0 {
		t.Fatalf("payload should be empty")
	}
}

func TestNewNotInterested(t *testing.T) {
	msg := NewNotInterested()
	if msg.ID != MsgNotInterested {
		t.Fatalf("id = %d", msg.ID)
	}
}

func TestParsePiece(t *testing.T) {
	payload := make([]byte, 12)
	binary.BigEndian.PutUint32(payload[0:4], 3)
	binary.BigEndian.PutUint32(payload[4:8], 0)
	copy(payload[8:], []byte{0xFF, 0xFE, 0xFD, 0xFC})

	idx, begin, block, err := ParsePiece(payload)
	if err != nil {
		t.Fatal(err)
	}
	if idx != 3 {
		t.Fatalf("index = %d, want 3", idx)
	}
	if begin != 0 {
		t.Fatalf("begin = %d, want 0", begin)
	}
	if !bytes.Equal(block, []byte{0xFF, 0xFE, 0xFD, 0xFC}) {
		t.Fatalf("block mismatch")
	}
}

func TestParsePieceTooShort(t *testing.T) {
	_, _, _, err := ParsePiece([]byte{0x01})
	if err == nil {
		t.Fatal("expected error for short payload")
	}
}

func TestParseHaveBadLength(t *testing.T) {
	_, err := ParseHave([]byte{0x01, 0x02})
	if err == nil {
		t.Fatal("expected error for bad length")
	}
}

func TestParseRequestBadLength(t *testing.T) {
	_, _, _, err := ParseRequest([]byte{0x01, 0x02})
	if err == nil {
		t.Fatal("expected error for bad length")
	}
}
