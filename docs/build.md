# Building

```bash
./build.sh                    # this system (Linux, or macOS on a Mac) -> ./audioviz
GOOS=windows ./build.sh       # Windows, cross-compiled from Linux     -> ./audioviz.exe
./build.sh install            # builds and installs `audioviz` in ~/go/bin (or $GOBIN)
```

After the first `./build.sh` (which builds the static libraries), a plain `go install ./cmd/audioviz`
works too. Put `~/go/bin` in your `PATH` to run `audioviz` from any folder.

The result is a single file with nothing to install. x264, a minimal FFmpeg (video decoding
only), whisper.cpp, stable-diffusion.cpp and demucs.cpp are downloaded and built once per target as static libraries
(`third_party/build` for Linux, `third_party/build-<os>-<arch>` for the others) and end up inside
the binary, just like the studio's web interface.

| Target | GPU (video) | GPU (lyrics recognition) | Runtime dependencies |
|---|---|---|---|
| Linux x86-64 | Vulkan (driver's `libvulkan.so.1`, loaded if present) | Vulkan | the C library only |
| Windows x86-64 | Vulkan (`vulkan-1.dll`, installed with the GPU driver, loaded if present) | Vulkan | system DLLs only (kernel32, advapi32, bcrypt, the Universal C Runtime of Windows 10/11) |
| macOS (arm64 / x86-64) | MoltenVK (Vulkan on Metal) linked in | **Metal** | system frameworks only |

Without a usable GPU everything falls back to the CPU automatically. If a GPU driver hangs during
setup, AudioViz gives up on it after 20 s and uses the CPU (`AUDIOVIZ_VK_DEBUG=1` prints each
Vulkan setup step).

## Requirements for building only

- **Linux**: `go`, `gcc`/`g++`, `cmake`, `nasm`, `git`, Vulkan headers (`vulkan-headers`), SPIR-V
  headers (`spirv-headers`) and `glslc`.
- **Windows (cross-compiling from Linux)**: the above plus `zig` and the LLVM tools (`llvm-ar`,
  `llvm-nm`, `llvm-ranlib`, `llvm-strip`, `llvm-windres`). No MinGW or Windows needed.
- **macOS (on a Mac)**: `go`, Xcode command line tools, `cmake`, `nasm` (x86 Macs), `git`, `curl`.
  The script downloads the static MoltenVK of an official release. macOS cannot be cross-compiled
  from Linux in practice: Apple's SDK (Metal and other frameworks) only comes with Xcode.

These are built as static libraries, only once per target:
- `scripts/build-x264.sh`: x264 (H.264 encoding)
- `scripts/build-ffmpeg.sh`: minimal FFmpeg (only for decoding video backgrounds)
- `scripts/build-ml.sh`: whisper.cpp (lyrics) and stable-diffusion.cpp (images) on one ggml (Vulkan or Metal backend + CPU), and demucs.cpp (vocal separation)
- `scripts/build-moltenvk.sh`: macOS only, MoltenVK for the renderer
- `scripts/target.sh`: shared by all of them, describes the target and the cross toolchain

Tested: Linux (native), Windows build cross-compiled and run under Wine (CPU rendering, export,
studio). Still to test on real hardware: Windows GPU rendering and the whole macOS build.

License: x264 is GPL, so the resulting binary is GPL too (FFmpeg is built in LGPL mode).
