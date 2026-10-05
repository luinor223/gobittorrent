package bencode

import (
	"reflect"
	"slices"
	"strconv"
)

// Marshal encodes v as bencode. It supports int, int64, string, []byte,
// []any and map[string]any; dictionary keys are written in sorted order.
func Marshal(v any) ([]byte, error) {
	return appendValue(nil, v)
}

func appendValue(dst []byte, v any) ([]byte, error) {
	switch x := v.(type) {
	case int:
		return appendInt(dst, int64(x)), nil
	case int64:
		return appendInt(dst, x), nil
	case []byte:
		return appendString(dst, string(x)), nil
	case string:
		return appendString(dst, x), nil
	case []any:
		return appendList(dst, x)
	case map[string]any:
		return appendDict(dst, x)
	}

	return nil, &UnsupportedTypeError{Type: reflect.TypeOf(v)}
}

func appendInt(dst []byte, v int64) []byte {
	dst = append(dst, 'i')
	dst = strconv.AppendInt(dst, v, 10)
	dst = append(dst, 'e')
	return dst
}

func appendString(dst []byte, v string) []byte {
	dst = strconv.AppendInt(dst, int64(len(v)), 10)
	dst = append(dst, ':')
	dst = append(dst, v...)

	return dst
}

func appendList(dst []byte, arr []any) ([]byte, error) {
	dst = append(dst, 'l')
	var err error
	for _, v := range arr {
		dst, err = appendValue(dst, v)
		if err != nil {
			return nil, err
		}
	}
	dst = append(dst, 'e')
	return dst, nil
}

func appendDict(dst []byte, m map[string]any) ([]byte, error) {
	dst = append(dst, 'd')
	var err error
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, key := range keys {
		dst = appendString(dst, key)
		dst, err = appendValue(dst, m[key])
		if err != nil {
			return nil, err
		}
	}
	dst = append(dst, 'e')
	return dst, nil
}
