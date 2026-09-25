#!/usr/bin/env python3
"""
stranger_client.py - An independent BHTTP/1.0 client implemented in pure Python.
Written solely by reading SPEC.md to prove cross-language, independent interoperability.
"A client that only works against your own server is an implementation, not a protocol."
"""

import socket
import struct
import sys

STATIC_TABLE = [
    "",
    ":method",
    ":path",
    ":status",
    "content-type",
    "content-length",
    "host",
    "user-agent",
    "server",
    "date",
    "connection"
]
NAME_TO_IDX = {name: i for i, name in enumerate(STATIC_TABLE) if i > 0}

def pack_frame(frame_type: int, flags: int, stream_id: int, payload: bytes) -> bytes:
    length = len(payload)
    if length > 0xFFFFFF:
        raise ValueError("Payload exceeds 24 bits")
    # 8-byte fixed header: Length (24) | Type (8) | Flags (8) | StreamID (24)
    hdr = bytearray(8)
    hdr[0] = (length >> 16) & 0xFF
    hdr[1] = (length >> 8) & 0xFF
    hdr[2] = length & 0xFF
    hdr[3] = frame_type & 0xFF
    hdr[4] = flags & 0xFF
    hdr[5] = (stream_id >> 16) & 0xFF
    hdr[6] = (stream_id >> 8) & 0xFF
    hdr[7] = stream_id & 0xFF
    return bytes(hdr) + payload

def unpack_frame_header(hdr_bytes: bytes):
    if len(hdr_bytes) < 8:
        raise ValueError("Truncated header")
    length = (hdr_bytes[0] << 16) | (hdr_bytes[1] << 8) | hdr_bytes[2]
    frame_type = hdr_bytes[3]
    flags = hdr_bytes[4]
    stream_id = (hdr_bytes[5] << 16) | (hdr_bytes[6] << 8) | hdr_bytes[7]
    return length, frame_type, flags, stream_id

def encode_headers(headers: list) -> bytes:
    buf = bytearray()
    for name, value in headers:
        name_lower = name.lower()
        val_bytes = value.encode('utf-8')
        if name_lower in NAME_TO_IDX:
            idx = NAME_TO_IDX[name_lower]
            buf.append(idx)
            buf.extend(struct.pack('>H', len(val_bytes)))
            buf.extend(val_bytes)
        else:
            buf.append(0x00) # Literal
            name_bytes = name_lower.encode('utf-8')
            buf.extend(struct.pack('>H', len(name_bytes)))
            buf.extend(name_bytes)
            buf.extend(struct.pack('>H', len(val_bytes)))
            buf.extend(val_bytes)
    return bytes(buf)

def decode_headers(payload: bytes) -> dict:
    headers = {}
    pos = 0
    total = len(payload)
    while pos < total:
        idx = payload[pos]
        pos += 1
        if 1 <= idx < len(STATIC_TABLE):
            name = STATIC_TABLE[idx]
            v_len = struct.unpack('>H', payload[pos:pos+2])[0]
            pos += 2
            val = payload[pos:pos+v_len].decode('utf-8')
            pos += v_len
            headers[name] = val
        elif idx == 0:
            n_len = struct.unpack('>H', payload[pos:pos+2])[0]
            pos += 2
            name = payload[pos:pos+n_len].decode('utf-8')
            pos += n_len
            v_len = struct.unpack('>H', payload[pos:pos+2])[0]
            pos += 2
            val = payload[pos:pos+v_len].decode('utf-8')
            pos += v_len
            headers[name] = val
        else:
            raise ValueError(f"Unknown index {idx}")
    return headers

def read_exact(sock, n):
    data = bytearray()
    while len(data) < n:
        chunk = sock.recv(n - len(data))
        if not chunk:
            raise EOFError("Socket closed prematurely")
        data.extend(chunk)
    return bytes(data)

def run_stranger_tests(host: str, port: int):
    print(f"[stranger_client] Connecting to {host}:{port} via raw TCP...")
    sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    sock.connect((host, port))

    try:
        # TEST 1: Forward-compatibility test - Send unknown frame type 0xFE before request
        print("[stranger_client] [Test 1] Transmitting unknown frame type 0xFE (Future v2 extension)...")
        unknown_frame = pack_frame(frame_type=0xFE, flags=0x00, stream_id=1, payload=b"future-v2-experimental-payload")
        sock.sendall(unknown_frame)

        # TEST 2: Request GET /index.html on stream 1
        print("[stranger_client] [Test 2] Transmitting GET /index.html on stream 1...")
        req_hdrs = [
            (":method", "GET"),
            (":path", "/index.html"),
            ("host", f"{host}:{port}"),
            ("user-agent", "StrangerPythonClient/1.0"),
            ("x-stranger-protocol", "interoperability-verified") # Literal header
        ]
        headers_payload = encode_headers(req_hdrs)
        # Type 0x01 (HEADERS), Flags 0x05 (END_STREAM | END_HEADERS), Stream 1
        req_frame = pack_frame(frame_type=0x01, flags=0x05, stream_id=1, payload=headers_payload)
        sock.sendall(req_frame)

        # Read response for Stream 1
        status = None
        body = bytearray()
        while True:
            hdr_bytes = read_exact(sock, 8)
            length, f_type, flags, s_id = unpack_frame_header(hdr_bytes)
            payload = read_exact(sock, length) if length > 0 else b""

            if f_type == 0x01: # HEADERS
                resp_hdrs = decode_headers(payload)
                status = resp_hdrs.get(":status")
                print(f"[stranger_client] Received HEADERS: status={status}, content-type={resp_hdrs.get('content-type')}")
            elif f_type == 0x02: # DATA
                body.extend(payload)
            elif f_type not in (0x01, 0x02, 0x03, 0x04):
                print(f"[stranger_client] Cleanly skipped unknown frame 0x{f_type:02X}")

            if (flags & 0x01) != 0: # END_STREAM
                break

        assert status == "200", f"Expected status 200, got {status}"
        assert b"Welcome to BHTTP/1.0" in body or b"Hello" in body, "Expected body content missing"
        print(f"[stranger_client] SUCCESS: Retrieved {len(body)} bytes for /index.html with status 200!")

        # TEST 3: Persistent Connection - Send second request on the SAME socket (stream 3)
        print("[stranger_client] [Test 3] Testing persistent connection: GET /style.css on same socket (stream 3)...")
        req_hdrs2 = [
            (":method", "GET"),
            (":path", "/style.css"),
            ("host", f"{host}:{port}"),
            ("user-agent", "StrangerPythonClient/1.0")
        ]
        req_frame2 = pack_frame(frame_type=0x01, flags=0x05, stream_id=3, payload=encode_headers(req_hdrs2))
        sock.sendall(req_frame2)

        status2 = None
        body2 = bytearray()
        while True:
            hdr_bytes = read_exact(sock, 8)
            length, f_type, flags, s_id = unpack_frame_header(hdr_bytes)
            payload = read_exact(sock, length) if length > 0 else b""

            if f_type == 0x01:
                resp_hdrs = decode_headers(payload)
                status2 = resp_hdrs.get(":status")
            elif f_type == 0x02:
                body2.extend(payload)

            if (flags & 0x01) != 0:
                break

        assert status2 == "200", f"Expected status 200, got {status2}"
        assert len(body2) > 0, "Empty css body"
        print(f"[stranger_client] SUCCESS: Persistent connection confirmed! Retrieved /style.css ({len(body2)} bytes)")

        # TEST 4: 404 Not Found on same socket (stream 5)
        print("[stranger_client] [Test 4] Requesting nonexistent file /missing.html on stream 5...")
        req_hdrs3 = [
            (":method", "GET"),
            (":path", "/missing.html"),
            ("host", f"{host}:{port}"),
            ("user-agent", "StrangerPythonClient/1.0")
        ]
        sock.sendall(pack_frame(frame_type=0x01, flags=0x05, stream_id=5, payload=encode_headers(req_hdrs3)))

        status3 = None
        body3 = bytearray()
        while True:
            hdr_bytes = read_exact(sock, 8)
            length, f_type, flags, s_id = unpack_frame_header(hdr_bytes)
            payload = read_exact(sock, length) if length > 0 else b""
            if f_type == 0x01:
                status3 = decode_headers(payload).get(":status")
            elif f_type == 0x02:
                body3.extend(payload)
            if (flags & 0x01) != 0:
                break

        assert status3 == "404", f"Expected status 404, got {status3}"
        print(f"[stranger_client] SUCCESS: 404 response verified ({body3.decode('utf-8', errors='ignore').strip()})")

        print("\nALL STRANGER CLIENT SPEC INTEROPERABILITY TESTS PASSED 100%!")

    finally:
        sock.close()

if __name__ == "__main__":
    p = int(sys.argv[1]) if len(sys.argv) > 1 else 9000
    h = sys.argv[2] if len(sys.argv) > 2 else "127.0.0.1"
    run_stranger_tests(h, p)
