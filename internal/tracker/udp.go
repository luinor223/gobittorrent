package tracker

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"os"
	"time"
)

// protocolID is the magic constant that starts every connect request (BEP 15).
const protocolID uint64 = 0x41727101980

// maxRetries is how many times roundTrip sends a packet before it gives up.
const maxRetries = 4

// udpTimeout is the first reply timeout; it doubles on each retry. A var so tests can shorten it.
var udpTimeout = 15 * time.Second

const (
	actionConnect  uint32 = 0
	actionAnnounce uint32 = 1
	actionError    uint32 = 3
)

// errTxMismatch marks a reply to some other request; roundTrip ignores it.
var errTxMismatch = errors.New("tracker: transaction ID mismatch")

// announceUDP announces to the UDP tracker at host ("name:port"): connect, then announce.
func announceUDP(ctx context.Context, host string, req Request) (*Response, error) {
	dialer := net.Dialer{}
	conn, err := dialer.DialContext(ctx, "udp", host)
	if err != nil {
		return nil, fmt.Errorf("tracker: %w", err)
	}
	defer conn.Close()

	connectTxID := rand.Uint32()
	var connID uint64
	err = roundTrip(ctx, conn, encodeConnect(connectTxID), func(b []byte) error {
		var err error
		connID, err = decodeConnect(b, connectTxID)
		return err
	})
	if err != nil {
		return nil, err
	}

	announceTxID := rand.Uint32()
	key := rand.Uint32()
	addrLen := 4
	if ua, ok := conn.RemoteAddr().(*net.UDPAddr); ok && !ua.AddrPort().Addr().Unmap().Is4() {
		addrLen = 16
	}
	var resp *Response
	err = roundTrip(ctx, conn, encodeAnnounce(connID, announceTxID, req, key), func(b []byte) error {
		var err error
		resp, err = decodeAnnounce(b, announceTxID, addrLen)
		return err
	})
	if err != nil {
		return nil, err
	}

	return resp, nil
}

// roundTrip sends packet and waits for a reply that decode accepts, resending
// with a doubling timeout (BEP 15).
func roundTrip(ctx context.Context, conn net.Conn, packet []byte, decode func([]byte) error) error {
	stop := context.AfterFunc(ctx, func() { conn.SetReadDeadline(time.Now()) })
	defer stop()

	buf := make([]byte, 64<<10)
	for retry := range maxRetries {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("tracker: %w", err)
		}
		if _, err := conn.Write(packet); err != nil {
			return fmt.Errorf("tracker: %w", err)
		}

		deadline := time.Now().Add(udpTimeout << retry)
		if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
			deadline = d
		}
		conn.SetReadDeadline(deadline)

		// Keep reading until a matching reply or the deadline.
		for {
			n, err := conn.Read(buf)
			if errors.Is(err, os.ErrDeadlineExceeded) {
				break
			}
			if err != nil {
				return fmt.Errorf("tracker: %w", err)
			}

			err = decode(buf[:n])
			if errors.Is(err, errTxMismatch) {
				continue
			}
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("tracker: %w", err)
	}
	return fmt.Errorf("tracker: no response after %d attempts", maxRetries)
}

// encodeConnect builds the 16-byte connect request.
func encodeConnect(txID uint32) []byte {
	b := make([]byte, 16)
	binary.BigEndian.PutUint64(b[:8], protocolID)
	binary.BigEndian.PutUint32(b[8:12], actionConnect)
	binary.BigEndian.PutUint32(b[12:16], txID)

	return b
}

// decodeConnect parses a connect reply and returns its connection ID.
func decodeConnect(b []byte, txID uint32) (connID uint64, err error) {
	if len(b) < 8 {
		return 0, errors.New("tracker: packet too short")
	}
	respAction := binary.BigEndian.Uint32(b[:4])
	respTxID := binary.BigEndian.Uint32(b[4:8])

	if respTxID != txID {
		return 0, errTxMismatch
	}
	if respAction == actionError {
		return 0, &FailureError{Reason: string(b[8:])}
	}
	if respAction != actionConnect {
		return 0, fmt.Errorf("tracker: unexpected action %d", respAction)
	}

	if len(b) < 16 {
		return 0, errors.New("tracker: packet too short")
	}
	respConnID := binary.BigEndian.Uint64(b[8:16])
	return respConnID, nil
}

// encodeAnnounce builds the 98-byte announce request.
func encodeAnnounce(connID uint64, txID uint32, req Request, key uint32) []byte {
	b := make([]byte, 98)
	binary.BigEndian.PutUint64(b[:8], connID)
	binary.BigEndian.PutUint32(b[8:12], actionAnnounce)
	binary.BigEndian.PutUint32(b[12:16], txID)
	copy(b[16:36], req.InfoHash[:])
	copy(b[36:56], req.PeerID[:])
	binary.BigEndian.PutUint64(b[56:64], uint64(req.Downloaded))
	binary.BigEndian.PutUint64(b[64:72], uint64(req.Left))
	binary.BigEndian.PutUint64(b[72:80], uint64(req.Uploaded))
	binary.BigEndian.PutUint32(b[80:84], udpEvent(req.Event))
	binary.BigEndian.PutUint32(b[84:88], 0) // IP address (use the sender's)
	binary.BigEndian.PutUint32(b[88:92], key)
	binary.BigEndian.PutUint32(b[92:96], 0xFFFFFFFF) // num_want (default)
	binary.BigEndian.PutUint16(b[96:98], req.Port)

	return b
}

// decodeAnnounce parses an announce reply. addrLen is 4 when the tracker was
// reached over IPv4 and 16 over IPv6; it sets the size of each compact peer.
func decodeAnnounce(b []byte, txID uint32, addrLen int) (*Response, error) {
	if len(b) < 8 {
		return nil, errors.New("tracker: packet too short")
	}
	respAction := binary.BigEndian.Uint32(b[:4])
	respTxID := binary.BigEndian.Uint32(b[4:8])

	if respTxID != txID {
		return nil, errTxMismatch
	}
	if respAction == actionError {
		return nil, &FailureError{Reason: string(b[8:])}
	}
	if respAction != actionAnnounce {
		return nil, fmt.Errorf("tracker: unexpected action %d", respAction)
	}

	if len(b) < 20 {
		return nil, errors.New("tracker: packet too short")
	}
	interval := int32(binary.BigEndian.Uint32(b[8:12]))
	leechers := int32(binary.BigEndian.Uint32(b[12:16]))
	seeders := int32(binary.BigEndian.Uint32(b[16:20]))
	if interval <= 0 {
		return nil, errors.New("tracker: invalid interval")
	}

	peers, err := parseCompactAddr(b[20:], addrLen)
	if err != nil {
		return nil, err
	}

	return &Response{
		Interval: time.Duration(interval) * time.Second,
		Seeders:  int(seeders),
		Leechers: int(leechers),
		Peers:    peers,
	}, nil
}

// udpEvent maps an Event to its BEP 15 number, which differs from the HTTP order.
func udpEvent(e Event) uint32 {
	switch e {
	case EventCompleted:
		return 1
	case EventStarted:
		return 2
	case EventStopped:
		return 3
	}
	return 0
}
