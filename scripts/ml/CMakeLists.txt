# Builds stable-diffusion.cpp (image generation), whisper.cpp (speech recognition) and
# depth-anything.cpp (depth maps) against a single ggml: both vendor their own copy, and two static copies in one binary would clash.
# stable-diffusion.cpp's ggml (its fork, built with GGML_MAX_NAME=160) is the one used; whisper.cpp
# reuses the existing ggml target. GGML_MAX_NAME changes the size of ggml_tensor, so it is set for
# every project here.
cmake_minimum_required(VERSION 3.18)
project(audioviz_ml C CXX)

set(SD_SRC "" CACHE PATH "stable-diffusion.cpp source")
set(WHISPER_SRC "" CACHE PATH "whisper.cpp source")
set(DA_SRC "" CACHE PATH "depth-anything.cpp source")

add_compile_definitions(GGML_MAX_NAME=160)
add_subdirectory(${SD_SRC} sd)
add_subdirectory(${WHISPER_SRC} whisper)

# depth-anything.cpp: its library alone (its own CMake would add a second ggml), with the source
# list of its CMakeLists. Its Winograd AVX-512 path is left out (the portable path is used: not
# every CPU has AVX-512).
if(DA_SRC)
  file(STRINGS ${DA_SRC}/CMakeLists.txt DA_LINES REGEX "^    src/.*\\.cpp")
  set(DA_SOURCES "")
  foreach(x ${DA_LINES})
    string(STRIP "${x}" x)
    list(APPEND DA_SOURCES ${DA_SRC}/${x})
  endforeach()
  add_library(depthanything STATIC ${DA_SOURCES})
  target_compile_features(depthanything PUBLIC cxx_std_17)
  target_include_directories(depthanything PUBLIC ${DA_SRC}/include
    PRIVATE ${DA_SRC}/src ${DA_SRC}/third_party ${DA_SRC}/third_party/stb)
  target_link_libraries(depthanything PUBLIC ggml)
endif()
