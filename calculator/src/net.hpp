#pragma once

#include <cstdint>
#include <stdexcept>
#include <string>

#ifdef _WIN32
#include <winsock2.h>
#include <ws2tcpip.h>
using socket_t = SOCKET;
constexpr socket_t invalid_socket = INVALID_SOCKET;
#else
#include <arpa/inet.h>
#include <netdb.h>
#include <sys/socket.h>
#include <unistd.h>
using socket_t = int;
constexpr socket_t invalid_socket = -1;
#endif

class SocketRuntime {
public:
  SocketRuntime() {
#ifdef _WIN32
    WSADATA data{};
    if (WSAStartup(MAKEWORD(2, 2), &data) != 0) throw std::runtime_error("WSAStartup failed");
#endif
  }
  ~SocketRuntime() {
#ifdef _WIN32
    WSACleanup();
#endif
  }
};

inline void close_socket(socket_t s) {
  if (s == invalid_socket) return;
#ifdef _WIN32
  closesocket(s);
#else
  ::close(s);
#endif
}

inline std::string socket_error_message(const std::string& prefix) {
#ifdef _WIN32
  return prefix + " (" + std::to_string(WSAGetLastError()) + ")";
#else
  return prefix + ": " + std::to_string(errno);
#endif
}

