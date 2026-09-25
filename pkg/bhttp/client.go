package bhttp

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Response represents a parsed BHTTP response.
type Response struct {
	StatusCode int
	StatusText string
	Headers    HeaderBlock
	Body       []byte
	StreamID   uint32
}

// Client represents a BHTTP/1.0 client.
type Client struct {
	Verbose      bool
	Timeout      time.Duration
	Conn         net.Conn
	currentAddr  string
	nextStreamID uint32
	OutWriter    io.Writer
	ErrWriter    io.Writer
}

// NewClient initializes a new BHTTP client.
func NewClient(verbose bool) *Client {
	return &Client{
		Verbose:      verbose,
		Timeout:      10 * time.Second,
		nextStreamID: 1, // RFC mandate: client-initiated streams are odd
		OutWriter:    os.Stdout,
		ErrWriter:    os.Stderr,
	}
}

// Close closes the underlying persistent TCP connection if open.
func (c *Client) Close() error {
	if c.Conn != nil {
		err := c.Conn.Close()
		c.Conn = nil
		return err
	}
	return nil
}

// ParseURL parses strings like "localhost:9000/index.html" or "http://localhost:9000/index.html".
func ParseURL(raw string) (hostPort string, path string, err error) {
	if !strings.HasPrefix(raw, "http://") && !strings.HasPrefix(raw, "https://") && !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}

	u, err := url.Parse(raw)
	if err != nil {
		return "", "", fmt.Errorf("invalid URL %q: %w", raw, err)
	}

	hostPort = u.Host
	if !strings.Contains(hostPort, ":") {
		hostPort = hostPort + ":9000" // Default BHTTP port
	}

	path = u.Path
	if path == "" {
		path = "/"
	}
	if u.RawQuery != "" {
		path += "?" + u.RawQuery
	}

	return hostPort, path, nil
}

// EnsureConnection connects or reuses the existing persistent TCP connection.
// BHTTP INVARIANT: "and never open a second connection"
func (c *Client) EnsureConnection(addr string) error {
	if c.Conn != nil && c.currentAddr == addr {
		return nil // Reuse persistent connection!
	}

	if c.Conn != nil {
		_ = c.Conn.Close()
		c.Conn = nil
	}

	conn, err := net.DialTimeout("tcp", addr, c.Timeout)
	if err != nil {
		return fmt.Errorf("failed to connect to %s: %w", addr, err)
	}

	c.Conn = conn
	c.currentAddr = addr
	return nil
}

// Get performs a BHTTP GET request for the specified path over the persistent connection.
func (c *Client) Get(addr, path string) (*Response, error) {
	if err := c.EnsureConnection(addr); err != nil {
		return nil, err
	}

	streamID := c.nextStreamID
	c.nextStreamID += 2 // Next odd stream ID

	// Build request headers
	reqHeaders := HeaderBlock{
		{Name: ":method", Value: "GET"},
		{Name: ":path", Value: path},
		{Name: "host", Value: addr},
		{Name: "user-agent", Value: "bcurl/1.0"},
		{Name: "connection", Value: "keep-alive"},
	}

	encodedHeaders, err := EncodeHeaderBlock(reqHeaders)
	if err != nil {
		return nil, fmt.Errorf("failed to encode request headers: %w", err)
	}

	// Build HEADERS frame: END_STREAM (0x01) | END_HEADERS (0x04)
	reqFrame := &Frame{
		Header: FrameHeader{
			Length:   uint32(len(encodedHeaders)),
			Type:     FrameHeaders,
			Flags:    FlagEndStream | FlagEndHeaders,
			StreamID: streamID,
		},
		Payload: encodedHeaders,
	}

	// -v hexdumps every frame
	if c.Verbose {
		fmt.Fprint(c.ErrWriter, DumpFrame(reqFrame, ">>> TRANSMIT"))
	}

	// Transmit request frame
	if err := WriteFrame(c.Conn, reqFrame); err != nil {
		return nil, fmt.Errorf("failed to write request frame: %w", err)
	}

	// Read response frames until END_STREAM
	resp := &Response{
		StreamID: streamID,
	}

	for {
		frame, err := ReadFrame(c.Conn)
		if err != nil {
			return nil, fmt.Errorf("failed to read response frame: %w", err)
		}

		// -v hexdumps every frame
		if c.Verbose {
			fmt.Fprint(c.ErrWriter, DumpFrame(frame, "<<< RECEIVE"))
		}

		// MANDATORY EXTENSION RULE: If receiver encounters unknown frame type, skip it cleanly!
		if !IsKnownType(frame.Header.Type) {
			if c.Verbose {
				fmt.Fprintf(c.ErrWriter, "[bcurl] Cleanly skipped unknown frame type 0x%02X (%d octets)\n",
					frame.Header.Type, frame.Header.Length)
			}
			continue
		}

		// Discard frames from unrelated streams (unless connection-level stream 0)
		if frame.Header.StreamID != streamID && frame.Header.StreamID != 0 {
			continue
		}

		switch frame.Header.Type {
		case FrameHeaders:
			headers, err := DecodeHeaderBlock(frame.Payload)
			if err != nil {
				return nil, fmt.Errorf("malformed response headers: %w", err)
			}
			resp.Headers = headers
			statusStr := headers.Status()
			if code, err := strconv.Atoi(statusStr); err == nil {
				resp.StatusCode = code
			}

		case FrameData:
			resp.Body = append(resp.Body, frame.Payload...)

		case FrameGoAway:
			return resp, errors.New("server sent GOAWAY")
		}

		if frame.HasFlag(FlagEndStream) {
			break
		}
	}

	return resp, nil
}
