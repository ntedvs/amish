package torrent

import (
	"crypto/sha1"
	"testing"
)

func TestVerifyPieceValid(t *testing.T) {
	data := []byte("hello world this is piece data")
	hash := sha1.Sum(data)
	if !VerifyPiece(data, hash) {
		t.Fatal("expected valid piece")
	}
}

func TestVerifyPieceInvalid(t *testing.T) {
	data := []byte("hello world")
	badHash := [20]byte{0xFF}
	if VerifyPiece(data, badHash) {
		t.Fatal("expected invalid piece")
	}
}

func TestVerifyPieceEmpty(t *testing.T) {
	data := []byte{}
	hash := sha1.Sum(data)
	if !VerifyPiece(data, hash) {
		t.Fatal("empty data should verify against its own hash")
	}
}

func TestPieceWorkFields(t *testing.T) {
	pw := PieceWork{
		Index:  5,
		Hash:   [20]byte{0xAA},
		Length: 262144,
	}
	if pw.Index != 5 {
		t.Fatal("index mismatch")
	}
	if pw.Length != 262144 {
		t.Fatal("length mismatch")
	}
}

func TestPieceResultSuccess(t *testing.T) {
	pr := PieceResult{
		Index: 3,
		Data:  []byte("piece data"),
	}
	if pr.Err != nil {
		t.Fatal("expected no error")
	}
	if pr.Index != 3 {
		t.Fatal("index mismatch")
	}
}

func TestPieceResultError(t *testing.T) {
	pr := PieceResult{
		Index: 7,
		Err:   ErrNoPeers,
	}
	if pr.Err == nil {
		t.Fatal("expected error")
	}
	if pr.Data != nil {
		t.Fatal("expected nil data on error")
	}
}
