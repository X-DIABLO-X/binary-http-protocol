package main

import (
	"fmt"
	"os"
	"strings"

	"bhttp/pkg/bhttp"
)

func main() {
	var sb strings.Builder

	sb.WriteString("# BHTTP/1.0 Annotated Hexdump\n\n")
	sb.WriteString("This document provides a byte-by-byte annotated hexdump of one complete conforming BHTTP/1.0 request and response exchange between `bcurl` and `bserve`.\n\n")
	sb.WriteString("> **\"If you cannot annotate your own bytes, the spec is not finished.\"**\n\n")
	sb.WriteString("---\n\n")

	// 1. Client Request HEADERS Frame
	reqHeaders := bhttp.HeaderBlock{
		{Name: ":method", Value: "GET"},
		{Name: ":path", Value: "/index.html"},
		{Name: "host", Value: "localhost:9000"},
		{Name: "user-agent", Value: "bcurl/1.0"},
		{Name: "connection", Value: "keep-alive"},
	}
	reqPayload, err := bhttp.EncodeHeaderBlock(reqHeaders)
	if err != nil {
		panic(err)
	}

	reqFrame := &bhttp.Frame{
		Header: bhttp.FrameHeader{
			Length:   uint32(len(reqPayload)),
			Type:     bhttp.FrameHeaders,
			Flags:    bhttp.FlagEndStream | bhttp.FlagEndHeaders, // 0x05
			StreamID: 1,
		},
		Payload: reqPayload,
	}

	sb.WriteString("## 1. Request: Client to Server (`bcurl` -> `bserve`)\n\n")
	sb.WriteString("The client initiates stream `1` by transmitting a single `HEADERS` frame carrying flags `END_STREAM (0x01)` and `END_HEADERS (0x04)`.\n\n")
	sb.WriteString(bhttp.FormatMarkdownAnnotatedHexdump(reqFrame, "Frame 1: Request HEADERS (Stream 1)"))

	// 2. Server Response HEADERS Frame
	respBody := []byte("<!DOCTYPE html>\n<html>\n<body>\n<h1>Hello BHTTP</h1>\n</body>\n</html>\n")
	respHeaders := bhttp.HeaderBlock{
		{Name: ":status", Value: "200"},
		{Name: "content-type", Value: "text/html; charset=utf-8"},
		{Name: "content-length", Value: fmt.Sprintf("%d", len(respBody))},
		{Name: "server", Value: "bserve/1.0"},
		{Name: "date", Value: "Fri, 25 Sep 2026 12:45:00 GMT"},
		{Name: "connection", Value: "keep-alive"},
	}
	respHdrPayload, err := bhttp.EncodeHeaderBlock(respHeaders)
	if err != nil {
		panic(err)
	}

	respHdrFrame := &bhttp.Frame{
		Header: bhttp.FrameHeader{
			Length:   uint32(len(respHdrPayload)),
			Type:     bhttp.FrameHeaders,
			Flags:    bhttp.FlagEndHeaders, // 0x04 (not END_STREAM because DATA follows)
			StreamID: 1,
		},
		Payload: respHdrPayload,
	}

	sb.WriteString("---\n\n")
	sb.WriteString("## 2. Response: Server to Client (`bserve` -> `bcurl`)\n\n")
	sb.WriteString("The server replies on the same stream (`1`) with two frames: a `HEADERS` frame containing metadata, followed by a `DATA` frame with the file contents.\n\n")
	sb.WriteString(bhttp.FormatMarkdownAnnotatedHexdump(respHdrFrame, "Frame 2: Response HEADERS (Stream 1)"))

	// 3. Server Response DATA Frame
	respDataFrame := &bhttp.Frame{
		Header: bhttp.FrameHeader{
			Length:   uint32(len(respBody)),
			Type:     bhttp.FrameData,
			Flags:    bhttp.FlagEndStream, // 0x01 signals completion of response
			StreamID: 1,
		},
		Payload: respBody,
	}

	sb.WriteString(bhttp.FormatMarkdownAnnotatedHexdump(respDataFrame, "Frame 3: Response DATA (Stream 1)"))

	sb.WriteString("---\n\n")
	sb.WriteString("## 3. Structural Summary & Field Verification\n\n")
	sb.WriteString("### Frame Header Layout (64-Bit / 8-Octet Alignment)\n\n")
	sb.WriteString("```\n")
	sb.WriteString(" 0                   1                   2                   3\n")
	sb.WriteString(" 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1\n")
	sb.WriteString("+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+\n")
	sb.WriteString("|                       Length (24)             |   Type (8)    |\n")
	sb.WriteString("+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+\n")
	sb.WriteString("|   Flags (8)   |                 Stream ID (24)                |\n")
	sb.WriteString("+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+\n")
	sb.WriteString("```\n\n")
	sb.WriteString("### HPACK-Lite Static Index Matching\n\n")
	sb.WriteString("| Header Name | Index | Wire Representation |\n")
	sb.WriteString("|:---|:---:|:---|\n")
	sb.WriteString("| `:method` | `0x01` | `[01] [00 03] [47 45 54]` (`\"GET\"`) |\n")
	sb.WriteString("| `:path` | `0x02` | `[02] [00 0B] [2F 69 6E 64 65 78 2E 68 74 6D 6C]` (`\"/index.html\"`) |\n")
	sb.WriteString("| `:status` | `0x03` | `[03] [00 03] [32 30 30]` (`\"200\"`) |\n")
	sb.WriteString("| `content-type` | `0x04` | `[04] [00 18] [...]` (`\"text/html; charset=utf-8\"`) |\n")
	sb.WriteString("| `content-length` | `0x05` | `[05] [00 02] [36 34]` (`\"64\"`) |\n")
	sb.WriteString("| `host` | `0x06` | `[06] [00 0E] [...]` (`\"localhost:9000\"`) |\n")
	sb.WriteString("| `user-agent` | `0x07` | `[07] [00 09] [...]` (`\"bcurl/1.0\"`) |\n")
	sb.WriteString("| `server` | `0x08` | `[08] [00 0A] [...]` (`\"bserve/1.0\"`) |\n")
	sb.WriteString("| `date` | `0x09` | `[09] [00 1D] [...]` (`\"Fri, 25 Sep 2026 12:45:00 GMT\"`) |\n")
	sb.WriteString("| `connection` | `0x0A` | `[0A] [00 0A] [...]` (`\"keep-alive\"`) |\n\n")

	sb.WriteString("### Forward Compatibility / Extension Invariant\n\n")
	sb.WriteString("When any receiver encounters an unknown Frame Type (e.g. `0x99` or `0xFE` in BHTTP/2), it extracts the 24-bit Length from octets `00..02`, discards exactly that number of octets, and continues reading the stream without desynchronization or dropping the TCP connection.\n")

	if err := os.WriteFile("ANNOTATED_HEXDUMP.md", []byte(sb.String()), 0644); err != nil {
		panic(err)
	}
	fmt.Println("Generated ANNOTATED_HEXDUMP.md successfully!")
}
