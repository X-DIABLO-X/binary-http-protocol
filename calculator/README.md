# JC/1 Binary Calculator and File Server

This is my implementation of the **Build a calculator that stays on the line** assignment.

The project is intentionally small. It uses C++17, TCP sockets, and a binary protocol designed for this assignment. It does not use an HTTP framework or any third-party library.

## What the project does

There are two programs:

- `serve` is the server. It accepts a TCP connection, reads binary request frames, performs calculator operations or serves a file, and keeps the connection open for the next request.
- `b` is the client. It opens one TCP connection, sends one or more requests through it, and prints the response bodies.

The supported calculator routes are:

```text
GET /add?a=2&b=3   -> 200 5
GET /sub?a=10&b=4  -> 200 6
GET /mul?a=6&b=7   -> 200 42
GET /div?a=9&b=3   -> 200 3
```

The server also serves files below the document-root directory passed on the command line.

## Requirements

- C++17 compiler
- CMake 3.16 or newer
- TCP sockets available on the operating system

On Windows, the code uses Winsock. On Linux and other POSIX systems, it uses the normal socket API.

## Build

From the repository directory:

```sh
cmake -S . -B build
cmake --build build --config Release
```

This creates the `serve` and `b` executables. With a single-configuration generator they are usually in `build/`; with Visual Studio they are in `build/Release/`.

## Run the server

Use a document-root directory and a port:

```sh
./serve ./www 9000
```

On Windows, use `serve.exe` instead of `./serve` if needed.

The sample `www/index.html` file is included so that the file-serving path can be tested immediately.

## Run the client

The client command follows the assignment format. The `curl` word is a required subcommand; this is not the system curl program.

Request one calculator result:

```sh
./b curl "localhost:9000/add?a=2&b=3"
```

Request a file:

```sh
./b curl localhost:9000/index.html
```

Send several requests over the same TCP connection:

```sh
./b curl "localhost:9000/add?a=2&b=3" "localhost:9000/sub?a=10&b=4" "localhost:9000/mul?a=6&b=7" "localhost:9000/div?a=9&b=3"
```

Use `-v` to print the complete request and response frames as hexdumps to stderr:

```sh
./b curl -v "localhost:9000/add?a=2&b=3"
```

Response bodies go to stdout. The client exits with a non-zero status if any response is `400` or higher.

## Error behavior

The server returns:

- `400` for malformed frames, missing `Host`, invalid numbers, missing or repeated query parameters, division by zero, request bodies, and unsafe paths.
- `404` for an unknown calculator route or a file that does not exist.
- `405` for methods other than `GET`.

Successful calculator responses contain only the decimal answer, without a newline. File responses contain the exact file bytes.

## Protocol summary

Each frame has a 12-byte big-endian header:

| Bytes | Meaning |
|---|---|
| `0-1` | Magic `JC` |
| `2` | Version `1` |
| `3` | Frame type: request `1`, response `2` |
| `4` | Flags, currently `0` |
| `5` | Number of encoded headers |
| `6-7` | Reserved, must be `0` |
| `8-11` | Payload length |

The payload is read using the declared length, so the receiver knows exactly where one request ends and the next one begins. Header names use a ten-entry static table, while header values are length-prefixed. Unknown frame types are skipped using their payload length so that a later protocol version can add frame types without breaking the stream.

The full wire specification is in [docs/protocol-spec.md](docs/protocol-spec.md) and [docs/protocol-spec.pdf](docs/protocol-spec.pdf).

## Testing the assignment requirements

The implementation was checked with the following cases:

1. All four successful calculator operations.
2. Division by zero (`400`).
3. Invalid numbers and missing parameters (`400`).
4. Unknown `/pow` route (`404`).
5. `POST` request (`405`).
6. Missing `Host` header (`400`).
7. Missing file (`404`).
8. Six requests and six ordered responses on one TCP connection.
9. An unknown frame followed by a valid request.
10. Verbose hexdumps for complete request and response frames.

The actual byte-level example from a client/server run is documented in [docs/annotated-capture.md](docs/annotated-capture.md).

## Submission contents

The repository contains:

```text
CMakeLists.txt
README.md
src/net.hpp
src/protocol.hpp
src/serve.cpp
src/b.cpp
www/index.html
docs/protocol-spec.md
docs/protocol-spec.pdf
docs/annotated-capture.md
```
