#pragma once

#include "net.hpp"

#include <array>
#include <charconv>
#include <cstddef>
#include <cstdint>
#include <cstring>
#include <stdexcept>
#include <string>
#include <vector>

namespace protocol {

constexpr std::size_t FRAME_HEADER_SIZE = 12;
constexpr std::uint8_t REQUEST = 1;
constexpr std::uint8_t RESPONSE = 2;
constexpr std::uint8_t GET = 1;

struct Header {
  std::string name;
  std::string value;
};

struct Frame {
  std::uint8_t type{};
  std::uint8_t flags{};
  std::uint8_t header_count{};
  std::vector<std::uint8_t> payload;
};

struct Request {
  std::uint8_t method{};
  std::string path;
  std::vector<Header> headers;
  std::vector<std::uint8_t> body;
};

struct Response {
  std::uint16_t status{};
  std::vector<Header> headers;
  std::vector<std::uint8_t> body;
};

inline const std::array<std::string, 10>& static_names() {
  static const std::array<std::string, 10> names = {
      "host", "user-agent", "accept", "content-length", "content-type",
      "connection", "server", "date", "cache-control", "transfer-encoding"};
  return names;
}

inline void put_u16(std::vector<std::uint8_t>& out, std::uint16_t value) {
  out.push_back(static_cast<std::uint8_t>(value >> 8));
  out.push_back(static_cast<std::uint8_t>(value));
}

inline void put_u32(std::vector<std::uint8_t>& out, std::uint32_t value) {
  out.push_back(static_cast<std::uint8_t>(value >> 24));
  out.push_back(static_cast<std::uint8_t>(value >> 16));
  out.push_back(static_cast<std::uint8_t>(value >> 8));
  out.push_back(static_cast<std::uint8_t>(value));
}

inline std::uint16_t get_u16(const std::vector<std::uint8_t>& in, std::size_t& p) {
  if (p + 2 > in.size()) throw std::runtime_error("truncated uint16");
  std::uint16_t value = (static_cast<std::uint16_t>(in[p]) << 8) | in[p + 1];
  p += 2;
  return value;
}

inline std::uint32_t get_u32(const std::vector<std::uint8_t>& in, std::size_t& p) {
  if (p + 4 > in.size()) throw std::runtime_error("truncated uint32");
  std::uint32_t value = (static_cast<std::uint32_t>(in[p]) << 24) |
                        (static_cast<std::uint32_t>(in[p + 1]) << 16) |
                        (static_cast<std::uint32_t>(in[p + 2]) << 8) | in[p + 3];
  p += 4;
  return value;
}

inline void require_size(std::size_t n, std::size_t limit, const char* what) {
  if (n > limit) throw std::runtime_error(std::string(what) + " too long");
}

inline std::vector<std::uint8_t> encode_headers(const std::vector<Header>& headers) {
  if (headers.size() > 255) throw std::runtime_error("too many headers");
  std::vector<std::uint8_t> out;
  for (const auto& header : headers) {
    std::uint8_t index = 0;
    const auto& names = static_names();
    for (std::size_t i = 0; i < names.size(); ++i) {
      if (header.name == names[i]) index = static_cast<std::uint8_t>(i + 1);
    }
    if (index == 0) {
      require_size(header.name.size(), 255, "header name");
      out.push_back(0);
      out.push_back(static_cast<std::uint8_t>(header.name.size()));
      out.insert(out.end(), header.name.begin(), header.name.end());
    } else {
      out.push_back(index);
    }
    require_size(header.value.size(), 65535, "header value");
    put_u16(out, static_cast<std::uint16_t>(header.value.size()));
    out.insert(out.end(), header.value.begin(), header.value.end());
  }
  return out;
}

inline std::vector<Header> decode_headers(const std::vector<std::uint8_t>& in,
                                          std::size_t& p, std::uint8_t count) {
  if (count > 32) throw std::runtime_error("too many headers");
  std::vector<Header> headers;
  const auto& names = static_names();
  for (std::uint8_t i = 0; i < count; ++i) {
    if (p >= in.size()) throw std::runtime_error("truncated header");
    std::uint8_t index = in[p++];
    std::string name;
    if (index == 0) {
      if (p >= in.size()) throw std::runtime_error("truncated header name");
      std::size_t length = in[p++];
      if (p + length > in.size()) throw std::runtime_error("truncated header name");
      name.assign(reinterpret_cast<const char*>(&in[p]), length);
      p += length;
    } else {
      if (index > names.size()) throw std::runtime_error("bad header index");
      name = names[index - 1];
    }
    std::uint16_t length = get_u16(in, p);
    if (p + length > in.size()) throw std::runtime_error("truncated header value");
    std::string value(reinterpret_cast<const char*>(&in[p]), length);
    p += length;
    headers.push_back({std::move(name), std::move(value)});
  }
  return headers;
}

inline std::vector<std::uint8_t> encode_request(const Request& request) {
  require_size(request.path.size(), 65535, "path");
  auto headers = encode_headers(request.headers);
  std::vector<std::uint8_t> out;
  out.push_back(request.method);
  put_u16(out, static_cast<std::uint16_t>(request.path.size()));
  out.insert(out.end(), request.path.begin(), request.path.end());
  out.insert(out.end(), headers.begin(), headers.end());
  put_u32(out, static_cast<std::uint32_t>(request.body.size()));
  out.insert(out.end(), request.body.begin(), request.body.end());
  return out;
}

inline Request decode_request(const Frame& frame) {
  if (frame.type != REQUEST) throw std::runtime_error("not a request");
  std::size_t p = 0;
  if (p >= frame.payload.size()) throw std::runtime_error("missing method");
  Request request;
  request.method = frame.payload[p++];
  std::uint16_t path_length = get_u16(frame.payload, p);
  if (p + path_length > frame.payload.size()) throw std::runtime_error("truncated path");
  request.path.assign(reinterpret_cast<const char*>(&frame.payload[p]), path_length);
  p += path_length;
  request.headers = decode_headers(frame.payload, p, frame.header_count);
  std::uint32_t body_length = get_u32(frame.payload, p);
  if (p + body_length != frame.payload.size()) throw std::runtime_error("bad body length");
  request.body.assign(frame.payload.begin() + static_cast<std::ptrdiff_t>(p), frame.payload.end());
  return request;
}

inline std::vector<std::uint8_t> encode_response(const Response& response) {
  auto headers = encode_headers(response.headers);
  std::vector<std::uint8_t> out;
  put_u16(out, response.status);
  out.insert(out.end(), headers.begin(), headers.end());
  put_u32(out, static_cast<std::uint32_t>(response.body.size()));
  out.insert(out.end(), response.body.begin(), response.body.end());
  return out;
}

inline Response decode_response(const Frame& frame) {
  if (frame.type != RESPONSE) throw std::runtime_error("not a response");
  std::size_t p = 0;
  Response response;
  response.status = get_u16(frame.payload, p);
  response.headers = decode_headers(frame.payload, p, frame.header_count);
  std::uint32_t body_length = get_u32(frame.payload, p);
  if (p + body_length != frame.payload.size()) throw std::runtime_error("bad body length");
  response.body.assign(frame.payload.begin() + static_cast<std::ptrdiff_t>(p), frame.payload.end());
  return response;
}

inline bool read_exact(socket_t socket, void* data, std::size_t size) {
  auto* bytes = static_cast<std::uint8_t*>(data);
  std::size_t read = 0;
  while (read < size) {
#ifdef _WIN32
    int n = ::recv(socket, reinterpret_cast<char*>(bytes + read), static_cast<int>(size - read), 0);
#else
    ssize_t n = ::recv(socket, bytes + read, size - read, 0);
#endif
    if (n == 0) return false;
    if (n < 0) throw std::runtime_error(socket_error_message("recv failed"));
    read += static_cast<std::size_t>(n);
  }
  return true;
}

inline void write_all(socket_t socket, const void* data, std::size_t size) {
  auto* bytes = static_cast<const std::uint8_t*>(data);
  std::size_t written = 0;
  while (written < size) {
#ifdef _WIN32
    int n = ::send(socket, reinterpret_cast<const char*>(bytes + written), static_cast<int>(size - written), 0);
#else
    ssize_t n = ::send(socket, bytes + written, size - written, 0);
#endif
    if (n <= 0) throw std::runtime_error(socket_error_message("send failed"));
    written += static_cast<std::size_t>(n);
  }
}

inline bool read_frame(socket_t socket, Frame& frame) {
  std::array<std::uint8_t, FRAME_HEADER_SIZE> header{};
  if (!read_exact(socket, header.data(), header.size())) return false;
  if (header[0] != 'J' || header[1] != 'C' || header[2] != 1 || header[6] != 0 || header[7] != 0) {
    throw std::runtime_error("invalid frame header");
  }
  std::uint32_t payload_length = (static_cast<std::uint32_t>(header[8]) << 24) |
                                 (static_cast<std::uint32_t>(header[9]) << 16) |
                                 (static_cast<std::uint32_t>(header[10]) << 8) | header[11];
  if (payload_length > 64U * 1024U * 1024U) throw std::runtime_error("frame too large");
  frame.type = header[3];
  frame.flags = header[4];
  frame.header_count = header[5];
  frame.payload.resize(payload_length);
  if (payload_length != 0 && !read_exact(socket, frame.payload.data(), payload_length)) {
    throw std::runtime_error("truncated frame payload");
  }
  return true;
}

inline std::vector<std::uint8_t> encode_frame(const Frame& frame) {
  if (frame.payload.size() > UINT32_MAX) throw std::runtime_error("payload too large");
  std::vector<std::uint8_t> bytes;
  bytes.reserve(FRAME_HEADER_SIZE + frame.payload.size());
  bytes.push_back('J');
  bytes.push_back('C');
  bytes.push_back(1);
  bytes.push_back(frame.type);
  bytes.push_back(frame.flags);
  bytes.push_back(frame.header_count);
  bytes.push_back(0);
  bytes.push_back(0);
  put_u32(bytes, static_cast<std::uint32_t>(frame.payload.size()));
  bytes.insert(bytes.end(), frame.payload.begin(), frame.payload.end());
  return bytes;
}

inline void send_frame(socket_t socket, const Frame& frame) {
  auto bytes = encode_frame(frame);
  write_all(socket, bytes.data(), bytes.size());
}

inline std::vector<std::uint8_t> frame_bytes(const Frame& frame) { return encode_frame(frame); }

inline std::string header_value(const std::vector<Header>& headers, const std::string& name) {
  for (const auto& header : headers) if (header.name == name) return header.value;
  return {};
}

inline bool has_header(const std::vector<Header>& headers, const std::string& name) {
  for (const auto& header : headers) if (header.name == name && !header.value.empty()) return true;
  return false;
}

} // namespace protocol

