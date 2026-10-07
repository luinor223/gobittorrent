package bencode

import (
	"reflect"
	"strings"
)

// RawMessage is a raw encoded bencode value. Unmarshal stores the original
// bytes of a value in it, and Marshal writes it out unchanged.
type RawMessage []byte

var rawMessageType = reflect.TypeFor[RawMessage]()

// field describes a struct field that maps to a bencode dictionary key.
type field struct {
	index     int
	omitEmpty bool
}

// structFields maps each bencode key to the index of its struct field.
func structFields(t reflect.Type) map[string]field {
	fields := make(map[string]field)
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		name := f.Name
		omitEmpty := false
		if tag := f.Tag.Get("bencode"); tag != "" {
			if tag == "-" {
				continue
			}
			var options string
			name, options, _ = strings.Cut(tag, ",")
			if options == "omitempty" {
				omitEmpty = true
			}
		}
		fields[name] = field{index: i, omitEmpty: omitEmpty}
	}
	return fields
}
