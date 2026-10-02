#!/bin/bash
# Builds x264 as a static library in third_party/build (linked into the binary).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SRC="$ROOT/third_party/x264"
OUT="$ROOT/third_party/build"
if [ -f "$OUT/lib/libx264.a" ]; then echo "x264 already built"; exit 0; fi
mkdir -p "$ROOT/third_party"
if [ ! -d "$SRC" ]; then
  git clone --depth 1 -b stable https://code.videolan.org/videolan/x264.git "$SRC"
fi
cd "$SRC"
./configure --prefix="$OUT" --enable-static --enable-pic --disable-cli --disable-opencl \
  --disable-avs --disable-swscale --disable-lavf --disable-ffms --disable-gpac --disable-lsmash \
  --bit-depth=8 --chroma-format=420
make -j"$(nproc)"
make install
