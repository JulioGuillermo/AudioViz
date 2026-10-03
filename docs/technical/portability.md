# Portability: Linux, Windows and macOS

AudioViz builds for Linux (native), Windows (cross-compiled from Linux with zig) and macOS (on a
Mac). Everything system-specific is selected at compile time:

| Where | Mechanism | Linux | Windows | macOS |
|---|---|---|---|---|
| Native libraries (`whisper.go`, `demucs.go`, `x264.go`, `ffdec.go`) | `#cgo <os>[,<arch>]` lines | `third_party/build`, `-l:libstdc++.a`, `-ldl` | `third_party/build-windows-amd64`, zig's `-lc++`, `-lbcrypt` (FFmpeg's random seed) | `third_party/build-darwin-<arch>`, `-lc++`, frameworks |
| CPU baseline | `#cgo <os>,amd64 CXXFLAGS`, `CPUFLAGS` in `target.sh` | `-march=x86-64-v3` | `-mcpu=x86_64_v3` (zig's spelling) | x86-64-v3 on Intel Macs, default on Apple Silicon |
| Speech recognition GPU (`build-ml.sh`) | CMake options per target | ggml Vulkan | ggml Vulkan | **ggml Metal** (embedded shaders) + Accelerate |
| Vulkan loader for ggml (`vkshim.c`) | `#if defined(_WIN32)` / `!__APPLE__` | `dlopen("libvulkan.so.1")` | `LoadLibraryA("vulkan-1.dll")` | not compiled (Metal) |
| Renderer's Vulkan loader (`vk.c`) | `#if defined(_WIN32)`, `AUDIOVIZ_MOLTENVK_STATIC` | `dlopen` | `LoadLibraryA` | MoltenVK linked statically (`moltenvk` build tag, `moltenvk_darwin.go`); otherwise a Vulkan SDK / MoltenVK dylib if installed |
| Portability extensions | runtime check | — | — | instance flag `ENUMERATE_PORTABILITY` + device extension `VK_KHR_portability_subset` (required by MoltenVK) |
| Opening the browser | `open_<os>.go` | `xdg-open` | `rundll32 url.dll,FileProtocolHandler` | `open` |
| File browser roots | `fsroots_windows.go` / `fsroots_other.go` | `/` | pseudo-folder `::drives` listing `C:\`, `D:\`… above each drive root | `/` |
| Home folder | `os.UserHomeDir` | `$HOME` | user profile | `$HOME` |
| Paths in the YAML | `config.isRooted` | absolute | absolute, or starting with `/` / `\` (root of the current drive) | absolute |
| Web UI paths | `basename` / picker split on `/` and `\` | | | |
| Linking | `build.sh` | `-static-libstdc++ -static-libgcc` | `-static` (one .exe) | system frameworks (cannot be fully static) |

## Robustness: drivers that hang

A broken Vulkan driver can block inside its setup calls instead of failing. `gpu.New` and
`gpu.ListDevices` run them with a 20 s limit; past it the GPU is abandoned for the rest of the
process (`gpu.ErrStuck`) and rendering continues on the CPU. `AUDIOVIZ_VK_DEBUG=1` prints each
Vulkan setup step, to see where a driver stops.

## Status

| | Linux | Windows | macOS (Mac mini M4, macOS 27) |
|---|---|---|---|
| Builds | ✅ | ✅ cross-compiled from Linux (zig 0.16, LLVM tools) | ✅ on the Mac (Xcode, Go, CMake from Homebrew): 23 MB, only system frameworks |
| CPU rendering / export | ✅ | ✅ under Wine: frame identical to the Linux CPU render, MP4 export (H.264 + FLAC) | ✅ frame identical to Linux |
| GPU rendering | ✅ Intel Lunar Lake: 63.8 fps exporting 1920×1088 @ 60 with lyrics, texts, visualizer and particles | ⚠ under Wine the GPU is found but Wine blocks inside vkCreateInstance in a headless session; the timeout falls back to CPU. **To test on real Windows.** | ✅ MoltenVK 1.4.2 on Apple M4: frame identical to the Linux GPU render; **115.6 fps** on the same export (CPU: 24.4 fps) |
| Studio | ✅ | ✅ under Wine (UI, Windows paths, drive list) | ✅ (preview, file browser, thumbnails) |
| Lyrics generation | ✅ Vulkan | to test | ✅ Metal: whole song in 4 min 7 s (Demucs on CPU 3 min 25 s, Whisper on the M4 39 s), 379 of 380 words; word starts match Linux (median 0, 90 % within 0.02 s) |

Note: recognition results differ slightly between GPU backends (floating point). On the demo
song the outro (lines repeated over the instrumental) was placed right on Metal and squeezed on
Vulkan in one run: that part of the alignment is fragile (see known-issues).

## Windows cross-compilation in short

`scripts/target.sh` (with `GOOS=windows`) creates wrappers named like a MinGW toolchain
(`x86_64-w64-mingw32-gcc` → `zig cc -target x86_64-windows-gnu`, `-ar` → `llvm-ar`, etc.) in
`third_party/toolchain-windows-amd64`, so x264's and FFmpeg's `configure` (`--cross-prefix`) and
CMake (`CMAKE_SYSTEM_NAME=Windows`) use them. ggml-vulkan gets the Vulkan + SPIR-V headers copied
from the build machine, a placeholder import library (the binary resolves Vulkan at runtime) and
a host toolchain file for its shader generator. CMake names Windows static libs without the `lib`
prefix; `build-ml.sh` normalizes them so the cgo flags are the same on every system.

## macOS in short

On a Mac, `./build.sh`: whisper.cpp with `GGML_METAL=ON` (shaders embedded in the library, no
`.metallib` to ship) and Accelerate; `scripts/build-moltenvk.sh` downloads the static
`libMoltenVK.a` and the Vulkan headers from an official MoltenVK release; `build.sh` adds the
`moltenvk` tag, which links it with the Metal/IOSurface/QuartzCore/… frameworks. The renderer's
compute shader runs unchanged on Metal through MoltenVK. Distributing to other Macs without
Gatekeeper warnings needs signing and notarization (Apple developer account).
