package bencode

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
)

// unmarshalAny decodes data into an any, the generic form these tests compare against.
func unmarshalAny(data []byte) (any, error) {
	var v any
	err := Unmarshal(data, &v)
	return v, err
}

func TestUnmarshal(t *testing.T) {
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
			got, err := unmarshalAny([]byte(tt.input))
			if err != nil {
				t.Fatalf("Unmarshal(%q) returned error: %v", tt.input, err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Unmarshal(%q)\n got: %#v\nwant: %#v", tt.input, got, tt.want)
			}
		})
	}
}

func TestUnmarshalInvalid(t *testing.T) {
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
			got, err := unmarshalAny([]byte(tt.input))
			if err == nil {
				t.Errorf("Unmarshal(%q) = %#v, want error", tt.input, got)
			}
		})
	}
}

func TestUnmarshalDeepNesting(t *testing.T) {
	const depth = 1000
	input := strings.Repeat("l", depth) + strings.Repeat("e", depth)
	if _, err := unmarshalAny([]byte(input)); err != nil {
		t.Fatalf("Unmarshal of %d nested lists returned error: %v", depth, err)
	}
}

func TestUnmarshalTorrent(t *testing.T) {
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

	got, err := unmarshalAny([]byte(input))
	if err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Unmarshal\n got: %#v\nwant: %#v", got, want)
	}
}

// FuzzUnmarshal checks that Unmarshal never panics, whatever the input.
// Run with: go test -fuzz=FuzzUnmarshal ./internal/bencode
func FuzzUnmarshal(f *testing.F) {
	seeds := []string{
		"i42e", "4:spam", "l4:spami42ee", "d3:bar4:spam3:fooi42ee",
		"le", "de", "i-0e", "5:abc", "di1e3:fooe", "lli1eeli2eee",
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		unmarshalAny(data)
	})
}

type testFile struct {
	Length int64    `bencode:"length"`
	Path   []string `bencode:"path"`
}

type testInfo struct {
	Name        string     `bencode:"name"`
	PieceLength int64      `bencode:"piece length"`
	Files       []testFile `bencode:"files"`
}

func TestUnmarshalTyped(t *testing.T) {
	tests := []struct {
		name  string
		input string
		into  any // pointer to a zero value of the target type
		want  any // expected value pointed to
	}{
		// Example 1: basic types
		{"int", "i42e", new(int), 42},
		{"int8", "i-128e", new(int8), int8(-128)},
		{"uint16", "i65535e", new(uint16), uint16(65535)},
		{"uint64", "i9223372036854775807e", new(uint64), uint64(9223372036854775807)},
		{"string", "4:spam", new(string), "spam"},
		{"bytes", "4:spam", new([]byte), []byte("spam")},
		{"string slice", "l1:a1:be", new([]string), []string{"a", "b"}},
		{"empty slice", "le", new([]int), []int{}},
		{"nested slices", "ll1:aelel1:bee", new([][]string), [][]string{{"a"}, {}, {"b"}}},
		{"int map", "d1:ai1e1:bi2ee", new(map[string]int), map[string]int{"a": 1, "b": 2}},
		{"any", "l4:spami42ee", new(any), []any{"spam", int64(42)}},

		// Arrays
		{"array exact", "li1ei2ee", new([2]int), [2]int{1, 2}},
		{"array fewer elements", "li1ee", new([3]int), [3]int{1, 0, 0}},
		{"array more elements", "li1ei2ei3ee", new([2]int), [2]int{1, 2}},

		// Example 2: structs, nested structs and an ignored key
		{
			"struct",
			"d5:filesld6:lengthi5e4:pathl1:a1:beee4:name3:dir12:piece lengthi16e7:privatei1ee",
			new(testInfo),
			testInfo{Name: "dir", PieceLength: 16, Files: []testFile{{Length: 5, Path: []string{"a", "b"}}}},
		},
		{"struct ignores unknown nested values", "d1:xld1:yi1eee4:name1:ae", new(testInfo), testInfo{Name: "a"}},

		// Example 3: RawMessage
		{
			"raw message",
			"d1:bd1:x1:ye1:ti1ee",
			new(struct {
				Type int64      `bencode:"t"`
				Body RawMessage `bencode:"b"`
			}),
			struct {
				Type int64      `bencode:"t"`
				Body RawMessage `bencode:"b"`
			}{Type: 1, Body: RawMessage("d1:x1:ye")},
		},
		{"raw message top level", "li1ee", new(RawMessage), RawMessage("li1ee")},

		// Example 5: pointers
		{
			"pointer field",
			"d4:infod4:name1:aee",
			new(struct {
				Info *testInfo `bencode:"info"`
			}),
			struct {
				Info *testInfo `bencode:"info"`
			}{Info: &testInfo{Name: "a"}},
		},

		// Field naming rules
		{
			"untagged field uses its name",
			"d4:Namei1e4:namei2ee",
			new(struct{ Name int }),
			struct{ Name int }{Name: 1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := Unmarshal([]byte(tt.input), tt.into); err != nil {
				t.Fatalf("Unmarshal(%q) returned error: %v", tt.input, err)
			}
			got := reflect.ValueOf(tt.into).Elem().Interface()
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Unmarshal(%q)\n got: %#v\nwant: %#v", tt.input, got, tt.want)
			}
		})
	}
}

func TestUnmarshalKeepsUntouchedFields(t *testing.T) {
	type T struct {
		A      int `bencode:"a"`
		B      int `bencode:"b"`
		Skip   int `bencode:"-"`
		hidden int
	}
	v := T{B: 7, Skip: 8, hidden: 9}
	if err := Unmarshal([]byte("d1:ai1e4:Skipi100e6:hiddeni100ee"), &v); err != nil {
		t.Fatal(err)
	}
	want := T{A: 1, B: 7, Skip: 8, hidden: 9}
	if v != want {
		t.Errorf("got %+v, want %+v", v, want)
	}
}

func TestUnmarshalRawMessageIsCopied(t *testing.T) {
	data := []byte("d1:bi1ee")
	var v struct {
		B RawMessage `bencode:"b"`
	}
	if err := Unmarshal(data, &v); err != nil {
		t.Fatal(err)
	}
	data[4] = 'X' // change the input after decoding
	if string(v.B) != "i1e" {
		t.Errorf("RawMessage changed with its input: %q", v.B)
	}
}

// Example 4: the real info hash.
func TestUnmarshalDebianInfoHash(t *testing.T) {
	data, err := os.ReadFile("testdata/debian.torrent")
	if err != nil {
		t.Fatal(err)
	}
	var tor struct {
		Announce string     `bencode:"announce"`
		Info     RawMessage `bencode:"info"`
	}
	if err := Unmarshal(data, &tor); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if got, want := tor.Announce, "http://bttracker.debian.org:6969/announce"; got != want {
		t.Errorf("Announce = %q, want %q", got, want)
	}
	if got, want := len(tor.Info), 60578; got != want {
		t.Errorf("len(Info) = %d, want %d", got, want)
	}
	sum := sha1.Sum(tor.Info)
	if got, want := hex.EncodeToString(sum[:]), "7acf8fb590b2060dd9c3146ef770169d593433b0"; got != want {
		t.Errorf("info hash = %s, want %s", got, want)
	}

	var info testInfo
	if err := Unmarshal(tor.Info, &info); err != nil {
		t.Fatalf("Unmarshal(Info) returned error: %v", err)
	}
	if info.Name != "debian-13.7.0-amd64-netinst.iso" || info.PieceLength != 262144 {
		t.Errorf("info = %+v", info)
	}
}

func TestUnmarshalErrors(t *testing.T) {
	var n int
	tests := []struct {
		name    string
		input   string
		into    any
		wantErr any // pointer to the expected error type
	}{
		// Invalid targets
		{"nil", "i42e", nil, new(*InvalidUnmarshalError)},
		{"non-pointer", "i42e", n, new(*InvalidUnmarshalError)},
		{"nil pointer", "i42e", (*int)(nil), new(*InvalidUnmarshalError)},

		// Values that don't fit the target
		{"string into int", "4:spam", new(int), new(*UnmarshalTypeError)},
		{"int into string", "i42e", new(string), new(*UnmarshalTypeError)},
		{"int8 overflow", "i300e", new(int8), new(*UnmarshalTypeError)},
		{"negative into uint", "i-1e", new(uint), new(*UnmarshalTypeError)},
		{"list into map", "le", new(map[string]int), new(*UnmarshalTypeError)},
		{"dict into slice", "de", new([]int), new(*UnmarshalTypeError)},
		{"string into string slice", "4:spam", new([]string), new(*UnmarshalTypeError)},
		{"int map key", "d1:ai1ee", new(map[int]int), new(*UnmarshalTypeError)},
		{"float target", "i1e", new(float64), new(*UnmarshalTypeError)},
		{"wrong field type", "d4:name4:spame", new(struct {
			Name int `bencode:"name"`
		}), new(*UnmarshalTypeError)},

		// Syntax errors are still syntax errors
		{"leading zero", "i03e", new(int), new(*SyntaxError)},
		{"trailing data", "i1ei2e", new(int), new(*SyntaxError)},
		{"unterminated", "l4:spam", new([]string), new(*SyntaxError)},
		{"too deep", deepList(1001), new(any), new(*SyntaxError)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Unmarshal([]byte(tt.input), tt.into)
			if err == nil {
				t.Fatalf("Unmarshal(%q) returned nil error", tt.input)
			}
			if !errors.As(err, tt.wantErr) {
				t.Errorf("Unmarshal(%q) error = %T (%v), want %T", tt.input, err, err, reflect.ValueOf(tt.wantErr).Elem().Interface())
			}
		})
	}
}

func TestUnmarshalErrorOffsets(t *testing.T) {
	tests := []struct {
		input string
		into  any
		want  int
	}{
		{"d3:fooe", new(any), 6},       // missing value for key "foo"
		{"i1ei2e", new(int), 3},        // trailing data
		{"li1e4:spame", new([]int), 4}, // the string that can't go into an int
	}
	for _, tt := range tests {
		err := Unmarshal([]byte(tt.input), tt.into)
		var se *SyntaxError
		var te *UnmarshalTypeError
		switch {
		case errors.As(err, &se):
			if se.Offset != tt.want {
				t.Errorf("Unmarshal(%q): SyntaxError offset = %d, want %d", tt.input, se.Offset, tt.want)
			}
		case errors.As(err, &te):
			if te.Offset != tt.want {
				t.Errorf("Unmarshal(%q): UnmarshalTypeError offset = %d, want %d", tt.input, te.Offset, tt.want)
			}
		default:
			t.Errorf("Unmarshal(%q) error = %v, want a positioned error", tt.input, err)
		}
	}
}

// Every input kind into every target kind must succeed or return an error, never panic.
func TestUnmarshalNeverPanics(t *testing.T) {
	inputs := []string{"i1e", "i-1e", "1:a", "le", "li1ee", "de", "d1:ai1ee"}
	targets := []func() any{
		func() any { return new(int) },
		func() any { return new(uint8) },
		func() any { return new(string) },
		func() any { return new([]byte) },
		func() any { return new([]string) },
		func() any { return new([2]int) },
		func() any { return new(map[string]int) },
		func() any { return new(testInfo) },
		func() any { return new(any) },
		func() any { return new(RawMessage) },
		func() any { return new(*int) },
		func() any { return new(float64) },
		func() any { return new(chan int) },
	}
	for _, in := range inputs {
		for _, target := range targets {
			into := target()
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("Unmarshal(%q, %T) panicked: %v", in, into, r)
					}
				}()
				_ = Unmarshal([]byte(in), into)
			}()
		}
	}
}

func deepList(n int) string {
	b := make([]byte, 0, 2*n)
	for range n {
		b = append(b, 'l')
	}
	for range n {
		b = append(b, 'e')
	}
	return string(b)
}
