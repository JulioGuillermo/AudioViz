# Known issues and technical debt

Found while writing the technical documentation by reading the code (2026-10-03). None of them is
fixed yet unless marked. Each entry says where it lives; the linked page explains the context.

## Worth fixing first (user-visible)

| Issue | Where | Effect |
|---|---|---|
| `project.yaml` is fully rewritten on every studio save | `internal/studio/project.go`, `config.Save` | Hand-written comments, key order and formatting are lost; edits made by hand while the studio is open are overwritten (no mtime check) |
| Two saves can write the same `project.yaml.tmp` at once | project autosave + lyrics autosave → `config.Save` | Rare corrupted/lost save |
| Empty LRC rows (`[00:12.00]`) do not close the previous line | `internal/lyrics/parse.go` (`parseLRC` drops wordless rows before the loop that handles them) | The line lasts until the estimated end instead of the marked one; `docs/lyrics/README.md` says it closes it |
| Cancelling an export leaves `<output>.video.tmp` (and its file handle open) | `engine.ExportRange` returns before closing the MP4 writer | Stray temp file |
| Lyrics generation always writes `<project dir>/lyrics.lrc` | `hGenLyrics` | A project using another lyrics file is re-pointed to `lyrics.lrc` (the old `lyrics.lrc`, if any, is renamed to a timestamped backup) |
| The second pass of lyrics generation gives Whisper the lyrics as prompt | `lyricsgen/pipeline.go` (doubtful sections) | Resync showed a prompt with the target text makes Whisper skip those words; this pass may anchor little. Measure before changing |
| Resync is a plain HTTP request, not a job | `hResync` | Cannot be cancelled; if the Whisper model is missing it downloads ~574 MB inside the request with no progress |
| `/api/upload` overwrites a file with the same name in `assets/` | `internal/studio/server.go` | Silent replacement |
| AV1 videos (common in `.webm`) are not decodable | `scripts/build-ffmpeg.sh` has no AV1 decoder | "no supported video stream" |
| Animated GIF frames are all pre-scaled and kept in memory | `internal/video/input.go` | ~830 MB for 100 frames at 1080×1920 |

## Security

The studio listens on `127.0.0.1` by default but has **no authentication and no Origin/CSRF
check**, and `readJSON` ignores `Content-Type`. Any web page open in the same browser can POST
to it (open a project, export, generate), and `/api/fs` + `/api/media` serve any file the user
can read. With `--addr 0.0.0.0:...` this extends to the network. Fix: check `Origin`/`Host`,
require `application/json`, and/or a per-session token in the page.

## Behaviour that depends on fps

Per-frame constants make the same song react differently at 30 and 60 fps: beat pulse decay
(×0.82 per frame), bar decay and the flux threshold (`internal/audio/analyzer.go`), particle
velocities in px/frame and the ripple phase `idx·0.05·speed` (renderer). Converting them to
per-second rates would make the look fps-independent.

## Rendering: GPU vs CPU and limits

| Issue | Where |
|---|---|
| Silent caps on the GPU only: glitch strips ≤ 64 (exceeded above intensity ≈ 0.83 at 1920 px), particles ≤ 1024 per frame and ≤ 512 per tile, lyric words ≤ 48 per tile | `render.comp`, `vk.c` |
| CPU/GPU differences: lyric chromatic aberration threshold (> 0 vs > 0.3 px), CPU oscilloscope ignores `gradient_axis: length`, glitch vs chromatic aberration order, ripple nearest (CPU) vs bilinear (GPU), CPU skips glitch ≤ 0.05 | see [rendering.md](rendering.md) |
| Each pixel only checks nearby bars (±2 circular, ±1 linear): very thick bars or wide glow get clipped | `render.comp` |
| After a seek particles are re-simulated for 5 s: preview frames are close to, not identical to, the export; `max_life` > 5 s is not fully rebuilt | `engine/session.go` |
| `find_mem` may fall back to non-coherent memory for Params/background/text buffers, which are never flushed | `internal/gpu/vk.c` (only matters on unusual hardware) |
| Back-to-back transitions jump if a segment starts while the previous one is still entering | `engine/session.go`, `config/lerp.go` |
| Lyrics always use the first segment's path and offset | `engine/session.go` |
| A shadow offset larger than about `pad − blur − outline` is cut at the word rectangle | `internal/lyrics/layout.go` |
| `Lerp` deep-copies through YAML even without a fade, 3× per frame | `config/lerp.go`, `Session.Prepare` (not measured as a bottleneck) |

## Audio analysis

| Issue | Where |
|---|---|
| More than 2 channels: even channels go to L, odd to R (5.1: centre only in L, LFE only in R) | `internal/audio/decoder.go` |
| With a 2048-point FFT several of the lowest bars read the same bin and show identical values | `internal/audio/analyzer.go` |
| `transient` reaches 1.5, while `docs/reactivity.md` says sources are 0–1 | `analyzer.go` |
| `SpectralFlux` is the positive change of bass energy, not spectral flux | `analyzer.go` (naming only) |
| Unknown `react_to` names fall back to bass silently; unknown YAML keys are ignored everywhere | `internal/audio/bands.go`, `internal/config` |
| Bars default to 56 in `DefaultConfig` but 48 in `Validate`/`FixVisualizer` | `internal/config/config.go` |

## Lyrics generation

| Issue | Where |
|---|---|
| Vocals cache key ignores the decode volume (studio uses `audio.volume`, CLI uses 1) | `lyricsgen`, `hGenLyrics` (effect probably small: thresholds are relative) |
| A truncated vocals cache file is accepted (only `len % 4` is checked); models are checked by size only, no checksum | `lyricsgen/pipeline.go`, `models.go` |
| `AUDIOVIZ_NO_VULKAN` only affects recognition, not the renderer (doc fixed) | `vkshim.c` |
| Alignment-heads preset `14` (large-v3-turbo) is a bare number, not taken from `whisper.h` | `lyricsgen/whisper.go` |
| Separation thread count formula duplicated in Go and C | `demucs.go`, `dmx.cpp` |
| `Word.Prob` is a running pairwise average (debug output only) | `whisper.go` |

## SRT parsing

Blocks are split on `"\n\n"`, so a separator line containing spaces merges two cues
(`internal/lyrics/parse.go`).

## Build

| Issue | Where |
|---|---|
| x264 and FFmpeg are shallow clones of branches (`stable`, `release/7.1`), not pinned revisions: builds are not reproducible | `scripts/build-x264.sh`, `build-ffmpeg.sh` |
| Changing `WHISPER_REV`/`DEMUCS_REV` has no effect until the matching `.a` is deleted | `scripts/build-ml.sh` |
| FFmpeg's PNG decoder probably isn't built (needs zlib, disabled by `--disable-autodetect`); only affects PNG inside video files (unverified) | `scripts/build-ffmpeg.sh` |

## Tests

`TestDecodeAndAnalyze` fails (instead of skipping) when the demo song is not present, and it is
not in the repository; `TestNormalizationStats` skips but asserts nothing. There are no tests
for the FLAC encoder (a manual round trip at 16/24-bit, 11.025/44.1/48 kHz was lossless), the
WAV/MP3/OGG decoders, `Lerp`, the LRC parser or the alignment code.

## Dead code and unused fields

- `internal/video/output.go` (original `ffmpeg`-pipe writer): nothing references it.
- `fxPart` (`engine/session.go`), `visualizer.RenderGlow`, `lyrics.New`, `Engine.StyleAt`,
  `Engine.Band`: no callers.
- Config fields never read: `visualizer.stereo_symmetric`, `visualizer.draw_mode`,
  `output.codec`, `output.audio_bitrate` (still written by `--init`).
- Shader: `Params.PartColor` never written; `flags3.z` always 0, `flags3.w` always 1.
- Studio: `job.Log` never written (the job log box never shows); `/api/probe` unused by the UI;
  `/api/state` fields `version`, `logs`, `lyricsTool` unused; SSE `log`/`project` events ignored;
  `Resync`'s `source` ignored by the UI.
- `tools/lyrics/` (empty, from the removed Python tool) and its `.gitignore` entries; a stray
  `.gitignore` line `I didn*t ask to be the devil`.

## Fixed since

- Background transitions to/from "no background" now fade (black side), and a transition between
  two segments with the same image works (each keeps its motion) — with the background
  layers/transitions work.

## Fixed while documenting

- Misleading comments: `smoothing` (it is the rise weight: higher = faster), `audio.sensitivity`
  (scales the bars only), `EncodeFLAC` (`maxSamples`), `WriteVideoAnnexB` (keeps SEI), shader
  `viz3.z` (center gap), `--init` help, `Resync`/`decayEnd`/`alignNW`/`finish` comments.
- Docs: `docs/output.md` example CRF (18), `docs/cli.md` subcommands, `docs/build.md` runtime
  dependencies, `AUDIOVIZ_NO_VULKAN` scope in `docs/lyrics/generate.md`, build requirements in
  `build.sh`.
