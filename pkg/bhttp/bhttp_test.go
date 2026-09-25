package bhttp

import (
	"bytes"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestFrameHeaderRoundtrip verifies that FrameHeader encoding and decoding are lossless.
func TestFrameHeaderRoundtrip(t *testing.T) {
	testCases := []FrameHeader{
		{Length: 0, Type: FrameHeaders, Flags: FlagEndStream | FlagEndHeaders, StreamID: 1},
		{Length: 1024, Type: FrameData, Flags: FlagEndStream, StreamID: 3},
		{Length: MaxPayloadSize, Type: FramePing, Flags: FlagAck, StreamID: 0},
		{Length: 42, Type: 0x99, Flags: 0x80, StreamID: 12345}, // Unknown experimental frame
	}

	for _, tc := range testCases {
		encoded := tc.EncodeHeader()
		decoded, err := DecodeHeader(encoded[:])
		if err != nil {
			t.Fatalf("DecodeHeader failed: %v", err)
		}
		if decoded != tc {
			t.Fatalf("Mismatch: expected %+v, got %+v", tc, decoded)
		}
	}
}

// TestHeaderBlockHPACKLite verifies static indexing and literal encoding.
func TestHeaderBlockHPACKLite(t *testing.T) {
	orig := HeaderBlock{
		{Name: ":method", Value: "GET"},
		{Name: ":path", Value: "/index.html"},
		{Name: "host", Value: "localhost:9000"},
		{Name: "user-agent", Value: "bcurl/1.0"},
		{Name: "x-custom-header", Value: "custom-value-123"}, // Literal name
	}

	encoded, err := EncodeHeaderBlock(orig)
	if err != nil {
		t.Fatalf("EncodeHeaderBlock failed: %v", err)
	}

	decoded, err := DecodeHeaderBlock(encoded)
	if err != nil {
		t.Fatalf("DecodeHeaderBlock failed: %v", err)
	}

	if decoded.Method() != "GET" {
		t.Errorf("Expected method GET, got %q", decoded.Method())
	}
	if decoded.Path() != "/index.html" {
		t.Errorf("Expected path /index.html, got %q", decoded.Path())
	}
	if decoded.Get("host") != "localhost:9000" {
		t.Errorf("Expected host localhost:9000, got %q", decoded.Get("host"))
	}
	if decoded.Get("x-custom-header") != "custom-value-123" {
		t.Errorf("Expected custom header custom-value-123, got %q", decoded.Get("x-custom-header"))
	}
}

// TestMalformedHeaderDetection verifies that truncated headers produce an error.
func TestMalformedHeaderDetection(t *testing.T) {
	hb := HeaderBlock{
		{Name: ":path", Value: "/index.html"},
	}
	encoded, _ := EncodeHeaderBlock(hb)

	// Truncate the payload mid-string
	truncated := encoded[:len(encoded)-4]
	_, err := DecodeHeaderBlock(truncated)
	if err == nil {
		t.Fatalf("Expected error decoding truncated header block, got nil")
	}
}

// TestUnknownFrameSkipping verifies the critical forward-compatibility rule.
func TestUnknownFrameSkipping(t *testing.T) {
	buf := new(bytes.Buffer)

	// 1. Write an unknown frame (Type 0xFE, Future Version 2 Frame)
	unknownPayload := []byte("future-v2-protocol-extension-payload")
	unknownFrame := &Frame{
		Header: FrameHeader{
			Length:   uint32(len(unknownPayload)),
			Type:     0xFE, // Unknown type
			Flags:    0x00,
			StreamID: 1,
		},
		Payload: unknownPayload,
	}
	if err := WriteFrame(buf, unknownFrame); err != nil {
		t.Fatalf("WriteFrame failed: %v", err)
	}

	// 2. Write a valid standard HEADERS frame after it
	validHeaders, _ := EncodeHeaderBlock(HeaderBlock{{Name: ":method", Value: "GET"}})
	headersFrame := &Frame{
		Header: FrameHeader{
			Length:   uint32(len(validHeaders)),
			Type:     FrameHeaders,
			Flags:    FlagEndHeaders,
			StreamID: 1,
		},
		Payload: validHeaders,
	}
	if err := WriteFrame(buf, headersFrame); err != nil {
		t.Fatalf("WriteFrame failed: %v", err)
	}

	// Receiver reading loop:
	// Frame 1: Must read unknown frame without failing
	f1, err := ReadFrame(buf)
	if err != nil {
		t.Fatalf("ReadFrame 1 failed: %v", err)
	}
	if IsKnownType(f1.Header.Type) {
		t.Errorf("Expected 0xFE to be unknown type")
	}
	if !bytes.Equal(f1.Payload, unknownPayload) {
		t.Errorf("Payload mismatch on skipped frame")
	}

	// Frame 2: Must cleanly read the valid frame immediately after!
	f2, err := ReadFrame(buf)
	if err != nil {
		t.Fatalf("ReadFrame 2 failed: %v", err)
	}
	if f2.Header.Type != FrameHeaders {
		t.Errorf("Expected FrameHeaders, got 0x%02X", f2.Header.Type)
	}
}

// TestEndToEndServerClient tests full interaction between bserve and bcurl logic.
func TestEndToEndServerClient(t *testing.T) {
	tmpDir := t.TempDir()
	indexFile := filepath.Join(tmpDir, "index.html")
	if err := os.WriteFile(indexFile, []byte("<h1>Hello BHTTP</h1>"), 0644); err != nil {
		t.Fatalf("Failed to create index.html: %v", err)
	}

	// Find free port
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	srv, err := NewServer(tmpDir, port)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}
	srv.LogDebug = false

	go func() {
		_ = srv.Start()
	}()
	defer func() { _ = srv.Stop() }()

	// Allow server to start
	time.Sleep(50 * time.Millisecond)
	addr := ln.Addr().String()

	client := NewClient(false)
	defer client.Close()

	// 1. Test 200 OK for /index.html
	resp, err := client.Get(addr, "/index.html")
	if err != nil {
		t.Fatalf("Client GET /index.html failed: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}
	if !strings.Contains(string(resp.Body), "Hello BHTTP") {
		t.Errorf("Unexpected body: %s", string(resp.Body))
	}

	// 2. Test persistent connection: fetch root "/" on the SAME socket
	resp2, err := client.Get(addr, "/")
	if err != nil {
		t.Fatalf("Client GET / failed on persistent connection: %v", err)
	}
	if resp2.StatusCode != 200 {
		t.Errorf("Expected status 200 for /, got %d", resp2.StatusCode)
	}
	if resp2.StreamID != 3 {
		t.Errorf("Expected stream ID 3 for second request, got %d", resp2.StreamID)
	}

	// 3. Test 404 Not Found for non-existent file
	resp404, err := client.Get(addr, "/nonexistent.txt")
	if err != nil {
		t.Fatalf("Client GET /nonexistent.txt failed: %v", err)
	}
	if resp404.StatusCode != 404 {
		t.Errorf("Expected status 404, got %d", resp404.StatusCode)
	}

	// 4. Test directory traversal rejection (400 Bad Request)
	resp400, err := client.Get(addr, "/../../etc/passwd")
	if err != nil {
		t.Fatalf("Client GET traversal failed: %v", err)
	}
	if resp400.StatusCode != 400 {
		t.Errorf("Expected status 400 for directory traversal, got %d", resp400.StatusCode)
	}
}
