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
