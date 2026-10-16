package peer

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

func testHandshake() Handshake {
	h := Handshake{}
	h.Reserved[5] = 0x10
	copy(h.InfoHash[:], "infohash-0123456789x")
	copy(h.PeerID[:], "-GB0001-abcdefghijkl")
	return h
}

func TestHandshakeRoundTrip(t *testing.T) {
	h := testHandshake()
	var buf bytes.Buffer
	if err := WriteHandshake(&buf, h); err != nil {
		t.Fatal(err)
	}

	b := buf.Bytes()
	if len(b) != 68 {
		t.Fatalf("len = %d, want 68", len(b))
	}
	if b[0] != 19 || string(b[1:20]) != "BitTorrent protocol" {
		t.Errorf("header = %q", b[:20])
	}
	if !bytes.Equal(b[20:28], h.Reserved[:]) ||
		!bytes.Equal(b[28:48], h.InfoHash[:]) ||
		!bytes.Equal(b[48:68], h.PeerID[:]) {
		t.Errorf("fields at wrong offsets: %x", b)
	}

	got, err := ReadHandshake(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if got != h {
		t.Errorf("got %+v, want %+v", got, h)
	}
}

func TestReadHandshakeInvalid(t *testing.T) {
	var buf bytes.Buffer
	WriteHandshake(&buf, testHandshake())
	valid := buf.Bytes()

	badLen := bytes.Clone(valid)
	badLen[0] = 18
	badPstr := bytes.Clone(valid)
	copy(badPstr[1:], "BitTorrent Protocol")

	tests := []struct {
		name string
		in   []byte
	}{
		{"empty", nil},
		{"bad pstrlen", badLen},
		{"bad pstr", badPstr},
		{"truncated", valid[:40]},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ReadHandshake(bytes.NewReader(tt.in)); err == nil {
				t.Error("want error, got nil")
			}
		})
	}
}

func TestReadHandshakeTruncatedIsUnexpectedEOF(t *testing.T) {
	var buf bytes.Buffer
	WriteHandshake(&buf, testHandshake())
	_, err := ReadHandshake(bytes.NewReader(buf.Bytes()[:40]))
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("err = %v, want io.ErrUnexpectedEOF", err)
	}
}
