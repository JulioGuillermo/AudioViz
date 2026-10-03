#!/bin/bash
# Builds x264 as a static library for the target (see target.sh), linked into the binary.
set -euo pipefail
source "$(dirname "$0")/target.sh"
SRC="$TP/x264"
if [ -f "$OUT/lib/libx264.a" ]; then echo "x264 already built ($TARGET_OS/$TARGET_ARCH)"; exit 0; fi
if [ ! -d "$SRC" ]; then
  git clone --depth 1 -b stable https://code.videolan.org/videolan/x264.git "$SRC"
fi
# Out-of-tree build, so several targets can share the sources (an in-tree build blocks it)
if [ -f "$SRC/config.mak" ]; then make -C "$SRC" distclean >/dev/null 2>&1 || true; fi
B="$OBJDIR/x264"
rm -rf "$B" && mkdir -p "$B" && cd "$B"
extra=()
if [ "$TARGET_OS" = windows ]; then
  extra=(--host=x86_64-w64-mingw32 --cross-prefix=x86_64-w64-mingw32- --enable-win32thread)
fi
"$SRC/configure" --prefix="$OUT" --enable-static --enable-pic --disable-cli --disable-opencl \
  --disable-avs --disable-swscale --disable-lavf --disable-ffms --disable-gpac --disable-lsmash \
  --bit-depth=8 --chroma-format=420 "${extra[@]}"
make -j"$NPROC"
make install
