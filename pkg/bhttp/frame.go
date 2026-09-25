package bhttp

import (
	"errors"
	"fmt"
	"io"
)

// Frame Type constants
const (
	FrameHeaders uint8 = 0x01
	FrameData    uint8 = 0x02
	FrameGoAway  uint8 = 0x03
	FramePing    uint8 = 0x04
)

// Flag constants
const (
	FlagEndStream  uint8 = 0x01 // Valid on HEADERS and DATA
	FlagEndHeaders uint8 = 0x04 // Valid on HEADERS
	FlagAck        uint8 = 0x01 // Valid on PING
)

// Header size in bytes
const FrameHeaderSize = 8

// Maximum frame payload size (24-bit integer = 16 MiB - 1)
const MaxPayloadSize = (1 << 24) - 1

var (
	ErrPayloadTooLarge  = errors.New("frame payload exceeds maximum 24-bit size (16 MiB)")
	ErrStreamIDTooLarge = errors.New("stream ID exceeds maximum 24-bit size")
	ErrTruncatedHeader  = errors.New("frame header truncated")
	ErrTruncatedPayload = errors.New("frame payload truncated")
)

// FrameHeader represents the 8-byte fixed-size BHTTP frame header.
// Layout:
// - Length:    24 bits (Payload length in bytes, 0..16,777,215)
// - Type:       8 bits (Frame Type, 0..255)
// - Flags:      8 bits (Type-specific flags)
// - StreamID:  24 bits (Stream identifier, 0..16,777,215)
type FrameHeader struct {
	Length   uint32
	Type     uint8
	Flags    uint8
	StreamID uint32
}

// Frame represents a complete BHTTP frame.
type Frame struct {
	Header  FrameHeader
	Payload []byte
}

// HasFlag checks if a specific flag bit is set.
func (f *Frame) HasFlag(flag uint8) bool {
	return (f.Header.Flags & flag) != 0
}

// TypeName returns a human-readable name for known frame types.
func (h *FrameHeader) TypeName() string {
	switch h.Type {
	case FrameHeaders:
		return "HEADERS"
	case FrameData:
		return "DATA"
	case FrameGoAway:
		return "GOAWAY"
	case FramePing:
		return "PING"
	default:
		return fmt.Sprintf("UNKNOWN(0x%02X)", h.Type)
	}
}

// FlagsString returns a human-readable list of active flags.
func (h *FrameHeader) FlagsString() string {
	if h.Flags == 0 {
		return "NONE"
	}
	var flags []string
	switch h.Type {
	case FrameHeaders:
		if (h.Flags & FlagEndStream) != 0 {
			flags = append(flags, "END_STREAM")
		}
		if (h.Flags & FlagEndHeaders) != 0 {
			flags = append(flags, "END_HEADERS")
		}
	case FrameData:
		if (h.Flags & FlagEndStream) != 0 {
			flags = append(flags, "END_STREAM")
		}
	case FramePing:
		if (h.Flags & FlagAck) != 0 {
			flags = append(flags, "ACK")
		}
	}
	if len(flags) == 0 {
		return fmt.Sprintf("0x%02X", h.Flags)
	}
	res := ""
	for i, f := range flags {
		if i > 0 {
			res += "|"
		}
		res += f
	}
	return res
}

// EncodeHeader serializes the 8-byte frame header into big-endian wire format.
func (h *FrameHeader) EncodeHeader() [FrameHeaderSize]byte {
	var buf [FrameHeaderSize]byte
	// 24-bit Length
	buf[0] = byte(h.Length >> 16)
	buf[1] = byte(h.Length >> 8)
	buf[2] = byte(h.Length)
	// 8-bit Type
	buf[3] = h.Type
	// 8-bit Flags
	buf[4] = h.Flags
	// 24-bit Stream ID
	buf[5] = byte(h.StreamID >> 16)
	buf[6] = byte(h.StreamID >> 8)
	buf[7] = byte(h.StreamID)
	return buf
}

// DecodeHeader deserializes an 8-byte slice into a FrameHeader.
func DecodeHeader(buf []byte) (FrameHeader, error) {
	if len(buf) < FrameHeaderSize {
		return FrameHeader{}, ErrTruncatedHeader
	}
	length := uint32(buf[0])<<16 | uint32(buf[1])<<8 | uint32(buf[2])
	frameType := buf[3]
	flags := buf[4]
	streamID := uint32(buf[5])<<16 | uint32(buf[6])<<8 | uint32(buf[7])

	return FrameHeader{
		Length:   length,
		Type:     frameType,
		Flags:    flags,
		StreamID: streamID,
	}, nil
}

// WriteFrame writes a complete frame (header + payload) to the writer.
func WriteFrame(w io.Writer, f *Frame) error {
	if len(f.Payload) > MaxPayloadSize {
		return ErrPayloadTooLarge
	}
	if f.Header.StreamID > MaxPayloadSize {
		return ErrStreamIDTooLarge
	}

	f.Header.Length = uint32(len(f.Payload))
	hdrBytes := f.Header.EncodeHeader()

	if _, err := w.Write(hdrBytes[:]); err != nil {
		return err
	}
	if len(f.Payload) > 0 {
		if _, err := w.Write(f.Payload); err != nil {
			return err
		}
	}
	return nil
}

// ReadFrame reads a single frame from the reader.
// In accordance with the BHTTP/1.0 specification:
// Any frame with an unknown Type is read in its entirety and returned,
// allowing the receiver to cleanly skip it without connection termination.
func ReadFrame(r io.Reader) (*Frame, error) {
	var hdrBuf [FrameHeaderSize]byte
	if _, err := io.ReadFull(r, hdrBuf[:]); err != nil {
		return nil, err
	}

	hdr, err := DecodeHeader(hdrBuf[:])
	if err != nil {
		return nil, err
	}

	payload := make([]byte, hdr.Length)
	if hdr.Length > 0 {
		if _, err := io.ReadFull(r, payload); err != nil {
			if errors.Is(err, io.EOF) {
				return nil, ErrTruncatedPayload
			}
			return nil, err
		}
	}

	return &Frame{
		Header:  hdr,
		Payload: payload,
	}, nil
}

// IsKnownType checks whether a frame type is defined in BHTTP/1.0.
func IsKnownType(frameType uint8) bool {
	switch frameType {
	case FrameHeaders, FrameData, FrameGoAway, FramePing:
		return true
	default:
		return false
	}
}

// SerializeFrame returns the exact byte representation of the frame on the wire.
func SerializeFrame(f *Frame) ([]byte, error) {
	f.Header.Length = uint32(len(f.Payload))
	hdrBytes := f.Header.EncodeHeader()
	wire := make([]byte, FrameHeaderSize+len(f.Payload))
	copy(wire[:FrameHeaderSize], hdrBytes[:])
	if len(f.Payload) > 0 {
		copy(wire[FrameHeaderSize:], f.Payload)
	}
	return wire, nil
}
