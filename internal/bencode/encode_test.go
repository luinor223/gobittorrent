package bencode

import (
	"bytes"
	"os"
	"reflect"
	"testing"
)

func TestMarshal(t *testing.T) {
	tests := []struct {
		name  string
		input any
		want  string
	}{
		// Examples from docs/problems/02-bencode-marshal.md
		{"example 1: int64", int64(42), "i42e"},
		{"example 2: string", "spam", "4:spam"},
		{"example 3: list", []any{"spam", int64(42)}, "l4:spami42ee"},
		{"example 4: dict sorted", map[string]any{"foo": int64(42), "bar": "spam"}, "d3:bar4:spam3:fooi42ee"},
		{"example 5: nested", map[string]any{"list": []any{"a", map[string]any{}}}, "d4:listl1:adeee"},
		{"example 6: binary string", "\x00\xff", "2:\x00\xff"},
		{"example 7: utf-8 counts bytes", "héllo", "6:héllo"},

		// Integers
		{"int", 7, "i7e"},
		{"zero", int64(0), "i0e"},
		{"negative", int64(-7), "i-7e"},
		{"int64 max", int64(9223372036854775807), "i9223372036854775807e"},
		{"int64 min", int64(-9223372036854775808), "i-9223372036854775808e"},

		// Strings
		{"empty string", "", "0:"},
		{"byte slice", []byte("spam"), "4:spam"},
		{"empty byte slice", []byte{}, "0:"},
		{"multi-digit length", "0123456789", "10:0123456789"},

		// Lists
		{"empty list", []any{}, "le"},
		{"nested lists", []any{[]any{int64(1)}, []any{int64(2)}}, "lli1eeli2eee"},
		{"mixed list", []any{1, "a", []byte("b"), map[string]any{"c": int64(2)}}, "li1e1:a1:bd1:ci2eee"},

		// Dicts
		{"empty dict", map[string]any{}, "de"},
		{"empty key", map[string]any{"": int64(1)}, "d0:i1ee"},
		{"byte order, not length", map[string]any{"bb": int64(2), "a": int64(1), "c": int64(3)}, "d1:ai1e2:bbi2e1:ci3ee"},
		{"uppercase before lowercase", map[string]any{"b": int64(2), "B": int64(1)}, "d1:Bi1e1:bi2ee"},
		{"nested dict", map[string]any{"a": map[string]any{"b": map[string]any{"c": int64(1)}}}, "d1:ad1:bd1:ci1eeee"},

		// Typed values
		{"int8", int8(-5), "i-5e"},
		{"uint16", uint16(80), "i80e"},
		{"string slice", []string{"a", "b"}, "l1:a1:be"},
		{"byte array", [2]byte{'h', 'i'}, "2:hi"},
		{"int array", [2]int{1, 2}, "li1ei2ee"},
		{"string map", map[string]string{"b": "2", "a": "1"}, "d1:a1:11:b1:2e"},
		{"pointer", &[]int{1}, "li1ee"},
		{"empty struct", struct{}{}, "de"},
		{"struct sorted by key", struct {
			Name   string `bencode:"name"`
			Length int64  `bencode:"length"`
			Skip   int    `bencode:"-"`
			hidden int
		}{Name: "a", Length: 5}, "d6:lengthi5e4:name1:ae"},
		{"struct omitempty", struct {
			A string `bencode:"a,omitempty"`
			B string `bencode:"b,omitempty"`
		}{B: "x"}, "d1:b1:xe"},
		{"struct without tags", struct{ X int }{X: 1}, "d1:Xi1ee"},
		{"raw message", struct {
			Info RawMessage `bencode:"info"`
		}{Info: RawMessage("d1:ai1ee")}, "d4:infod1:ai1eee"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Marshal(tt.input)
			if err != nil {
				t.Fatalf("Marshal(%#v) returned error: %v", tt.input, err)
			}
			if string(got) != tt.want {
				t.Errorf("Marshal(%#v)\n got: %q\nwant: %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestMarshalInvalid(t *testing.T) {
	tests := []struct {
		name  string
		input any
	}{
		// From docs/problems/02-bencode-marshal.md
		{"float", 3.14},
		{"bool", true},
		{"nil", nil},
		{"float in list", []any{1.5}},
		{"nil in dict", map[string]any{"a": nil}},

		{"deeply nested", []any{map[string]any{"a": []any{int64(1), 2.5}}}},
		{"nil pointer", (*int)(nil)},
		{"non-string map key", map[int]string{1: "a"}},
		{"uint64 too large", uint64(1 << 63)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Marshal(tt.input)
			if err == nil {
				t.Errorf("Marshal(%#v) = %q, want error", tt.input, got)
			}
		})
	}
}

// Map iteration order is random, so encode the same dict many times.
func TestMarshalDeterministic(t *testing.T) {
	m := map[string]any{}
	for _, k := range []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"} {
		m[k] = k
	}
	first, err := Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	for range 100 {
		got, err := Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, first) {
			t.Fatalf("output changed between calls:\n%q\n%q", first, got)
		}
	}
}

func TestMarshalRoundTrip(t *testing.T) {
	values := []any{
		int64(42),
		"spam",
		[]any{},
		map[string]any{},
		[]any{"spam", int64(-7), []any{map[string]any{"k": "v"}}},
		map[string]any{"info": map[string]any{"name": "a.iso", "pieces": "\x00\x01\x02"}},
	}
	for _, v := range values {
		b, err := Marshal(v)
		if err != nil {
			t.Fatalf("Marshal(%#v) returned error: %v", v, err)
		}
		got, err := unmarshalAny(b)
		if err != nil {
			t.Fatalf("Unmarshal(%q) returned error: %v", b, err)
		}
		if !reflect.DeepEqual(got, v) {
			t.Errorf("round trip changed value\n got: %#v\nwant: %#v", got, v)
		}
	}
}

func TestMarshalDebianTorrentRoundTrip(t *testing.T) {
	data, err := os.ReadFile("testdata/debian.torrent")
	if err != nil {
		t.Fatal(err)
	}
	v, err := unmarshalAny(data)
	if err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	got, err := Marshal(v)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("re-encoded torrent differs from original: got %d bytes, want %d", len(got), len(data))
	}
}

// FuzzMarshalRoundTrip checks that anything Unmarshal accepts survives
// Marshal and Unmarshal again unchanged.
// Run with: go test -fuzz=FuzzMarshalRoundTrip ./internal/bencode
func FuzzMarshalRoundTrip(f *testing.F) {
	for _, s := range []string{"i42e", "4:spam", "l4:spami42ee", "d3:bar4:spam3:fooi42ee", "d1:bi1e1:ai2ee"} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		v, err := unmarshalAny(data)
		if err != nil {
			return
		}
		b, err := Marshal(v)
		if err != nil {
			t.Fatalf("Marshal(Unmarshal(%q)) returned error: %v", data, err)
		}
		got, err := unmarshalAny(b)
		if err != nil {
			t.Fatalf("Unmarshal(Marshal(...)) of %q returned error: %v", b, err)
		}
		if !reflect.DeepEqual(got, v) {
			t.Fatalf("round trip changed value\n got: %#v\nwant: %#v", got, v)
		}
	})
}
