# Portability: Windows and macOS

AudioViz currently builds and runs on **Linux x86-64** only. Nothing in the design is Linux
specific: every bundled library (x264, FFmpeg, whisper.cpp/ggml, demucs.cpp/Eigen) and Go
itself build on Windows and macOS. What needs work is the thin layer that talks to the system.
This page lists every known tie, as of this writing, and what each port needs.

## Ties to Linux in the code

| Where | What | Windows | macOS |
|---|---|---|---|
| `internal/gpu/vk.c` (renderer) | Loads the Vulkan loader with `dlopen` (`libvulkan.so.1`, `libvulkan.so`, `libvulkan.1.dylib`, `libMoltenVK.dylib`) | Needs `LoadLibraryA("vulkan-1.dll")` + `GetProcAddress` (the DLL ships with NVIDIA/AMD/Intel drivers) | The names are already tried; see *GPU on macOS* below |
| `internal/lyricsgen/vkshim.c` (ggml-vulkan) | Same idea for the 4 `vk*` symbols ggml imports, but only tries `libvulkan.so.1` / `libvulkan.so` | Same `LoadLibrary` change | Add the dylib names, or use Metal instead (below) |
| `internal/gpu/gpu.go` | `#cgo linux LDFLAGS: -ldl` | No `-ldl` | `dlopen` is in libSystem, no flag needed |
| cgo `LDFLAGS` in `encode/x264.go`, `lyricsgen/whisper.go`, `lyricsgen/demucs.go` | `-ldl`, `-lpthread`, `-l:libstdc++.a` (GNU ld syntax) | MinGW: `-lstdc++` static via `-static`; no `-ldl` | Apple ld has no `-l:`; libc++ is part of the system |
| `scripts/build-ml.sh`, `#cgo CXXFLAGS` in `demucs.go` | `-march=x86-64-v3`, `GGML_AVX2/FMA/F16C` | Fine on x86-64 | Apple Silicon needs arm64 flags (NEON is on by default); Intel Macs keep x86-64-v3 |
| `cmd/audioviz/main.go` (studio) | Opens the browser with `xdg-open` or `open` | `rundll32 url.dll,FileProtocolHandler <url>` (or `cmd /c start`) | `open` already works |
| `internal/studio/server.go` | State sends `HOME` from `os.Getenv("HOME")` | Use `os.UserHomeDir()` | OK |
| Studio file browser (`hFS` + `FilePicker`) | "Parent folder" uses `filepath.Dir`; no list of roots | Needs a drive list (`C:\`, `D:\`…) to switch drives | OK |
| `build.sh`, `scripts/*.sh` | bash, `make`, `cmake`, `nasm` | Cross-compile from Linux with mingw-w64, or MSYS2 on Windows | Run on a Mac (Xcode command line tools), or osxcross |
| `-tags netgo,osusergo`, `-extldflags '-static-libstdc++ -static-libgcc'` | Avoid glibc NSS/resolver and dynamic libstdc++ | `-extldflags -static` with MinGW gives a fully static `.exe` | macOS cannot link fully static (libSystem is always dynamic); the result still needs nothing installed |

Paths elsewhere use `filepath` and `os.UserCacheDir()` (cache: `%LocalAppData%\audioviz` on
Windows, `~/Library/Caches/audioviz` on macOS), so they port as they are.

## GPU on macOS

macOS has no native Vulkan. Options:

- **MoltenVK** (Vulkan on top of Metal) can be linked statically into the binary. The compute
  shader must be checked against MoltenVK's limits: it uses storage images/buffers and shared
  memory for tile culling, and writes NV12 planes. Test before relying on it.
- For **speech recognition**, ggml has a native **Metal** backend (`GGML_METAL=ON`) that is
  better on Apple GPUs than Vulkan through MoltenVK. It would replace `libggml-vulkan.a` and the
  shim on macOS builds.
- Without either, everything still works on the CPU (renderer fallback, ggml CPU backend).

## Windows specifics

- Build from Linux with `x86_64-w64-mingw32-gcc`/`g++` (cgo: `GOOS=windows CGO_ENABLED=1 CC=...`),
  building x264, FFmpeg, whisper.cpp and demucs.cpp with the same cross toolchain. The result is
  a single static `.exe` that only needs the GPU driver.
- glslc / SPIR-V: shaders are compiled at build time and embedded, so nothing changes at runtime.
- SmartScreen warns about unsigned executables the first time; signing is optional.

## macOS specifics

- Build on a Mac (or osxcross) for `arm64` and/or `amd64`; a universal binary can be made with
  `lipo`.
- Distributing to other people without Gatekeeper blocking it requires signing and notarization
  (Apple developer account). For personal use it can be allowed manually.

## Suggested order

1. Windows: `LoadLibrary` in `vk.c` and `vkshim.c`, browser opener, home dir, drive list in the
   file browser, cross-compile scripts. Test on a real Windows machine with a GPU.
2. macOS: arm64 flags, Metal backend for ggml, MoltenVK for the renderer (or CPU rendering
   first), build on a Mac.
