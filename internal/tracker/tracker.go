package tracker

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"net/netip"
	"net/url"
	"time"
)

// peerIDPrefix identifies this client in Azureus style: -GB0001- is gobittorrent 0.0.0.1.
const peerIDPrefix = "-GB0001-"

// Event tells the tracker why an announce is sent.
type Event string

const (
	EventNone      Event = ""          // regular periodic announce
	EventStarted   Event = "started"   // first announce for a download
	EventStopped   Event = "stopped"   // the client is leaving the swarm
	EventCompleted Event = "completed" // the download just finished
)

// Request is what Announce sends to the tracker.
type Request struct {
	InfoHash   [20]byte // torrent to announce
	PeerID     [20]byte // our ID, from NewPeerID
	Port       uint16   // port we accept peer connections on
	Uploaded   int64    // bytes sent so far
	Downloaded int64    // bytes received so far
	Left       int64    // bytes still missing
	Event      Event
}

// Response is a successful tracker reply.
type Response struct {
	Interval    time.Duration    // wait this long before the next announce
	MinInterval time.Duration    // never announce more often than this; 0 if not given
	Seeders     int              // peers with the whole torrent
	Leechers    int              // peers still downloading
	Peers       []netip.AddrPort // peers to connect to, IPv4 and IPv6
}

// FailureError reports that the tracker refused the announce.
type FailureError struct {
	Reason string
}

func (e *FailureError) Error() string {
	return "tracker: " + e.Reason
}

// NewPeerID returns a random peer ID with the client prefix "-GB0001-".
func NewPeerID() [20]byte {
	var id [20]byte
	copy(id[:], peerIDPrefix)
	rand.Read(id[8:])

	return id
}

// Announce sends req to the HTTP/UDP tracker at announceURL and returns its peers.
func Announce(ctx context.Context, announceURL string, req Request) (*Response, error) {
	u, err := url.Parse(announceURL)
	if err != nil {
		return nil, fmt.Errorf("tracker: %w", err)
	}
	switch u.Scheme {
	case "http", "https":
		return announceHTTP(ctx, u, req)
	case "udp":
		if u.Port() == "" {
			return nil, fmt.Errorf("tracker: %s has no port", u)
		}
		return announceUDP(ctx, u.Host, req)
	default:
		return nil, fmt.Errorf("tracker: unsupported scheme %q", u.Scheme)
	}
}

// parseCompactAddr splits a compact peer list (BEP 23, BEP 7) into addresses.
func parseCompactAddr(b []byte, addrLen int) ([]netip.AddrPort, error) {
	size := addrLen + 2
	if len(b)%size != 0 {
		return nil, fmt.Errorf("tracker: compact peers length %d is not a multiple of %d", len(b), size)
	}
	var peers []netip.AddrPort
	for i := 0; i < len(b); i += size {
		addr, _ := netip.AddrFromSlice(b[i : i+addrLen])
		port := binary.BigEndian.Uint16(b[i+addrLen : i+size])
		if port == 0 {
			continue
		}
		peers = append(peers, netip.AddrPortFrom(addr, port))
	}
	return peers, nil
}
