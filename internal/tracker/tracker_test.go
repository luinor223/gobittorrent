package tracker

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

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

func TestBuildURL(t *testing.T) {
	var req Request
	for i := range req.InfoHash {
		req.InfoHash[i] = byte(i * 13) // includes bytes that need escaping
	}
	copy(req.PeerID[:], "-GB0001-abcdefghijkl")
	req.Port = 6881
	req.Uploaded = 1
	req.Downloaded = 2
	req.Left = 5_000_000_000 // > 4 GiB, must not overflow
	req.Event = EventStarted

	got, err := buildURL("http://tracker.example/announce?passkey=abc", req)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(got)
	if err != nil {
		t.Fatalf("buildURL returned an unparseable URL %q: %v", got, err)
	}
	if u.Scheme != "http" || u.Host != "tracker.example" || u.Path != "/announce" {
		t.Errorf("URL = %q, want it to keep http://tracker.example/announce", got)
	}

	q := u.Query()
	want := map[string]string{
		"passkey":    "abc",
		"info_hash":  string(req.InfoHash[:]),
		"peer_id":    "-GB0001-abcdefghijkl",
		"port":       "6881",
		"uploaded":   "1",
		"downloaded": "2",
		"left":       "5000000000",
		"compact":    "1",
		"event":      "started",
	}
	for k, v := range want {
		if q.Get(k) != v {
			t.Errorf("query %s = %q, want %q", k, q.Get(k), v)
		}
	}
	if len(q) != len(want) {
		t.Errorf("query has %d keys, want %d: %v", len(q), len(want), q)
	}
}

func TestBuildURLNoEvent(t *testing.T) {
	got, err := buildURL("http://t/announce", Request{})
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(got)
	if u.Query().Has("event") {
		t.Errorf("URL %q has event, want none for EventNone", got)
	}
}

func TestBuildURLInvalid(t *testing.T) {
	if _, err := buildURL("http://bad host/\x7f", Request{}); err == nil {
		t.Error("err = nil, want an error for an invalid URL")
	}
}

func TestParseResponse(t *testing.T) {
	tests := []struct {
		name string
		body map[string]any
		want *Response
	}{
		{
			name: "compact peers",
			body: map[string]any{
				"interval":     1800,
				"min interval": 900,
				"complete":     12,
				"incomplete":   3,
				"peers":        compact("10.0.0.1:6881", "192.168.0.2:80"),
			},
			want: &Response{
				Interval:    30 * time.Minute,
				MinInterval: 15 * time.Minute,
				Seeders:     12,
				Leechers:    3,
				Peers:       addrs("10.0.0.1:6881", "192.168.0.2:80"),
			},
		},
		{
			name: "dict peers",
			body: map[string]any{
				"interval": 60,
				"peers": []any{
					map[string]any{"ip": "10.0.0.1", "port": 6881, "peer id": "xxxxxxxxxxxxxxxxxxxx"},
					map[string]any{"ip": "2001:db8::1", "port": 51413},
				},
			},
			want: &Response{
				Interval: time.Minute,
				Peers:    addrs("10.0.0.1:6881", "[2001:db8::1]:51413"),
			},
		},
		{
			name: "dict peers skips bad ip and port 0",
			body: map[string]any{
				"interval": 60,
				"peers": []any{
					map[string]any{"ip": "tracker.example", "port": 1},
					map[string]any{"ip": "10.0.0.2", "port": 0},
					map[string]any{"ip": "10.0.0.3", "port": 3},
				},
			},
			want: &Response{Interval: time.Minute, Peers: addrs("10.0.0.3:3")},
		},
		{
			name: "compact skips port 0",
			body: map[string]any{
				"interval": 60,
				"peers":    compact("10.0.0.1:0", "10.0.0.2:2"),
			},
			want: &Response{Interval: time.Minute, Peers: addrs("10.0.0.2:2")},
		},
		{
			name: "peers6 appended",
			body: map[string]any{
				"interval": 60,
				"peers":    compact("10.0.0.1:1"),
				"peers6":   compact("[2001:db8::1]:2", "[::1]:3"),
			},
			want: &Response{
				Interval: time.Minute,
				Peers:    addrs("10.0.0.1:1", "[2001:db8::1]:2", "[::1]:3"),
			},
		},
		{
			name: "no peers key",
			body: map[string]any{"interval": 60},
			want: &Response{Interval: time.Minute},
		},
		{
			name: "empty compact peers",
			body: map[string]any{"interval": 60, "peers": ""},
			want: &Response{Interval: time.Minute},
		},
		{
			name: "unknown keys ignored",
			body: map[string]any{"interval": 60, "warning message": "slow down", "tracker id": "x"},
			want: &Response{Interval: time.Minute},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseResponse(marshal(t, tt.body))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got  %+v\nwant %+v", got, tt.want)
			}
		})
	}
}

func TestParseResponseFailure(t *testing.T) {
	// No interval: a failure reply only needs "failure reason".
	body := marshal(t, map[string]any{"failure reason": "torrent not found"})
	_, err := parseResponse(body)

	var fe *FailureError
	if !errors.As(err, &fe) {
		t.Fatalf("err = %v, want *FailureError", err)
	}
	if fe.Reason != "torrent not found" {
		t.Errorf("Reason = %q, want %q", fe.Reason, "torrent not found")
	}
	if got, want := err.Error(), "tracker: torrent not found"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestParseResponseInvalid(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"not bencode", "<html>502 Bad Gateway</html>"},
		{"not a dict", "i5e"},
		{"missing interval", "d5:peers0:e"},
		{"zero interval", "d8:intervali0ee"},
		{"negative interval", "d8:intervali-5ee"},
		{"compact peers not multiple of 6", "d8:intervali60e5:peers5:abcdee"},
		{"peers6 not multiple of 18", "d8:intervali60e6:peers66:abcdefe"},
		{"peers is an int", "d8:intervali60e5:peersi5ee"},
		{"peers is a dict", "d8:intervali60e5:peersdee"},
		{"peers list of non-dicts", "d8:intervali60e5:peersli1eee"},
		{"dict peer port too big", "d8:intervali60e5:peersld2:ip8:10.0.0.14:porti70000eeee"},
		{"truncated", "d8:intervali60e5:peers12:abc"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseResponse([]byte(tt.body))
			if err == nil {
				t.Fatalf("got %+v, want an error", got)
			}
			if !strings.HasPrefix(err.Error(), "tracker: ") {
				t.Errorf("err = %q, want prefix %q", err, "tracker: ")
			}
		})
	}
}

func TestParseResponseWrapsBencodeError(t *testing.T) {
	_, err := parseResponse([]byte("garbage"))
	var se *bencode.SyntaxError
	if !errors.As(err, &se) {
		t.Errorf("err = %v, want it to wrap *bencode.SyntaxError", err)
	}
}

func TestAnnounce(t *testing.T) {
	var gotQuery url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		w.Write(marshal(t, map[string]any{
			"interval": 1800,
			"peers":    compact("10.0.0.1:6881"),
		}))
	}))
	defer srv.Close()

	req := Request{PeerID: NewPeerID(), Port: 6881, Left: 100, Event: EventStarted}
	req.InfoHash[0] = 0xff
	resp, err := Announce(context.Background(), srv.URL+"/announce", req)
	if err != nil {
		t.Fatal(err)
	}
	if want := addrs("10.0.0.1:6881"); !reflect.DeepEqual(resp.Peers, want) {
		t.Errorf("Peers = %v, want %v", resp.Peers, want)
	}
	if resp.Interval != 30*time.Minute {
		t.Errorf("Interval = %v, want 30m", resp.Interval)
	}
	if gotQuery.Get("info_hash") != string(req.InfoHash[:]) {
		t.Errorf("tracker got info_hash %q, want the raw bytes %q", gotQuery.Get("info_hash"), req.InfoHash[:])
	}
	if gotQuery.Get("event") != "started" {
		t.Errorf("tracker got event %q, want %q", gotQuery.Get("event"), "started")
	}
}

func TestAnnounceErrors(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		check   func(error) bool
	}{
		{
			name: "HTTP 404",
			handler: func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "not found", http.StatusNotFound)
			},
		},
		{
			name: "HTTP 502 with bencode body",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadGateway)
				w.Write([]byte("d8:intervali60ee"))
			},
		},
		{
			name: "body too large",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte("d8:intervali60e5:peers"))
				w.Write([]byte("2000000:"))
				w.Write(make([]byte, 2_000_000))
				w.Write([]byte("e"))
			},
		},
		{
			name: "failure reason",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte("d14:failure reason6:bannede"))
			},
			check: func(err error) bool {
				var fe *FailureError
				return errors.As(err, &fe) && fe.Reason == "banned"
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(tt.handler)
			defer srv.Close()

			_, err := Announce(context.Background(), srv.URL, Request{})
			if err == nil {
				t.Fatal("err = nil, want an error")
			}
			if !strings.HasPrefix(err.Error(), "tracker: ") {
				t.Errorf("err = %q, want prefix %q", err, "tracker: ")
			}
			if tt.check != nil && !tt.check(err) {
				t.Errorf("err = %v, wrong kind of error", err)
			}
		})
	}
}

func TestAnnounceContextCanceled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done() // hang until the client gives up
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := Announce(ctx, srv.URL, Request{})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want it to wrap context.DeadlineExceeded", err)
	}
}

func TestAnnounceBadURL(t *testing.T) {
	if _, err := Announce(context.Background(), "http://127.0.0.1:1/announce", Request{}); err == nil {
		t.Error("err = nil, want a connection error")
	}
}

func FuzzParseResponse(f *testing.F) {
	f.Add([]byte("d8:intervali1800e5:peers6:\x0a\x00\x00\x01\x1a\xe1e"))
	f.Add([]byte("d8:intervali60e5:peersld2:ip8:10.0.0.14:porti1eeee"))
	f.Add([]byte("d8:intervali60e6:peers618:\x20\x01\x0d\xb8\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x01\x00\x02e"))
	f.Add([]byte("d14:failure reason3:nopee"))
	f.Fuzz(func(t *testing.T, body []byte) {
		resp, err := parseResponse(body)
		if err != nil {
			return
		}
		if resp.Interval <= 0 {
			t.Fatalf("Interval = %v, want > 0", resp.Interval)
		}
		for _, p := range resp.Peers {
			if !p.IsValid() || p.Port() == 0 {
				t.Fatalf("invalid peer %v", p)
			}
		}
	})
}
