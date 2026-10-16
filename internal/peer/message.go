package peer

import (
	"encoding/binary"
	"fmt"
	"io"
)

// maxMessageLen is the largest message ReadMessage accepts (1 MiB).
const maxMessageLen = 1 << 20

// MessageID is the type of a message: the byte after the length prefix.
type MessageID uint8

const (
	MsgChoke         MessageID = 0
	MsgUnchoke       MessageID = 1
	MsgInterested    MessageID = 2
	MsgNotInterested MessageID = 3
	MsgHave          MessageID = 4
	MsgBitfield      MessageID = 5
	MsgRequest       MessageID = 6
	MsgPiece         MessageID = 7
	MsgCancel        MessageID = 8
)

// Message is one framed message. A nil *Message is a keep-alive.
type Message struct {
	ID      MessageID
	Payload []byte
}

// WriteMessage writes m to w as one frame: length, ID, payload. A nil m
// writes a keep-alive.
func WriteMessage(w io.Writer, m *Message) error {
	if m == nil {
		if _, err := w.Write([]byte{0, 0, 0, 0}); err != nil {
			return fmt.Errorf("peer: write message: %w", err)
		}
		return nil
	}
	b := make([]byte, 0, 5+len(m.Payload))
	length := uint32(1 + len(m.Payload))
	b = binary.BigEndian.AppendUint32(b, length)
	b = append(b, byte(m.ID))
	b = append(b, m.Payload...)

	if _, err := w.Write(b); err != nil {
		return fmt.Errorf("peer: write message: %w", err)
	}

	return nil
}

// ReadMessage reads one frame from r. It returns nil, nil for a keep-alive,
// and an error for a length above 1 MiB. Unknown IDs are not an error.
func ReadMessage(r io.Reader) (*Message, error) {
	var lenBuf [4]byte
	if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
		return nil, fmt.Errorf("peer: read message: %w", err)
	}

	msgLen := binary.BigEndian.Uint32(lenBuf[:])
	if msgLen > maxMessageLen {
		return nil, fmt.Errorf("peer: message length %d exceeds %d", msgLen, maxMessageLen)
	}
	if msgLen == 0 {
		return nil, nil
	}

	msgBuf := make([]byte, msgLen)
	if _, err := io.ReadFull(r, msgBuf); err != nil {
		return nil, fmt.Errorf("peer: read message: %w", err)
	}

	m := &Message{ID: MessageID(msgBuf[0])}
	if msgLen > 1 {
		m.Payload = msgBuf[1:]
	}
	return m, nil
}

// NewHave returns a have message for piece index.
func NewHave(index uint32) *Message {
	return &Message{
		ID:      MsgHave,
		Payload: binary.BigEndian.AppendUint32(nil, index),
	}
}

// NewRequest returns a request for length bytes of piece index, starting at
// offset begin.
func NewRequest(index, begin, length uint32) *Message {
	payload := make([]byte, 0, 12)
	payload = binary.BigEndian.AppendUint32(payload, index)
	payload = binary.BigEndian.AppendUint32(payload, begin)
	payload = binary.BigEndian.AppendUint32(payload, length)
	return &Message{
		ID:      MsgRequest,
		Payload: payload,
	}
}

// ParseHave returns the piece index of a have message.
func ParseHave(m *Message) (index uint32, err error) {
	if m.ID != MsgHave {
		err = fmt.Errorf("peer: message ID %d, want %d", m.ID, MsgHave)
		return
	}
	if len(m.Payload) != 4 {
		err = fmt.Errorf("peer: have payload length %d, want 4", len(m.Payload))
		return
	}
	index = binary.BigEndian.Uint32(m.Payload)
	return
}

// ParsePiece splits a piece message into its piece index, its offset in the
// piece and its data. block shares memory with m.Payload.
func ParsePiece(m *Message) (index, begin uint32, block []byte, err error) {
	if m.ID != MsgPiece {
		err = fmt.Errorf("peer: message ID %d, want %d", m.ID, MsgPiece)
		return
	}
	if len(m.Payload) < 8 {
		err = fmt.Errorf("peer: piece payload length %d, want >= 8", len(m.Payload))
		return
	}

	index = binary.BigEndian.Uint32(m.Payload[0:4])
	begin = binary.BigEndian.Uint32(m.Payload[4:8])
	block = m.Payload[8:]
	return
}
