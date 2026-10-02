#!/bin/bash
# Builds the AI libraries for lyrics generation, as static libs in third_party/build:
#   - whisper.cpp (speech recognition) with Vulkan (GPU) + CPU backend
#   - demucs.cpp (vocal separation)
# They end up inside the binary: no Python or anything else needed at runtime.
# Build-time requirements only: git, cmake, g++, glslc and Vulkan headers.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TP="$ROOT/third_party"
OUT="$TP/build"
WHISPER_REV=60c0be6ac8fa71b1a2ae2dd938a31a34a508e774
DEMUCS_REV=f1206e9adeea103aef4a636b9e62297cf1f8e34e
mkdir -p "$OUT/lib" "$OUT/include"

if [ ! -f "$OUT/lib/libwhisper.a" ]; then
  if [ ! -d "$TP/whisper.cpp" ]; then
    git clone https://github.com/ggml-org/whisper.cpp "$TP/whisper.cpp"
  fi
  git -C "$TP/whisper.cpp" checkout -q "$WHISPER_REV"
  # x86-64-v3 (AVX2/FMA): portable to any CPU from the last ~10 years
  cmake -S "$TP/whisper.cpp" -B "$TP/whisper.cpp/build" -DCMAKE_BUILD_TYPE=Release \
    -DBUILD_SHARED_LIBS=OFF -DGGML_OPENMP=OFF -DGGML_NATIVE=OFF \
    -DGGML_AVX=ON -DGGML_AVX2=ON -DGGML_FMA=ON -DGGML_F16C=ON -DGGML_VULKAN=ON \
    -DWHISPER_BUILD_TESTS=OFF -DWHISPER_BUILD_EXAMPLES=OFF -DWHISPER_BUILD_SERVER=OFF
  cmake --build "$TP/whisper.cpp/build" -j"$(nproc)" --target whisper
  B="$TP/whisper.cpp/build"
  cp "$B/src/libwhisper.a" "$B/ggml/src/libggml.a" "$B/ggml/src/libggml-base.a" \
     "$B/ggml/src/libggml-cpu.a" "$B/ggml/src/ggml-vulkan/libggml-vulkan.a" "$OUT/lib/"
  cp "$TP/whisper.cpp/include/whisper.h" "$TP/whisper.cpp/ggml/include/"*.h "$OUT/include/"
fi

if [ ! -f "$OUT/lib/libdemucs.a" ]; then
  if [ ! -d "$TP/demucs.cpp" ]; then
    git clone https://github.com/sevagh/demucs.cpp "$TP/demucs.cpp"
  fi
  git -C "$TP/demucs.cpp" checkout -q "$DEMUCS_REV"
  git -C "$TP/demucs.cpp" submodule update --init --depth 1 vendor/eigen
  OBJ="$TP/demucs.cpp/obj"
  mkdir -p "$OBJ"
  FL="-O3 -march=x86-64-v3 -ffast-math -fno-finite-math-only -DNDEBUG -std=c++17 -fPIC -w \
      -I$TP/demucs.cpp/src -I$TP/demucs.cpp/vendor/eigen"
  pids=()
  for f in "$TP/demucs.cpp/src/"*.cpp; do
    g++ $FL -c "$f" -o "$OBJ/$(basename "$f").o" & pids+=($!)
  done
  for p in "${pids[@]}"; do wait "$p"; done
  ar rcs "$OUT/lib/libdemucs.a" "$OBJ/"*.o
fi
echo "AI libraries ready in $OUT/lib"
