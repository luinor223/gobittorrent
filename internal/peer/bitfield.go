package peer

// Bitfield records which pieces a peer has. Bit 0 is the high bit of byte 0.
type Bitfield []byte

// highBit is the mask for bit 0 of a byte; the protocol counts from the high bit.
const highBit = 0b1000_0000

// NewBitfield returns an empty bitfield with room for numPieces pieces.
func NewBitfield(numPieces int) Bitfield {
	return make(Bitfield, (numPieces+7)/8)
}

// Has reports whether piece index is set. It returns false for an index out of range.
func (b Bitfield) Has(index int) bool {
	if index < 0 || index/8 >= len(b) {
		return false
	}
	mask := byte(highBit >> (index % 8))

	return b[index/8]&mask != 0
}

// Set marks piece index as present. It does nothing for an index out of range.
func (b Bitfield) Set(index int) {
	if index < 0 || index/8 >= len(b) {
		return
	}

	mask := byte(highBit >> (index % 8))
	b[index/8] |= mask
}
