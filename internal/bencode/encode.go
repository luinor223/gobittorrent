package bencode

import (
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"
)

// Marshal encodes v as bencode. It supports int, int64, string, []byte,
// []any and map[string]any; dictionary keys are written in sorted order.
func Marshal(v any) ([]byte, error) {
	return appendValue(nil, reflect.ValueOf(v))
}

func appendValue(dst []byte, v reflect.Value) ([]byte, error) {
	if !v.IsValid() {
		return nil, &UnsupportedTypeError{} // untyped nil
	}
	if v.Type() == rawMessageType {
		return append(dst, v.Bytes()...), nil
	}

	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			return nil, &UnsupportedTypeError{Type: v.Type()}
		}
		return appendValue(dst, v.Elem())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return appendInt(dst, v.Int()), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		u := v.Uint()
		if u > 1<<63-1 {
			return nil, &UnsupportedTypeError{Type: v.Type()} // doesn't fit in int64
		}
		return appendInt(dst, int64(u)), nil
	case reflect.String:
		return appendString(dst, v.String()), nil
	case reflect.Slice, reflect.Array:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			b := make([]byte, v.Len())
			reflect.Copy(reflect.ValueOf(b), v)
			return appendString(dst, string(b)), nil
		}
		return appendList(dst, v)
	case reflect.Map:
		if v.Type().Key().Kind() != reflect.String {
			return nil, &UnsupportedTypeError{Type: v.Type()}
		}
		return appendDict(dst, v)
	case reflect.Struct:
		return appendStruct(dst, v)
	}

	return nil, &UnsupportedTypeError{Type: v.Type()}
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

func appendList(dst []byte, v reflect.Value) ([]byte, error) {
	dst = append(dst, 'l')
	var err error
	for i := range v.Len() {
		dst, err = appendValue(dst, v.Index(i))
		if err != nil {
			return nil, err
		}
	}
	dst = append(dst, 'e')
	return dst, nil
}

func appendDict(dst []byte, v reflect.Value) ([]byte, error) {
	dst = append(dst, 'd')
	var err error
	keys := v.MapKeys()
	slices.SortFunc(keys, func(a, b reflect.Value) int {
		return strings.Compare(a.String(), b.String())
	})

	for _, key := range keys {
		dst = appendString(dst, key.String())
		dst, err = appendValue(dst, v.MapIndex(key))
		if err != nil {
			return nil, err
		}
	}
	dst = append(dst, 'e')
	return dst, nil
}

func appendStruct(dst []byte, v reflect.Value) ([]byte, error) {
	dst = append(dst, 'd')
	fields := structFields(v.Type())

	var err error
	for _, k := range slices.Sorted(maps.Keys(fields)) {
		if fields[k].omitEmpty && v.Field(fields[k].index).IsZero() {
			continue
		}
		dst = appendString(dst, k)
		dst, err = appendValue(dst, v.Field(fields[k].index))
		if err != nil {
			return nil, err
		}
	}
	dst = append(dst, 'e')
	return dst, nil
}
