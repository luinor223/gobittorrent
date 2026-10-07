package metainfo

import (
	"crypto/sha1"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/luinor223/gobittorrent/internal/bencode"
)

var ErrInvalid = errors.New("metainfo: invalid torrent")

type Torrent struct {
	Announce     string
	AnnounceList [][]string
	InfoHash     [20]byte
	Info         Info
}

type Info struct {
	Name        string
	PieceLength int64
	Pieces      [][20]byte
	Length      int64
	Files       []File
	Private     bool
}

type File struct {
	Length int64
	Path   []string
}

type rawTorrent struct {
	Announce     string             `bencode:"announce"`
	AnnounceList [][]string         `bencode:"announce-list"`
	Info         bencode.RawMessage `bencode:"info"`
}

type rawInfo struct {
	Name        string    `bencode:"name"`
	PieceLength int64     `bencode:"piece length"`
	Pieces      string    `bencode:"pieces"`
	Length      int64     `bencode:"length"`
	Files       []rawFile `bencode:"files"`
	Private     int       `bencode:"private"`
}

type rawFile struct {
	Length int64    `bencode:"length"`
	Path   []string `bencode:"path"`
}

func Parse(data []byte) (*Torrent, error) {
	var rt rawTorrent
	if err := bencode.Unmarshal(data, &rt); err != nil {
		return nil, fmt.Errorf("metainfo: %w", err)
	}
	if len(rt.Info) == 0 {
		return nil, fmt.Errorf("%w: missing info dict", ErrInvalid)
	}

	var ri rawInfo
	if err := bencode.Unmarshal(rt.Info, &ri); err != nil {
		return nil, fmt.Errorf("metainfo: %w", err)
	}
	if ri.Name == "" {
		return nil, fmt.Errorf("%w: empty name", ErrInvalid)
	}
	if ri.Name == "." || ri.Name == ".." || strings.Contains(ri.Name, "/") {
		return nil, fmt.Errorf("%w: unsafe name %q", ErrInvalid, ri.Name)
	}
	if ri.PieceLength <= 0 {
		return nil, fmt.Errorf("%w: piece length %d is not positive", ErrInvalid, ri.PieceLength)
	}
	if ri.Length < 0 {
		return nil, fmt.Errorf("%w: length %d is negative", ErrInvalid, ri.PieceLength)
	}
	if len(ri.Pieces) == 0 || len(ri.Pieces)%20 != 0 {
		return nil, fmt.Errorf("%w: pieces length %d is not a positive multiple of 20", ErrInvalid, len(ri.Pieces))
	}
	hasLength := ri.Length > 0
	hasFiles := len(ri.Files) > 0
	if hasLength == hasFiles {
		return nil, fmt.Errorf("%w: need exactly one of length or files", ErrInvalid)
	}
	var files []File
	total := ri.Length
	for i, file := range ri.Files {
		if file.Length < 0 {
			return nil, fmt.Errorf("%w: file %d has negative length %d", ErrInvalid, i, file.Length)
		}
		if file.Length > math.MaxInt64-total {
			return nil, fmt.Errorf("%w: total length overflows", ErrInvalid)
		}
		total += file.Length
		if len(file.Path) == 0 {
			return nil, fmt.Errorf("%w: file %d has empty path", ErrInvalid, i)
		}
		for _, step := range file.Path {
			if step == "" || step == "." || step == ".." || strings.Contains(step, "/") {
				return nil, fmt.Errorf("%w: file %d has unsafe path %q", ErrInvalid, i, file.Path)
			}
		}
		files = append(files, File{Length: file.Length, Path: file.Path})
	}

	pieces := make([][20]byte, len(ri.Pieces)/20)
	for i := 0; i < len(ri.Pieces); i += 20 {
		copy(pieces[i/20][:], ri.Pieces[i:i+20])
	}

	info := Info{
		Name:        ri.Name,
		PieceLength: ri.PieceLength,
		Length:      ri.Length,
		Private:     ri.Private == 1,
		Pieces:      pieces,
		Files:       files,
	}

	want := (total + info.PieceLength - 1) / info.PieceLength
	if int64(len(info.Pieces)) != want {
		return nil, fmt.Errorf("%w: got %d piece hashes, want %d", ErrInvalid, len(info.Pieces), want)
	}

	return &Torrent{
		Announce:     rt.Announce,
		AnnounceList: rt.AnnounceList,
		InfoHash:     sha1.Sum(rt.Info),
		Info:         info,
	}, nil

}

func (i *Info) TotalLength() int64 {
	total := i.Length
	for _, file := range i.Files {
		total += file.Length
	}
	return total
}
func (i *Info) PieceSize(index int) int64 {
	switch {
	case index < 0 || index >= len(i.Pieces):
		return 0
	case index == len(i.Pieces)-1:
		return i.TotalLength() - int64(index)*i.PieceLength
	}
	return i.PieceLength
}
