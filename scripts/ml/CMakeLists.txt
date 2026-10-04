# Builds stable-diffusion.cpp (image generation) and whisper.cpp (speech recognition) against a
# single ggml: both vendor their own copy, and two static copies in one binary would clash.
# stable-diffusion.cpp's ggml (its fork, built with GGML_MAX_NAME=160) is the one used; whisper.cpp
# reuses the existing ggml target. GGML_MAX_NAME changes the size of ggml_tensor, so it is set for
# every project here.
cmake_minimum_required(VERSION 3.18)
project(audioviz_ml C CXX)

set(SD_SRC "" CACHE PATH "stable-diffusion.cpp source")
set(WHISPER_SRC "" CACHE PATH "whisper.cpp source")

add_compile_definitions(GGML_MAX_NAME=160)
add_subdirectory(${SD_SRC} sd)
add_subdirectory(${WHISPER_SRC} whisper)
