# Build internals

How `./build.sh` produces a single `audioviz` binary that depends only on glibc, and why each
step and flag is there. The short user-facing version (requirements, one command) is
[../build.md](../build.md); this page is for whoever maintains the build or ports it.


> **Targets.** The build now has a target (`scripts/target.sh`): this machine by default, or
> `GOOS=windows` to cross-compile a Windows `.exe` from Linux with zig. Libraries go to
> `third_party/build` (Linux) or `third_party/build-<os>-<arch>`, out-of-tree build dirs to
> `third_party/obj-<os>-<arch>`. The cgo flags pick the right directory per `GOOS`/`GOARCH`.
> Per-system details (Windows, macOS/Metal, what is tested) are in [portability.md](portability.md);
> the sections below describe the Linux build, which the others follow.

## Result

Expected `ldd` output (glibc only):

```
$ ldd audioviz
    linux-vdso.so.1
    libm.so.6        # glibc
    libc.so.6        # glibc
    /lib64/ld-linux-x86-64.so.2
```

Inside the binary:

| Component | How it gets in |
|---|---|
| Go code, the web studio (`internal/studio/web`) | Go compiler, `//go:embed web` |
| Vulkan compute shader `render.spv` | `//go:embed shaders/render.spv` (internal/gpu/gpu.go) |
| x264 (H.264 encoder) | static `libx264.a` via cgo (internal/encode/x264.go) |
| Decode-only FFmpeg (libavformat, libavcodec, libswscale, libavutil) | static `.a` via cgo (internal/video/ffdec.go) |
| whisper.cpp + stable-diffusion.cpp on **one** ggml (CPU and Vulkan backends, incl. ggml's own SPIR-V shaders) | static `.a` via cgo (internal/lyricsgen/whisper.go, internal/imagegen/sd.go; Vulkan bridge in internal/ggml) |
| demucs.cpp (+ header-only Eigen) | static `libdemucs.a` + cgo C++ glue `dmx.cpp` (internal/lyricsgen/demucs.go) |
| libstdc++, libgcc | linked statically (`-static-libstdc++ -static-libgcc`, `-l:libstdc++.a`) |
| libvulkan | **not linked**: opened with `dlopen` at runtime if present |

Not inside: the ML model weights (downloaded on first use to `~/.cache/audioviz/models`, see
[lyrics-generation.md](lyrics-generation.md)) and the GPU driver.

## `build.sh` step by step

```bash
set -euo pipefail
cd "$(dirname "$0")"
scripts/build-x264.sh
scripts/build-ffmpeg.sh
scripts/build-ml.sh
if command -v glslc >/dev/null && [ internal/gpu/shaders/render.comp -nt internal/gpu/shaders/render.spv ]; then
  glslc -O --target-env=vulkan1.1 internal/gpu/shaders/render.comp -o internal/gpu/shaders/render.spv
fi
CGO_ENABLED=1 go build -trimpath -tags netgo,osusergo \
  -ldflags="-s -w -extldflags '-static-libstdc++ -static-libgcc'" -o audioviz ./cmd/audioviz
echo "OK -> ./audioviz ($(du -h audioviz | cut -f1))"; ldd audioviz 2>/dev/null || true
```

1. **Third-party static libraries** (each script is idempotent, see below). They install into
   `third_party/build/{lib,include}`.
2. **Shader**: the renderer's GLSL compute shader is compiled to SPIR-V only if `glslc` exists
   **and** `render.comp` is newer than `render.spv` (`-nt` is also true when the `.spv` doesn't
   exist). `-O` optimizes; `--target-env=vulkan1.1` matches the API version the renderer requests.
   The resulting `render.spv` is embedded, so it must be present at `go build` time: it is meant to
   be kept in the repository so that building without `glslc` works as long as the shader is not
   modified.
3. **Go build**:

   | Flag | Why |
   |---|---|
   | `CGO_ENABLED=1` | x264, FFmpeg, whisper, demucs and the Vulkan C code are cgo |
   | `-trimpath` | no local paths in the binary (reproducibility, smaller, no leaked home dirs) |
   | `-tags netgo` | pure-Go DNS resolver. Otherwise `net` uses cgo `getaddrinfo`, which pulls in libresolv / NSS modules loaded dynamically by glibc (and the warning about `getaddrinfo` in statically-linked programs). The binary does HTTP for model downloads, so `net` is linked |
   | `-tags osusergo` | pure-Go `os/user` (reads `/etc/passwd`) instead of cgo `getpwuid_r` → NSS |
   | `-ldflags -s -w` | strip symbol table and DWARF (size) |
   | `-extldflags '-static-libstdc++ -static-libgcc'` | passed to the external linker (gcc/g++): link the C++ runtime and libgcc statically, since whisper.cpp/ggml and demucs.cpp are C++ and a shared `libstdc++.so.6` would tie the binary to the build machine's GCC version |

   Not `-extldflags -static`: a fully static glibc binary cannot `dlopen` the system's
   `libvulkan.so.1` (and through it the GPU driver) reliably, which the GPU path needs. So glibc
   (`libc`, `libm`, the dynamic loader) stays dynamic, and everything else is static.

4. Prints the size and `ldd` output as a quick check that no unexpected `.so` slipped in.

Build-time requirements: `go` (go.mod says `go 1.26.6`), `gcc`/`g++`, `cmake`, `nasm`
(x264's and FFmpeg's x86 assembly), `git`, Vulkan headers (`vulkan/vulkan.h`, needed by
`vk.c`, `vkshim.c` and ggml-vulkan) and `glslc` (shaderc; needed by ggml-vulkan's build, and for
`render.comp` when it changes). The comment at the top of `build.sh` lists a subset; this list
and `../build.md` are the complete one.

## `third_party/` layout

```
third_party/
├── x264/            git clone (branch stable, depth 1)            ← .gitignore
├── ffmpeg/          git clone (branch release/7.1, depth 1)       ← .gitignore
├── whisper.cpp/     git clone, checked out at WHISPER_REV         ← .gitignore
├── stable-diffusion.cpp/  git clone at SD_REV (+ its ggml submodule) ← .gitignore
├── obj-<os>-<arch>/ml/    cmake build tree of scripts/ml (both + ggml)
├── demucs.cpp/      git clone, checked out at DEMUCS_REV          ← .gitignore
│   ├── vendor/eigen submodule (depth 1)
│   └── obj/         object files
└── build/                                                           ← .gitignore
    ├── include/     x264.h, libav*/…, whisper.h, stable-diffusion.h, ggml*.h
    └── lib/         libx264.a, libavformat.a, libavcodec.a, libswscale.a, libavutil.a,
                     libwhisper.a, libstable-diffusion.a, libggml.a, libggml-base.a,
                     libggml-cpu.a, libggml-vulkan.a,
                     libdemucs.a  (+ pkgconfig/ from the autotools installs)
```

Nothing under `third_party/` is committed. Each script decides what to do by looking at
`third_party/build/lib`:

| Script | Skips when | To force a rebuild |
|---|---|---|
| `build-x264.sh` | `libx264.a` exists | delete `third_party/build/lib/libx264.a` (and `third_party/x264` to fetch a newer `stable`) |
| `build-ffmpeg.sh` | `libavcodec.a` **and** `libswscale.a` exist | delete those (and `third_party/ffmpeg` to re-clone) |
| `build-ml.sh` (whisper + stable-diffusion part) | `libwhisper.a` **and** `libstable-diffusion.a` exist | delete either; the script then re-checks out `WHISPER_REV` / `SD_REV` and rebuilds both |
| `build-ml.sh` (demucs part) | `libdemucs.a` exists | delete `libdemucs.a` |

Pitfall: changing `WHISPER_REV`/`DEMUCS_REV` in the script does **nothing** while the `.a`
exists; delete the library too. Also, a stale header set in `third_party/build/include` from an
older whisper.cpp is overwritten only when the whisper part re-runs.

## The libraries

### x264 — `scripts/build-x264.sh`

- Source: `https://code.videolan.org/videolan/x264.git`, branch **`stable`**, shallow clone.
  **Not pinned to a revision**: a fresh clone months apart can give a different x264.
- Configure:

  ```
  --prefix=third_party/build --enable-static --enable-pic --disable-cli --disable-opencl
  --disable-avs --disable-swscale --disable-lavf --disable-ffms --disable-gpac --disable-lsmash
  --bit-depth=8 --chroma-format=420
  ```

  Library only (no CLI), position-independent (it goes into a PIE Go binary), no optional input
  libraries (they would add dependencies and are useless: frames come from our renderer as NV12),
  no OpenCL lookahead (would `dlopen` OpenCL), and only 8-bit 4:2:0 — the only format exported —
  which keeps the library smaller.
- cgo (`internal/encode/x264.go`):
  `CFLAGS: -I…/third_party/build/include`, `LDFLAGS: …/libx264.a -lm -lpthread -ldl`.

### FFmpeg — `scripts/build-ffmpeg.sh`

Used only to **decode** video backgrounds; images and GIFs are decoded in Go, audio is decoded
in Go, encoding is x264 and muxing is our own MP4 writer (see [media-io.md](media-io.md)).

- Source: `https://git.ffmpeg.org/ffmpeg.git`, branch **`release/7.1`**, shallow clone (the
  release branch, not a pinned tag/commit).
- Configure:

  ```
  --prefix=third_party/build --enable-static --disable-shared --enable-pic
  --disable-programs --disable-doc --disable-network --disable-autodetect
  --disable-everything --disable-avdevice --disable-avfilter --disable-swresample
  --enable-avformat --enable-avcodec --enable-swscale
  --enable-protocol=file
  --enable-demuxer=mov,matroska,avi,mpegts,m4v,h264,hevc
  --enable-decoder=h264,hevc,vp8,vp9,mpeg4,mjpeg,prores,png,mpeg2video
  --enable-parser=h264,hevc,vp8,vp9,mpeg4video,mpegvideo,mjpeg,png
  ```

  `--disable-everything` + explicit allow-lists keep it small and, more importantly,
  `--disable-autodetect` stops configure from picking up whatever system libraries happen to be
  installed (VAAPI, VDPAU, libdrm, zlib, xlib, SDL…) — each would become a runtime dependency.
  `mov` covers MP4/MOV/M4V, `matroska` covers MKV and WebM. No `--enable-gpl`/`--enable-nonfree`:
  the FFmpeg part stays **LGPL**.

  Note: FFmpeg's PNG decoder depends on zlib, which `--disable-autodetect` turns off, so the
  `png` decoder is most likely dropped by configure even though it is listed (it would only
  matter for PNG-in-MOV videos; still images do not go through FFmpeg). Check
  `third_party/ffmpeg/ffbuild/config.mak` (`CONFIG_PNG_DECODER`) if it ever matters.
- cgo (`internal/video/ffdec.go`): `CFLAGS: -I…/include`;
  `LDFLAGS: libavformat.a libavcodec.a libswscale.a libavutil.a -lm -lpthread` (order matters
  for static linking: users before their dependencies).

### whisper.cpp + stable-diffusion.cpp on one ggml — `scripts/build-ml.sh`, `scripts/ml/`

- Sources: `https://github.com/ggml-org/whisper.cpp`, pinned
  **`WHISPER_REV=60c0be6ac8fa71b1a2ae2dd938a31a34a508e774`**, and
  `https://github.com/leejet/stable-diffusion.cpp`, pinned **`SD_REV=3f8527a46c54ecf4cb4ed6003da8e8982283c73c`**
  with its `ggml` submodule (leejet's fork of ggml).
- **One ggml.** Both projects vendor ggml; two static copies in one binary would clash at link
  time. `scripts/ml/CMakeLists.txt` is a small superproject: `add_subdirectory` of
  stable-diffusion.cpp (which builds its ggml) and then of whisper.cpp, whose CMake reuses an
  existing `ggml` target. stable-diffusion.cpp compiles ggml with **`GGML_MAX_NAME=160`**, which
  changes the size of `struct ggml_tensor`, so the superproject sets it for everything, and the
  cgo files (`whisper.go`, `sd.go`) define it too. Without it whisper.cpp would read tensors with
  the wrong layout.
- WebP/WebM support of stable-diffusion.cpp is off (`SD_WEBP=OFF SD_WEBM=OFF`): Go writes the PNGs.
  Its other dependencies (zip, oniguruma, utf8proc) are object libraries inside `libstable-diffusion.a`.
- CMake:

  ```
  -DCMAKE_BUILD_TYPE=Release -DBUILD_SHARED_LIBS=OFF
  -DGGML_OPENMP=OFF                      # no libgomp.so dependency; ggml uses its own threadpool
  -DGGML_NATIVE=OFF                      # don't tune for the build machine...
  -DGGML_AVX=ON -DGGML_AVX2=ON -DGGML_FMA=ON -DGGML_F16C=ON   # ...but target x86-64-v3
  -DGGML_VULKAN=ON -DSD_VULKAN=ON        # GPU backend (macOS: GGML_METAL / SD_METAL)
  -DSD_BUILD_EXAMPLES=OFF -DSD_WEBP=OFF -DSD_WEBM=OFF
  -DWHISPER_BUILD_TESTS=OFF -DWHISPER_BUILD_EXAMPLES=OFF -DWHISPER_BUILD_SERVER=OFF
  ```

  built with `--target whisper stable-diffusion`. The Vulkan backend's build compiles its compute shaders from
  GLSL with `glslc` (via its `vulkan-shaders-gen` helper) and embeds them in
  `libggml-vulkan.a` — this is why `glslc` and the Vulkan headers are needed even if
  `render.comp` never changes.
- Copied out: `libwhisper.a libstable-diffusion.a libggml.a libggml-base.a libggml-cpu.a
  libggml-vulkan.a` and the headers `whisper.h`, `stable-diffusion.h`, ggml's `include/*.h`.
- cgo (`internal/lyricsgen/whisper.go`): `CFLAGS: -I…/include`;
  `LDFLAGS: libwhisper.a libggml.a libggml-vulkan.a libggml-cpu.a libggml-base.a -lm -lpthread -ldl`.
  Note there is **no `-lvulkan`**: see "Runtime Vulkan loading".

### demucs.cpp — `scripts/build-ml.sh`

- Source: `https://github.com/sevagh/demucs.cpp`, pinned
  **`DEMUCS_REV=f1206e9adeea103aef4a636b9e62297cf1f8e34e`**, plus the `vendor/eigen` submodule
  (depth 1).
- Not built with its CMake (which builds CLI tools and may pull OpenMP); instead every
  `src/*.cpp` is compiled **in parallel** (one background `g++` per file, then `wait`) with

  ```
  -O3 -march=x86-64-v3 -ffast-math -fno-finite-math-only -DNDEBUG -std=c++17 -fPIC -w
  -I demucs.cpp/src -I demucs.cpp/vendor/eigen
  ```

  and archived with `ar rcs libdemucs.a`. `-ffast-math` gives Eigen vectorized math;
  `-fno-finite-math-only` keeps NaN/Inf checks working (fast-math alone lets the compiler assume
  they never happen).
- cgo (`internal/lyricsgen/demucs.go`) compiles the glue `dmx.cpp` with
  `CXXFLAGS: -std=c++17 -O3 -march=x86-64-v3 -DNDEBUG -w -I…/demucs.cpp/src -I…/vendor/eigen`
  (it includes demucs headers directly, hence the source tree is needed at Go build time, not just
  the `.a`) and links `LDFLAGS: libdemucs.a -l:libstdc++.a`. `-l:libstdc++.a` asks the linker for
  the static archive by file name, so the C++ runtime is linked statically even by a plain
  `go build` without `build.sh`'s `-extldflags`; in the full build, `-static-libstdc++` also
  stops `g++` (which Go uses as the external linker when C++ is present) from adding the shared
  `libstdc++.so` on its own.

### CPU baseline

ggml (AVX2/FMA/F16C) and demucs (`-march=x86-64-v3`) target **x86-64-v3** (Intel Haswell 2013+,
AMD Excavator/Zen+). The binary starts on older CPUs, but lyrics generation would hit illegal
instructions there. Rendering and export use only Go and x264/FFmpeg (which dispatch their SIMD
at runtime), so they are unaffected. Building for another architecture (arm64) needs these flags
changed; see [portability.md](portability.md).

## Runtime Vulkan loading

The binary must start, render (on the CPU) and generate lyrics (on the CPU) on machines without a
Vulkan driver. Linking `libvulkan.so.1` would make the dynamic loader refuse to start the program
there, so Vulkan is never linked; it is opened at runtime in two places.

### Renderer: `internal/gpu/vk.c`

- Compiled `#define VK_NO_PROTOTYPES` (only the types from `vulkan.h`, no function symbols).
- `load_lib()` tries `dlopen("libvulkan.so.1")`, `"libvulkan.so"`, `"libvulkan.1.dylib"`,
  `"libMoltenVK.dylib"` (`RTLD_NOW | RTLD_LOCAL`), then `dlsym("vkGetInstanceProcAddr")`.
- All other entry points are function pointers resolved through the X-macro lists
  `VK_GLOBAL_FUNCS` (via `vkGetInstanceProcAddr(NULL, …)`), `VK_INSTANCE_FUNCS` (after
  `vkCreateInstance`) and `VK_DEVICE_FUNCS` (via `vkGetDeviceProcAddr`).
- Failure returns an error string ("libvulkan not found (Vulkan driver not installed)", …) to Go;
  `engine.NewRenderer` logs it and uses the CPU renderer unless `renderer: gpu` was forced.
- `internal/gpu/gpu.go`: `#cgo linux LDFLAGS: -ldl` (on glibc ≥ 2.34 `dlopen` lives in libc, the
  flag is harmless and keeps older glibc working).

### Whisper (ggml-vulkan): `internal/lyricsgen/vkshim.c`

ggml's Vulkan backend (C++, Vulkan-Hpp) references a handful of Vulkan symbols directly and
gets everything else through `vkGetInstanceProcAddr` / `vkGetDeviceProcAddr`. Instead of linking
`-lvulkan`, `vkshim.c` (compiled into the `lyricsgen` package by cgo, being a `.c` file in a cgo
package directory) **defines those symbols itself**:

| Symbol | Shim behaviour |
|---|---|
| `vkGetInstanceProcAddr` | on first call (`pthread_once`) `dlopen("libvulkan.so.1" / "libvulkan.so")`; forwards to the real one if found. If not: returns stubs where `vkCreateInstance` fails with `VK_ERROR_INITIALIZATION_FAILED`, `vkEnumerateInstanceVersion` reports 1.0 and the extension/layer enumerations return 0 items; anything else `NULL` |
| `vkGetDeviceProcAddr`, `vkCmdCopyBuffer`, `vkGetPhysicalDeviceFeatures2` | resolved lazily with `dlsym` on the loaded library (only reachable once an instance exists, i.e. Vulkan is present) |

Since instance creation fails cleanly, ggml finds no Vulkan device and whisper runs on the CPU
backend. `AUDIOVIZ_NO_VULKAN=1` (any non-empty value other than `0`) skips the `dlopen` to test
that path on a machine that has Vulkan.

If a future whisper.cpp revision makes ggml-vulkan import another Vulkan function directly, the
link fails with an undefined reference to it: add a forwarding function to `vkshim.c` like the
existing ones.

## GLSL → SPIR-V

| Shader | Compiled by | When | Embedded as |
|---|---|---|---|
| `internal/gpu/shaders/render.comp` (the whole frame renderer, see [rendering.md](rendering.md)) | `build.sh`: `glslc -O --target-env=vulkan1.1` | only if `glslc` exists and the `.comp` is newer | `//go:embed shaders/render.spv` → `renderSPV`, passed to `vkr_create` |
| ggml's Vulkan kernels | whisper.cpp's CMake (`vulkan-shaders-gen` + `glslc`) | when `libggml-vulkan.a` is built | inside `libggml-vulkan.a` |

`go generate` is not used. When editing `render.comp`, run `./build.sh` (or the `glslc` line) and
commit the regenerated `render.spv` together with the source.

## cgo flags summary

| File | Flags |
|---|---|
| `internal/encode/x264.go` | `CFLAGS: -I${SRCDIR}/../../third_party/build/include`; `LDFLAGS: …/libx264.a -lm -lpthread -ldl` |
| `internal/video/ffdec.go` | `CFLAGS: -I…/include`; `LDFLAGS: …/libavformat.a …/libavcodec.a …/libswscale.a …/libavutil.a -lm -lpthread` |
| `internal/gpu/gpu.go` | `linux LDFLAGS: -ldl` (+ `vk.c` in the package) |
| `internal/lyricsgen/whisper.go` | `CFLAGS: -I…/include`; `LDFLAGS: …/libwhisper.a …/libggml.a …/libggml-vulkan.a …/libggml-cpu.a …/libggml-base.a -lm -lpthread -ldl` (+ `vkshim.c`) |
| `internal/lyricsgen/demucs.go` | `CXXFLAGS: -std=c++17 -O3 -march=x86-64-v3 -DNDEBUG -w -I…/demucs.cpp/src -I…/demucs.cpp/vendor/eigen`; `LDFLAGS: …/libdemucs.a -l:libstdc++.a` (+ `dmx.cpp`) |

Static archives are given by absolute path through `${SRCDIR}` rather than `-L`/`-l`, so the
linker can never silently pick a system shared library with the same name instead.

## Licenses

| Component | License | Effect |
|---|---|---|
| x264 | GPL-2.0-or-later | the **distributed binary is GPL**: whoever ships it must offer the complete corresponding source (AudioViz + the third-party sources/scripts) |
| FFmpeg (as configured: no `--enable-gpl`, no `--enable-nonfree`) | LGPL-2.1-or-later | compatible with the GPL binary |
| whisper.cpp / ggml | MIT | — |
| demucs.cpp | MIT (check upstream at the pinned revision) | — |
| Eigen | MPL-2.0 (header-only) | — |
| Preact, htm (vendored in `web/vendor/preact-htm.js`) | MIT, Apache-2.0 | keep notices if redistributed separately |
| Lucide icons (`web/icons.js`) | ISC | notice kept at the top of `icons.js` |
| Model weights (downloaded, not embedded) | Whisper: MIT; htdemucs: MIT | not part of the binary |

Removing x264 (e.g. using a hardware or LGPL encoder) would be the only way to make the binary
non-GPL; everything else is permissive or LGPL.

## Development workflow notes

- There are no separate sync or dev scripts; `./build.sh` is the whole build, and after the first
  run (the slow part: x264, FFmpeg, whisper.cpp with its shader compilation, demucs) only
  `go build` does real work.
- For Go-only changes, re-running `./build.sh` is cheap (the scripts skip finished libraries); a
  plain `go build ./cmd/audioviz` also works once `third_party/build` exists, but without the
  `netgo,osusergo` tags and `-extldflags` it may pick up extra dynamic dependencies — check `ldd`
  before shipping such a binary. `go vet`/`go test` also need the libraries because the cgo packages link them.
- The web studio is embedded: JS/CSS changes need a Go rebuild to be served.
- Everything goes through cgo, so cross-compiling needs a C/C++ cross toolchain and libraries
  built for the target; see [portability.md](portability.md).
