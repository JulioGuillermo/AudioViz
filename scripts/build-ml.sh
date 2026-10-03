#!/bin/bash
# Builds the AI libraries for lyrics generation as static libs for the target (see target.sh):
#   - whisper.cpp (speech recognition): Vulkan GPU backend on Linux/Windows, Metal on macOS,
#     plus the CPU backend everywhere
#   - demucs.cpp (vocal separation, CPU)
# They end up inside the binary: no Python or anything else needed at runtime.
# Build-time requirements only: git, cmake, a C++ compiler, and (Linux/Windows) glslc + Vulkan headers.
set -euo pipefail
source "$(dirname "$0")/target.sh"
WHISPER_REV=60c0be6ac8fa71b1a2ae2dd938a31a34a508e774
DEMUCS_REV=f1206e9adeea103aef4a636b9e62297cf1f8e34e

if [ ! -f "$OUT/lib/libwhisper.a" ]; then
  if [ ! -d "$TP/whisper.cpp" ]; then
    git clone https://github.com/ggml-org/whisper.cpp "$TP/whisper.cpp"
  fi
  git -C "$TP/whisper.cpp" checkout -q "$WHISPER_REV"
  B="$OBJDIR/whisper"
  args=(-DCMAKE_BUILD_TYPE=Release -DBUILD_SHARED_LIBS=OFF -DGGML_OPENMP=OFF -DGGML_NATIVE=OFF
    -DWHISPER_BUILD_TESTS=OFF -DWHISPER_BUILD_EXAMPLES=OFF -DWHISPER_BUILD_SERVER=OFF)
  if [ "$TARGET_ARCH" = amd64 ]; then
    args+=(-DGGML_AVX=ON -DGGML_AVX2=ON -DGGML_FMA=ON -DGGML_F16C=ON)
  fi
  case "$TARGET_OS" in
    darwin)
      # Apple GPUs: Metal (shaders embedded in the library) + Accelerate for the CPU part
      args+=(-DGGML_METAL=ON -DGGML_METAL_EMBED_LIBRARY=ON -DGGML_VULKAN=OFF -DGGML_BLAS=ON -DGGML_ACCELERATE=ON) ;;
    windows)
      # Cross: Vulkan headers from this system, a placeholder import library (the binary
      # resolves Vulkan at runtime, see vkshim.c) and the host compiler for the shader generator
      VKINC="$TP/vulkan-headers"
      mkdir -p "$VKINC"
      for d in vulkan vk_video spirv; do  # Vulkan, video and SPIR-V headers (ggml-vulkan uses all three)
        [ -d "$VKINC/$d" ] || [ ! -d "/usr/include/$d" ] || cp -r "/usr/include/$d" "$VKINC/"
      done
      [ -f "$TC/libvulkan-1.a" ] || llvm-ar rc "$TC/libvulkan-1.a"
      cat >"$TC/host.cmake" <<EOF
set(CMAKE_C_COMPILER $(command -v gcc))
set(CMAKE_CXX_COMPILER $(command -v g++))
set(CMAKE_BUILD_TYPE Release)
EOF
      args+=(-DCMAKE_SYSTEM_NAME=Windows -DCMAKE_SYSTEM_PROCESSOR=AMD64
        -DCMAKE_C_COMPILER="$TC/$CC" -DCMAKE_CXX_COMPILER="$TC/$CXX"
        -DCMAKE_AR="$TC/$AR" -DCMAKE_RANLIB="$TC/$RANLIB" -DCMAKE_RC_COMPILER="$TC/x86_64-w64-mingw32-windres"
        -DGGML_VULKAN=ON -DVulkan_INCLUDE_DIR="$VKINC" -DVulkan_LIBRARY="$TC/libvulkan-1.a"
        -DVulkan_GLSLC_EXECUTABLE="$(command -v glslc)" -DGGML_VULKAN_SHADERS_GEN_TOOLCHAIN="$TC/host.cmake") ;;
    *)
      args+=(-DGGML_VULKAN=ON) ;;
  esac
  cmake -S "$TP/whisper.cpp" -B "$B" "${args[@]}"
  cmake --build "$B" -j"$NPROC" --target whisper
  # Every static library produced (whisper, ggml, ggml-base, ggml-cpu and the GPU backend)
  # (on Windows CMake names them without the "lib" prefix: normalize so the cgo flags are the same)
  while IFS= read -r f; do
    n=$(basename "$f"); [[ $n == lib* ]] || n="lib$n"
    cp "$f" "$OUT/lib/$n"
  done < <(find "$B" -name '*.a' \( -name '*whisper.a' -o -name '*ggml*.a' \))
  cp "$TP/whisper.cpp/include/whisper.h" "$TP/whisper.cpp/ggml/include/"*.h "$OUT/include/"
fi

if [ ! -f "$OUT/lib/libdemucs.a" ]; then
  if [ ! -d "$TP/demucs.cpp" ]; then
    git clone https://github.com/sevagh/demucs.cpp "$TP/demucs.cpp"
  fi
  git -C "$TP/demucs.cpp" checkout -q "$DEMUCS_REV"
  git -C "$TP/demucs.cpp" submodule update --init --depth 1 vendor/eigen
  OBJ="$OBJDIR/demucs"
  mkdir -p "$OBJ"
  FL="-O3 $CPUFLAGS -ffast-math -fno-finite-math-only -DNDEBUG -std=c++17 -w \
      -I$TP/demucs.cpp/src -I$TP/demucs.cpp/vendor/eigen"
  [ "$TARGET_OS" = windows ] || FL="$FL -fPIC"
  pids=()
  for f in "$TP/demucs.cpp/src/"*.cpp; do
    $CXX $FL -c "$f" -o "$OBJ/$(basename "$f").o" & pids+=($!)
  done
  for p in "${pids[@]}"; do wait "$p"; done
  $AR rcs "$OUT/lib/libdemucs.a" "$OBJ/"*.o
fi
echo "AI libraries ready in $OUT/lib ($TARGET_OS/$TARGET_ARCH)"
