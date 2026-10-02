# Building

```bash
./build.sh
```
Produces `./audioviz`, which depends only on libc. x264 and a minimal FFmpeg (video decoding
only) are downloaded and built once as static libraries (`third_party/`) and end up inside
the binary, just like the studio's web interface.

Requirements **for building only**: `go`, `gcc`/`g++`, `cmake`, `nasm`, `git`, Vulkan headers
(`vulkan-headers`) and `glslc` (for the Whisper shaders and, if you modify it, `internal/gpu/shaders/render.comp`).

These are built as static libraries, only once:
- `scripts/build-x264.sh`: x264 (H.264 encoding)
- `scripts/build-ffmpeg.sh`: minimal FFmpeg (only for decoding video backgrounds)
- `scripts/build-ml.sh`: whisper.cpp with the Vulkan backend (speech recognition) and demucs.cpp (vocal separation)

`libstdc++` and `libgcc` are linked statically and `libvulkan` is loaded at runtime only if present, so the
resulting binary depends only on libc.

Nothing needs to be installed to run it. The GPU is used through the system's Vulkan driver
(part of the graphics driver); if there is none, rendering falls back to the CPU automatically.

License: x264 is GPL, so the resulting binary is GPL too (FFmpeg is built in LGPL mode).
