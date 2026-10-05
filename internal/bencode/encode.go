package bencode

import (
	"errors"
	"slices"
	"strconv"
)

var errUnsupportedType = errors.New("unsupported type")

func Marshal(v any) ([]byte, error) {
	switch x := v.(type) {
	case int:
		return marshalInt(int64(x)), nil
	case int64:
		return marshalInt(x), nil
	case []byte:
		return marshalString(string(x)), nil
	case string:
		return marshalString(x), nil
	case []any:
		return marshalList(x)
	case map[string]any:
		return marshalDict(x)
	}

	return nil, errUnsupportedType
}

func marshalInt(v int64) []byte {
	data := []byte{'i'}
	data = strconv.AppendInt(data, v, 10)
	data = append(data, 'e')
	return data
}

func marshalString(v string) []byte {
	data := []byte(strconv.Itoa(len(v)))
	data = append(data, ':')
	data = append(data, v...)

	return data
}

func marshalList(arr []any) ([]byte, error) {
	data := []byte{'l'}
	for _, v := range arr {
		value, err := Marshal(v)
		if err != nil {
			return nil, err
		}
		data = append(data, value...)
	}
	data = append(data, 'e')
	return data, nil
}

func marshalDict(m map[string]any) ([]byte, error) {
	data := []byte{'d'}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, key := range keys {
		data = append(data, marshalString(key)...)
		value, err := Marshal(m[key])
		if err != nil {
			return nil, err
		}
		data = append(data, value...)
	}
	data = append(data, 'e')
	return data, nil
}
