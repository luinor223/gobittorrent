package tracker

import (
	"context"
	"net/netip"
	"testing"

	"github.com/luinor223/gobittorrent/internal/bencode"
)

// marshal encodes v as bencode, failing the test on error.
func marshal(t testing.TB, v any) []byte {
	t.Helper()
	b, err := bencode.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// compact packs peers in the BEP 23 / BEP 7 binary format.
func compact(peers ...string) string {
	var b []byte
	for _, p := range peers {
		ap := netip.MustParseAddrPort(p)
		b = append(b, ap.Addr().AsSlice()...)
		b = append(b, byte(ap.Port()>>8), byte(ap.Port()))
	}
	return string(b)
}

func addrs(peers ...string) []netip.AddrPort {
	var out []netip.AddrPort
	for _, p := range peers {
		out = append(out, netip.MustParseAddrPort(p))
	}
	return out
}

func TestNewPeerID(t *testing.T) {
	a, b := NewPeerID(), NewPeerID()
	if got := string(a[:8]); got != "-GB0001-" {
		t.Errorf("prefix = %q, want %q", got, "-GB0001-")
	}
	if a == b {
		t.Error("two peer IDs are equal; the suffix must be random")
	}
}

func TestAnnounceInvalidURL(t *testing.T) {
	for _, u := range []string{"http://bad host/\x7f", "ftp://tracker.example/announce", "tracker.example"} {
		if _, err := Announce(context.Background(), u, Request{}); err == nil {
			t.Errorf("Announce(%q): err = nil, want an error", u)
		}
	}
}
