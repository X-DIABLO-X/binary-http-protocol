# BHTTP/1.0 Protocol Specification
**Network Architecture Course Project — Binary HTTP Protocol**  
**Authors:** Harshit Tiwari (harshit.tiwari080805@gmail.com)  
**Status:** Standard / Informational  
**Date:** September 2026  

---

## 1. Introduction and Scope

BHTTP/1.0 (Binary Hypertext Transfer Protocol) is an application-layer, binary-framed protocol designed to provide clean, high-performance, and multiplexed-capable resource transfer over persistent TCP connections. Unlike text-based HTTP/1.1, BHTTP eliminates delimited line parsing (`\r\n`), ambiguous chunked transfer encodings, and textual header bloat through a deterministic fixed-width binary frame header and an HPACK-lite header compression scheme.

This specification provides all necessary technical rules for an independent developer to implement fully interoperable conforming clients and servers.

The key words **"MUST"**, **"MUST NOT"**, **"REQUIRED"**, **"SHALL"**, **"SHALL NOT"**, **"SHOULD"**, **"SHOULD NOT"**, and **"MAY"** in this document are to be interpreted as described in BCP 14 / RFC 2119.

---

## 2. Connection Management & Lifecycle

1. **Transport Layer:** BHTTP runs directly over an underlying reliable, ordered byte stream (standard TCP).
2. **Persistent Connections:** 
   - A server **MUST** keep the TCP connection open after serving a request to allow subsequent requests without reconnection.
   - A client **MUST** reuse the existing TCP connection for multiple requests destined for the same host and port, and **MUST NOT** open a second connection unless the current connection is terminated by the server or an unrecoverable transport error occurs.
3. **Stream Identifiers:** 
   - Requests and responses are associated with a stream.
   - Client-initiated streams **MUST** use odd integer identifiers (1, 3, 5, ...), incremented monotonically per request.
   - Stream ID `0` is reserved for connection-level control frames (e.g., connection-wide `PING` or `GOAWAY`).

---

## 3. Binary Frame Header Format

All communication in BHTTP occurs in discrete binary units called **frames**. Every frame begins with an **8-byte (64-bit)** fixed-size frame header in Network Byte Order (Big-Endian), followed by a variable-length payload.

```
 0                   1                   2                   3
 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                       Length (24)             |   Type (8)    |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|   Flags (8)   |                 Stream ID (24)                |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                         Payload (...)                         |
+---------------------------------------------------------------+
```

### 3.1 Header Fields
- **Length (24 bits):** The length of the frame payload in octets, represented as an unsigned 24-bit integer. The 8-byte frame header is not counted in this value. Maximum payload size is $2^{24}-1 = 16,777,215$ bytes (16 MiB).
- **Type (8 bits):** The frame type determining the semantics of the payload.
- **Flags (8 bits):** Frame-type-specific boolean flags. Flags not defined for a given frame type **MUST** be ignored upon receipt and set to `0x00` upon transmission.
- **Stream ID (24 bits):** An unsigned 24-bit integer identifying the logical stream associated with this frame.

### 3.2 Architectural Defense: Field Width Selection (vs. HTTP/2)
HTTP/2 selected a 9-byte header: **24-bit Length / 8-bit Type / 8-bit Flags / 31-bit Stream ID** (RFC 7540 §4.1). BHTTP selects an **8-byte header (24 / 8 / 8 / 24)** for the following concrete reasons:

1. **64-Bit Memory Alignment:** 8 bytes aligns precisely to machine word and cache-line boundaries on modern 64-bit architectures (x86_64, ARM64), eliminating unaligned access overhead and padding penalties during struct deserialization in systems languages (Go, C, Rust).
2. **24-bit Length (16 MiB):** As in HTTP/2, 16 bits (64 KiB) is overly restrictive for modern high-bandwidth transfers, necessitating excessive frame header overhead for images or static assets. Conversely, 32 bits (4 GiB) would allow malicious peers to monopolize receiver buffers and induce head-of-line blocking. 24 bits represents the optimal engineering balance.
3. **8-bit Type & Flags:** Provides 256 frame types and 8 orthogonal flags per type, ensuring vast headroom for protocol extensions.
4. **24-bit Stream ID (8.3M Streams):** A 24-bit field allows up to $2^{23} = 8,388,608$ unique client-initiated streams over a single persistent TCP connection. Even at 100 requests per second continuously, a connection can remain open for over 23 hours before ID exhaustion, saving 1 byte on every single frame transmitted compared to HTTP/2's 31-bit field.

---

## 4. Frame Types and Processing Rules

### 4.1 Frame Types
| Type ID | Name | Description |
|---|---|---|
| `0x01` | **HEADERS** | Carries HPACK-lite encoded request or response headers. |
| `0x02` | **DATA** | Carries raw binary payload bytes (e.g. file content). |
| `0x03` | **GOAWAY** | Initiates connection teardown or signals unrecoverable protocol error. |
| `0x04` | **PING** | Connection liveness probe and RTT measurement. |

### 4.2 Flags
- `END_STREAM (0x01)`: Valid on `HEADERS` and `DATA`. Signals that this frame is the final frame sent by the sender for the given `Stream ID`.
- `END_HEADERS (0x04)`: Valid on `HEADERS`. Signals the complete header block is contained within this frame.
- `ACK (0x01)`: Valid on `PING`. Echo response to an incoming PING.

### 4.3 Mandatory Forward-Compatibility Rule (Extensibility)
> **CRITICAL EXTENSION INVARIANT:**  
> A receiver that encounters a frame with an unrecognized **Type** **MUST cleanly skip** the frame by reading and discarding exactly `Length` octets from the connection. The receiver **MUST NOT** terminate the connection, **MUST NOT** treat unknown frame types as malformed, and **MUST** proceed to process subsequent frames.

This invariant guarantees safe future evolution to BHTTP/2.0 without breaking legacy BHTTP/1.0 nodes.

---

## 5. Header Encoding (HPACK-Lite)

BHTTP utilizes HPACK's first two foundational mechanisms: static table indexing and length-prefixed literals.

### 5.1 Static Name Table
Ten high-frequency header names are assigned 1-byte indices (`0x01` to `0x0A`):

| Index | Header Name | Primary Role |
|:---:|---|---|
| `0x01` | `:method` | HTTP Request Method (e.g., `GET`, `POST`) |
| `0x02` | `:path` | Request URI Path (e.g., `/index.html`) |
| `0x03` | `:status` | Response Status Code (e.g., `200`, `404`, `400`) |
| `0x04` | `content-type` | MIME type (e.g., `text/html; charset=utf-8`) |
| `0x05` | `content-length` | Payload length in decimal octets |
| `0x06` | `host` | Target host and port (`localhost:9000`) |
| `0x07` | `user-agent` | Client identification (`bcurl/1.0`) |
| `0x08` | `server` | Server identification (`bserve/1.0`) |
| `0x09` | `date` | HTTP timestamp (RFC 1123 format) |
| `0x0A` | `connection` | Connection state hint (`keep-alive`) |

### 5.2 Header Entry Wire Format
A header block payload consists of a sequence of encoded header fields until the `Length` octets of the `HEADERS` frame payload are exhausted. Each field entry is encoded in one of two formats:

#### Format A: Indexed Name (Index `0x01` - `0x0A`)
Used when the header name matches an entry in the static table:
```
+---------------+-------------------------------+---------------------+
| Index (1 byte)| Value Length (2 bytes, uint16)| Value String (UTF-8)|
+---------------+-------------------------------+---------------------+
```
1. `Index`: 1 octet (`0x01` to `0x0A`).
2. `Value Length`: 2 octets, Big-Endian unsigned integer.
3. `Value String`: Raw bytes of length `Value Length`.

#### Format B: Literal Name (Index `0x00`)
Used when transmitting custom or non-indexed header names:
```
+------+-----------------------+---------------------+-----------------------+---------------------+
| 0x00 | Name Len (2B, uint16) | Name String (UTF-8) | Val Len (2B, uint16)  | Val String (UTF-8)  |
+------+-----------------------+---------------------+-----------------------+---------------------+
```
1. `0x00`: Marker octet indicating literal name follows.
2. `Name Length`: 2 octets, Big-Endian unsigned integer.
3. `Name String`: UTF-8 string of length `Name Length` (lowercased).
4. `Value Length`: 2 octets, Big-Endian unsigned integer.
5. `Value String`: Raw bytes of length `Value Length`.

---

## 6. Client and Server Behavioral Semantics

### 6.1 Request Flow (Client Track: `bcurl`)
1. Resolves target host and port, establishes a single TCP socket.
2. Formats a `HEADERS` frame with `Stream ID = 1` (or next odd number), `Type = 0x01`, `Flags = 0x05` (`END_STREAM | END_HEADERS`).
3. Encodes mandatory request headers: `:method` (`GET`), `:path` (e.g. `/index.html`), `host`, and `user-agent`.
4. Reads incoming frames on the socket:
   - On `HEADERS` frame: decodes headers, extracts `:status`.
   - On `DATA` frame: streams payload bytes directly to standard output.
   - If an unknown frame type is received: cleanly reads and discards `Length` bytes.
   - Halts reading when a frame with `END_STREAM (0x01)` is received.
5. Exits with code `0` on status `2xx`/`3xx`; exits non-zero on `4xx`/`5xx`.

### 6.2 Serving Resources (Server Track: `bserve`)
1. Listens on specified port, accepts incoming TCP connections.
2. Enters persistent connection loop reading frames for the connection.
3. Upon receiving a `HEADERS` frame:
   - Parses `:method` and `:path`.
   - Sanitizes `:path` (resolving `.` and `..`) to strictly prevent directory traversal outside the served root. If a traversal attack or malformed header is detected, replies with status `400 Bad Request`.
   - Resolves target file: if requesting a directory (e.g. `/`), appends `index.html`.
   - If file does not exist or cannot be read: responds with status `404 Not Found`.
   - If file exists: opens file, detects MIME type, reads bytes.
4. Response Transmission:
   - Sends `HEADERS` frame on the same `Stream ID` with `:status`, `content-type`, `content-length`, `server`, and `date`. If file size is 0, sets `END_STREAM`.
   - If file size > 0, sends one or more `DATA` frames containing file bytes. The final `DATA` frame carries `END_STREAM (0x01)`.
5. Keeps the TCP connection open to service subsequent frames on new streams.

### 6.3 Error Codes and Handling
- **Malformed Frame Header / Truncated Payload:** Server replies with `400 Bad Request` or transmits `GOAWAY` frame (`Type = 0x03`) and closes socket.
- **Missing File:** Responds with `:status 404`, `content-type text/plain`, and body `404 Not Found\n`.
- **Unsupported Methods:** Responds with `:status 405 Method Not Allowed`.

---

## 7. Compliance Verification Checklist
- [x] Frame header is exactly 8 octets, fully defensible.
- [x] Unrecognized frame types are cleanly skipped without closing connection.
- [x] Ten static header names are indexed as single bytes; literals are length-prefixed.
- [x] Connection is kept open and reused; client never opens a second connection for same session.
- [x] Status codes 4xx/5xx cause client to exit non-zero.
- [x] Both ends can produce and verify annotated hexdumps.
