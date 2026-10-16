package peer

import (
	"bytes"
	"encoding/hex"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"testing/iotest"
)

// unhex decodes a hex string, ignoring spaces.
func unhex(t testing.TB, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(strings.ReplaceAll(s, " ", ""))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestWriteMessage(t *testing.T) {
	tests := []struct {
		name string
		m    *Message
		want string
	}{
		{"keep-alive", nil, "00000000"},
		{"interested", &Message{ID: MsgInterested}, "00000001 02"},
		{"have 5", NewHave(5), "00000005 04 00000005"},
		{"request", NewRequest(1, 16384, 16384), "0000000d 06 00000001 00004000 00004000"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := WriteMessage(&buf, tt.m); err != nil {
				t.Fatal(err)
			}
			if want := unhex(t, tt.want); !bytes.Equal(buf.Bytes(), want) {
				t.Errorf("got %x, want %x", buf.Bytes(), want)
			}
		})
	}
}

func TestReadMessageKeepAlive(t *testing.T) {
	m, err := ReadMessage(bytes.NewReader(unhex(t, "00000000")))
	if m != nil || err != nil {
		t.Errorf("got %v, %v; want nil, nil", m, err)
	}
}

func TestReadMessageStream(t *testing.T) {
	stream := unhex(t, "00000001 02  00000005 04 00000005  00000004 ff 616263")
	want := []*Message{
		{ID: MsgInterested},
		{ID: MsgHave, Payload: []byte{0, 0, 0, 5}},
		{ID: 0xff, Payload: []byte("abc")}, // unknown ID is not an error
	}

	readers := map[string]io.Reader{
		"whole":    bytes.NewReader(stream),
		"one byte": iotest.OneByteReader(bytes.NewReader(stream)),
	}
	for name, r := range readers {
		t.Run(name, func(t *testing.T) {
			for i, w := range want {
				m, err := ReadMessage(r)
				if err != nil {
					t.Fatalf("message %d: %v", i, err)
				}
				if !reflect.DeepEqual(m, w) {
					t.Errorf("message %d = %+v, want %+v", i, m, w)
				}
			}
			if _, err := ReadMessage(r); !errors.Is(err, io.EOF) {
				t.Errorf("after stream: err = %v, want io.EOF", err)
			}
		})
	}
}

func TestReadMessageTooLong(t *testing.T) {
	// Only the length is sent; a correct ReadMessage fails before allocating or reading more.
	_, err := ReadMessage(bytes.NewReader(unhex(t, "ffffffff")))
	if err == nil {
		t.Fatal("want error, got nil")
	}
	if errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("err = %v: tried to read the body instead of rejecting the length", err)
	}
}

func TestReadMessageTruncated(t *testing.T) {
	tests := map[string]string{
		"short length":  "0000",
		"short payload": "00000005 04 0000",
	}
	for name, in := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := ReadMessage(bytes.NewReader(unhex(t, in)))
			if !errors.Is(err, io.ErrUnexpectedEOF) {
				t.Errorf("err = %v, want io.ErrUnexpectedEOF", err)
			}
		})
	}
}

func TestParseHave(t *testing.T) {
	index, err := ParseHave(NewHave(3023))
	if err != nil || index != 3023 {
		t.Errorf("got %d, %v; want 3023, nil", index, err)
	}

	bad := []*Message{
		{ID: MsgPiece, Payload: []byte{0, 0, 0, 1}},
		{ID: MsgHave},
		{ID: MsgHave, Payload: []byte{0, 0, 1}},
		{ID: MsgHave, Payload: []byte{0, 0, 0, 0, 1}},
	}
	for _, m := range bad {
		if _, err := ParseHave(m); err == nil {
			t.Errorf("ParseHave(%+v): want error, got nil", m)
		}
	}
}

func TestParsePiece(t *testing.T) {
	m := &Message{ID: MsgPiece, Payload: unhex(t, "00000002 00004000 616263")}
	index, begin, block, err := ParsePiece(m)
	if err != nil {
		t.Fatal(err)
	}
	if index != 2 || begin != 16384 || string(block) != "abc" {
		t.Errorf("got %d, %d, %q; want 2, 16384, \"abc\"", index, begin, block)
	}

	bad := []*Message{
		{ID: MsgHave, Payload: make([]byte, 12)},
		{ID: MsgPiece},
		{ID: MsgPiece, Payload: make([]byte, 7)},
	}
	for _, m := range bad {
		if _, _, _, err := ParsePiece(m); err == nil {
			t.Errorf("ParsePiece(%+v): want error, got nil", m)
		}
	}
}

func FuzzReadMessage(f *testing.F) {
	f.Add([]byte{0, 0, 0, 0})
	f.Add([]byte{0, 0, 0, 1, 2})
	f.Add([]byte{0, 0, 0, 5, 4, 0, 0, 0, 5})
	f.Add([]byte{0xff, 0xff, 0xff, 0xff})
	f.Fuzz(func(t *testing.T, b []byte) {
		m, err := ReadMessage(bytes.NewReader(b))
		if err != nil || m == nil {
			return
		}
		// Whatever was read must write back to the same bytes.
		var buf bytes.Buffer
		if err := WriteMessage(&buf, m); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(buf.Bytes(), b[:buf.Len()]) {
			t.Errorf("round trip: got %x, want prefix of %x", buf.Bytes(), b)
		}
		ParseHave(m)
		ParsePiece(m)
	})
}
