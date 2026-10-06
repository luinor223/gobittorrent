package bencode

import (
	"os"
	"testing"
)

func TestUnmarshalDebianTorrent(t *testing.T) {
	data, err := os.ReadFile("testdata/debian.torrent")
	if err != nil {
		t.Fatal(err)
	}

	v, err := unmarshalAny(data)
	if err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}

	root, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("root is %T, want map[string]any", v)
	}
	if got, want := root["announce"], "http://bttracker.debian.org:6969/announce"; got != want {
		t.Errorf("announce = %v, want %v", got, want)
	}

	info, ok := root["info"].(map[string]any)
	if !ok {
		t.Fatalf("info is %T, want map[string]any", root["info"])
	}
	if got, want := info["name"], "debian-13.7.0-amd64-netinst.iso"; got != want {
		t.Errorf("info.name = %v, want %v", got, want)
	}

	pieceLength, ok := info["piece length"].(int64)
	if !ok {
		t.Fatalf("info.piece length is %T, want int64", info["piece length"])
	}
	if got, want := pieceLength, int64(262144); got != want {
		t.Errorf("info.piece length = %v, want %v", got, want)
	}

	length, ok := info["length"].(int64)
	if !ok {
		t.Fatalf("info.length is %T, want int64", info["pieces"])
	}
	if got, want := length, int64(792723456); got != want {
		t.Errorf("info.length = %v, want %v", got, want)
	}

	pieces, ok := info["pieces"].(string)
	if !ok {
		t.Fatalf("info.pieces is %T, want string", info["pieces"])
	}
	if got, want := len(pieces), 60480; got != want {
		t.Errorf("len(info.pieces) = %d, want %d", got, want)
	}
	if len(pieces)%20 != 0 {
		t.Errorf("len(info.pieces) = %d, not a multiple of 20", len(pieces))
	}
	if got, want := int64(len(pieces)/20), (length+pieceLength-1)/pieceLength; got != want {
		t.Errorf("hash count = %d, want %d pieces", got, want)
	}
}
