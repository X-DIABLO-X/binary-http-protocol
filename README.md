# BHTTP/1.0 — HTTP, In Binary

[![Go Tests](https://img.shields.io/badge/tests-passing-brightgreen.svg)]()
[![Spec Conformance](https://img.shields.io/badge/RFC-BHTTP%2F1.0-blue.svg)]()
[![License](https://img.shields.io/badge/license-MIT-green.svg)]()

> **Course Project:** *HTTP, in Binary • Two Tracks, One Protocol*  
> **"In pairs: one server, one client, and the only thing that crosses between you is the spec. A client that only works against your own server is an implementation, not a protocol."**

---

## 📖 Table of Contents
1. [Overview & Deliverables](#overview--deliverables)
2. [Protocol Architecture](#protocol-architecture)
3. [The Frame Header & Architectural Defense](#the-frame-header--architectural-defense)
4. [HPACK-Lite Header Compression](#hpack-lite-header-compression)
5. [Track 1 — The Server (`bserve`)](#track-1--the-server-bserve)
6. [Track 2 — The Client (`bcurl`)](#track-2--the-client-bcurl)
7. [Extensibility & Forward Compatibility](#extensibility--forward-compatibility)
8. [Annotated Hexdump Breakdown](#annotated-hexdump-breakdown)
9. [Independent "Stranger" Client Verification](#independent-stranger-client-verification)
10. [Quickstart & Usage](#quickstart--usage)

---

## 🎯 Overview & Deliverables

This repository contains the complete specification and reference implementation for **BHTTP/1.0** (Binary Hypertext Transfer Protocol), an application-layer binary protocol built over persistent TCP connections.

### Hand-in Deliverables Checklist
1. **[SPEC.md](SPEC.md):** The two-page formal protocol specification, unambiguous and self-contained for any third-party implementer.
2. **Programs:**
   - **Track 1:** `./bserve` (Persistent TCP static file server with path routing, traversal guards, 404/400 handling).
   - **Track 2:** `./bcurl` (Binary client with `-v` frame hexdumping, persistent socket reuse, non-zero exit on 4xx/5xx).
3. **[ANNOTATED_HEXDUMP.md](ANNOTATED_HEXDUMP.md):** Complete byte-by-byte visual annotation of request and response wire bytes.
4. **Stranger Interoperability:** An independent Python client (`tests/stranger_client.py`) written strictly from `SPEC.md` without sharing code.

---

## 🧱 Protocol Architecture

```
+-------------------------------------------------------------------------+
|                              BHTTP/1.0                                  |
|   Persistent TCP Connection (Reused across multiple request streams)    |
+-------------------------------------------------------------------------+
       |                                                    ^
       | Request Stream 1 (HEADERS frame, END_STREAM)       | Response Stream 1
       v                                                    | (HEADERS + DATA)
+----------------+                                   +----------------+
|  Track 2 Client|                                   |  Track 1 Server|
|    ./bcurl     |                                   |    ./bserve    |
+----------------+                                   +----------------+
       |                                                    ^
       | Request Stream 3 (HEADERS frame, END_STREAM)       | Response Stream 3
       v (SAME TCP SOCKET — NEVER RECONNECTED)              | (HEADERS + DATA)
```

---

## 🔬 The Frame Header & Architectural Defense

Every frame in BHTTP starts with an **8-byte (64-bit)** fixed header transmitted in Network Byte Order (Big-Endian):

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

### Why did HTTP/2 choose 24 / 8 / 8 / 31 (9 Bytes)?
- **24-bit Length (16 MiB max payload):** Large enough to prevent unnecessary chunking overhead for images and video blocks, yet tight enough to prevent single frames from monopolizing TCP buffers and causing head-of-line blocking on multiplexed streams.
- **8-bit Type (256 types):** Generous headroom for core frames and future experimental types.
- **8-bit Flags:** 8 orthogonal bit flags per frame type.
- **31-bit Stream ID (2.1 billion streams):** Web browsers communicating with internet-scale CDNs keep single TCP/TLS connections open for days across thousands of asset fetches and server pushes. 31 bits ensures stream IDs never roll over during long-lived sessions.

### Why BHTTP/1.0 Defends 24 / 8 / 8 / 24 (8 Bytes, 64-Bit Aligned):
1. **64-bit Word & Cache Alignment:** 8 bytes aligns cleanly with CPU registers, memory buses, and cache lines on modern 64-bit architectures (`x86_64`, `ARM64`), avoiding misaligned memory accesses and structure padding.
2. **24-bit Length (16 MiB):** Retains HTTP/2's ideal payload ceiling, protecting servers against memory exhaustion while eliminating chunk overhead.
3. **24-bit Stream ID (8,388,608 streams):** A persistent BHTTP connection can handle **8.3 million client requests**. Even at 100 requests per second continuously over a single persistent TCP socket, stream IDs will last for over **23 hours** of continuous uninterrupted traffic. Saving 1 full byte per frame across every packet yields noticeable wire-level bandwidth efficiency.

---

## 🗜️ HPACK-Lite Header Compression

BHTTP encodes headers without plain-text line parsing overhead (`\r\n`), using HPACK's two core mechanisms:

### 1. Static Table Indexing (1-Byte Index)
Ten high-frequency header names are assigned 1-byte indices:

| Index | Name | Index | Name |
|:---:|---|:---:|---|
| `0x01` | `:method` | `0x06` | `host` |
| `0x02` | `:path` | `0x07` | `user-agent` |
| `0x03` | `:status` | `0x08` | `server` |
| `0x04` | `content-type` | `0x09` | `date` |
| `0x05` | `content-length` | `0x0A` | `connection` |

- **Format A (Indexed Name):** `[Index (1B)][Value Length (2B, uint16)][Value String (NB)]`
- **Format B (Literal Name):** `[0x00][Name Length (2B)][Name String][Value Length (2B)][Value String]`

---

## 🚀 Track 1 — The Server (`bserve`)

```bash
$ ./bserve ./www 9000
```

- Accepts incoming TCP connections.
- Serves files relative to `./www`.
- Sanitizes paths and blocks directory traversal (`/../../etc/passwd` -> `400 Bad Request`).
- If file is missing -> replies `404 Not Found`.
- If request is malformed -> replies `400 Bad Request`.
- **Keeps the connection open:** Stays in a persistent loop servicing multiple requests over the same socket.

---

## ⚡ Track 2 — The Client (`bcurl`)

```bash
$ ./bcurl -v localhost:9000/index.html
```

- Builds binary request frame on odd stream IDs (`1, 3, 5...`).
- Reads `HEADERS` and `DATA` response frames; streams payload directly to `stdout`.
- With `-v`: prints full annotated byte breakdowns of every transmitted and received frame to `stderr`.
- Exits with a **non-zero status code** when receiving `4xx` or `5xx`.
- **Never opens a second connection:** Reuses the established TCP connection across multiple sequential requests.

---

## 🔮 Extensibility & Forward Compatibility

> **"And one line you may not skip: a receiver meeting a frame type it does not know MUST skip it cleanly. That is how you leave room for a version 2."**

When any conforming BHTTP receiver (client or server) encounters an unrecognized frame type (e.g. `0xFE` or `0x99`):
1. It reads the 8-byte header and extracts `Length`.
2. It discards exactly `Length` bytes from the TCP socket.
3. It does not terminate the connection or desynchronize framing.
4. It seamlessly processes subsequent frames.

---

## 🔍 Annotated Hexdump Breakdown

Here is a snippet from [ANNOTATED_HEXDUMP.md](ANNOTATED_HEXDUMP.md):

```hexdump
0000  00 00 3e 01 05 00 00 01 01 00 03 47 45 54 02 00   |..>........GET..|
0010  0b 2f 69 6e 64 65 78 2e 68 74 6d 6c 06 00 0e 6c   |./index.html...l|
0020  6f 63 61 6c 68 6f 73 74 3a 39 30 30 30 07 00 09   |ocalhost:9000...|
0030  62 63 75 72 6c 2f 31 2e 30 0a 00 0a 6b 65 65 70   |bcurl/1.0...keep|
0040  2d 61 6c 69 76 65                                 |-alive|
```

| Offset (Dec) | Hex Octets | Field / Semantic Interpretation |
|:------------:|:----------:|:--------------------------------|
| `00..02` | `00 00 3E` | **Payload Length:** `62` octets |
| `03` | `01` | **Frame Type:** `HEADERS` (`0x01`) |
| `04` | `05` | **Flags:** `END_STREAM \| END_HEADERS` (`0x05`) |
| `05..07` | `00 00 01` | **Stream ID:** `1` |
| `08` | `01` | **Static Index [1]:** `:method` |
| `09..10` | `00 03` | Value Length: `3` octets |
| `11..13` | `47 45 54` | Value: `"GET"` |
| `14` | `02` | **Static Index [2]:** `:path` |
| `15..16` | `00 0B` | Value Length: `11` octets |
| `17..27` | `2F ... 6C` | Value: `"/index.html"` |

Full request, response headers, and response data breakdowns are available in [ANNOTATED_HEXDUMP.md](ANNOTATED_HEXDUMP.md).

---

## 🧪 Independent "Stranger" Client Verification

To satisfy the foundational protocol principle:
> *"A client that only works against your own server is an implementation, not a protocol."*

An independent Python test client (`tests/stranger_client.py`) was written with zero code shared from the Go implementation, operating purely from `SPEC.md`.

Run the interoperability test:
```bash
# Start server
./bserve ./www 9000 &

# Run stranger client
python tests/stranger_client.py 9000
```

**Results:**
- ✅ Injected unknown frame type `0xFE` skipped cleanly by server
- ✅ Stream 1 `/index.html` retrieved (200 OK)
- ✅ Stream 3 `/style.css` retrieved over persistent socket (200 OK)
- ✅ Stream 5 `/missing.html` returned 404 over persistent socket

---

## 🛠️ Quickstart & Usage

### 1. Build Binaries
```bash
make build
# Or directly:
go build -o bserve.exe ./cmd/bserve
go build -o bcurl.exe ./cmd/bcurl
```

### 2. Run Test Suite
```bash
make test
```

### 3. Launch Server
```bash
./bserve ./www 9000
```

### 4. Fetch Resources
```bash
# Verbose mode with annotated frame hexdumps:
./bcurl -v localhost:9000/index.html

# Piping body output directly:
./bcurl localhost:9000/style.css > downloaded.css
```

---

## 📄 License
MIT License. Created for the Network Architecture Course Project.
