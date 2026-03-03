package bencode

import (
	"testing"
)

func TestDecodeInt(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    int64
		wantErr bool
	}{
		{"zero", "i0e", 0, false},
		{"positive", "i42e", 42, false},
		{"negative", "i-7e", -7, false},
		{"large", "i9999999e", 9999999, false},
		{"leading zero", "i03e", 0, true},
		{"negative zero", "i-0e", 0, true},
		{"empty", "ie", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Decode([]byte(tt.input))
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDecodeString(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{"simple", "4:spam", "spam", false},
		{"empty", "0:", "", false},
		{"binary", "3:\x00\x01\x02", "\x00\x01\x02", false},
		{"long", "11:hello world", "hello world", false},
		{"truncated", "10:short", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Decode([]byte(tt.input))
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDecodeList(t *testing.T) {
	tests := []struct {
		name  string
		input string
		len   int
	}{
		{"empty", "le", 0},
		{"ints", "li1ei2ei3ee", 3},
		{"mixed", "li42e4:spame", 2},
		{"nested", "lli1eeli2eee", 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Decode([]byte(tt.input))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			list, ok := got.([]any)
			if !ok {
				t.Fatalf("expected list, got %T", got)
			}
			if len(list) != tt.len {
				t.Fatalf("got len %d, want %d", len(list), tt.len)
			}
		})
	}
}

func TestDecodeDict(t *testing.T) {
	input := "d3:bar4:spam3:fooi42ee"
	got, err := Decode([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	dict, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("expected dict, got %T", got)
	}
	if dict["bar"] != "spam" {
		t.Fatalf("dict[bar] = %v, want spam", dict["bar"])
	}
	if dict["foo"] != int64(42) {
		t.Fatalf("dict[foo] = %v, want 42", dict["foo"])
	}
}

func TestDecodeDictNested(t *testing.T) {
	input := "d4:infod4:name4:testee"
	got, err := Decode([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	dict := got.(map[string]any)
	info := dict["info"].(map[string]any)
	if info["name"] != "test" {
		t.Fatalf("got %v, want test", info["name"])
	}
}

func TestDecodeBytes(t *testing.T) {
	input := "4:spamXXX"
	val, n, err := DecodeBytes([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "spam" {
		t.Fatalf("got %v, want spam", val)
	}
	if n != 6 {
		t.Fatalf("consumed %d bytes, want 6", n)
	}
}

func TestEncodeInt(t *testing.T) {
	got, err := Encode(int64(42))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "i42e" {
		t.Fatalf("got %q, want i42e", got)
	}
}

func TestEncodeIntPlain(t *testing.T) {
	got, err := Encode(7)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "i7e" {
		t.Fatalf("got %q, want i7e", got)
	}
}

func TestEncodeString(t *testing.T) {
	got, err := Encode("spam")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "4:spam" {
		t.Fatalf("got %q, want 4:spam", got)
	}
}

func TestEncodeBytes(t *testing.T) {
	got, err := Encode([]byte{0x01, 0x02})
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "2:\x01\x02" {
		t.Fatalf("got %q", got)
	}
}

func TestEncodeList(t *testing.T) {
	got, err := Encode([]any{int64(1), "two"})
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "li1e3:twoe" {
		t.Fatalf("got %q", got)
	}
}

func TestEncodeDict(t *testing.T) {
	got, err := Encode(map[string]any{"b": int64(2), "a": int64(1)})
	if err != nil {
		t.Fatal(err)
	}
	// Keys must be sorted.
	if string(got) != "d1:ai1e1:bi2ee" {
		t.Fatalf("got %q", got)
	}
}

func TestEncodeUnsupported(t *testing.T) {
	_, err := Encode(3.14)
	if err == nil {
		t.Fatal("expected error for float")
	}
}

func TestRoundTrip(t *testing.T) {
	original := map[string]any{
		"announce": "http://tracker.example.com/announce",
		"info": map[string]any{
			"name":         "test.txt",
			"piece length": int64(262144),
			"length":       int64(1048576),
		},
	}

	encoded, err := Encode(original)
	if err != nil {
		t.Fatal(err)
	}

	decoded, err := Decode(encoded)
	if err != nil {
		t.Fatal(err)
	}

	dict := decoded.(map[string]any)
	if dict["announce"] != "http://tracker.example.com/announce" {
		t.Fatalf("announce mismatch: %v", dict["announce"])
	}
	info := dict["info"].(map[string]any)
	if info["name"] != "test.txt" {
		t.Fatalf("name mismatch: %v", info["name"])
	}
}
