// Package bencode implements encoding and decoding of the BitTorrent bencode format.
// Bencode supports four types: integers, byte strings, lists, and dictionaries.
package bencode

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
)

var (
	ErrUnexpectedEOF  = errors.New("bencode: unexpected end of input")
	ErrInvalidFormat   = errors.New("bencode: invalid format")
	ErrInvalidInteger  = errors.New("bencode: invalid integer")
	ErrInvalidString   = errors.New("bencode: invalid string length")
	ErrUnsupportedType = errors.New("bencode: unsupported type")
)

// Decode reads a bencoded value from raw bytes and returns a Go value.
// Returns one of: int64, string (byte string), []any, or map[string]any.
func Decode(data []byte) (any, error) {
	r := bytes.NewReader(data)
	val, err := decodeValue(r)
	if err != nil {
		return nil, err
	}
	return val, nil
}

// DecodeBytes is like Decode but also returns how many bytes were consumed.
func DecodeBytes(data []byte) (any, int, error) {
	r := bytes.NewReader(data)
	val, err := decodeValue(r)
	if err != nil {
		return nil, 0, err
	}
	consumed := len(data) - r.Len()
	return val, consumed, nil
}

// Encode serializes a Go value into bencode format.
// Accepts int, int64, string, []byte, []any, and map[string]any.
func Encode(v any) ([]byte, error) {
	var buf bytes.Buffer
	if err := encodeValue(&buf, v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func decodeValue(r *bytes.Reader) (any, error) {
	b, err := r.ReadByte()
	if err != nil {
		return nil, ErrUnexpectedEOF
	}

	switch {
	case b == 'i':
		return decodeInt(r)
	case b == 'l':
		return decodeList(r)
	case b == 'd':
		return decodeDict(r)
	case b >= '0' && b <= '9':
		r.UnreadByte()
		return decodeString(r)
	default:
		return nil, fmt.Errorf("%w: unexpected byte %q", ErrInvalidFormat, b)
	}
}

// decodeInt reads an integer: i<digits>e
func decodeInt(r *bytes.Reader) (int64, error) {
	var buf bytes.Buffer
	for {
		b, err := r.ReadByte()
		if err != nil {
			return 0, ErrUnexpectedEOF
		}
		if b == 'e' {
			break
		}
		buf.WriteByte(b)
	}

	s := buf.String()
	if s == "" {
		return 0, ErrInvalidInteger
	}
	// Leading zeros are not allowed (except for "0" itself).
	if len(s) > 1 && s[0] == '0' {
		return 0, fmt.Errorf("%w: leading zero", ErrInvalidInteger)
	}
	// Negative zero is not allowed.
	if s == "-0" {
		return 0, fmt.Errorf("%w: negative zero", ErrInvalidInteger)
	}

	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: %v", ErrInvalidInteger, err)
	}
	return n, nil
}

// decodeString reads a byte string: <length>:<data>
func decodeString(r *bytes.Reader) (string, error) {
	var lenBuf bytes.Buffer
	for {
		b, err := r.ReadByte()
		if err != nil {
			return "", ErrUnexpectedEOF
		}
		if b == ':' {
			break
		}
		if b < '0' || b > '9' {
			return "", fmt.Errorf("%w: non-digit in length", ErrInvalidString)
		}
		lenBuf.WriteByte(b)
	}

	length, err := strconv.Atoi(lenBuf.String())
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidString, err)
	}

	data := make([]byte, length)
	_, err = io.ReadFull(r, data)
	if err != nil {
		return "", ErrUnexpectedEOF
	}
	return string(data), nil
}

// decodeList reads a list: l<values>e
func decodeList(r *bytes.Reader) ([]any, error) {
	var list []any
	for {
		b, err := r.ReadByte()
		if err != nil {
			return nil, ErrUnexpectedEOF
		}
		if b == 'e' {
			return list, nil
		}
		r.UnreadByte()

		val, err := decodeValue(r)
		if err != nil {
			return nil, err
		}
		list = append(list, val)
	}
}

// decodeDict reads a dictionary: d<key><value>...e
// Keys must be strings and appear in sorted order.
func decodeDict(r *bytes.Reader) (map[string]any, error) {
	dict := make(map[string]any)
	for {
		b, err := r.ReadByte()
		if err != nil {
			return nil, ErrUnexpectedEOF
		}
		if b == 'e' {
			return dict, nil
		}
		r.UnreadByte()

		keyVal, err := decodeValue(r)
		if err != nil {
			return nil, err
		}
		key, ok := keyVal.(string)
		if !ok {
			return nil, fmt.Errorf("%w: dict key must be a string", ErrInvalidFormat)
		}

		val, err := decodeValue(r)
		if err != nil {
			return nil, err
		}
		dict[key] = val
	}
}

func encodeValue(buf *bytes.Buffer, v any) error {
	switch val := v.(type) {
	case int:
		fmt.Fprintf(buf, "i%de", val)
	case int64:
		fmt.Fprintf(buf, "i%de", val)
	case string:
		fmt.Fprintf(buf, "%d:%s", len(val), val)
	case []byte:
		fmt.Fprintf(buf, "%d:", len(val))
		buf.Write(val)
	case []any:
		buf.WriteByte('l')
		for _, item := range val {
			if err := encodeValue(buf, item); err != nil {
				return err
			}
		}
		buf.WriteByte('e')
	case map[string]any:
		buf.WriteByte('d')
		// Keys must be sorted.
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(buf, "%d:%s", len(k), k)
			if err := encodeValue(buf, val[k]); err != nil {
				return err
			}
		}
		buf.WriteByte('e')
	default:
		return fmt.Errorf("%w: %T", ErrUnsupportedType, v)
	}
	return nil
}
