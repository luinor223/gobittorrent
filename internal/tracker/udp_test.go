package tracker

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"net"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const testConnID uint64 = 0xaabbccddeeff0011

// header builds the 8-byte action + transaction ID prefix of a reply.
func header(action, txID uint32) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint32(b[0:], action)
	binary.BigEndian.PutUint32(b[4:], txID)
	return b
}

func connectReply(txID uint32, connID uint64) []byte {
	return binary.BigEndian.AppendUint64(header(actionConnect, txID), connID)
}

func announceReply(txID uint32, interval, leechers, seeders uint32, peers string) []byte {
	b := header(actionAnnounce, txID)
	b = binary.BigEndian.AppendUint32(b, interval)
	b = binary.BigEndian.AppendUint32(b, leechers)
	b = binary.BigEndian.AppendUint32(b, seeders)
	return append(b, peers...)
}

func errorReply(txID uint32, msg string) []byte {
	return append(header(actionError, txID), msg...)
}

func TestEncodeConnect(t *testing.T) {
	got := hex.EncodeToString(encodeConnect(0x12345678))
	want := "0000041727101980" + "00000000" + "12345678"
	if got != want {
		t.Errorf("encodeConnect = %s, want %s", got, want)
	}
}

func TestDecodeConnect(t *testing.T) {
	id, err := decodeConnect(connectReply(7, testConnID), 7)
	if err != nil {
		t.Fatal(err)
	}
	if id != testConnID {
		t.Errorf("connID = %#x, want %#x", id, testConnID)
	}
}

func TestDecodeConnectErrors(t *testing.T) {
	tests := []struct {
		name string
		b    []byte
	}{
		{"empty", nil},
		{"7 bytes", connectReply(7, testConnID)[:7]},
		{"12 bytes", connectReply(7, testConnID)[:12]},
		{"announce action", announceReply(7, 60, 0, 0, "")},
		{"unknown action", append(header(9, 7), make([]byte, 8)...)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := decodeConnect(tt.b, 7); err == nil {
				t.Error("err = nil, want an error")
			}
		})
	}
}

func TestDecodeTxMismatch(t *testing.T) {
	if _, err := decodeConnect(connectReply(8, testConnID), 7); !errors.Is(err, errTxMismatch) {
		t.Errorf("decodeConnect: err = %v, want errTxMismatch", err)
	}
	if _, err := decodeAnnounce(announceReply(8, 60, 0, 0, ""), 7, 4); !errors.Is(err, errTxMismatch) {
		t.Errorf("decodeAnnounce: err = %v, want errTxMismatch", err)
	}
}

func TestDecodeErrorAction(t *testing.T) {
	// An error packet is shorter than a connect reply; the message must survive.
	b := errorReply(7, "bad")
	_, err1 := decodeConnect(b, 7)
	_, err2 := decodeAnnounce(b, 7, 4)
	for _, err := range []error{err1, err2} {
		var fe *FailureError
		if !errors.As(err, &fe) || fe.Reason != "bad" {
			t.Errorf("err = %v, want *FailureError with reason %q", err, "bad")
		}
	}
}

func TestEncodeAnnounce(t *testing.T) {
	req := Request{
		Port:       6881,
		Downloaded: 1,
		Left:       5_000_000_000,
		Uploaded:   3,
		Event:      EventStarted,
	}
	copy(req.InfoHash[:], "aaaaaaaaaaaaaaaaaaaa")
	copy(req.PeerID[:], "-GB0001-bbbbbbbbbbbb")

	b := encodeAnnounce(testConnID, 0x12345678, req, 0xdeadbeef)
	if len(b) != 98 {
		t.Fatalf("len = %d, want 98", len(b))
	}
	be := binary.BigEndian
	checks := []struct {
		field     string
		got, want uint64
	}{
		{"connection_id", be.Uint64(b[0:]), testConnID},
		{"action", uint64(be.Uint32(b[8:])), 1},
		{"transaction_id", uint64(be.Uint32(b[12:])), 0x12345678},
		{"downloaded", be.Uint64(b[56:]), 1},
		{"left", be.Uint64(b[64:]), 5_000_000_000},
		{"uploaded", be.Uint64(b[72:]), 3},
		{"event", uint64(be.Uint32(b[80:])), 2},
		{"ip", uint64(be.Uint32(b[84:])), 0},
		{"key", uint64(be.Uint32(b[88:])), 0xdeadbeef},
		{"num_want", uint64(be.Uint32(b[92:])), 0xFFFFFFFF},
		{"port", uint64(be.Uint16(b[96:])), 6881},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %d, want %d", c.field, c.got, c.want)
		}
	}
	if !bytes.Equal(b[16:36], req.InfoHash[:]) {
		t.Errorf("info_hash = %q, want %q", b[16:36], req.InfoHash[:])
	}
	if !bytes.Equal(b[36:56], req.PeerID[:]) {
		t.Errorf("peer_id = %q, want %q", b[36:56], req.PeerID[:])
	}
}

func TestUDPEvent(t *testing.T) {
	want := map[Event]uint32{EventNone: 0, EventCompleted: 1, EventStarted: 2, EventStopped: 3}
	for e, n := range want {
		if got := udpEvent(e); got != n {
			t.Errorf("udpEvent(%q) = %d, want %d", e, got, n)
		}
	}
}

func TestDecodeAnnounce(t *testing.T) {
	tests := []struct {
		name    string
		peers   string
		addrLen int
		want    []string
	}{
		{"IPv4", compact("10.0.0.1:6881", "192.168.0.2:80"), 4, []string{"10.0.0.1:6881", "192.168.0.2:80"}},
		{"IPv6", compact("[2001:db8::1]:51413"), 16, []string{"[2001:db8::1]:51413"}},
		{"no peers", "", 4, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := decodeAnnounce(announceReply(7, 1800, 3, 12, tt.peers), 7, tt.addrLen)
			if err != nil {
				t.Fatal(err)
			}
			want := &Response{
				Interval: 30 * time.Minute,
				Leechers: 3,
				Seeders:  12,
				Peers:    addrs(tt.want...),
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("got  %+v\nwant %+v", got, want)
			}
		})
	}
}

func TestDecodeAnnounceErrors(t *testing.T) {
	tests := []struct {
		name string
		b    []byte
	}{
		{"empty", nil},
		{"header only", header(actionAnnounce, 7)},
		{"19 bytes", announceReply(7, 60, 0, 0, "")[:19]},
		{"connect action", connectReply(7, testConnID)},
		{"zero interval", announceReply(7, 0, 0, 0, "")},
		{"negative interval", announceReply(7, 0xFFFFFFFF, 0, 0, "")},
		{"peers not multiple of 6", announceReply(7, 60, 0, 0, "abcde")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := decodeAnnounce(tt.b, 7, 4)
			if err == nil {
				t.Fatal("err = nil, want an error")
			}
			if !strings.HasPrefix(err.Error(), "tracker: ") {
				t.Errorf("err = %q, want prefix %q", err, "tracker: ")
			}
		})
	}
}

// fakeUDPTracker serves on 127.0.0.1. For every packet it receives, it calls
// handle with the packet and its index (0, 1, …) and sends back each reply.
// It returns the tracker's udp:// URL.
func fakeUDPTracker(t *testing.T, handle func(n int, pkt []byte) [][]byte) string {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pc.Close() })

	go func() {
		buf := make([]byte, 1500)
		for n := 0; ; n++ {
			size, from, err := pc.ReadFrom(buf)
			if err != nil {
				return // closed
			}
			for _, reply := range handle(n, append([]byte(nil), buf[:size]...)) {
				pc.WriteTo(reply, from)
			}
		}
	}()
	return "udp://" + pc.LocalAddr().String() + "/announce"
}

// txID returns the transaction ID of a request packet.
func txID(pkt []byte) uint32 {
	return binary.BigEndian.Uint32(pkt[12:16])
}

// isConnect reports whether pkt is a connect request.
func isConnect(pkt []byte) bool {
	return len(pkt) == 16 && binary.BigEndian.Uint64(pkt) == protocolID
}

// standardTracker answers connect and announce requests correctly.
func standardTracker(t *testing.T) func(int, []byte) [][]byte {
	return func(_ int, pkt []byte) [][]byte {
		if isConnect(pkt) {
			return [][]byte{connectReply(txID(pkt), testConnID)}
		}
		if got := binary.BigEndian.Uint64(pkt); got != testConnID {
			t.Errorf("announce connection_id = %#x, want %#x", got, testConnID)
		}
		return [][]byte{announceReply(txID(pkt), 1800, 3, 12, compact("10.0.0.1:6881"))}
	}
}

// shortTimeout makes UDP retries fast for the length of one test.
func shortTimeout(t *testing.T) {
	old := udpTimeout
	udpTimeout = 20 * time.Millisecond
	t.Cleanup(func() { udpTimeout = old })
}

func TestAnnounceUDP(t *testing.T) {
	url := fakeUDPTracker(t, standardTracker(t))
	resp, err := Announce(context.Background(), url, Request{Event: EventStarted})
	if err != nil {
		t.Fatal(err)
	}
	want := &Response{
		Interval: 30 * time.Minute,
		Leechers: 3,
		Seeders:  12,
		Peers:    addrs("10.0.0.1:6881"),
	}
	if !reflect.DeepEqual(resp, want) {
		t.Errorf("got  %+v\nwant %+v", resp, want)
	}
}

func TestAnnounceUDPRetry(t *testing.T) {
	shortTimeout(t)
	std := standardTracker(t)
	url := fakeUDPTracker(t, func(n int, pkt []byte) [][]byte {
		if n == 0 {
			return nil // drop the first connect request
		}
		return std(n, pkt)
	})
	if _, err := Announce(context.Background(), url, Request{}); err != nil {
		t.Fatalf("err = %v, want success after a retry", err)
	}
}

func TestAnnounceUDPStrayPacket(t *testing.T) {
	std := standardTracker(t)
	url := fakeUDPTracker(t, func(n int, pkt []byte) [][]byte {
		stray := connectReply(txID(pkt)+1, 1) // wrong transaction ID
		return append([][]byte{stray}, std(n, pkt)...)
	})
	resp, err := Announce(context.Background(), url, Request{})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Peers) != 1 {
		t.Errorf("Peers = %v, want 1 peer", resp.Peers)
	}
}

func TestAnnounceUDPFailure(t *testing.T) {
	url := fakeUDPTracker(t, func(_ int, pkt []byte) [][]byte {
		return [][]byte{errorReply(txID(pkt), "torrent not registered")}
	})
	_, err := Announce(context.Background(), url, Request{})
	var fe *FailureError
	if !errors.As(err, &fe) || fe.Reason != "torrent not registered" {
		t.Errorf("err = %v, want *FailureError", err)
	}
}

func TestAnnounceUDPContextDeadline(t *testing.T) {
	url := fakeUDPTracker(t, func(int, []byte) [][]byte { return nil }) // never answers

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := Announce(ctx, url, Request{})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want it to wrap context.DeadlineExceeded", err)
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("Announce took %v, want it to stop at the ctx deadline", d)
	}
}

func TestAnnounceUDPGivesUp(t *testing.T) {
	shortTimeout(t)
	var sends atomic.Int32
	url := fakeUDPTracker(t, func(int, []byte) [][]byte {
		sends.Add(1)
		return nil
	})
	_, err := Announce(context.Background(), url, Request{})
	if err == nil || !strings.Contains(err.Error(), "no response") {
		t.Errorf("err = %v, want a no-response error", err)
	}
	time.Sleep(10 * time.Millisecond) // let the fake tracker count the last packet
	if got := sends.Load(); got != maxRetries {
		t.Errorf("tracker got %d packets, want %d", got, maxRetries)
	}
}

func TestAnnounceUDPNoPort(t *testing.T) {
	if _, err := Announce(context.Background(), "udp://tracker.example/announce", Request{}); err == nil {
		t.Error("err = nil, want an error for a udp:// URL without a port")
	}
}

func FuzzDecodeAnnounce(f *testing.F) {
	f.Add(announceReply(7, 1800, 3, 12, compact("10.0.0.1:6881")), 4)
	f.Add(announceReply(7, 60, 0, 0, compact("[::1]:1")), 16)
	f.Add(errorReply(7, "bad"), 4)
	f.Fuzz(func(t *testing.T, b []byte, addrLen int) {
		if addrLen != 4 && addrLen != 16 {
			return
		}
		resp, err := decodeAnnounce(b, 7, addrLen)
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
