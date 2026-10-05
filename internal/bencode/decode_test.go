package bencode

import (
	"reflect"
	"strings"
	"testing"
)

func TestDecode(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  any
	}{
		{"example 1: integer", "i42e", int64(42)},
		{"example 2: string", "4:spam", "spam"},
		{"example 3: list", "l4:spami42ee", []any{"spam", int64(42)}},
		{"example 4: dict", "d3:bar4:spam3:fooi42ee", map[string]any{"bar": "spam", "foo": int64(42)}},
		{"example 5: nested", "d4:listl1:a1:bee", map[string]any{"list": []any{"a", "b"}}},
		{"example 6: empty list", "le", []any{}},
		{"example 7: binary string", "3:\x00\xff\x10", "\x00\xff\x10"},

		// Integers
		{"zero", "i0e", int64(0)},
		{"negative", "i-7e", int64(-7)},
		{"int64 max", "i9223372036854775807e", int64(9223372036854775807)},
		{"int64 min", "i-9223372036854775808e", int64(-9223372036854775808)},

		// Strings
		{"empty string", "0:", ""},
		{"string with syntax chars", "6:i1e:de", "i1e:de"},
		{"string with colon", "3:a:b", "a:b"},
		{"long length", "10:0123456789", "0123456789"},

		// Lists
		{"nested empty lists", "llee", []any{[]any{}}},
		{"list of lists", "lli1eeli2eee", []any{[]any{int64(1)}, []any{int64(2)}}},
		{"mixed list", "li1e1:ad1:bi2eee", []any{int64(1), "a", map[string]any{"b": int64(2)}}},

		// Dicts
		{"empty dict", "de", map[string]any{}},
		{"empty key", "d0:i1ee", map[string]any{"": int64(1)}},
		{"unsorted keys accepted", "d3:fooi1e3:bari2ee", map[string]any{"foo": int64(1), "bar": int64(2)}},
		{"nested dict", "d1:ad1:bd1:ci1eeee", map[string]any{
			"a": map[string]any{"b": map[string]any{"c": int64(1)}},
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Decode([]byte(tt.input))
			if err != nil {
				t.Fatalf("Decode(%q) returned error: %v", tt.input, err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Decode(%q)\n got: %#v\nwant: %#v", tt.input, got, tt.want)
			}
		})
	}
}

func TestDecodeInvalid(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		// From docs/problems/01-bencode.md
		{"empty input", ""},
		{"missing e", "i42"},
		{"empty integer", "ie"},
		{"negative zero", "i-0e"},
		{"leading zero", "i03e"},
		{"non-digit in integer", "i4x2e"},
		{"string too short", "5:abc"},
		{"negative string length", "-1:a"},
		{"unterminated list", "l4:spam"},
		{"non-string dict key", "di1e3:fooe"},
		{"key without value", "d3:fooe"},
		{"trailing data", "i1ei2e"},
		{"unknown prefix", "x"},

		// Integers
		{"lone i", "i"},
		{"only minus", "i-e"},
		{"double minus", "i--1e"},
		{"plus sign", "i+1e"},
		{"int64 overflow", "i9223372036854775808e"},
		{"int64 underflow", "i-9223372036854775809e"},

		// Strings
		{"missing colon", "4spam"},
		{"length without data", "3:"},
		{"length overflow", "99999999999999999999:a"},

		// Lists and dicts
		{"lone l", "l"},
		{"lone d", "d"},
		{"unterminated dict", "d1:ai1e"},
		{"list key in dict", "dle1:ae"},
		{"stray end", "e"},
		{"trailing e", "i1ee"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Decode([]byte(tt.input))
			if err == nil {
				t.Errorf("Decode(%q) = %#v, want error", tt.input, got)
			}
		})
	}
}

func TestDecodeDeepNesting(t *testing.T) {
	const depth = 1000
	input := strings.Repeat("l", depth) + strings.Repeat("e", depth)
	if _, err := Decode([]byte(input)); err != nil {
		t.Fatalf("Decode of %d nested lists returned error: %v", depth, err)
	}
}

func TestDecodeTorrent(t *testing.T) {
	pieces := strings.Repeat("\xab", 20) + strings.Repeat("\x00", 20)
	input := "d" +
		"8:announce" + "40:http://tracker.example.com:6969/announce" +
		"13:creation date" + "i1727654400e" +
		"4:info" + "d" +
		"6:length" + "i524288e" +
		"4:name" + "8:file.iso" +
		"12:piece length" + "i262144e" +
		"6:pieces" + "40:" + pieces +
		"e" +
		"e"

	want := map[string]any{
		"announce":      "http://tracker.example.com:6969/announce",
		"creation date": int64(1727654400),
		"info": map[string]any{
			"length":       int64(524288),
			"name":         "file.iso",
			"piece length": int64(262144),
			"pieces":       pieces,
		},
	}

	got, err := Decode([]byte(input))
	if err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Decode\n got: %#v\nwant: %#v", got, want)
	}
}

// FuzzDecode checks that Decode never panics, whatever the input.
// Run with: go test -fuzz=FuzzDecode ./internal/bencode
func FuzzDecode(f *testing.F) {
	seeds := []string{
		"i42e", "4:spam", "l4:spami42ee", "d3:bar4:spam3:fooi42ee",
		"le", "de", "i-0e", "5:abc", "di1e3:fooe", "lli1eeli2eee",
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		Decode(data)
	})
}
