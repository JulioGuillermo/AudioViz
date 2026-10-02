#!/bin/bash
# Compila un FFmpeg mínimo (solo decodificación de vídeo) como librerías estáticas en
# third_party/build. Se enlaza dentro del binario: no hace falta ffmpeg instalado.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SRC="$ROOT/third_party/ffmpeg"
OUT="$ROOT/third_party/build"
if [ -f "$OUT/lib/libavcodec.a" ] && [ -f "$OUT/lib/libswscale.a" ]; then echo "ffmpeg ya compilado"; exit 0; fi
mkdir -p "$ROOT/third_party"
if [ ! -d "$SRC" ]; then
  git clone --depth 1 -b release/7.1 https://git.ffmpeg.org/ffmpeg.git "$SRC"
fi
cd "$SRC"
./configure --prefix="$OUT" --enable-static --disable-shared --enable-pic \
  --disable-programs --disable-doc --disable-network --disable-autodetect \
  --disable-everything --disable-avdevice --disable-avfilter --disable-swresample \
  --enable-avformat --enable-avcodec --enable-swscale \
  --enable-protocol=file \
  --enable-demuxer=mov,matroska,avi,mpegts,m4v,h264,hevc \
  --enable-decoder=h264,hevc,vp8,vp9,mpeg4,mjpeg,prores,png,mpeg2video \
  --enable-parser=h264,hevc,vp8,vp9,mpeg4video,mpegvideo,mjpeg,png
make -j"$(nproc)"
make install
