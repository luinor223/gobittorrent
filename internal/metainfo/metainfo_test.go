package metainfo

import (
	"encoding/hex"
	"errors"
	"math"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/luinor223/gobittorrent/internal/bencode"
)

// hashes returns n fake 20-byte piece hashes.
func hashes(n int) string {
	return strings.Repeat("abcdefghijklmnopqrst", n)
}

// file builds one entry of a multi-file "files" list.
func file(length int, path ...string) map[string]any {
	p := make([]any, len(path))
	for i, s := range path {
		p[i] = s
	}
	return map[string]any{"length": length, "path": p}
}

// singleInfo returns a valid single-file info dict: 10 bytes, 4-byte pieces.
func singleInfo() map[string]any {
	return map[string]any{
		"name":         "a.txt",
		"piece length": 4,
		"pieces":       hashes(3),
		"length":       10,
	}
}

// multiInfo returns a valid multi-file info dict: 3+5 bytes, 4-byte pieces.
func multiInfo() map[string]any {
	return map[string]any{
		"name":         "root",
		"piece length": 4,
		"pieces":       hashes(2),
		"files":        []any{file(3, "a"), file(5, "dir", "b")},
	}
}

// encode wraps info in a torrent with an announce URL.
func encode(t *testing.T, info map[string]any) []byte {
	t.Helper()
	data, err := bencode.Marshal(map[string]any{
		"announce": "http://tracker",
		"info":     info,
	})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestParseDebian(t *testing.T) {
	data, err := os.ReadFile("testdata/debian.torrent")
	if err != nil {
		t.Fatal(err)
	}
	tor, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}

	if got, want := tor.Announce, "http://bttracker.debian.org:6969/announce"; got != want {
		t.Errorf("Announce = %q, want %q", got, want)
	}
	if tor.AnnounceList != nil {
		t.Errorf("AnnounceList = %v, want nil", tor.AnnounceList)
	}
	if got, want := hex.EncodeToString(tor.InfoHash[:]), "7acf8fb590b2060dd9c3146ef770169d593433b0"; got != want {
		t.Errorf("InfoHash = %s, want %s", got, want)
	}

	info := tor.Info
	if got, want := info.Name, "debian-13.7.0-amd64-netinst.iso"; got != want {
		t.Errorf("Name = %q, want %q", got, want)
	}
	if info.PieceLength != 262144 {
		t.Errorf("PieceLength = %d, want 262144", info.PieceLength)
	}
	if len(info.Pieces) != 3024 {
		t.Errorf("len(Pieces) = %d, want 3024", len(info.Pieces))
	}
	if info.Length != 792723456 {
		t.Errorf("Length = %d, want 792723456", info.Length)
	}
	if info.Files != nil {
		t.Errorf("Files = %v, want nil", info.Files)
	}
	if got := info.TotalLength(); got != 792723456 {
		t.Errorf("TotalLength() = %d, want 792723456", got)
	}
	if got := info.PieceSize(3023); got != 262144 {
		t.Errorf("PieceSize(3023) = %d, want 262144", got)
	}
}

func TestParseSingleFile(t *testing.T) {
	tor, err := Parse(encode(t, singleInfo()))
	if err != nil {
		t.Fatal(err)
	}
	if tor.Announce != "http://tracker" {
		t.Errorf("Announce = %q, want %q", tor.Announce, "http://tracker")
	}
	if tor.Info.Length != 10 || tor.Info.Files != nil {
		t.Errorf("Length, Files = %d, %v; want 10, nil", tor.Info.Length, tor.Info.Files)
	}
	if tor.Info.Private {
		t.Error("Private = true, want false")
	}
}

func TestParseMultiFile(t *testing.T) {
	tor, err := Parse(encode(t, multiInfo()))
	if err != nil {
		t.Fatal(err)
	}
	info := tor.Info
	want := []File{{3, []string{"a"}}, {5, []string{"dir", "b"}}}
	if !reflect.DeepEqual(info.Files, want) {
		t.Errorf("Files = %v, want %v", info.Files, want)
	}
	if info.Length != 0 {
		t.Errorf("Length = %d, want 0", info.Length)
	}
	if got := info.TotalLength(); got != 8 {
		t.Errorf("TotalLength() = %d, want 8", got)
	}
	if len(info.Pieces) != 2 {
		t.Errorf("len(Pieces) = %d, want 2", len(info.Pieces))
	}
}

func TestParsePieces(t *testing.T) {
	info := singleInfo()
	info["pieces"] = "aaaaaaaaaaaaaaaaaaaa" + "bbbbbbbbbbbbbbbbbbbb" + "cccccccccccccccccccc"
	tor, err := Parse(encode(t, info))
	if err != nil {
		t.Fatal(err)
	}
	for i, c := range []byte("abc") {
		for _, b := range tor.Info.Pieces[i] {
			if b != c {
				t.Fatalf("Pieces[%d] = %q, want all %q", i, tor.Info.Pieces[i], c)
			}
		}
	}
}

func TestParseAnnounceListAndPrivate(t *testing.T) {
	info := singleInfo()
	info["private"] = 1
	data, err := bencode.Marshal(map[string]any{
		"announce":      "http://a",
		"announce-list": []any{[]any{"http://a", "http://b"}, []any{"udp://c"}},
		"comment":       "unknown keys are ignored",
		"info":          info,
	})
	if err != nil {
		t.Fatal(err)
	}
	tor, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"http://a", "http://b"}, {"udp://c"}}
	if !reflect.DeepEqual(tor.AnnounceList, want) {
		t.Errorf("AnnounceList = %v, want %v", tor.AnnounceList, want)
	}
	if !tor.Info.Private {
		t.Error("Private = false, want true")
	}
}

func TestParseInfoHashUsesRawBytes(t *testing.T) {
	data := encode(t, singleInfo())
	tor, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}

	// Unknown keys inside info must still count towards the hash.
	info := singleInfo()
	info["source"] = "x"
	tor2, err := Parse(encode(t, info))
	if err != nil {
		t.Fatal(err)
	}
	if tor.InfoHash == tor2.InfoHash {
		t.Error("InfoHash ignores unknown keys in info; it must hash the raw bytes")
	}
}

func TestPieceSize(t *testing.T) {
	info := &Info{PieceLength: 4, Length: 10, Pieces: make([][20]byte, 3)}
	tests := []struct {
		index int
		want  int64
	}{
		{0, 4},
		{1, 4},
		{2, 2}, // short last piece
		{3, 0}, // out of range
		{-1, 0},
	}
	for _, tt := range tests {
		if got := info.PieceSize(tt.index); got != tt.want {
			t.Errorf("PieceSize(%d) = %d, want %d", tt.index, got, tt.want)
		}
	}
}

func TestParseInvalid(t *testing.T) {
	tests := []struct {
		name   string
		modify func(info map[string]any)
	}{
		{"empty name", func(i map[string]any) { i["name"] = "" }},
		{"name dot", func(i map[string]any) { i["name"] = "." }},
		{"name dotdot", func(i map[string]any) { i["name"] = ".." }},
		{"name with slash", func(i map[string]any) { i["name"] = "a/b" }},
		{"piece length 0", func(i map[string]any) { i["piece length"] = 0 }},
		{"piece length negative", func(i map[string]any) { i["piece length"] = -4 }},
		{"pieces 39 bytes", func(i map[string]any) { i["pieces"] = hashes(2)[:39] }},
		{"pieces empty", func(i map[string]any) { i["pieces"] = "" }},
		{"too few hashes", func(i map[string]any) { i["pieces"] = hashes(2) }},
		{"too many hashes", func(i map[string]any) { i["pieces"] = hashes(4) }},
		{"both length and files", func(i map[string]any) { i["files"] = []any{file(10, "a")} }},
		{"neither length nor files", func(i map[string]any) { delete(i, "length") }},
		{"negative file length", func(i map[string]any) {
			delete(i, "length")
			i["files"] = []any{file(-1, "a"), file(11, "b")}
		}},
		{"empty path", func(i map[string]any) {
			delete(i, "length")
			i["files"] = []any{file(10)}
		}},
		{"empty path component", func(i map[string]any) {
			delete(i, "length")
			i["files"] = []any{file(10, "a", "")}
		}},
		{"path traversal", func(i map[string]any) {
			delete(i, "length")
			i["files"] = []any{file(10, "..", "x")}
		}},
		{"path dot", func(i map[string]any) {
			delete(i, "length")
			i["files"] = []any{file(10, ".")}
		}},
		{"path component with slash", func(i map[string]any) {
			delete(i, "length")
			i["files"] = []any{file(10, "a/../../etc")}
		}},
		{"total length overflows", func(i map[string]any) {
			delete(i, "length")
			i["files"] = []any{file(math.MaxInt64, "a"), file(math.MaxInt64, "b"), file(2, "c")}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := singleInfo()
			tt.modify(info)
			_, err := Parse(encode(t, info))
			if !errors.Is(err, ErrInvalid) {
				t.Errorf("err = %v, want ErrInvalid", err)
			}
		})
	}
}

func TestParseMissingInfo(t *testing.T) {
	data, err := bencode.Marshal(map[string]any{"announce": "http://tracker"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(data); !errors.Is(err, ErrInvalid) {
		t.Errorf("err = %v, want ErrInvalid", err)
	}
}

func TestParseBencodeErrors(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{"not bencode", "hello"},
		{"truncated", "d8:announce"},
		{"info not a dict", "d4:infoi1ee"},
		{"name not a string", "d4:infod4:namei1eee"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.data))
			if err == nil {
				t.Fatal("err = nil, want an error")
			}
			if !strings.HasPrefix(err.Error(), "metainfo: ") {
				t.Errorf("err = %q, want prefix %q", err, "metainfo: ")
			}
		})
	}

	_, err := Parse([]byte("hello"))
	var syntaxErr *bencode.SyntaxError
	if !errors.As(err, &syntaxErr) {
		t.Errorf("err = %v, want it to wrap *bencode.SyntaxError", err)
	}
}

func FuzzParse(f *testing.F) {
	if data, err := os.ReadFile("testdata/debian.torrent"); err == nil {
		f.Add(data)
	}
	f.Add([]byte("d4:infod4:name1:a12:piece lengthi4e6:pieces20:abcdefghijklmnopqrst6:lengthi4eee"))
	f.Fuzz(func(t *testing.T, data []byte) {
		tor, err := Parse(data)
		if err != nil {
			return
		}
		// Anything Parse accepts must be internally consistent.
		info := tor.Info
		for i := range info.Pieces {
			if s := info.PieceSize(i); s <= 0 || s > info.PieceLength {
				t.Fatalf("PieceSize(%d) = %d, want in (0, %d]", i, s, info.PieceLength)
			}
		}
	})
}
