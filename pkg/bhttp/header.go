package bhttp

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
)

var (
	ErrMalformedHeader = errors.New("malformed header block: unexpected end of payload")
	ErrHeaderTooLarge  = errors.New("header field exceeds maximum 16-bit length (65535 bytes)")
	ErrInvalidIndex    = errors.New("invalid static table index")
)

// Static Table of 10 high-frequency header names (HPACK-lite Mechanism 1)
var StaticTable = [...]string{
	"",                // 0 is reserved for literal names
	":method",         // Index 1
	":path",           // Index 2
	":status",         // Index 3
	"content-type",    // Index 4
	"content-length",  // Index 5
	"host",            // Index 6
	"user-agent",      // Index 7
	"server",          // Index 8
	"date",            // Index 9
	"connection",      // Index 10
}

var staticNameToIndex map[string]uint8

func init() {
	staticNameToIndex = make(map[string]uint8, len(StaticTable))
	for idx := 1; idx < len(StaticTable); idx++ {
		staticNameToIndex[StaticTable[idx]] = uint8(idx)
	}
}

// HeaderField represents a single HTTP key-value header pair.
type HeaderField struct {
	Name  string
	Value string
}

// HeaderBlock is a collection of HeaderFields.
type HeaderBlock []HeaderField

// Get retrieves the first value associated with the given key (case-insensitive).
func (hb HeaderBlock) Get(name string) string {
	lower := strings.ToLower(name)
	for _, h := range hb {
		if strings.ToLower(h.Name) == lower {
			return h.Value
		}
	}
	return ""
}

// Set sets the first occurrence of key, or appends it.
func (hb *HeaderBlock) Set(name, value string) {
	lower := strings.ToLower(name)
	for i, h := range *hb {
		if strings.ToLower(h.Name) == lower {
			(*hb)[i].Value = value
			return
		}
	}
	*hb = append(*hb, HeaderField{Name: name, Value: value})
}

// Method returns the :method pseudo-header.
func (hb HeaderBlock) Method() string {
	return hb.Get(":method")
}

// Path returns the :path pseudo-header.
func (hb HeaderBlock) Path() string {
	return hb.Get(":path")
}

// Status returns the :status pseudo-header.
func (hb HeaderBlock) Status() string {
	return hb.Get(":status")
}

// EncodeHeaderBlock serializes a HeaderBlock using HPACK-lite rules:
// - Indexed names (1..10) use 1-byte index followed by 2-byte value length and value bytes.
// - Literal names use 0x00 followed by 2-byte name length, name bytes, 2-byte value length, and value bytes.
func EncodeHeaderBlock(hb HeaderBlock) ([]byte, error) {
	var buf []byte

	for _, h := range hb {
		nameLower := strings.ToLower(strings.TrimSpace(h.Name))
		val := h.Value

		if len(val) > 65535 {
			return nil, ErrHeaderTooLarge
		}

		if idx, ok := staticNameToIndex[nameLower]; ok {
			// Format A: Indexed Name
			buf = append(buf, idx)
			var lenBuf [2]byte
			binary.BigEndian.PutUint16(lenBuf[:], uint16(len(val)))
			buf = append(buf, lenBuf[:]...)
			buf = append(buf, val...)
		} else {
			// Format B: Literal Name
			if len(nameLower) > 65535 {
				return nil, ErrHeaderTooLarge
			}
			buf = append(buf, 0x00) // Literal marker
			var lenBuf [2]byte
			binary.BigEndian.PutUint16(lenBuf[:], uint16(len(nameLower)))
			buf = append(buf, lenBuf[:]...)
			buf = append(buf, nameLower...)

			binary.BigEndian.PutUint16(lenBuf[:], uint16(len(val)))
			buf = append(buf, lenBuf[:]...)
			buf = append(buf, val...)
		}
	}

	return buf, nil
}

// DecodedEntry contains detailed metadata about a decoded header field for annotation/diagnostics.
type DecodedEntry struct {
	Offset    int
	Index     uint8
	IsIndexed bool
	Name      string
	Value     string
	WireBytes []byte
}

// DecodeHeaderBlock deserializes a raw payload into a HeaderBlock.
func DecodeHeaderBlock(payload []byte) (HeaderBlock, error) {
	entries, err := DecodeHeaderBlockDetailed(payload)
	if err != nil {
		return nil, err
	}
	hb := make(HeaderBlock, len(entries))
	for i, e := range entries {
		hb[i] = HeaderField{Name: e.Name, Value: e.Value}
	}
	return hb, nil
}

// DecodeHeaderBlockDetailed deserializes payload into detailed entries for hexdump annotations.
func DecodeHeaderBlockDetailed(payload []byte) ([]DecodedEntry, error) {
	var entries []DecodedEntry
	pos := 0
	total := len(payload)

	for pos < total {
		entryStart := pos
		index := payload[pos]
		pos++

		if index >= 1 && int(index) < len(StaticTable) {
			// Format A: Indexed Name
			if pos+2 > total {
				return nil, ErrMalformedHeader
			}
			valLen := int(binary.BigEndian.Uint16(payload[pos : pos+2]))
			pos += 2

			if pos+valLen > total {
				return nil, ErrMalformedHeader
			}
			val := string(payload[pos : pos+valLen])
			pos += valLen

			entryBytes := payload[entryStart:pos]
			entries = append(entries, DecodedEntry{
				Offset:    entryStart,
				Index:     index,
				IsIndexed: true,
				Name:      StaticTable[index],
				Value:     val,
				WireBytes: entryBytes,
			})
		} else if index == 0 {
			// Format B: Literal Name
			if pos+2 > total {
				return nil, ErrMalformedHeader
			}
			nameLen := int(binary.BigEndian.Uint16(payload[pos : pos+2]))
			pos += 2

			if pos+nameLen > total {
				return nil, ErrMalformedHeader
			}
			name := string(payload[pos : pos+nameLen])
			pos += nameLen

			if pos+2 > total {
				return nil, ErrMalformedHeader
			}
			valLen := int(binary.BigEndian.Uint16(payload[pos : pos+2]))
			pos += 2

			if pos+valLen > total {
				return nil, ErrMalformedHeader
			}
			val := string(payload[pos : pos+valLen])
			pos += valLen

			entryBytes := payload[entryStart:pos]
			entries = append(entries, DecodedEntry{
				Offset:    entryStart,
				Index:     0,
				IsIndexed: false,
				Name:      name,
				Value:     val,
				WireBytes: entryBytes,
			})
		} else {
			return nil, fmt.Errorf("%w: unknown index 0x%02X (> 10)", ErrInvalidIndex, index)
		}
	}

	return entries, nil
}
