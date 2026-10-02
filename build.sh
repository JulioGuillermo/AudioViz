#!/bin/bash
# Compila el binario autónomo: x264 estático dentro, sin ffmpeg ni otras dependencias en ejecución.
# Requisitos SOLO para compilar: go, gcc, nasm, git, cabeceras de Vulkan y glslc (si se cambia el shader).
set -euo pipefail
cd "$(dirname "$0")"
scripts/build-x264.sh
scripts/build-ffmpeg.sh
if command -v glslc >/dev/null && [ internal/gpu/shaders/render.comp -nt internal/gpu/shaders/render.spv ]; then
  glslc -O --target-env=vulkan1.1 internal/gpu/shaders/render.comp -o internal/gpu/shaders/render.spv
fi
CGO_ENABLED=1 go build -trimpath -ldflags="-s -w" -o audioviz ./cmd/audioviz
echo "OK -> ./audioviz ($(du -h audioviz | cut -f1))"; ldd audioviz 2>/dev/null || true
