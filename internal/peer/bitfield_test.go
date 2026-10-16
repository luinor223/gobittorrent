package peer

import (
	"bytes"
	"testing"
)

func TestBitfield(t *testing.T) {
	b := NewBitfield(10)
	if len(b) != 2 {
		t.Fatalf("len = %d, want 2", len(b))
	}

	for _, i := range []int{0, 2, 9} {
		b.Set(i)
	}
	if want := (Bitfield{0xa0, 0x40}); !bytes.Equal(b, want) {
		t.Errorf("bytes = %x, want %x", b, want)
	}

	tests := []struct {
		index int
		want  bool
	}{
		{0, true}, {1, false}, {2, true}, {8, false}, {9, true},
		{-1, false}, {16, false}, {100, false},
	}
	for _, tt := range tests {
		if got := b.Has(tt.index); got != tt.want {
			t.Errorf("Has(%d) = %v, want %v", tt.index, got, tt.want)
		}
	}
}

func TestBitfieldSetOutOfRange(t *testing.T) {
	b := NewBitfield(10)
	b.Set(-1)
	b.Set(16)
	b.Set(100)
	if !bytes.Equal(b, Bitfield{0, 0}) {
		t.Errorf("bytes = %x, want 0000", b)
	}
}

func TestNewBitfieldSize(t *testing.T) {
	for numPieces, want := range map[int]int{0: 0, 1: 1, 8: 1, 9: 2, 3024: 378} {
		if got := len(NewBitfield(numPieces)); got != want {
			t.Errorf("NewBitfield(%d) has %d bytes, want %d", numPieces, got, want)
		}
	}
}
