cmake_minimum_required(VERSION 3.16)
project(line_calculator LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_CXX_EXTENSIONS OFF)

add_executable(serve src/serve.cpp)
add_executable(b src/b.cpp)
target_include_directories(serve PRIVATE src)
target_include_directories(b PRIVATE src)

if (WIN32)
  target_link_libraries(serve PRIVATE ws2_32)
  target_link_libraries(b PRIVATE ws2_32)
endif()

