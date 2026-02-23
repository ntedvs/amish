package torrent

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
)

func Decode(r io.Reader) (any, error) {
	br := bufio.NewReader(r)
	return decode(br)
}

func decode(br *bufio.Reader) (any, error) {
	b, err := br.ReadByte()
	if err != nil {
		return nil, err
	}
	br.UnreadByte()

	switch {
	case b == 'i':
		return decodeInt(br)
	case b == 'l':
		return decodeList(br)
	case b == 'd':
		return decodeDict(br)
	case b >= '0' && b <= '9':
		return decodeString(br)
	default:
		return nil, fmt.Errorf("unexpected byte: %c", b)
	}
}

func decodeInt(br *bufio.Reader) (any, error) {
	br.ReadByte()
	var buf []byte

	for {
		b, err := br.ReadByte()
		if err != nil {
			return nil, err
		}

		if b == 'e' {
			break
		}

		buf = append(buf, b)
	}

	n, err := strconv.ParseInt(string(buf), 10, 64)
	if err != nil {
		return nil, err
	}

	return n, nil
}

func decodeString(br *bufio.Reader) (any, error) {
	var buf []byte

	for {
		b, err := br.ReadByte()
		if err != nil {
			return nil, err
		}

		if b == ':' {
			break
		}

		buf = append(buf, b)
	}

	n, err := strconv.ParseInt(string(buf), 10, 64)
	if err != nil {
		return nil, err
	}

	data := make([]byte, n)
	_, err = io.ReadFull(br, data)
	if err != nil {
		return nil, err
	}

	return string(data), nil
}

func decodeList(br *bufio.Reader) (any, error) {
	br.ReadByte()
}

func decodeDict(br *bufio.Reader) (any, error) {}
