package tracker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"time"

	"github.com/luinor223/gobittorrent/internal/bencode"
)

// maxBody caps how much of a tracker reply is read
const maxBody = 1 << 20

// rawResponse is the bencoded tracker reply.
type rawResponse struct {
	FailureReason string             `bencode:"failure reason"`
	Interval      int64              `bencode:"interval"`
	MinInterval   int64              `bencode:"min interval"`
	Complete      int64              `bencode:"complete"`
	Incomplete    int64              `bencode:"incomplete"`
	Peers         bencode.RawMessage `bencode:"peers"`
	Peers6        []byte             `bencode:"peers6"`
}

// announceHTTP sends req to the HTTP tracker at announceURL and returns its peers.
func announceHTTP(ctx context.Context, announceURL *url.URL, req Request) (*Response, error) {
	reqURL, err := buildURL(announceURL, req)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("tracker: %w", err)
	}
	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("tracker: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tracker: HTTP %s", resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return nil, fmt.Errorf("tracker: %w", err)
	}
	if len(body) > maxBody {
		return nil, errors.New("tracker: response too large")
	}

	return parseResponse(body)
}

// buildURL adds the announce parameters to announceURL, keeping its existing query.
func buildURL(announceURL *url.URL, req Request) (string, error) {
	q := announceURL.Query()
	q.Set("info_hash", string(req.InfoHash[:]))
	q.Set("peer_id", string(req.PeerID[:]))
	q.Set("port", strconv.Itoa(int(req.Port)))
	q.Set("uploaded", strconv.FormatInt(req.Uploaded, 10))
	q.Set("downloaded", strconv.FormatInt(req.Downloaded, 10))
	q.Set("left", strconv.FormatInt(req.Left, 10))
	q.Set("compact", "1")
	if req.Event != EventNone {
		q.Set("event", string(req.Event))
	}
	announceURL.RawQuery = q.Encode()

	return announceURL.String(), nil
}

// parseResponse decodes a tracker reply, accepting compact and dictionary peer lists.
func parseResponse(body []byte) (*Response, error) {
	var rr rawResponse
	if err := bencode.Unmarshal(body, &rr); err != nil {
		return nil, fmt.Errorf("tracker: %w", err)
	}

	if rr.FailureReason != "" {
		return nil, &FailureError{Reason: rr.FailureReason}
	}

	if rr.Interval <= 0 {
		return nil, errors.New("tracker: invalid interval")
	}

	var peers []netip.AddrPort
	if len(rr.Peers) > 0 {
		switch rr.Peers[0] {
		case 'l':
			var rawPeers []struct {
				IP   string `bencode:"ip"`
				Port uint16 `bencode:"port"`
			}
			if err := bencode.Unmarshal(rr.Peers, &rawPeers); err != nil {
				return nil, fmt.Errorf("tracker: %w", err)
			}

			for _, peer := range rawPeers {
				addr, err := netip.ParseAddr(peer.IP)
				if err != nil || peer.Port == 0 {
					continue
				}
				peers = append(peers, netip.AddrPortFrom(addr, peer.Port))
			}
		case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
			var b []byte
			if err := bencode.Unmarshal(rr.Peers, &b); err != nil {
				return nil, fmt.Errorf("tracker: %w", err)
			}
			var err error
			if peers, err = parseCompactAddr(b, 4); err != nil {
				return nil, err
			}
		default:
			return nil, errors.New("tracker: malformed peers")
		}
	}

	peers6, err := parseCompactAddr(rr.Peers6, 16)
	if err != nil {
		return nil, err
	}
	peers = append(peers, peers6...)

	return &Response{
		Interval:    time.Duration(rr.Interval) * time.Second,
		MinInterval: time.Duration(rr.MinInterval) * time.Second,
		Seeders:     int(rr.Complete),
		Leechers:    int(rr.Incomplete),
		Peers:       peers,
	}, nil
}
