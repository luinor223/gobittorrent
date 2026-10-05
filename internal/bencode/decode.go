package bencode

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
)

// decoder reads data starting at pos.
type decoder struct {
	data []byte
	pos  int
}

// Decode parses data as exactly one bencode value.
func Decode(data []byte) (any, error) {
	d := decoder{data: data}
	value, err := d.decodeValue()
	if err != nil {
		return nil, err
	}
	if d.pos < len(d.data) {
		return nil, d.syntaxError("trailing data after value")
	}

	return value, nil
}

// decodeValue dispatches on the first byte at d.pos.
func (d *decoder) decodeValue() (any, error) {
	if d.pos >= len(d.data) {
		return nil, d.syntaxError("unexpected end of input")
	}
	switch d.data[d.pos] {
	case 'i':
		return d.decodeInt()
	case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		return d.decodeString()
	case 'l':
		return d.decodeList()
	case 'd':
		return d.decodeDict()
	}
	return nil, d.syntaxError(fmt.Sprintf("invalid value prefix %q", d.data[d.pos]))
}

// decodeInt reads an integer like "i42e".
func (d *decoder) decodeInt() (int64, error) {
	if d.pos >= len(d.data) || d.data[d.pos] != 'i' {
		return 0, d.syntaxError("expected integer")
	}

	start := d.pos + 1
	n := bytes.IndexByte(d.data[start:], 'e')
	if n == -1 {
		return 0, d.syntaxError("unterminated integer")
	}
	end := start + n

	s := string(d.data[start:end])
	if !isCanonicalInt(s) {
		return 0, d.syntaxError(fmt.Sprintf("invalid integer %q", s))
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, d.syntaxError(fmt.Sprintf("integer %s overflows int64", s))
	}

	d.pos = end + 1
	return v, nil
}

// isCanonicalInt rejects leading zeros, "-0" and non-digits.
func isCanonicalInt(s string) bool {
	digits := strings.TrimPrefix(s, "-")
	if digits == "" {
		return false
	}
	if digits[0] == '0' && (len(digits) > 1 || len(digits) != len(s)) {
		return false
	}
	for i := 0; i < len(digits); i++ {
		if digits[i] < '0' || digits[i] > '9' {
			return false
		}
	}
	return true
}

// isNonNegativeInt is isCanonicalInt without a minus sign.
func isNonNegativeInt(s string) bool {
	digits := strings.TrimPrefix(s, "-")

	return isCanonicalInt(digits) && len(digits) == len(s)
}

// decodeString reads a string like "4:spam".
func (d *decoder) decodeString() (string, error) {
	n := bytes.IndexByte(d.data[d.pos:], ':')
	if n == -1 {
		return "", d.syntaxError("missing ':' after string length")
	}

	s := string(d.data[d.pos : d.pos+n])

	if !isNonNegativeInt(s) {
		return "", d.syntaxError(fmt.Sprintf("invalid string length %q", s))
	}

	length, err := strconv.Atoi(s)
	if err != nil {
		return "", d.syntaxError(fmt.Sprintf("string length %s overflows int", s))
	}
	start := d.pos + n + 1
	if length > len(d.data)-start {
		return "", d.syntaxError(fmt.Sprintf("string of length %d runs past end of input", length))
	}
	value := d.data[start : start+length]
	d.pos = start + length

	return string(value), nil
}

// decodeList reads a list like "l4:spami42ee".
func (d *decoder) decodeList() ([]any, error) {
	if d.pos >= len(d.data) || d.data[d.pos] != 'l' {
		return nil, d.syntaxError("expected list")
	}
	d.pos++

	result := []any{}

	for d.pos < len(d.data) {
		if d.data[d.pos] == 'e' {
			d.pos++
			return result, nil
		}
		value, err := d.decodeValue()
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}

	return nil, d.syntaxError("unterminated list")
}

// decodeDict reads a dictionary like "d3:cow3:mooe".
func (d *decoder) decodeDict() (map[string]any, error) {
	if d.pos >= len(d.data) || d.data[d.pos] != 'd' {
		return nil, d.syntaxError("expected dictionary")
	}
	d.pos++

	result := map[string]any{}

	for d.pos < len(d.data) {
		if d.data[d.pos] == 'e' {
			d.pos++
			return result, nil
		}
		if c := d.data[d.pos]; c < '0' || c > '9' {
			return nil, d.syntaxError("dictionary key must be a string")
		}
		key, err := d.decodeString()
		if err != nil {
			return nil, err
		}
		if d.pos < len(d.data) && d.data[d.pos] == 'e' {
			return nil, d.syntaxError(fmt.Sprintf("missing value for key %q", key))
		}
		value, err := d.decodeValue()
		if err != nil {
			return nil, err
		}
		result[key] = value
	}

	return nil, d.syntaxError("unterminated dictionary")
}

// syntaxError returns a *SyntaxError at the current position.
func (d *decoder) syntaxError(msg string) error {
	return &SyntaxError{Offset: d.pos, Msg: msg}
}
