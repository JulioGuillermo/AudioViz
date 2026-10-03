# Sourced by the build scripts: describes the target being built.
#
#   TARGET_OS / TARGET_ARCH  linux|windows|darwin, amd64|arm64 (from GOOS/GOARCH, default: this machine)
#   OUT                      where the static libraries go (third_party/build for linux-amd64,
#                            third_party/build-<os>-<arch> otherwise; the cgo flags expect these)
#   OBJDIR                   per-target build directory for out-of-tree builds
#   CC CXX AR RANLIB NM      compilers and tools for the target
#   CROSS                    1 when cross-compiling
#   CPUFLAGS                 baseline CPU flags (x86-64-v3 on amd64, none on arm64)
#
# Cross-compiling to Windows uses zig (bundled mingw-w64 headers and libc++) and the LLVM tools;
# wrappers with mingw names are created so autoconf-style scripts and CMake find them.
# macOS needs Apple's SDK (Metal and other frameworks): build it on a Mac.

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TP="$ROOT/third_party"
host_os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$(uname -m)" in x86_64 | amd64) host_arch=amd64 ;; arm64 | aarch64) host_arch=arm64 ;; *) host_arch=$(uname -m) ;; esac
TARGET_OS=${GOOS:-$host_os}
TARGET_ARCH=${GOARCH:-$host_arch}
CROSS=0
[ "$TARGET_OS/$TARGET_ARCH" != "$host_os/$host_arch" ] && CROSS=1

if [ "$TARGET_OS" = linux ] && [ "$TARGET_ARCH" = amd64 ]; then
  OUT="$TP/build"
else
  OUT="$TP/build-$TARGET_OS-$TARGET_ARCH"
fi
OBJDIR="$TP/obj-$TARGET_OS-$TARGET_ARCH"
mkdir -p "$OUT/lib" "$OUT/include" "$OBJDIR"

case "$TARGET_ARCH" in
  amd64) CPUFLAGS="-march=x86-64-v3" ;;  # AVX2/FMA: any x86 CPU from the last ~10 years
  *) CPUFLAGS="" ;;
esac

NPROC=$(nproc 2>/dev/null || sysctl -n hw.ncpu 2>/dev/null || echo 4)

if [ "$TARGET_OS" = windows ]; then
  [ "$TARGET_ARCH" = amd64 ] || { echo "only windows/amd64 is supported" >&2; exit 1; }
  command -v zig >/dev/null || { echo "cross-compiling to Windows needs zig" >&2; exit 1; }
  TRIPLE=x86_64-w64-mingw32
  TC="$TP/toolchain-windows-amd64"
  mkdir -p "$TC"
  w() { printf '#!/bin/sh\nexec %s "$@"\n' "$2" >"$TC/$1"; chmod +x "$TC/$1"; }
  w "$TRIPLE-gcc" "zig cc -target x86_64-windows-gnu"
  w "$TRIPLE-g++" "zig c++ -target x86_64-windows-gnu"
  w "$TRIPLE-ar" "llvm-ar"
  w "$TRIPLE-ranlib" "llvm-ranlib"
  w "$TRIPLE-nm" "llvm-nm"
  w "$TRIPLE-strip" "llvm-strip"
  w "$TRIPLE-windres" "llvm-windres"
  w "$TRIPLE-strings" "$(command -v llvm-strings || command -v strings)"
  export PATH="$TC:$PATH"
  CC="$TRIPLE-gcc" CXX="$TRIPLE-g++" AR="$TRIPLE-ar" RANLIB="$TRIPLE-ranlib" NM="$TRIPLE-nm"
  CPUFLAGS="-mcpu=x86_64_v3" # zig's spelling of x86-64-v3
else
  CC=${CC:-cc} CXX=${CXX:-c++} AR=${AR:-ar} RANLIB=${RANLIB:-ranlib} NM=${NM:-nm}
  if [ "$CROSS" = 1 ]; then
    echo "cross-compiling to $TARGET_OS/$TARGET_ARCH is not supported (only to windows); build on that system" >&2
    exit 1
  fi
fi
export CC CXX AR RANLIB NM
