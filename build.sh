#!/bin/bash
# Builds the self-contained binary for this system, or for Windows from Linux:
#   ./build.sh                      -> ./audioviz      (this system: Linux or macOS)
#   GOOS=windows ./build.sh         -> ./audioviz.exe  (cross-compiled with zig)
#   ./build.sh install              -> installs audioviz in ~/go/bin (or GOBIN), usable from any folder
# Build-time requirements ONLY: go, a C/C++ compiler, cmake, nasm, git; Vulkan headers and
# glslc (Linux, Windows); zig + LLVM tools (llvm-ar, llvm-nm...) for Windows; Xcode command line
# tools on macOS. Nothing is needed to run the result.
set -euo pipefail
cd "$(dirname "$0")"
source scripts/target.sh
scripts/build-x264.sh
scripts/build-ffmpeg.sh
scripts/build-ml.sh
if [ "$TARGET_OS" = darwin ]; then
  scripts/build-moltenvk.sh || echo "MoltenVK not available: the renderer will look for a Vulkan SDK at runtime or use the CPU"
fi
cd "$ROOT"
# Shaders (the .spv are in git, so building without glslc works): recompiled when the shader
# or the shared common.glsl changed
for sh in render fluid fluidsim; do
  src=internal/gpu/shaders/$sh.comp spv=internal/gpu/shaders/$sh.spv
  if command -v glslc >/dev/null && { [ $src -nt $spv ] || [ internal/gpu/shaders/common.glsl -nt $spv ]; }; then
    glslc -O --target-env=vulkan1.1 $src -o $spv
  fi
done
tags=netgo,osusergo
bin=audioviz
case "$TARGET_OS" in
  linux) ldx="-extldflags '-static-libstdc++ -static-libgcc'" ;;  # depends only on libc
  windows) bin=audioviz.exe; ldx="-extldflags '-static'" ;;       # a single static .exe
  darwin)
    ldx=""
    [ -f "$OUT/lib/libMoltenVK.a" ] || tags="$tags,nomoltenvk" ;;
esac
if [ "${1:-}" = install ]; then
  # ./build.sh install: same build, installed as "audioviz" in $(go env GOBIN) or ~/go/bin,
  # so it can be run from any folder (that folder must be in PATH)
  GOOS=$TARGET_OS GOARCH=$TARGET_ARCH CGO_ENABLED=1 \
    go install -trimpath -tags "$tags" -ldflags="-s -w $ldx" ./cmd/audioviz
  dir=$(go env GOBIN); [ -n "$dir" ] || dir="$(go env GOPATH)/bin"
  echo "Installed -> $dir/$bin"
  case ":$PATH:" in *":$dir:"*) ;; *) echo "Note: $dir is not in PATH; add it to use 'audioviz' from any folder" ;; esac
  exit 0
fi
GOOS=$TARGET_OS GOARCH=$TARGET_ARCH CGO_ENABLED=1 \
  go build -trimpath -tags "$tags" -ldflags="-s -w $ldx" -o "$bin" ./cmd/audioviz
echo "OK -> ./$bin ($(du -h "$bin" | cut -f1), $TARGET_OS/$TARGET_ARCH)"
case "$TARGET_OS" in
  linux) ldd "$bin" 2>/dev/null || true ;;
  darwin) otool -L "$bin" 2>/dev/null || true ;;
  windows) command -v llvm-objdump >/dev/null && llvm-objdump -p "$bin" | grep "DLL Name" || true ;;
esac
