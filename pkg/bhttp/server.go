package bhttp

import (
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Server represents a BHTTP/1.0 compliant file server.
type Server struct {
	RootDir  string
	Port     int
	Listener net.Listener

	mu       sync.Mutex
	active   bool
	connWg   sync.WaitGroup
	LogDebug bool
}

// NewServer initializes a new BHTTP server.
func NewServer(rootDir string, port int) (*Server, error) {
	absRoot, err := filepath.Abs(rootDir)
	if err != nil {
		return nil, fmt.Errorf("invalid root directory %q: %w", rootDir, err)
	}

	info, err := os.Stat(absRoot)
	if err != nil {
		return nil, fmt.Errorf("root directory %q inaccessible: %w", rootDir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("root path %q is not a directory", rootDir)
	}

	return &Server{
		RootDir:  absRoot,
		Port:     port,
		LogDebug: true,
	}, nil
}

// Start begins listening and serving TCP connections.
func (s *Server) Start() error {
	addr := fmt.Sprintf("0.0.0.0:%d", s.Port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}

	s.mu.Lock()
	s.Listener = ln
	s.active = true
	s.mu.Unlock()

	log.Printf("[bserve] Listening on %s (serving root: %s)\n", addr, s.RootDir)

	for {
		conn, err := ln.Accept()
		if err != nil {
			s.mu.Lock()
			active := s.active
			s.mu.Unlock()
			if !active {
				return nil // Clean shutdown
			}
			log.Printf("[bserve] Accept error: %v\n", err)
			continue
		}

		s.connWg.Add(1)
		go s.handleConnection(conn)
	}
}

// Stop gracefully shuts down the server.
func (s *Server) Stop() error {
	s.mu.Lock()
	s.active = false
	if s.Listener != nil {
		_ = s.Listener.Close()
	}
	s.mu.Unlock()

	s.connWg.Wait()
	log.Printf("[bserve] Stopped.\n")
	return nil
}

// ServeConnection services a single TCP connection until termination.
func (s *Server) ServeConnection(conn net.Conn) {
	s.connWg.Add(1)
	s.handleConnection(conn)
}

// handleConnection manages persistent TCP connection sessions.
func (s *Server) handleConnection(conn net.Conn) {
	defer s.connWg.Done()
	defer conn.Close()

	remoteAddr := conn.RemoteAddr().String()
	if s.LogDebug {
		log.Printf("[bserve] Accepted new TCP connection from %s\n", remoteAddr)
	}

	for {
		// Read one binary frame from TCP stream
		frame, err := ReadFrame(conn)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
				if s.LogDebug {
					log.Printf("[bserve] Connection closed by client %s\n", remoteAddr)
				}
				return
			}

			// Malformed frame header or payload truncation -> Send 400 Bad Request and close
			log.Printf("[bserve] Malformed frame from %s: %v\n", remoteAddr, err)
			s.sendErrorResponse(conn, 1, 400, "Bad Request: Malformed Frame")
			return
		}

		// EXTENSION RULE: If receiver encounters unknown frame type, skip it cleanly!
		if !IsKnownType(frame.Header.Type) {
			if s.LogDebug {
				log.Printf("[bserve] Cleanly skipped unknown frame type 0x%02X (%d octets) on stream %d\n",
					frame.Header.Type, frame.Header.Length, frame.Header.StreamID)
			}
			continue // Keep connection open and proceed!
		}

		switch frame.Header.Type {
		case FramePing:
			// Liveness probe: echo PING with ACK flag set
			ackFrame := &Frame{
				Header: FrameHeader{
					Length:   uint32(len(frame.Payload)),
					Type:     FramePing,
					Flags:    FlagAck,
					StreamID: frame.Header.StreamID,
				},
				Payload: frame.Payload,
			}
			_ = WriteFrame(conn, ackFrame)

		case FrameGoAway:
			if s.LogDebug {
				log.Printf("[bserve] Received GOAWAY from %s, terminating connection\n", remoteAddr)
			}
			return

		case FrameHeaders:
			s.handleHeadersFrame(conn, frame)

		case FrameData:
			// For simple GET requests, client should not send unsolicited DATA frames
			if s.LogDebug {
				log.Printf("[bserve] Received DATA frame on stream %d (%d bytes)\n", frame.Header.StreamID, frame.Header.Length)
			}
		}

		// Loop continues: BHTTP MANDATE — keep the connection open!
	}
}

// handleHeadersFrame processes an incoming HEADERS frame.
func (s *Server) handleHeadersFrame(conn net.Conn, frame *Frame) {
	streamID := frame.Header.StreamID

	hb, err := DecodeHeaderBlock(frame.Payload)
	if err != nil {
		log.Printf("[bserve] Failed to decode headers on stream %d: %v\n", streamID, err)
		s.sendErrorResponse(conn, streamID, 400, "Bad Request: Malformed Headers")
		return
	}

	method := strings.ToUpper(hb.Method())
	path := hb.Path()

	if s.LogDebug {
		log.Printf("[bserve] [Stream %d] %s %s\n", streamID, method, path)
	}

	if method != "GET" && method != "HEAD" {
		s.sendErrorResponse(conn, streamID, 405, "Method Not Allowed")
		return
	}

	if path == "" || !strings.HasPrefix(path, "/") {
		s.sendErrorResponse(conn, streamID, 400, "Bad Request: Invalid Path")
		return
	}

	// Remove query parameters or fragments if present
	if idx := strings.IndexAny(path, "?#"); idx != -1 {
		path = path[:idx]
	}

	// Prevent directory traversal attacks
	if strings.Contains(path, "..") {
		log.Printf("[bserve] Directory traversal attempt blocked: %s\n", path)
		s.sendErrorResponse(conn, streamID, 400, "Bad Request: Directory Traversal Denied")
		return
	}

	relPath := strings.TrimPrefix(filepath.ToSlash(path), "/")
	resolvedPath := filepath.Join(s.RootDir, filepath.FromSlash(relPath))

	// Verify resolvedPath remains strictly inside RootDir
	rel, err := filepath.Rel(s.RootDir, resolvedPath)
	if err != nil || strings.HasPrefix(rel, "..") || rel == ".." {
		log.Printf("[bserve] Directory traversal attempt blocked: %s\n", path)
		s.sendErrorResponse(conn, streamID, 400, "Bad Request: Directory Traversal Denied")
		return
	}

	// If resolved path is a directory, check for index.html
	fileInfo, err := os.Stat(resolvedPath)
	if err == nil && fileInfo.IsDir() {
		candidateIndex := filepath.Join(resolvedPath, "index.html")
		if idxInfo, idxErr := os.Stat(candidateIndex); idxErr == nil && !idxInfo.IsDir() {
			resolvedPath = candidateIndex
			fileInfo = idxInfo
		} else {
			// Directory without index.html -> 404 Not Found
			s.sendErrorResponse(conn, streamID, 404, "404 Not Found")
			return
		}
	}

	if err != nil {
		if os.IsNotExist(err) {
			s.sendErrorResponse(conn, streamID, 404, "404 Not Found")
			return
		}
		s.sendErrorResponse(conn, streamID, 500, "500 Internal Server Error")
		return
	}

	// Read file contents
	content, err := os.ReadFile(resolvedPath)
	if err != nil {
		log.Printf("[bserve] Error reading file %s: %v\n", resolvedPath, err)
		s.sendErrorResponse(conn, streamID, 500, "500 Internal Server Error")
		return
	}

	// Detect MIME type
	contentType := detectMimeType(resolvedPath, content)

	// Send 200 OK Response
	s.sendSuccessResponse(conn, streamID, contentType, content, method == "HEAD")
}

// sendSuccessResponse constructs and writes 200 HEADERS and DATA frames.
func (s *Server) sendSuccessResponse(conn net.Conn, streamID uint32, contentType string, content []byte, headOnly bool) {
	respHeaders := HeaderBlock{
		{Name: ":status", Value: "200"},
		{Name: "content-type", Value: contentType},
		{Name: "content-length", Value: strconv.Itoa(len(content))},
		{Name: "server", Value: "bserve/1.0"},
		{Name: "date", Value: time.Now().UTC().Format(http.TimeFormat)},
		{Name: "connection", Value: "keep-alive"},
	}

	encodedHeaders, err := EncodeHeaderBlock(respHeaders)
	if err != nil {
		log.Printf("[bserve] Failed to encode response headers: %v\n", err)
		return
	}

	headersFlags := FlagEndHeaders
	if len(content) == 0 || headOnly {
		headersFlags |= FlagEndStream
	}

	// 1. Send HEADERS frame
	headersFrame := &Frame{
		Header: FrameHeader{
			Length:   uint32(len(encodedHeaders)),
			Type:     FrameHeaders,
			Flags:    headersFlags,
			StreamID: streamID,
		},
		Payload: encodedHeaders,
	}

	if err := WriteFrame(conn, headersFrame); err != nil {
		log.Printf("[bserve] Error writing HEADERS frame on stream %d: %v\n", streamID, err)
		return
	}

	if headOnly || len(content) == 0 {
		return
	}

	// 2. Send DATA frames (in chunks up to 64 KiB)
	const chunkSize = 64 * 1024
	total := len(content)
	offset := 0

	for offset < total {
		end := offset + chunkSize
		if end > total {
			end = total
		}
		chunk := content[offset:end]

		dataFlags := uint8(0)
		if end == total {
			dataFlags |= FlagEndStream
		}

		dataFrame := &Frame{
			Header: FrameHeader{
				Length:   uint32(len(chunk)),
				Type:     FrameData,
				Flags:    dataFlags,
				StreamID: streamID,
			},
			Payload: chunk,
		}

		if err := WriteFrame(conn, dataFrame); err != nil {
			log.Printf("[bserve] Error writing DATA frame on stream %d: %v\n", streamID, err)
			return
		}

		offset = end
	}
}

// sendErrorResponse transmits an error HEADERS frame and DATA body.
func (s *Server) sendErrorResponse(conn net.Conn, streamID uint32, statusCode int, message string) {
	statusStr := strconv.Itoa(statusCode)
	body := []byte(message + "\n")

	respHeaders := HeaderBlock{
		{Name: ":status", Value: statusStr},
		{Name: "content-type", Value: "text/plain; charset=utf-8"},
		{Name: "content-length", Value: strconv.Itoa(len(body))},
		{Name: "server", Value: "bserve/1.0"},
		{Name: "date", Value: time.Now().UTC().Format(http.TimeFormat)},
		{Name: "connection", Value: "keep-alive"},
	}

	encodedHeaders, _ := EncodeHeaderBlock(respHeaders)

	headersFrame := &Frame{
		Header: FrameHeader{
			Length:   uint32(len(encodedHeaders)),
			Type:     FrameHeaders,
			Flags:    FlagEndHeaders,
			StreamID: streamID,
		},
		Payload: encodedHeaders,
	}

	dataFrame := &Frame{
		Header: FrameHeader{
			Length:   uint32(len(body)),
			Type:     FrameData,
			Flags:    FlagEndStream,
			StreamID: streamID,
		},
		Payload: body,
	}

	_ = WriteFrame(conn, headersFrame)
	_ = WriteFrame(conn, dataFrame)
}

// detectMimeType provides robust MIME type detection from file extension and content.
func detectMimeType(filePath string, content []byte) string {
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".html", ".htm":
		return "text/html; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".js":
		return "application/javascript; charset=utf-8"
	case ".json":
		return "application/json; charset=utf-8"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".svg":
		return "image/svg+xml"
	case ".txt":
		return "text/plain; charset=utf-8"
	case ".pdf":
		return "application/pdf"
	case ".xml":
		return "application/xml"
	default:
		ctype := mime.TypeByExtension(ext)
		if ctype != "" {
			return ctype
		}
		if len(content) > 0 {
			return http.DetectContentType(content)
		}
		return "application/octet-stream"
	}
}
