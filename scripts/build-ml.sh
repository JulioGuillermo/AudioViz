#!/bin/bash
# Builds the AI libraries as static libs for the target (see target.sh):
#   - whisper.cpp (speech recognition) and stable-diffusion.cpp (image generation), sharing one
#     ggml (scripts/ml/CMakeLists.txt): Vulkan GPU backend on Linux/Windows, Metal on macOS,
#     plus the CPU backend everywhere
#   - depth-anything.cpp (depth maps), on the same ggml
#   - demucs.cpp (vocal separation, CPU)
# They end up inside the binary: no Python or anything else needed at runtime.
# Build-time requirements only: git, cmake, a C++ compiler, and (Linux/Windows) glslc + Vulkan headers.
set -euo pipefail
source "$(dirname "$0")/target.sh"
WHISPER_REV=60c0be6ac8fa71b1a2ae2dd938a31a34a508e774
DEMUCS_REV=f1206e9adeea103aef4a636b9e62297cf1f8e34e
SD_REV=3f8527a46c54ecf4cb4ed6003da8e8982283c73c
DA_REV=14f7461d1f704761a038ac9f50dbde8fdb7275e2

# What the whisper + stable-diffusion libraries are built from: the revisions, the superproject
# and our patches (ggml, depth-anything.cpp). When it changes (e.g. a new patch), they are rebuilt.
ML_STAMP="$WHISPER_REV $SD_REV $DA_REV $(cat "$ROOT/scripts/ml/CMakeLists.txt" "$ROOT"/scripts/ml/patches/*.patch "$ROOT"/scripts/ml/patches-da/*.patch 2>/dev/null | cksum)"

if [ ! -f "$OUT/lib/libwhisper.a" ] || [ ! -f "$OUT/lib/libstable-diffusion.a" ] || [ ! -f "$OUT/lib/libdepthanything.a" ] ||
   [ "$(cat "$OUT/lib/.ml-stamp" 2>/dev/null)" != "$ML_STAMP" ]; then
  if [ ! -d "$TP/whisper.cpp" ]; then
    git clone https://github.com/ggml-org/whisper.cpp "$TP/whisper.cpp"
  fi
  git -C "$TP/whisper.cpp" checkout -q "$WHISPER_REV"
  if [ ! -d "$TP/stable-diffusion.cpp" ]; then
    git clone https://github.com/leejet/stable-diffusion.cpp "$TP/stable-diffusion.cpp"
  fi
  git -C "$TP/stable-diffusion.cpp" checkout -q "$SD_REV"
  git -C "$TP/stable-diffusion.cpp" submodule update --init --depth 1 ggml
  if [ ! -d "$TP/depth-anything.cpp" ]; then
    git clone https://github.com/mudler/depth-anything.cpp "$TP/depth-anything.cpp"
  fi
  git -C "$TP/depth-anything.cpp" fetch -q --depth 1 origin "$DA_REV" 2>/dev/null || true
  git -C "$TP/depth-anything.cpp" checkout -q "$DA_REV"
  # and to depth-anything.cpp (scripts/ml/patches-da)
  git -C "$TP/depth-anything.cpp" checkout -q -- .
  for p in "$ROOT"/scripts/ml/patches-da/*.patch; do
    [ -f "$p" ] && git -C "$TP/depth-anything.cpp" apply "$p"
  done
  # Our patches to ggml (scripts/ml/patches), applied to a clean checkout
  git -C "$TP/stable-diffusion.cpp/ggml" checkout -q -- .
  for p in "$ROOT"/scripts/ml/patches/*.patch; do
    [ -f "$p" ] && git -C "$TP/stable-diffusion.cpp/ggml" apply "$p"
  done
  B="$OBJDIR/ml"
  args=(-DCMAKE_BUILD_TYPE=Release -DBUILD_SHARED_LIBS=OFF -DGGML_OPENMP=OFF -DGGML_NATIVE=OFF
    -DSD_SRC="$TP/stable-diffusion.cpp" -DWHISPER_SRC="$TP/whisper.cpp" -DDA_SRC="$TP/depth-anything.cpp"
    -DSD_BUILD_EXAMPLES=OFF -DSD_WEBP=OFF -DSD_WEBM=OFF
    -DWHISPER_BUILD_TESTS=OFF -DWHISPER_BUILD_EXAMPLES=OFF -DWHISPER_BUILD_SERVER=OFF)
  if [ "$TARGET_ARCH" = amd64 ]; then
    args+=(-DGGML_AVX=ON -DGGML_AVX2=ON -DGGML_FMA=ON -DGGML_F16C=ON)
  fi
  case "$TARGET_OS" in
    darwin)
      # Apple GPUs: Metal (shaders embedded in the library) + Accelerate for the CPU part
      args+=(-DSD_METAL=ON -DGGML_METAL=ON -DGGML_METAL_EMBED_LIBRARY=ON -DGGML_VULKAN=OFF -DGGML_BLAS=ON -DGGML_ACCELERATE=ON) ;;
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
        -DSD_VULKAN=ON -DGGML_VULKAN=ON -DVulkan_INCLUDE_DIR="$VKINC" -DVulkan_LIBRARY="$TC/libvulkan-1.a"
        -DVulkan_GLSLC_EXECUTABLE="$(command -v glslc)" -DGGML_VULKAN_SHADERS_GEN_TOOLCHAIN="$TC/host.cmake") ;;
    *)
      args+=(-DSD_VULKAN=ON -DGGML_VULKAN=ON) ;;
  esac
  cmake -S "$ROOT/scripts/ml" -B "$B" "${args[@]}"
  cmake --build "$B" -j"$NPROC" --target whisper stable-diffusion depthanything
  # Every static library produced (whisper, stable-diffusion, ggml, ggml-base, ggml-cpu and the GPU backend)
  # (on Windows CMake names them without the "lib" prefix: normalize so the cgo flags are the same)
  while IFS= read -r f; do
    n=$(basename "$f"); [[ $n == lib* ]] || n="lib$n"
    cp "$f" "$OUT/lib/$n"
  done < <(find "$B" -name '*.a' \( -name '*whisper.a' -o -name '*stable-diffusion.a' -o -name '*depthanything.a' -o -name '*ggml*.a' \))
  cp "$TP/whisper.cpp/include/whisper.h" "$TP/stable-diffusion.cpp/include/stable-diffusion.h" "$TP/depth-anything.cpp/include/da_capi.h" \
    "$TP/stable-diffusion.cpp/ggml/include/"*.h "$OUT/include/"
  echo "$ML_STAMP" > "$OUT/lib/.ml-stamp"
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
