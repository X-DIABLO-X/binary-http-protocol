package bhttp

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"
)

// DumpFrame generates an annotated hexdump of a Frame.
// direction is either ">>> TRANSMIT" or "<<< RECEIVE".
func DumpFrame(f *Frame, direction string) string {
	var sb strings.Builder

	wire, _ := SerializeFrame(f)

	sb.WriteString(fmt.Sprintf("\n=== %s FRAME [%s] Stream=%d Flags=%s Length=%d ===\n",
		direction, f.Header.TypeName(), f.Header.StreamID, f.Header.FlagsString(), f.Header.Length))

	// 1. Raw Hexdump (like xxd -C)
	sb.WriteString("--- RAW WIRE BYTES (8B Header + %d Payload) ---\n")
	sb.WriteString(HexDumpBlock(wire, 0))

	// 2. Structured Byte-by-Byte Annotation
	sb.WriteString("\n--- ANNOTATED BYTE BREAKDOWN ---\n")

	// Fixed Header (8 bytes)
	sb.WriteString(fmt.Sprintf("[00..02]  %-8s  Length: %d octets (0x%06X)\n",
		fmt.Sprintf("%02X %02X %02X", wire[0], wire[1], wire[2]),
		f.Header.Length, f.Header.Length))
	sb.WriteString(fmt.Sprintf("[03]      %-8s  Type: 0x%02X (%s)\n",
		fmt.Sprintf("%02X", wire[3]),
		wire[3], f.Header.TypeName()))
	sb.WriteString(fmt.Sprintf("[04]      %-8s  Flags: 0x%02X (%s)\n",
		fmt.Sprintf("%02X", wire[4]),
		wire[4], f.Header.FlagsString()))
	sb.WriteString(fmt.Sprintf("[05..07]  %-8s  Stream ID: %d (0x%06X)\n",
		fmt.Sprintf("%02X %02X %02X", wire[5], wire[6], wire[7]),
		f.Header.StreamID, f.Header.StreamID))

	// Payload Breakdown based on Frame Type
	switch f.Header.Type {
	case FrameHeaders:
		sb.WriteString("--- HEADERS PAYLOAD (HPACK-Lite Decoded) ---\n")
		entries, err := DecodeHeaderBlockDetailed(f.Payload)
		if err != nil {
			sb.WriteString(fmt.Sprintf("  [MALFORMED HEADERS: %v]\n", err))
		} else {
			for _, e := range entries {
				hdrWireOffset := FrameHeaderSize + e.Offset
				if e.IsIndexed {
					// Format A: [Index (1B)][Len (2B)][Value (NB)]
					idxHex := fmt.Sprintf("%02X", e.WireBytes[0])
					lenHex := fmt.Sprintf("%02X %02X", e.WireBytes[1], e.WireBytes[2])
					valHex := HexSummary(e.WireBytes[3:], 16)
					sb.WriteString(fmt.Sprintf("  [%02d]     Index 0x%s -> \"%s\"\n", hdrWireOffset, idxHex, e.Name))
					sb.WriteString(fmt.Sprintf("  [%02d..%02d] Value Length: %d (0x%s)\n", hdrWireOffset+1, hdrWireOffset+2, len(e.Value), lenHex))
					sb.WriteString(fmt.Sprintf("  [%02d..%02d] Value Bytes:  [%s] => \"%s\"\n",
						hdrWireOffset+3, hdrWireOffset+len(e.WireBytes)-1, valHex, e.Value))
				} else {
					// Format B: [0x00 (1B)][NameLen (2B)][Name (NB)][ValLen (2B)][Val (MB)]
					sb.WriteString(fmt.Sprintf("  [%02d]     Literal Marker (0x00)\n", hdrWireOffset))
					nameLen := int(binary.BigEndian.Uint16(e.WireBytes[1:3]))
					sb.WriteString(fmt.Sprintf("  [%02d..%02d] Name Length:  %d\n", hdrWireOffset+1, hdrWireOffset+2, nameLen))
					sb.WriteString(fmt.Sprintf("  [%02d..%02d] Name String:  \"%s\"\n", hdrWireOffset+3, hdrWireOffset+2+nameLen, e.Name))
					valOffset := 3 + nameLen
					valLen := int(binary.BigEndian.Uint16(e.WireBytes[valOffset : valOffset+2]))
					sb.WriteString(fmt.Sprintf("  [%02d..%02d] Value Length: %d\n", hdrWireOffset+valOffset, hdrWireOffset+valOffset+1, valLen))
					sb.WriteString(fmt.Sprintf("  [%02d..%02d] Value String: \"%s\"\n",
						hdrWireOffset+valOffset+2, hdrWireOffset+len(e.WireBytes)-1, e.Value))
				}
			}
		}

	case FrameData:
		sb.WriteString("--- DATA PAYLOAD (Raw Bytes) ---\n")
		if len(f.Payload) == 0 {
			sb.WriteString("  (empty data frame)\n")
		} else {
			previewLen := len(f.Payload)
			if previewLen > 64 {
				previewLen = 64
				sb.WriteString(fmt.Sprintf("  Payload Size: %d bytes (showing first 64 bytes):\n", len(f.Payload)))
			} else {
				sb.WriteString(fmt.Sprintf("  Payload Size: %d bytes:\n", len(f.Payload)))
			}
			sb.WriteString(HexDumpBlock(f.Payload[:previewLen], FrameHeaderSize))
			if len(f.Payload) > 64 {
				sb.WriteString(fmt.Sprintf("  ... (+%d more bytes truncated in display) ...\n", len(f.Payload)-64))
			}
		}

	default:
		sb.WriteString(fmt.Sprintf("--- UNKNOWN PAYLOAD (Type 0x%02X, %d bytes) ---\n", f.Header.Type, len(f.Payload)))
		if len(f.Payload) > 0 {
			sb.WriteString(HexDumpBlock(f.Payload, FrameHeaderSize))
		}
	}

	sb.WriteString("=====================================================\n\n")
	return sb.String()
}

// HexDumpBlock produces formatted hex+ascii columns.
func HexDumpBlock(data []byte, baseOffset int) string {
	var sb strings.Builder
	for i := 0; i < len(data); i += 16 {
		chunkEnd := i + 16
		if chunkEnd > len(data) {
			chunkEnd = len(data)
		}
		chunk := data[i:chunkEnd]

		// Offset
		sb.WriteString(fmt.Sprintf("%04x  ", baseOffset+i))

		// Hex bytes
		var hexParts []string
		for _, b := range chunk {
			hexParts = append(hexParts, fmt.Sprintf("%02x", b))
		}
		hexStr := strings.Join(hexParts, " ")
		sb.WriteString(fmt.Sprintf("%-48s  |", hexStr))

		// ASCII preview
		for _, b := range chunk {
			if b >= 32 && b <= 126 {
				sb.WriteByte(b)
			} else {
				sb.WriteByte('.')
			}
		}
		sb.WriteString("|\n")
	}
	return sb.String()
}

// HexSummary formats a byte slice into spaced hex string, truncated if necessary.
func HexSummary(data []byte, max int) string {
	if len(data) <= max {
		return hex.EncodeToString(data)
	}
	return hex.EncodeToString(data[:max]) + "..."
}

// FormatMarkdownAnnotatedHexdump generates GitHub-flavored markdown table and breakdown.
func FormatMarkdownAnnotatedHexdump(f *Frame, label string) string {
	var buf bytes.Buffer
	wire, _ := SerializeFrame(f)

	buf.WriteString(fmt.Sprintf("### %s\n\n", label))
	buf.WriteString(fmt.Sprintf("- **Frame Type:** `%s` (`0x%02X`)\n", f.Header.TypeName(), f.Header.Type))
	buf.WriteString(fmt.Sprintf("- **Flags:** `%s` (`0x%02X`)\n", f.Header.FlagsString(), f.Header.Flags))
	buf.WriteString(fmt.Sprintf("- **Stream ID:** `%d` (`0x%06X`)\n", f.Header.StreamID, f.Header.StreamID))
	buf.WriteString(fmt.Sprintf("- **Payload Length:** `%d` octets\n\n", f.Header.Length))

	buf.WriteString("```hexdump\n")
	buf.WriteString(HexDumpBlock(wire, 0))
	buf.WriteString("```\n\n")

	buf.WriteString("| Offset (Dec) | Hex Octets | Field / Semantic Interpretation |\n")
	buf.WriteString("|:------------:|:----------:|:--------------------------------|\n")

	// Header breakdown
	buf.WriteString(fmt.Sprintf("| `00..02` | `%02X %02X %02X` | **Length:** `%d` octets (payload size) |\n",
		wire[0], wire[1], wire[2], f.Header.Length))
	buf.WriteString(fmt.Sprintf("| `03` | `%02X` | **Type:** `%s` (`0x%02X`) |\n",
		wire[3], f.Header.TypeName(), wire[3]))
	buf.WriteString(fmt.Sprintf("| `04` | `%02X` | **Flags:** `%s` (`0x%02X`) |\n",
		wire[4], f.Header.FlagsString(), wire[4]))
	buf.WriteString(fmt.Sprintf("| `05..07` | `%02X %02X %02X` | **Stream ID:** `%d` |\n",
		wire[5], wire[6], wire[7], f.Header.StreamID))

	if f.Header.Type == FrameHeaders {
		entries, _ := DecodeHeaderBlockDetailed(f.Payload)
		for _, e := range entries {
			base := FrameHeaderSize + e.Offset
			if e.IsIndexed {
				buf.WriteString(fmt.Sprintf("| `%02d` | `%02X` | **Static Index [%d]:** `%s` |\n",
					base, e.WireBytes[0], e.Index, e.Name))
				buf.WriteString(fmt.Sprintf("| `%02d..%02d` | `%02X %02X` | Value Length: `%d` octets |\n",
					base+1, base+2, e.WireBytes[1], e.WireBytes[2], len(e.Value)))
				valHex := ""
				for i, b := range []byte(e.Value) {
					if i > 0 {
						valHex += " "
					}
					valHex += fmt.Sprintf("%02X", b)
				}
				buf.WriteString(fmt.Sprintf("| `%02d..%02d` | `%s` | Value: `\"%s\"` |\n",
					base+3, base+len(e.WireBytes)-1, valHex, e.Value))
			} else {
				buf.WriteString(fmt.Sprintf("| `%02d` | `00` | **Literal Name Marker (0x00)** |\n", base))
				nameLen := int(binary.BigEndian.Uint16(e.WireBytes[1:3]))
				buf.WriteString(fmt.Sprintf("| `%02d..%02d` | `%02X %02X` | Name Length: `%d` |\n",
					base+1, base+2, e.WireBytes[1], e.WireBytes[2], nameLen))
				buf.WriteString(fmt.Sprintf("| `%02d..%02d` | `%s` | Name String: `\"%s\"` |\n",
					base+3, base+2+nameLen, HexSummary([]byte(e.Name), 32), e.Name))
				valOffset := 3 + nameLen
				valLen := int(binary.BigEndian.Uint16(e.WireBytes[valOffset : valOffset+2]))
				buf.WriteString(fmt.Sprintf("| `%02d..%02d` | `%02X %02X` | Value Length: `%d` |\n",
					base+valOffset, base+valOffset+1, e.WireBytes[valOffset], e.WireBytes[valOffset+1], valLen))
				buf.WriteString(fmt.Sprintf("| `%02d..%02d` | `%s` | Value String: `\"%s\"` |\n",
					base+valOffset+2, base+len(e.WireBytes)-1, HexSummary([]byte(e.Value), 32), e.Value))
			}
		}
	} else if f.Header.Type == FrameData {
		buf.WriteString(fmt.Sprintf("| `08..%02d` | *payload* | Data Bytes: `\"%s\"` |\n",
			FrameHeaderSize+len(f.Payload)-1, strings.ReplaceAll(string(f.Payload), "\n", "\\n")))
	}
	buf.WriteString("\n")
	return buf.String()
}
