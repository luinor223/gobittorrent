package bencode

import (
	"fmt"
	"reflect"
)

// SyntaxError describes malformed bencode input.
type SyntaxError struct {
	Offset int    // byte offset where the problem was found
	Msg    string // description of the problem
}

func (e *SyntaxError) Error() string {
	return fmt.Sprintf("bencode: %s at offset %d", e.Msg, e.Offset)
}

// UnsupportedTypeError describes a Go value that bencode can't represent.
type UnsupportedTypeError struct {
	Type reflect.Type // nil for an untyped nil value
}

func (e *UnsupportedTypeError) Error() string {
	if e.Type == nil {
		return "bencode: unsupported value nil"
	}
	return "bencode: unsupported type " + e.Type.String()
}

// UnmarshalTypeError describes a bencode value that can't be stored in a Go type.
type UnmarshalTypeError struct {
	Value  string       // "integer", "string", "list" or "dictionary"
	Type   reflect.Type // the Go type it couldn't be stored in
	Offset int          // byte offset of the value
	Field  string       // the struct field path, e.g. "Info.PieceLength", if any
}

func (e *UnmarshalTypeError) Error() string {
	if e.Field != "" {
		return fmt.Sprintf("bencode: cannot unmarshal %s into Go struct field %s of type %s at offset %d",
			e.Value, e.Field, e.Type, e.Offset)
	}
	return fmt.Sprintf("bencode: cannot unmarshal %s into Go value of type %s at offset %d",
		e.Value, e.Type, e.Offset)
}

// InvalidUnmarshalError describes an invalid argument passed to Unmarshal.
type InvalidUnmarshalError struct {
	Type reflect.Type
}

func (e *InvalidUnmarshalError) Error() string {
	if e.Type == nil {
		return "bencode: Unmarshal(nil)"
	}
	if e.Type.Kind() != reflect.Pointer {
		return "bencode: Unmarshal(non-pointer " + e.Type.String() + ")"
	}
	return "bencode: Unmarshal(nil " + e.Type.String() + ")"
}
