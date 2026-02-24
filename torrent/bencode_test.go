package torrent

import (
	"reflect"
	"strings"
	"testing"
)

func TestDecodeInt(t *testing.T) {
	result, err := Decode(strings.NewReader("i42e"))

	if err != nil {
		t.Fatal(err)
	}

	if result != int64(42) {
		t.Fatalf("got %v, want 42", result)
	}
}

func TestDecodeNegativeInt(t *testing.T) {
	result, err := Decode(strings.NewReader("i-7e"))

	if err != nil {
		t.Fatal(err)
	}

	if result != int64(-7) {
		t.Fatalf("got %v, want -7", result)
	}
}

func TestDecodeZero(t *testing.T) {
	result, err := Decode(strings.NewReader("i0e"))

	if err != nil {
		t.Fatal(err)
	}

	if result != int64(0) {
		t.Fatalf("got %v, want 0", result)
	}
}

func TestDecodeString(t *testing.T) {
	result, err := Decode(strings.NewReader("4:spam"))

	if err != nil {
		t.Fatal(err)
	}

	if result != "spam" {
		t.Fatalf("got %v, want spam", result)
	}
}

func TestDecodeEmptyString(t *testing.T) {
	result, err := Decode(strings.NewReader("0:"))

	if err != nil {
		t.Fatal(err)
	}

	if result != "" {
		t.Fatalf("got %v, want empty string", result)
	}
}

func TestDecodeList(t *testing.T) {
	result, err := Decode(strings.NewReader("l4:spami42ee"))

	if err != nil {
		t.Fatal(err)
	}

	want := []any{"spam", int64(42)}

	if !reflect.DeepEqual(result, want) {
		t.Fatalf("got %v, want %v", result, want)
	}
}

func TestDecodeEmptyList(t *testing.T) {
	result, err := Decode(strings.NewReader("le"))

	if err != nil {
		t.Fatal(err)
	}

	want := make([]any, 0)

	if !reflect.DeepEqual(result, want) {
		t.Fatalf("got %v, want %v", result, want)
	}
}

func TestDecodeNestedList(t *testing.T) {
	result, err := Decode(strings.NewReader("lli1ei2eeli3eee"))

	if err != nil {
		t.Fatal(err)
	}

	want := []any{[]any{int64(1), int64(2)}, []any{int64(3)}}

	if !reflect.DeepEqual(result, want) {
		t.Fatalf("got %v, want %v", result, want)
	}
}

func TestDecodeDict(t *testing.T) {
	result, err := Decode(strings.NewReader("d3:cow3:moo4:spam4:eggse"))

	if err != nil {
		t.Fatal(err)
	}

	want := map[string]any{"cow": "moo", "spam": "eggs"}

	if !reflect.DeepEqual(result, want) {
		t.Fatalf("got %v, want %v", result, want)
	}
}

func TestDecodeEmptyDict(t *testing.T) {
	result, err := Decode(strings.NewReader("de"))

	if err != nil {
		t.Fatal(err)
	}

	want := map[string]any{}

	if !reflect.DeepEqual(result, want) {
		t.Fatalf("got %v, want %v", result, want)
	}
}

func TestDecodeNestedDict(t *testing.T) {
	result, err := Decode(strings.NewReader("d4:infod5:counti3eee"))

	if err != nil {
		t.Fatal(err)
	}

	want := map[string]any{"info": map[string]any{"count": int64(3)}}

	if !reflect.DeepEqual(result, want) {
		t.Fatalf("got %v, want %v", result, want)
	}
}
