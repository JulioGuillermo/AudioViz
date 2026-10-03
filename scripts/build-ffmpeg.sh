#!/bin/bash
# Builds a minimal FFmpeg (video decoding only) as static libraries for the target (see
# target.sh). It is linked into the binary: no installed ffmpeg needed.
set -euo pipefail
source "$(dirname "$0")/target.sh"
SRC="$TP/ffmpeg"
if [ -f "$OUT/lib/libavcodec.a" ] && [ -f "$OUT/lib/libswscale.a" ]; then echo "ffmpeg already built ($TARGET_OS/$TARGET_ARCH)"; exit 0; fi
if [ ! -d "$SRC" ]; then
  git clone --depth 1 -b release/7.1 https://git.ffmpeg.org/ffmpeg.git "$SRC"
fi
# Out-of-tree build (FFmpeg refuses it if the source tree was configured in place)
if [ -f "$SRC/ffbuild/config.mak" ]; then make -C "$SRC" distclean >/dev/null 2>&1 || true; fi
B="$OBJDIR/ffmpeg"
rm -rf "$B" && mkdir -p "$B" && cd "$B"
extra=()
if [ "$TARGET_OS" = windows ]; then
  extra=(--enable-cross-compile --target-os=mingw32 --arch=x86_64 --cross-prefix=x86_64-w64-mingw32-
    --cc="$CC" --cxx="$CXX" --ar="$AR" --ranlib="$RANLIB" --nm="$NM")
fi
"$SRC/configure" --prefix="$OUT" --enable-static --disable-shared --enable-pic \
  --disable-programs --disable-doc --disable-network --disable-autodetect \
  --disable-everything --disable-avdevice --disable-avfilter --disable-swresample \
  --enable-avformat --enable-avcodec --enable-swscale \
  --enable-protocol=file \
  --enable-demuxer=mov,matroska,avi,mpegts,m4v,h264,hevc \
  --enable-decoder=h264,hevc,vp8,vp9,mpeg4,mjpeg,prores,png,mpeg2video \
  --enable-parser=h264,hevc,vp8,vp9,mpeg4video,mpegvideo,mjpeg,png ${extra[@]+"${extra[@]}"}
make -j"$NPROC"
make install
