#include "protocol.hpp"

#include <charconv>
#include <filesystem>
#include <fstream>
#include <iostream>
#include <limits>
#include <sstream>
#include <string>
#include <vector>

namespace fs = std::filesystem;
using protocol::Header;

static std::vector<std::uint8_t> bytes_of(const std::string& text) {
  return {text.begin(), text.end()};
}

static protocol::Frame make_response(std::uint16_t status, std::vector<std::uint8_t> body,
                                     const std::string& type = "text/plain") {
  protocol::Response response{status,
      {{"content-length", std::to_string(body.size())},
       {"content-type", type}, {"connection", "keep-alive"}, {"server", "jc/1"}},
      std::move(body)};
  auto payload = protocol::encode_response(response);
  return {protocol::RESPONSE, 0, static_cast<std::uint8_t>(response.headers.size()), std::move(payload)};
}

static bool parse_int64(const std::string& text, std::int64_t& value) {
  if (text.empty()) return false;
  const char* begin = text.data();
  const char* end = begin + text.size();
  auto result = std::from_chars(begin, end, value);
  return result.ec == std::errc{} && result.ptr == end;
}

static bool split_query(const std::string& path, std::string& route,
                        std::int64_t& a, std::int64_t& b) {
  auto question = path.find('?');
  route = path.substr(0, question);
  if (question == std::string::npos) return false;
  std::string query = path.substr(question + 1);
  bool got_a = false, got_b = false;
  std::size_t start = 0;
  while (start <= query.size()) {
    auto amp = query.find('&', start);
    std::string item = query.substr(start, amp == std::string::npos ? std::string::npos : amp - start);
    auto equal = item.find('=');
    if (equal == std::string::npos) return false;
    std::string name = item.substr(0, equal);
    std::string value = item.substr(equal + 1);
    std::int64_t parsed = 0;
    if (!parse_int64(value, parsed)) return false;
    if (name == "a" && !got_a) { a = parsed; got_a = true; }
    else if (name == "b" && !got_b) { b = parsed; got_b = true; }
    else return false;
    if (amp == std::string::npos) break;
    start = amp + 1;
  }
  return got_a && got_b;
}

static protocol::Frame handle_request(const protocol::Request& request, const fs::path& root) {
  if (request.method != protocol::GET) return make_response(405, {});
  if (!protocol::has_header(request.headers, "host")) return make_response(400, {});
  if (!request.body.empty()) return make_response(400, {});

  std::string route;
  std::int64_t a = 0, b = 0;
  if (request.path.rfind("/add", 0) == 0 || request.path.rfind("/sub", 0) == 0 ||
      request.path.rfind("/mul", 0) == 0 || request.path.rfind("/div", 0) == 0) {
    if (!split_query(request.path, route, a, b)) return make_response(400, {});
    if (route != "/add" && route != "/sub" && route != "/mul" && route != "/div") {
      return make_response(404, {});
    }
    std::int64_t result = 0;
    bool overflow = false;
    if (route == "/add") {
      overflow = (b > 0 && a > std::numeric_limits<std::int64_t>::max() - b) ||
                 (b < 0 && a < std::numeric_limits<std::int64_t>::min() - b);
      if (!overflow) result = a + b;
    } else if (route == "/sub") {
      overflow = (b < 0 && a > std::numeric_limits<std::int64_t>::max() + b) ||
                 (b > 0 && a < std::numeric_limits<std::int64_t>::min() + b);
      if (!overflow) result = a - b;
    } else if (route == "/mul") {
      if (a != 0 && b != 0) {
        if (a == -1) overflow = b == std::numeric_limits<std::int64_t>::min();
        else if (b == -1) overflow = a == std::numeric_limits<std::int64_t>::min();
        else if (a > 0) overflow = b > 0 ? a > std::numeric_limits<std::int64_t>::max() / b
                                           : b < std::numeric_limits<std::int64_t>::min() / a;
        else overflow = b > 0 ? a < std::numeric_limits<std::int64_t>::min() / b
                              : a < std::numeric_limits<std::int64_t>::max() / b;
      }
      if (!overflow) result = a * b;
    } else {
      if (b == 0) return make_response(400, {});
      if (a == std::numeric_limits<std::int64_t>::min() && b == -1) overflow = true;
      else result = a / b;
    }
    if (overflow) return make_response(400, {});
    return make_response(200, bytes_of(std::to_string(result)));
  }

  if (request.path.find("..") != std::string::npos || request.path.empty() || request.path[0] != '/') {
    return make_response(400, {});
  }
  std::string relative = request.path.substr(1);
  fs::path candidate = fs::weakly_canonical(root / fs::path(relative));
  fs::path canonical_root = fs::weakly_canonical(root);
  auto root_text = canonical_root.generic_string();
  auto candidate_text = candidate.generic_string();
  if (candidate_text != root_text && candidate_text.rfind(root_text + "/", 0) != 0) {
    return make_response(400, {});
  }
  std::ifstream input(candidate, std::ios::binary);
  if (!input) return make_response(404, {});
  std::vector<std::uint8_t> data((std::istreambuf_iterator<char>(input)), std::istreambuf_iterator<char>());
  return make_response(200, std::move(data), "application/octet-stream");
}

static socket_t make_listener(const std::string& port) {
  addrinfo hints{};
  hints.ai_family = AF_UNSPEC;
  hints.ai_socktype = SOCK_STREAM;
  hints.ai_flags = AI_PASSIVE;
  addrinfo* results = nullptr;
  if (getaddrinfo(nullptr, port.c_str(), &hints, &results) != 0) throw std::runtime_error("getaddrinfo failed");
  socket_t listener = invalid_socket;
  for (auto* item = results; item; item = item->ai_next) {
    listener = ::socket(item->ai_family, item->ai_socktype, item->ai_protocol);
    if (listener == invalid_socket) continue;
    int yes = 1;
    setsockopt(listener, SOL_SOCKET, SO_REUSEADDR, reinterpret_cast<const char*>(&yes), sizeof(yes));
    if (::bind(listener, item->ai_addr, static_cast<int>(item->ai_addrlen)) == 0 && ::listen(listener, 16) == 0) break;
    close_socket(listener);
    listener = invalid_socket;
  }
  freeaddrinfo(results);
  if (listener == invalid_socket) throw std::runtime_error(socket_error_message("could not bind"));
  return listener;
}

int main(int argc, char** argv) {
  if (argc != 3) {
    std::cerr << "usage: serve <root> <port>\n";
    return 2;
  }
  try {
    SocketRuntime runtime;
    fs::path root = fs::absolute(argv[1]);
    if (!fs::is_directory(root)) throw std::runtime_error("root is not a directory");
    socket_t listener = make_listener(argv[2]);
    std::cout << "serving " << root << " on port " << argv[2] << "\n";
    while (true) {
      socket_t client = ::accept(listener, nullptr, nullptr);
      if (client == invalid_socket) throw std::runtime_error(socket_error_message("accept failed"));
      try {
        while (true) {
          protocol::Frame frame;
          if (!protocol::read_frame(client, frame)) break;
          if (frame.type != protocol::REQUEST) continue;
          try {
            auto request = protocol::decode_request(frame);
            protocol::send_frame(client, handle_request(request, root));
          } catch (const std::exception&) {
            protocol::send_frame(client, make_response(400, {}));
          }
        }
      } catch (const std::exception&) {
        // A truncated or malformed peer frame ends this connection.
      }
      close_socket(client);
    }
  } catch (const std::exception& error) {
    std::cerr << "serve: " << error.what() << "\n";
    return 1;
  }
}
