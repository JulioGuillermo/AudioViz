#!/bin/bash
# macOS only: MoltenVK (Vulkan implemented on top of Metal) as a static library, plus the Vulkan
# headers, for the GPU renderer. With it the same compute shader runs on Apple GPUs and nothing
# has to be installed. Uses the prebuilt static library of an official MoltenVK release.
set -euo pipefail
source "$(dirname "$0")/target.sh"
[ "$TARGET_OS" = darwin ] || exit 0
if [ -f "$OUT/lib/libMoltenVK.a" ]; then echo "MoltenVK already there"; exit 0; fi
VER=v1.4.2
D="$TP/moltenvk-$VER"
if [ ! -d "$D" ]; then
  mkdir -p "$D"
  curl -fL "https://github.com/KhronosGroup/MoltenVK/releases/download/$VER/MoltenVK-macos.tar" -o "$D.tar"
  tar -xf "$D.tar" -C "$D"
fi
lib=$(find "$D" -path '*static*' -name 'libMoltenVK.a' | head -1)
inc=$(dirname "$(dirname "$(find "$D" -path '*/include/vulkan/vulkan.h' | head -1)")")
[ -n "$lib" ] && [ -d "$inc" ] || { echo "libMoltenVK.a / Vulkan headers not found in the MoltenVK package" >&2; exit 1; }
cp "$lib" "$OUT/lib/"
cp -R "$inc/"* "$OUT/include/"
echo "MoltenVK $VER ready in $OUT"
