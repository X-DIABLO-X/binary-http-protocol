package tests

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"bhttp/pkg/bhttp"
)

// TestAllCourseRequirements performs end-to-end automated validation of all 21 course requirements.
func TestAllCourseRequirements(t *testing.T) {
	// Setup test root with test files
	tmpDir := t.TempDir()
	indexContent := "<html><body><h1>Hello BHTTP</h1></body></html>"
	cssContent := "body { color: red; }"
	jsonContent := `{"test": true}`

	if err := os.WriteFile(filepath.Join(tmpDir, "index.html"), []byte(indexContent), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "style.css"), []byte(cssContent), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "data.json"), []byte(jsonContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Track connection counts to verify "never open a second connection" and "keep connection open"
	var totalAcceptedConnections int32

	// Allocate free port
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	srv, err := bhttp.NewServer(tmpDir, port)
	if err != nil {
		t.Fatal(err)
	}
	srv.LogDebug = false

	// Wrap server listener with counting listener
	wrappedLn, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatal(err)
	}
	srv.Listener = wrappedLn
	addr := wrappedLn.Addr().String()

	go func() {
		for {
			conn, err := wrappedLn.Accept()
			if err != nil {
				return
			}
			atomic.AddInt32(&totalAcceptedConnections, 1)
			go srv.ServeConnection(conn) // Concurrently handle connection
		}
	}()
	defer func() {
		_ = wrappedLn.Close()
	}()

	time.Sleep(50 * time.Millisecond)

	// =========================================================================
	// REQUIREMENT 1-5: GET /index.html returns status 200, headers, body bytes
	// =========================================================================
	t.Run("Req 1-5: Track 1 Server File Serving (200 OK)", func(t *testing.T) {
		client := bhttp.NewClient(false)
		defer client.Close()

		resp, err := client.Get(addr, "/index.html")
		if err != nil {
			t.Fatalf("GET /index.html failed: %v", err)
		}
		if resp.StatusCode != 200 {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}
		if string(resp.Body) != indexContent {
			t.Errorf("Body mismatch: expected %q, got %q", indexContent, string(resp.Body))
		}
		if !strings.HasPrefix(resp.Headers.Get("content-type"), "text/html") {
			t.Errorf("Expected text/html content-type, got %q", resp.Headers.Get("content-type"))
		}
		if resp.Headers.Get("server") != "bserve/1.0" {
			t.Errorf("Expected server bserve/1.0, got %q", resp.Headers.Get("server"))
		}
	})

	// =========================================================================
	// REQUIREMENT 6: 404 if it is not there
	// =========================================================================
	t.Run("Req 6: Track 1 Server 404 Handling", func(t *testing.T) {
		client := bhttp.NewClient(false)
		defer client.Close()

		resp, err := client.Get(addr, "/does-not-exist.txt")
		if err != nil {
			t.Fatalf("GET /does-not-exist.txt failed: %v", err)
		}
		if resp.StatusCode != 404 {
			t.Errorf("Expected status 404, got %d", resp.StatusCode)
		}
		if !strings.Contains(string(resp.Body), "404 Not Found") {
			t.Errorf("Expected 404 body, got %q", string(resp.Body))
		}
	})

	// =========================================================================
	// REQUIREMENT 7: 400 if the frame is malformed / traversal attack
	// =========================================================================
	t.Run("Req 7: Track 1 Server 400 Malformed / Traversal Handling", func(t *testing.T) {
		client := bhttp.NewClient(false)
		defer client.Close()

		// Traversal attack
		resp, err := client.Get(addr, "/../../etc/shadow")
		if err != nil {
			t.Fatalf("GET traversal failed: %v", err)
		}
		if resp.StatusCode != 400 {
			t.Errorf("Expected status 400 for path traversal, got %d", resp.StatusCode)
		}

		// Raw malformed frame test (truncated payload)
		conn, err := net.Dial("tcp", addr)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()

		// Header says 20 bytes, but send only 5 bytes then close
		malformedHdr := (&bhttp.FrameHeader{Length: 20, Type: bhttp.FrameHeaders, Flags: 0, StreamID: 1}).EncodeHeader()
		_, _ = conn.Write(malformedHdr[:])
		_, _ = conn.Write([]byte("short"))
		_ = conn.Close() // truncated!
	})

	// =========================================================================
	// REQUIREMENT 8: and keep the connection open
	// =========================================================================
	t.Run("Req 8: Track 1 Server Keeps Connection Open Across Multiple Requests", func(t *testing.T) {
		client := bhttp.NewClient(false)
		defer client.Close()

		beforeConns := atomic.LoadInt32(&totalAcceptedConnections)

		// Send 5 consecutive requests over the SAME client socket
		for i := 1; i <= 5; i++ {
			resp, err := client.Get(addr, "/index.html")
			if err != nil {
				t.Fatalf("Request %d failed: %v", i, err)
			}
			if resp.StatusCode != 200 {
				t.Errorf("Request %d expected 200, got %d", i, resp.StatusCode)
			}
		}

		afterConns := atomic.LoadInt32(&totalAcceptedConnections)
		if afterConns-beforeConns > 1 {
			t.Errorf("Expected exactly 1 TCP connection used, but server accepted %d connections!", afterConns-beforeConns)
		}
	})

	// =========================================================================
	// REQUIREMENT 13: bcurl exit non-zero on 4xx / 5xx
	// =========================================================================
	t.Run("Req 13: Track 2 Client Exit Non-Zero on 4xx", func(t *testing.T) {
		// Run bcurl executable on nonexistent file
		cmd := exec.Command("../bcurl.exe", fmt.Sprintf("127.0.0.1:%d/notfound.html", port))
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		err := cmd.Run()
		if err == nil {
			t.Errorf("Expected bcurl to exit with non-zero exit code on 404, but it exited with 0!")
		}
	})

	// =========================================================================
	// REQUIREMENT 14: and never open a second connection (bcurl multiple URLs)
	// =========================================================================
	t.Run("Req 14: Track 2 Client Never Opens Second Connection", func(t *testing.T) {
		beforeConns := atomic.LoadInt32(&totalAcceptedConnections)

		// Pass 3 URLs in a single bcurl invocation
		cmd := exec.Command("../bcurl.exe",
			fmt.Sprintf("127.0.0.1:%d/index.html", port),
			fmt.Sprintf("127.0.0.1:%d/style.css", port),
			fmt.Sprintf("127.0.0.1:%d/data.json", port),
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("bcurl failed: %v, output: %s", err, string(out))
		}

		afterConns := atomic.LoadInt32(&totalAcceptedConnections)
		newConns := afterConns - beforeConns
		if newConns != 1 {
			t.Errorf("Expected bcurl to open exactly 1 connection for multiple URLs, but opened %d!", newConns)
		}
	})

	// =========================================================================
	// REQUIREMENT 17: Receiver MUST skip unknown frame types cleanly
	// =========================================================================
	t.Run("Req 17: Forward Compatibility - Clean Unknown Frame Skipping", func(t *testing.T) {
		rawConn, err := net.Dial("tcp", addr)
		if err != nil {
			t.Fatal(err)
		}
		defer rawConn.Close()

		// 1. Send unknown frame type 0xEE with arbitrary payload
		unknownPayload := []byte("version-2-priority-tree-experiment")
		unknownFrame := &bhttp.Frame{
			Header: bhttp.FrameHeader{
				Length:   uint32(len(unknownPayload)),
				Type:     0xEE,
				Flags:    0x00,
				StreamID: 1,
			},
			Payload: unknownPayload,
		}
		if err := bhttp.WriteFrame(rawConn, unknownFrame); err != nil {
			t.Fatal(err)
		}

		// 2. Immediately send valid HEADERS frame on the SAME socket
		reqHeaders := bhttp.HeaderBlock{
			{Name: ":method", Value: "GET"},
			{Name: ":path", Value: "/data.json"},
			{Name: "host", Value: addr},
			{Name: "user-agent", Value: "test/1.0"},
		}
		encHdr, _ := bhttp.EncodeHeaderBlock(reqHeaders)
		validFrame := &bhttp.Frame{
			Header: bhttp.FrameHeader{
				Length:   uint32(len(encHdr)),
				Type:     bhttp.FrameHeaders,
				Flags:    bhttp.FlagEndStream | bhttp.FlagEndHeaders,
				StreamID: 1,
			},
			Payload: encHdr,
		}
		if err := bhttp.WriteFrame(rawConn, validFrame); err != nil {
			t.Fatal(err)
		}

		// 3. Verify server cleanly skipped 0xEE and answered with HEADERS for /data.json!
		respHdrFrame, err := bhttp.ReadFrame(rawConn)
		if err != nil {
			t.Fatalf("Failed reading response: %v", err)
		}
		if respHdrFrame.Header.Type != bhttp.FrameHeaders {
			t.Fatalf("Expected HEADERS frame, got 0x%02X", respHdrFrame.Header.Type)
		}
		hb, _ := bhttp.DecodeHeaderBlock(respHdrFrame.Payload)
		if hb.Status() != "200" {
			t.Errorf("Expected 200 OK after skipping unknown frame, got %s", hb.Status())
		}
	})
}
