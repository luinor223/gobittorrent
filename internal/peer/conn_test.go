package peer

import (
	"context"
	"net"
	"net/netip"
	"testing"
	"time"
)

// fakePeer listens on localhost and runs serve on the first connection.
func fakePeer(t *testing.T, serve func(net.Conn)) netip.AddrPort {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		serve(c)
	}()
	return netip.MustParseAddrPort(ln.Addr().String())
}

// replyWith reads the client's handshake and answers with h.
func replyWith(h Handshake) func(net.Conn) {
	return func(c net.Conn) {
		if _, err := ReadHandshake(c); err != nil {
			return
		}
		WriteHandshake(c, h)
		// Echo one message back, then hold the connection until the client closes.
		if m, err := ReadMessage(c); err == nil {
			WriteMessage(c, m)
		}
		c.Read(make([]byte, 1))
	}
}

func TestDial(t *testing.T) {
	theirs := testHandshake()
	addr := fakePeer(t, replyWith(theirs))

	var ourID [20]byte
	copy(ourID[:], "-GB0001-ourpeerid000")
	c, err := Dial(context.Background(), addr, theirs.InfoHash, ourID)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	if c.PeerID != theirs.PeerID {
		t.Errorf("PeerID = %q, want %q", c.PeerID, theirs.PeerID)
	}

	if err := c.WriteMessage(NewHave(7)); err != nil {
		t.Fatal(err)
	}
	m, err := c.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	if index, err := ParseHave(m); err != nil || index != 7 {
		t.Errorf("echo = %d, %v; want 7, nil", index, err)
	}
}

func TestDialWrongInfoHash(t *testing.T) {
	theirs := testHandshake()
	addr := fakePeer(t, replyWith(theirs))

	var other [20]byte
	copy(other[:], "some-other-torrent!!")
	if c, err := Dial(context.Background(), addr, other, [20]byte{}); err == nil {
		c.Close()
		t.Fatal("want error, got nil")
	}
}

func TestDialSilentPeer(t *testing.T) {
	// The peer accepts the TCP connection but never sends a handshake.
	addr := fakePeer(t, func(c net.Conn) { c.Read(make([]byte, 1024)) })

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	if c, err := Dial(ctx, addr, [20]byte{}, [20]byte{}); err == nil {
		c.Close()
		t.Fatal("want error, got nil")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("Dial took %v; the ctx deadline was not applied to the handshake", elapsed)
	}
}

func TestDialRefused(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := netip.MustParseAddrPort(ln.Addr().String())
	ln.Close() // nothing listens on addr now

	if c, err := Dial(context.Background(), addr, [20]byte{}, [20]byte{}); err == nil {
		c.Close()
		t.Fatal("want error, got nil")
	}
}
