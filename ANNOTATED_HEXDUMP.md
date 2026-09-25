# BHTTP/1.0 Annotated Hexdump

This document provides a byte-by-byte annotated hexdump of one complete conforming BHTTP/1.0 request and response exchange between `bcurl` and `bserve`.

> **"If you cannot annotate your own bytes, the spec is not finished."**

---

## 1. Request: Client to Server (`bcurl` -> `bserve`)

The client initiates stream `1` by transmitting a single `HEADERS` frame carrying flags `END_STREAM (0x01)` and `END_HEADERS (0x04)`.

### Frame 1: Request HEADERS (Stream 1)

- **Frame Type:** `HEADERS` (`0x01`)
- **Flags:** `END_STREAM|END_HEADERS` (`0x05`)
- **Stream ID:** `1` (`0x000001`)
- **Payload Length:** `62` octets

```hexdump
0000  00 00 3e 01 05 00 00 01 01 00 03 47 45 54 02 00   |..>........GET..|
0010  0b 2f 69 6e 64 65 78 2e 68 74 6d 6c 06 00 0e 6c   |./index.html...l|
0020  6f 63 61 6c 68 6f 73 74 3a 39 30 30 30 07 00 09   |ocalhost:9000...|
0030  62 63 75 72 6c 2f 31 2e 30 0a 00 0a 6b 65 65 70   |bcurl/1.0...keep|
0040  2d 61 6c 69 76 65                                 |-alive|
```

| Offset (Dec) | Hex Octets | Field / Semantic Interpretation |
|:------------:|:----------:|:--------------------------------|
| `00..02` | `00 00 3E` | **Length:** `62` octets (payload size) |
| `03` | `01` | **Type:** `HEADERS` (`0x01`) |
| `04` | `05` | **Flags:** `END_STREAM|END_HEADERS` (`0x05`) |
| `05..07` | `00 00 01` | **Stream ID:** `1` |
| `08` | `01` | **Static Index [1]:** `:method` |
| `09..10` | `00 03` | Value Length: `3` octets |
| `11..13` | `47 45 54` | Value: `"GET"` |
| `14` | `02` | **Static Index [2]:** `:path` |
| `15..16` | `00 0B` | Value Length: `11` octets |
| `17..27` | `2F 69 6E 64 65 78 2E 68 74 6D 6C` | Value: `"/index.html"` |
| `28` | `06` | **Static Index [6]:** `host` |
| `29..30` | `00 0E` | Value Length: `14` octets |
| `31..44` | `6C 6F 63 61 6C 68 6F 73 74 3A 39 30 30 30` | Value: `"localhost:9000"` |
| `45` | `07` | **Static Index [7]:** `user-agent` |
| `46..47` | `00 09` | Value Length: `9` octets |
| `48..56` | `62 63 75 72 6C 2F 31 2E 30` | Value: `"bcurl/1.0"` |
| `57` | `0A` | **Static Index [10]:** `connection` |
| `58..59` | `00 0A` | Value Length: `10` octets |
| `60..69` | `6B 65 65 70 2D 61 6C 69 76 65` | Value: `"keep-alive"` |

---

## 2. Response: Server to Client (`bserve` -> `bcurl`)

The server replies on the same stream (`1`) with two frames: a `HEADERS` frame containing metadata, followed by a `DATA` frame with the file contents.

### Frame 2: Response HEADERS (Stream 1)

- **Frame Type:** `HEADERS` (`0x01`)
- **Flags:** `END_HEADERS` (`0x04`)
- **Stream ID:** `1` (`0x000001`)
- **Payload Length:** `96` octets

```hexdump
0000  00 00 60 01 04 00 00 01 03 00 03 32 30 30 04 00   |..`........200..|
0010  18 74 65 78 74 2f 68 74 6d 6c 3b 20 63 68 61 72   |.text/html; char|
0020  73 65 74 3d 75 74 66 2d 38 05 00 02 36 37 08 00   |set=utf-8...67..|
0030  0a 62 73 65 72 76 65 2f 31 2e 30 09 00 1d 46 72   |.bserve/1.0...Fr|
0040  69 2c 20 32 35 20 53 65 70 20 32 30 32 36 20 31   |i, 25 Sep 2026 1|
0050  32 3a 34 35 3a 30 30 20 47 4d 54 0a 00 0a 6b 65   |2:45:00 GMT...ke|
0060  65 70 2d 61 6c 69 76 65                           |ep-alive|
```

| Offset (Dec) | Hex Octets | Field / Semantic Interpretation |
|:------------:|:----------:|:--------------------------------|
| `00..02` | `00 00 60` | **Length:** `96` octets (payload size) |
| `03` | `01` | **Type:** `HEADERS` (`0x01`) |
| `04` | `04` | **Flags:** `END_HEADERS` (`0x04`) |
| `05..07` | `00 00 01` | **Stream ID:** `1` |
| `08` | `03` | **Static Index [3]:** `:status` |
| `09..10` | `00 03` | Value Length: `3` octets |
| `11..13` | `32 30 30` | Value: `"200"` |
| `14` | `04` | **Static Index [4]:** `content-type` |
| `15..16` | `00 18` | Value Length: `24` octets |
| `17..40` | `74 65 78 74 2F 68 74 6D 6C 3B 20 63 68 61 72 73 65 74 3D 75 74 66 2D 38` | Value: `"text/html; charset=utf-8"` |
| `41` | `05` | **Static Index [5]:** `content-length` |
| `42..43` | `00 02` | Value Length: `2` octets |
| `44..45` | `36 37` | Value: `"67"` |
| `46` | `08` | **Static Index [8]:** `server` |
| `47..48` | `00 0A` | Value Length: `10` octets |
| `49..58` | `62 73 65 72 76 65 2F 31 2E 30` | Value: `"bserve/1.0"` |
| `59` | `09` | **Static Index [9]:** `date` |
| `60..61` | `00 1D` | Value Length: `29` octets |
| `62..90` | `46 72 69 2C 20 32 35 20 53 65 70 20 32 30 32 36 20 31 32 3A 34 35 3A 30 30 20 47 4D 54` | Value: `"Fri, 25 Sep 2026 12:45:00 GMT"` |
| `91` | `0A` | **Static Index [10]:** `connection` |
| `92..93` | `00 0A` | Value Length: `10` octets |
| `94..103` | `6B 65 65 70 2D 61 6C 69 76 65` | Value: `"keep-alive"` |

### Frame 3: Response DATA (Stream 1)

- **Frame Type:** `DATA` (`0x02`)
- **Flags:** `END_STREAM` (`0x01`)
- **Stream ID:** `1` (`0x000001`)
- **Payload Length:** `67` octets

```hexdump
0000  00 00 43 02 01 00 00 01 3c 21 44 4f 43 54 59 50   |..C.....<!DOCTYP|
0010  45 20 68 74 6d 6c 3e 0a 3c 68 74 6d 6c 3e 0a 3c   |E html>.<html>.<|
0020  62 6f 64 79 3e 0a 3c 68 31 3e 48 65 6c 6c 6f 20   |body>.<h1>Hello |
0030  42 48 54 54 50 3c 2f 68 31 3e 0a 3c 2f 62 6f 64   |BHTTP</h1>.</bod|
0040  79 3e 0a 3c 2f 68 74 6d 6c 3e 0a                  |y>.</html>.|
```

| Offset (Dec) | Hex Octets | Field / Semantic Interpretation |
|:------------:|:----------:|:--------------------------------|
| `00..02` | `00 00 43` | **Length:** `67` octets (payload size) |
| `03` | `02` | **Type:** `DATA` (`0x02`) |
| `04` | `01` | **Flags:** `END_STREAM` (`0x01`) |
| `05..07` | `00 00 01` | **Stream ID:** `1` |
| `08..74` | *payload* | Data Bytes: `"<!DOCTYPE html>\n<html>\n<body>\n<h1>Hello BHTTP</h1>\n</body>\n</html>\n"` |

---

## 3. Structural Summary & Field Verification

### Frame Header Layout (64-Bit / 8-Octet Alignment)

```
 0                   1                   2                   3
 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                       Length (24)             |   Type (8)    |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|   Flags (8)   |                 Stream ID (24)                |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
```

### HPACK-Lite Static Index Matching

| Header Name | Index | Wire Representation |
|:---|:---:|:---|
| `:method` | `0x01` | `[01] [00 03] [47 45 54]` (`"GET"`) |
| `:path` | `0x02` | `[02] [00 0B] [2F 69 6E 64 65 78 2E 68 74 6D 6C]` (`"/index.html"`) |
| `:status` | `0x03` | `[03] [00 03] [32 30 30]` (`"200"`) |
| `content-type` | `0x04` | `[04] [00 18] [...]` (`"text/html; charset=utf-8"`) |
| `content-length` | `0x05` | `[05] [00 02] [36 34]` (`"64"`) |
| `host` | `0x06` | `[06] [00 0E] [...]` (`"localhost:9000"`) |
| `user-agent` | `0x07` | `[07] [00 09] [...]` (`"bcurl/1.0"`) |
| `server` | `0x08` | `[08] [00 0A] [...]` (`"bserve/1.0"`) |
| `date` | `0x09` | `[09] [00 1D] [...]` (`"Fri, 25 Sep 2026 12:45:00 GMT"`) |
| `connection` | `0x0A` | `[0A] [00 0A] [...]` (`"keep-alive"`) |

### Forward Compatibility / Extension Invariant

When any receiver encounters an unknown Frame Type (e.g. `0x99` or `0xFE` in BHTTP/2), it extracts the 24-bit Length from octets `00..02`, discards exactly that number of octets, and continues reading the stream without desynchronization or dropping the TCP connection.
