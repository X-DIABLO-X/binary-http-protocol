#include "protocol.hpp"

#include <iomanip>
#include <iostream>
#include <sstream>
#include <string>
#include <vector>

static void dump_hex(const std::string& label, const std::vector<std::uint8_t>& bytes) {
  std::cerr << label << " (" << bytes.size() << " bytes)\n";
  for (std::size_t offset = 0; offset < bytes.size(); offset += 16) {
    std::cerr << "  " << std::hex << std::setw(4) << std::setfill('0') << offset << "  ";
    for (std::size_t i = 0; i < 16; ++i) {
      if (offset + i < bytes.size()) std::cerr << std::setw(2) << static_cast<unsigned>(bytes[offset + i]) << ' ';
      else std::cerr << "   ";
    }
    std::cerr << " |";
    for (std::size_t i = 0; i < 16 && offset + i < bytes.size(); ++i) {
      auto c = bytes[offset + i];
      std::cerr << (c >= 32 && c <= 126 ? static_cast<char>(c) : '.');
    }
    std::cerr << "|\n";
  }
  std::cerr << std::dec;
}

struct Endpoint { std::string host; std::string port; std::string path; };

static Endpoint parse_endpoint(const std::string& text) {
  auto colon = text.rfind(':');
  if (colon == std::string::npos || colon == 0 || colon + 1 >= text.size()) throw std::runtime_error("bad endpoint");
  auto slash = text.find('/', colon + 1);
  if (slash == std::string::npos || slash == text.size() - 1) throw std::runtime_error("endpoint needs a path");
  return {text.substr(0, colon), text.substr(colon + 1, slash - colon - 1), text.substr(slash)};
}

static socket_t connect_to(const Endpoint& endpoint) {
  addrinfo hints{};
  hints.ai_family = AF_UNSPEC;
  hints.ai_socktype = SOCK_STREAM;
  addrinfo* results = nullptr;
  if (getaddrinfo(endpoint.host.c_str(), endpoint.port.c_str(), &hints, &results) != 0) throw std::runtime_error("getaddrinfo failed");
  socket_t socket = invalid_socket;
  for (auto* item = results; item; item = item->ai_next) {
    socket = ::socket(item->ai_family, item->ai_socktype, item->ai_protocol);
    if (socket == invalid_socket) continue;
    if (::connect(socket, item->ai_addr, static_cast<int>(item->ai_addrlen)) == 0) break;
    close_socket(socket);
    socket = invalid_socket;
  }
  freeaddrinfo(results);
  if (socket == invalid_socket) throw std::runtime_error(socket_error_message("connect failed"));
  return socket;
}

int main(int argc, char** argv) {
  if (argc < 3 || std::string(argv[1]) != "curl") {
    std::cerr << "usage: b curl [-v] host:port/path [more paths...]\n";
    return 2;
  }
  bool verbose = false;
  int first_url = 2;
  if (std::string(argv[first_url]) == "-v") { verbose = true; ++first_url; }
  if (first_url >= argc) return 2;
  try {
    SocketRuntime runtime;
    std::vector<Endpoint> endpoints;
    for (int i = first_url; i < argc; ++i) endpoints.push_back(parse_endpoint(argv[i]));
    for (const auto& endpoint : endpoints) {
      if (endpoint.host != endpoints.front().host || endpoint.port != endpoints.front().port) {
        throw std::runtime_error("all paths in one invocation must use the same host and port");
      }
    }
    socket_t socket = connect_to(endpoints.front());
    int exit_code = 0;
    for (const auto& endpoint : endpoints) {
      protocol::Request request{protocol::GET, endpoint.path,
          {{"host", endpoint.host + ":" + endpoint.port}, {"user-agent", "b/1"},
           {"accept", "*/*"}, {"connection", "keep-alive"}}, {}};
      auto payload = protocol::encode_request(request);
      protocol::Frame outgoing{protocol::REQUEST, 0, static_cast<std::uint8_t>(request.headers.size()), std::move(payload)};
      if (verbose) dump_hex("request", protocol::frame_bytes(outgoing));
      protocol::send_frame(socket, outgoing);
      protocol::Frame incoming;
      do {
        if (!protocol::read_frame(socket, incoming)) throw std::runtime_error("server closed before response");
      } while (incoming.type != protocol::RESPONSE);
      if (verbose) dump_hex("response", protocol::frame_bytes(incoming));
      auto response = protocol::decode_response(incoming);
      std::cout.write(reinterpret_cast<const char*>(response.body.data()), static_cast<std::streamsize>(response.body.size()));
      std::cout.flush();
      if (response.status >= 400) exit_code = 1;
    }
    close_socket(socket);
    return exit_code;
  } catch (const std::exception& error) {
    std::cerr << "b: " << error.what() << "\n";
    return 1;
  }
}
