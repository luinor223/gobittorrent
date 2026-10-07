package tracker

import (
	"context"
	"net/netip"
	"time"
)

type Event string

const (
	EventNone      Event = ""
	EventStarted   Event = "started"
	EventStopped   Event = "stopped"
	EventCompleted Event = "completed"
)

type Request struct {
	InfoHash   [20]byte
	PeerID     [20]byte
	Port       uint16
	Uploaded   int64
	Downloaded int64
	Left       int64
	Event      Event
}

type Response struct {
	Interval    time.Duration
	MinInterval time.Duration
	Seeders     int
	Leechers    int
	Peers       []netip.AddrPort
}

type FailureError struct {
	Reason string
}

func NewPeerID() [20]byte

func Announce(ctx context.Context, announceURL string, req Request) (*Response, error)
