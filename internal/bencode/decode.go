package bencode

import (
	"bytes"
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

const maxDepth = 1000

// Unmarshal parses data as exactly one bencode value and stores the result
// in the value pointed to by v.
func Unmarshal(data []byte, v any) error {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return &InvalidUnmarshalError{Type: reflect.TypeOf(v)}
	}

	d := decoder{data: data}
	if err := d.unmarshalValue(rv.Elem()); err != nil {
		return err
	}
	if d.pos < len(d.data) {
		return d.syntaxError("trailing data after value")
	}
	return nil
}

type RawMessage []byte

var rawMessageType = reflect.TypeFor[RawMessage]()

// decoder reads data starting at pos.
type decoder struct {
	data  []byte
	pos   int
	depth int
}

// unmarshalValue dispatches on the first byte at d.pos.
func (d *decoder) unmarshalValue(v reflect.Value) error {
	if d.pos >= len(d.data) {
		return d.syntaxError("unexpected end of input")
	}
	if v.Type() == rawMessageType {
		start := d.pos
		if err := d.skipValue(); err != nil {
			return err
		}
		v.SetBytes(bytes.Clone(d.data[start:d.pos]))
		return nil
	}

	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			v.Set(reflect.New(v.Type().Elem()))
		}
		return d.unmarshalValue(v.Elem())
	}

	switch d.data[d.pos] {
	case 'i':
		return d.unmarshalInt(v)
	case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		return d.unmarshalString(v)
	case 'l', 'd':
		d.depth++
		defer func() { d.depth-- }()
		if d.depth > maxDepth {
			return d.syntaxError(fmt.Sprintf("nesting too deep, limit %d", maxDepth))
		}
		if d.data[d.pos] == 'l' {
			return d.unmarshalList(v)
		}
		return d.unmarshalDict(v)
	}
	return d.syntaxError(fmt.Sprintf("invalid value prefix %q", d.data[d.pos]))
}

func (d *decoder) unmarshalInt(v reflect.Value) error {
	start := d.pos
	n, err := d.decodeInt()
	if err != nil {
		return err
	}
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if v.OverflowInt(n) {
			return &UnmarshalTypeError{Value: "integer", Type: v.Type(), Offset: start}
		}
		v.SetInt(n)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if n < 0 || v.OverflowUint(uint64(n)) {
			return &UnmarshalTypeError{Value: "integer", Type: v.Type(), Offset: start}
		}
		v.SetUint(uint64(n))
	case reflect.Interface:
		if v.NumMethod() != 0 {
			return &UnmarshalTypeError{Value: "integer", Type: v.Type(), Offset: start}
		}
		v.Set(reflect.ValueOf(n))
	default:
		return &UnmarshalTypeError{Value: "integer", Type: v.Type(), Offset: start}
	}
	return nil
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
	neg := strings.HasPrefix(s, "-")
	digits := strings.TrimPrefix(s, "-")
	if digits == "" {
		return false
	}
	if digits[0] == '0' && (len(digits) > 1 || neg) {
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
	return !strings.HasPrefix(s, "-") && isCanonicalInt(s)
}

func (d *decoder) unmarshalString(v reflect.Value) error {
	start := d.pos
	s, err := d.decodeString()
	if err != nil {
		return err
	}
	switch v.Kind() {
	case reflect.String:
		v.SetString(s)
	case reflect.Slice:
		if v.Type().Elem().Kind() != reflect.Uint8 {
			return &UnmarshalTypeError{Value: "string", Type: v.Type(), Offset: start}
		}
		v.SetBytes([]byte(s))
	case reflect.Interface:
		if v.NumMethod() != 0 {
			return &UnmarshalTypeError{Value: "string", Type: v.Type(), Offset: start}
		}
		v.Set(reflect.ValueOf(s))
	default:
		return &UnmarshalTypeError{Value: "string", Type: v.Type(), Offset: start}
	}
	return nil
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

func (d *decoder) unmarshalList(v reflect.Value) error {
	start := d.pos

	if v.Kind() == reflect.Interface && v.NumMethod() == 0 {
		list := reflect.New(reflect.TypeFor[[]any]()).Elem()
		if err := d.unmarshalList(list); err != nil {
			return err
		}
		v.Set(list)
		return nil
	}

	if v.Kind() != reflect.Slice && v.Kind() != reflect.Array {
		return &UnmarshalTypeError{Value: "list", Type: v.Type(), Offset: start}
	}

	if d.pos >= len(d.data) || d.data[d.pos] != 'l' {
		return d.syntaxError("expected list")
	}
	d.pos++

	isSlice := v.Kind() == reflect.Slice
	if isSlice {
		v.Set(reflect.MakeSlice(v.Type(), 0, 0))
	}

	i := 0
	for d.pos < len(d.data) {
		if d.data[d.pos] == 'e' {
			d.pos++
			if !isSlice {
				for ; i < v.Len(); i++ {
					v.Index(i).SetZero()
				}
			}
			return nil
		}

		if isSlice {
			v.Set(reflect.Append(v, reflect.Zero(v.Type().Elem())))
		}
		if i < v.Len() {
			if err := d.unmarshalValue(v.Index(i)); err != nil {
				return err
			}
		} else if err := d.skipValue(); err != nil {
			return err
		}
		i++
	}

	return d.syntaxError("unterminated list")
}

// unmarshalDict reads a dictionary like "d3:cow3:mooe".
func (d *decoder) unmarshalDict(v reflect.Value) error {
	start := d.pos
	if v.Kind() == reflect.Interface && v.NumMethod() == 0 {
		dict := reflect.New(reflect.TypeFor[map[string]any]()).Elem()
		if err := d.unmarshalDict(dict); err != nil {
			return err
		}
		v.Set(dict)
		return nil
	}

	if d.pos >= len(d.data) || d.data[d.pos] != 'd' {
		return d.syntaxError("expected dictionary")
	}
	d.pos++

	var fields map[string]int
	switch v.Kind() {
	case reflect.Struct:
		fields = structFields(v.Type())
	case reflect.Map:
		if v.Type().Key().Kind() != reflect.String {
			return &UnmarshalTypeError{Value: "dictionary", Type: v.Type(), Offset: start}
		}
		if v.IsNil() {
			v.Set(reflect.MakeMap(v.Type()))
		}
	default:
		return &UnmarshalTypeError{Value: "dictionary", Type: v.Type(), Offset: start}
	}

	for d.pos < len(d.data) {
		if d.data[d.pos] == 'e' {
			d.pos++
			return nil
		}
		if c := d.data[d.pos]; c < '0' || c > '9' {
			return d.syntaxError("dictionary key must be a string")
		}
		key, err := d.decodeString()
		if err != nil {
			return err
		}
		if d.pos < len(d.data) && d.data[d.pos] == 'e' {
			return d.syntaxError(fmt.Sprintf("missing value for key %q", key))
		}
		switch v.Kind() {
		case reflect.Struct:
			i, ok := fields[key]
			if ok {
				if err := d.unmarshalValue(v.Field(i)); err != nil {
					return err
				}
			} else if err := d.skipValue(); err != nil {
				return err
			}
		case reflect.Map:
			elem := reflect.New(v.Type().Elem()).Elem()
			if err := d.unmarshalValue(elem); err != nil {
				return err
			}
			keyValue := reflect.ValueOf(key)
			v.SetMapIndex(keyValue, elem)
		}
	}

	return d.syntaxError("unterminated dictionary")
}

func (d *decoder) skipValue() error {
	if d.pos >= len(d.data) {
		return d.syntaxError("unexpected end of input")
	}
	switch d.data[d.pos] {
	case 'i':
		_, err := d.decodeInt()
		return err
	case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		_, err := d.decodeString()
		return err
	case 'l', 'd':
		d.depth++
		defer func() { d.depth-- }()
		if d.depth > maxDepth {
			return d.syntaxError(fmt.Sprintf("nesting too deep, limit %d", maxDepth))
		}
		d.pos++
		for d.pos < len(d.data) {
			if d.data[d.pos] == 'e' {
				d.pos++
				return nil
			}
			if err := d.skipValue(); err != nil {
				return err
			}
		}
	}
	return d.syntaxError(fmt.Sprintf("invalid value prefix %q", d.data[d.pos]))
}

// structFields maps each bencode key to the index of its struct field.
func structFields(t reflect.Type) map[string]int {
	fields := make(map[string]int)
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		name := f.Name
		if tag := f.Tag.Get("bencode"); tag != "" {
			if tag == "-" {
				continue
			}
			name, _, _ = strings.Cut(tag, ",")
		}
		fields[name] = i
	}
	return fields
}

// syntaxError returns a *SyntaxError at the current position.
func (d *decoder) syntaxError(msg string) error {
	return &SyntaxError{Offset: d.pos, Msg: msg}
}
