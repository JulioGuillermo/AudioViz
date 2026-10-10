#!/usr/bin/env bash
# The Intel GPU acceleration plugin (Linux): ggml's SYCL backend as a module AudioViz loads at run
# time (ggml_backend_load), built from the same patched ggml as the binary (scripts/build-ml.sh
# first), with the oneAPI runtime libraries it needs next to it, in one archive to install from
# the studio (Settings › Plugins).
#
# Needs the oneAPI Base Toolkit (icpx, oneDNN, oneMKL) in /opt/intel/oneapi or $ONEAPI_ROOT.
# The module takes ggml's own symbols from the AudioViz binary (it exports them), so it does not
# carry a second ggml.
set -eo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TP="$ROOT/third_party"
OBJ="$TP/obj-linux-amd64/sycl-plugin"
ONEAPI_ROOT="${ONEAPI_ROOT:-/opt/intel/oneapi}"
GGML="$TP/stable-diffusion.cpp/ggml"
[ -f "$GGML/src/ggml-sycl/ggml-sycl.cpp" ] || { echo "run scripts/build-ml.sh first (it prepares the patched ggml)"; exit 1; }
# shellcheck disable=SC1091
source "$ONEAPI_ROOT/setvars.sh" --force > /dev/null 2>&1 || true

cmake -S "$GGML" -B "$OBJ" -DCMAKE_BUILD_TYPE=Release -DBUILD_SHARED_LIBS=ON -DGGML_BACKEND_DL=ON \
  -DGGML_SYCL=ON -DGGML_SYCL_F16=ON -DGGML_SYCL_TARGET=INTEL -DGGML_SYCL_DNN=ON -DGGML_SYCL_GRAPH=ON \
  -DGGML_CPU=OFF -DGGML_VULKAN=OFF -DGGML_METAL=OFF -DGGML_NATIVE=OFF -DGGML_OPENMP=OFF \
  -DCMAKE_C_COMPILER=icx -DCMAKE_CXX_COMPILER=icpx \
  -DCMAKE_C_FLAGS="-DGGML_MAX_NAME=160" \
  -DCMAKE_CXX_FLAGS="-DGGML_MAX_NAME=160 -fno-sycl-id-queries-fit-in-int" > "$OBJ.log" 2>&1
cmake --build "$OBJ" -j"$(nproc)" --target ggml-sycl >> "$OBJ.log" 2>&1
ls "$OBJ/bin/libggml-sycl.so"

# ---- the package: the module, the libraries it needs (all but the system's: libc, libstdc++…),
# each finding the others next to it, and plugin.json; zipped for Settings › Plugins
LIB="$OBJ/bin/libggml-sycl.so"
PKG="$OBJ/pkg/intel-sycl"
rm -rf "$OBJ/pkg"; mkdir -p "$PKG"
cp "$LIB" "$PKG/"
system='^(libc|libm|libdl|libpthread|librt|ld-linux.*|libstdc\+\+|libgcc_s|libz|libudev|linux-vdso)\.so'
add() {  # $1: a library; it and what it needs, once
  local f
  for f in $(ldd "$1" 2>/dev/null | awk '/=> \//{print $3}'); do
    local b; b=$(basename "$f")
    [[ "$b" =~ $system ]] && continue
    [[ "$b" == libggml* ]] && continue
    [ -f "$PKG/$b" ] && continue
    cp -L "$f" "$PKG/$b"
    add "$f"
  done
}
add "$LIB"
# what the SYCL runtime loads by name (its Level Zero adapter, the memory framework)
for name in libur_adapter_level_zero.so.0 libur_adapter_level_zero_v2.so.0 libumf.so.1; do
  f=$(find "$ONEAPI_ROOT/compiler/latest/lib" "$ONEAPI_ROOT/umf/latest/lib" -maxdepth 1 -name "$name" 2>/dev/null | head -1)
  [ -n "$f" ] && [ ! -f "$PKG/$name" ] && { cp -L "$f" "$PKG/$name"; add "$f"; }
done
patchelf --remove-needed libggml-base.so.0 "$PKG/libggml-sycl.so"
for f in "$PKG"/*.so*; do patchelf --force-rpath --set-rpath '$ORIGIN' "$f"; done
ML=$(cksum < "$TP/build/lib/.ml-stamp" | cut -d' ' -f1)
cat > "$PKG/plugin.json" <<JSON
{
  "id": "intel-sycl",
  "name": "Intel GPU acceleration (SYCL)",
  "kind": "ggml-backend",
  "backend": "SYCL",
  "lib": "libggml-sycl.so",
  "os": "linux",
  "arch": "amd64",
  "ml": "$ML",
  "version": "$(date +%Y.%m.%d)"
}
JSON
(cd "$OBJ/pkg" && rm -f ../audioviz-intel-sycl-linux-amd64.zip && zip -qr ../audioviz-intel-sycl-linux-amd64.zip intel-sycl)
echo "OK -> $OBJ/audioviz-intel-sycl-linux-amd64.zip ($(du -h "$OBJ/audioviz-intel-sycl-linux-amd64.zip" | cut -f1); $(du -sh "$PKG" | cut -f1) installed)"
