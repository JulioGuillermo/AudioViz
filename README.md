# AudioViz

Audio-reactive music video generator. Give it a song and an image or video, and it renders a
music video where the background, a spectrum visualizer, particles and synced lyrics move with
the music. Everything runs from a **single binary with nothing to install**, rendering on the
**GPU through Vulkan** (any vendor) with an automatic CPU fallback.

- **Studio**: a web editor served by the binary itself, with a live GPU preview, a timeline of
  segments for every effect, lyrics editing word by word, titles, undo/redo and export.
- **Reactivity**: every effect follows a chosen band (bass, mids, treble, beats…), adaptively
  normalized so quiet and loud parts of the song both move.
- **Background**: images, GIFs or videos, with slow camera motion (zoom, pan, Ken Burns, drift,
  sway) and 13 transitions between them (fade, slide, wipe, circle, glitch, pixelate…), plus
  zoom, shake, chromatic aberration, ripple, glitch and flash effects driven by the audio.
- **Visualizer**: circular, horizontal, vertical or oscilloscope, with gradients and glow.
- **Particles**: sparks, bursts and dust, in layers that react to different bands.
- **Lyrics**: `.srt` / `.lrc` with per-word karaoke, positions, in/out animations, fire-like
  colors and audio reaction — and **generated from the plain-text lyrics** inside the binary
  (vocal separation with Demucs + Whisper recognition on the GPU + alignment to your text).
- **Texts**: titles and credits placed by hand, with the same styles as the lyrics.
- **Output**: H.264 (x264) with lossless FLAC audio in an MP4.

## Quick start

```bash
./build.sh                       # builds ./audioviz (see "Building")
./build.sh install               # or install it in ~/go/bin to use it from any folder
./audioviz studio                # opens the studio in the browser (New → creates a project)
./audioviz studio my-video/      # opens an existing project folder
./audioviz project.yaml          # renders a project from the command line
./audioviz --init project.yaml   # writes a project template with every option
```

In the studio: choose the song, a background, optionally the lyrics, then add segments on the
timeline to change effects over the song, and export. Projects are plain YAML files
(`project.yaml`) that can also be edited by hand — see
[projects/examples](projects/examples/horizontal_synthwave.yaml).

## Documentation

- **[User documentation](docs/index.md)**: studio, every effect and option, lyrics, texts,
  command line, building.
- **[Technical documentation](docs/technical/README.md)**: architecture, GPU renderer, audio
  analysis, lyrics generation and alignment, studio internals, build, known issues.

## Building

```bash
./build.sh
```

Requirements for building only (Linux): `go`, `gcc`/`g++`, `cmake`, `nasm`, `git`, Vulkan and
SPIR-V headers and `glslc`; other systems in [docs/build.md](docs/build.md). The script downloads and builds x264, a decode-only FFmpeg, whisper.cpp and
demucs.cpp once, as static libraries that end up inside the binary; on Linux the result depends
only on the C library. Details in [docs/build.md](docs/build.md).

The AI models for lyrics generation (Whisper large-v3-turbo, ~574 MB; htdemucs, ~84 MB) are
downloaded on first use to the user cache folder.

## Platforms

```bash
./build.sh                 # Linux, or macOS on a Mac  -> ./audioviz
GOOS=windows ./build.sh    # Windows, cross-compiled from Linux with zig -> ./audioviz.exe
```

| | GPU (video / lyrics) | Status |
|---|---|---|
| Linux x86-64 | Vulkan / Vulkan | ✅ main platform |
| macOS (Apple Silicon) | MoltenVK on Metal / Metal | ✅ tested on a Mac mini M4 (115 fps exporting 1080p60) |
| Windows x86-64 | Vulkan / Vulkan | 🧪 cross-compiled from Linux; tested under Wine (CPU rendering, export, studio). GPU still to test on real Windows |

Details in [docs/technical/portability.md](docs/technical/portability.md).

## License

The binary links x264, which is GPL-2.0, so distributed binaries are GPL-2.0. FFmpeg is built in
LGPL mode; whisper.cpp, demucs.cpp, Eigen and Preact/htm have permissive licenses.
