package aiiosdk

// .
// .
// .
// .
// .
// .
// .
// .

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const (
	// .
	// .
	// .
	FrameHeaderBytes = 4

	// .
	// .
	// .
	// .
	MaxControlFrameBytes = 1 << 20

	// .
	// .
	MaxServerFrameBytes = 16 << 20
)

var (
	// .
	// .
	// .
	// .
	ErrFrameTooLarge = errors.New("aiiosdk: frame exceeds limit")

	// .
	// .
	// .
	// .
	ErrEmptyPayload = errors.New("aiiosdk: refusing to write empty frame")
)

// .
// .
func WriteFrame(w io.Writer, payload []byte, maxFrameBytes int) error {
	if maxFrameBytes <= 0 {
		return fmt.Errorf("aiiosdk: max frame bytes must be positive, got %d", maxFrameBytes)
	}
	if len(payload) == 0 {
		return ErrEmptyPayload
	}
	if len(payload) > maxFrameBytes {
		return ErrFrameTooLarge
	}
	var header [FrameHeaderBytes]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(payload)))
	if _, err := w.Write(header[:]); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
}

// .
// .
// .
// .
// .
// .
// .
// .
// .
func ReadFrame(r io.Reader, maxFrameBytes int) ([]byte, error) {
	if maxFrameBytes <= 0 {
		return nil, fmt.Errorf("aiiosdk: max frame bytes must be positive, got %d", maxFrameBytes)
	}
	var header [FrameHeaderBytes]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		// .
		// .
		return nil, err
	}
	// .
	// .
	declared := uint64(binary.BigEndian.Uint32(header[:]))
	if declared > uint64(maxFrameBytes) {
		return nil, ErrFrameTooLarge
	}
	payload := make([]byte, int(declared))
	if _, err := io.ReadFull(r, payload); err != nil {
		if errors.Is(err, io.EOF) {
			// .
			return nil, io.ErrUnexpectedEOF
		}
		return nil, err
	}
	return payload, nil
}
