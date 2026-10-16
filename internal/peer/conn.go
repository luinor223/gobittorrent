package peer

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"time"
)

const (
	handshakeTimeout = 10 * time.Second
	readTimeout      = 2 * time.Minute
	writeTimeout     = 30 * time.Second
)

// Conn is a TCP connection to one peer, after a successful handshake.
type Conn struct {
	PeerID [20]byte // the remote peer's ID, from its handshake
	conn   net.Conn
}

// Dial connects to the peer at addr and exchanges handshakes. It returns an
// error if the peer's info hash is not infoHash. The handshake must finish
// within 10 seconds or by the ctx deadline, whichever is earlier.
func Dial(ctx context.Context, addr netip.AddrPort, infoHash, peerID [20]byte) (*Conn, error) {
	dialer := net.Dialer{}
	conn, err := dialer.DialContext(ctx, "tcp", addr.String())
	if err != nil {
		return nil, fmt.Errorf("peer: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, handshakeTimeout)
	defer cancel()

	deadline, _ := ctx.Deadline()
	conn.SetDeadline(deadline)

	if err := WriteHandshake(conn, Handshake{InfoHash: infoHash, PeerID: peerID}); err != nil {
		conn.Close()
		return nil, err
	}

	theirs, err := ReadHandshake(conn)
	if err != nil {
		conn.Close()
		return nil, err
	}

	if theirs.InfoHash != infoHash {
		conn.Close()
		return nil, errors.New("peer: info hash mismatch")
	}

	conn.SetDeadline(time.Time{})
	return &Conn{
		PeerID: theirs.PeerID,
		conn:   conn,
	}, nil
}

// ReadMessage reads the next message. It returns nil, nil for a keep-alive,
// and an error if nothing arrives within 2 minutes.
func (c *Conn) ReadMessage() (*Message, error) {
	c.conn.SetReadDeadline(time.Now().Add(readTimeout))
	return ReadMessage(c.conn)
}

// WriteMessage writes m, or a keep-alive if m is nil.
func (c *Conn) WriteMessage(m *Message) error {
	c.conn.SetWriteDeadline(time.Now().Add(writeTimeout))
	return WriteMessage(c.conn, m)
}

// Close closes the connection.
func (c *Conn) Close() error {
	return c.conn.Close()
}
