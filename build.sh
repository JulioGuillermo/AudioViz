#!/bin/bash
# Builds the self-contained binary: static x264 inside, no ffmpeg or other runtime dependencies.
# Build-time requirements ONLY: go, gcc, nasm, git, Vulkan headers and glslc (if the shader changes).
set -euo pipefail
cd "$(dirname "$0")"
scripts/build-x264.sh
scripts/build-ffmpeg.sh
scripts/build-ml.sh
if command -v glslc >/dev/null && [ internal/gpu/shaders/render.comp -nt internal/gpu/shaders/render.spv ]; then
  glslc -O --target-env=vulkan1.1 internal/gpu/shaders/render.comp -o internal/gpu/shaders/render.spv
fi
CGO_ENABLED=1 go build -trimpath -tags netgo,osusergo -ldflags="-s -w -extldflags '-static-libstdc++ -static-libgcc'" -o audioviz ./cmd/audioviz
echo "OK -> ./audioviz ($(du -h audioviz | cut -f1))"; ldd audioviz 2>/dev/null || true
