package peer

import (
	"fmt"
	"io"
)

const pstr = "BitTorrent protocol"

// handshakeLen is the size of a handshake: pstrlen, pstr, then the Handshake fields (68 bytes).
const handshakeLen = 1 + len(pstr) + len(Handshake{}.Reserved) + len(Handshake{}.InfoHash) + len(Handshake{}.PeerID)

// Handshake is the first message on every connection.
type Handshake struct {
	Reserved [8]byte
	InfoHash [20]byte
	PeerID   [20]byte
}

// WriteHandshake writes h to w as one 68-byte message.
func WriteHandshake(w io.Writer, h Handshake) error {
	b := make([]byte, 0, handshakeLen)
	b = append(b, byte(len(pstr)))
	b = append(b, []byte(pstr)...)
	b = append(b, h.Reserved[:]...)
	b = append(b, h.InfoHash[:]...)
	b = append(b, h.PeerID[:]...)

	if _, err := w.Write(b); err != nil {
		return fmt.Errorf("peer: write handshake: %w", err)
	}

	return nil
}

// ReadHandshake reads a handshake from r. It returns an error if the
// protocol string is not "BitTorrent protocol".
func ReadHandshake(r io.Reader) (Handshake, error) {
	var lenBuf [1]byte
	if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
		return Handshake{}, fmt.Errorf("peer: read handshake: %w", err)
	}
	if lenBuf[0] != byte(len(pstr)) {
		return Handshake{}, fmt.Errorf("peer: bad protocol string length %d", lenBuf[0])
	}

	var b [handshakeLen - 1]byte // everything after pstrlen
	if _, err := io.ReadFull(r, b[:]); err != nil {
		return Handshake{}, fmt.Errorf("peer: read handshake: %w", err)
	}
	if string(b[:len(pstr)]) != pstr {
		return Handshake{}, fmt.Errorf("peer: bad protocol string %q", b[:len(pstr)])
	}

	var h Handshake
	rest := b[len(pstr):]
	n := copy(h.Reserved[:], rest)
	n += copy(h.InfoHash[:], rest[n:])
	copy(h.PeerID[:], rest[n:])
	return h, nil
}
