# AudioViz — Documentation

Audio-reactive music video generator. A single binary with no dependencies:
**GPU rendering (Vulkan, any vendor)** with automatic CPU fallback, H.264 (built-in x264)
and lossless FLAC audio inside an MP4.

## Getting started
- **[AudioViz Studio](studio.md)**: graphical interface with preview and timeline (`audioviz studio`)
- **[Generating images](images.md)**: backgrounds from a prompt, or editing images, with AI inside the binary (Z-Image, FLUX.2 klein, Qwen Image)
- [Timeline in the YAML](timeline.md): change effects, background, visualizer or lyrics in each section
- [Command line](cli.md) — rendering, previews, single frames, GPU selection
- [Building](build.md)
- **[Technical documentation](technical/README.md)**: how everything works inside — architecture, GPU renderer, audio analysis, lyrics generation and alignment, studio internals, build, portability
- [YAML project structure](#yaml-structure)

## Audio and reactivity
- [Audio](audio.md) — formats, volume, sensitivity
- [Reactivity and normalization](reactivity.md) — `react_to` (bass/mid/treble/...) and adaptive normalization

## Background — [overview](background/README.md) · [motion](background/motion.md) · [transitions](background/transitions.md)
| Effect | What it does | Reacts by default to |
|---|---|---|
| [bass_zoom](background/bass_zoom.md) | Pulsing zoom from the center | bass |
| [shake](background/shake.md) | Camera shake/movement | bass |
| [chromatic_aberration](background/chromatic_aberration.md) | RGB channel separation | bass |
| [ripple](background/ripple.md) | Water / heat-haze ripple | mids |
| [glitch](background/glitch.md) | Displaced stripes (VHS) | beats |
| [color_flash](background/color_flash.md) | Brightness and contrast flash | bass |

## Visualizer — [overview](visualizer/README.md)
| Type | Description |
|---|---|
| [circular](visualizer/circular.md) | Central ring, L on the left and R on the right |
| [horizontal](visualizer/horizontal.md) | Bars from the center outwards to the sides |
| [vertical](visualizer/vertical.md) | Bars from the side edges inwards |
| [oscilloscope](visualizer/oscilloscope.md) | Two wavy neon lines |

- [Colors and gradients](visualizer/gradients.md) — per bar or along the bar, relative to the bar or to the maximum
- [Glow](visualizer/README.md#glow)

## Particles — [overview and layers](particles/README.md)
| Type | Description |
|---|---|
| [rising_sparks](particles/rising_sparks.md) | Sparks rising from the bottom (fire) |
| [radial_burst](particles/radial_burst.md) | Burst from the center |
| [ambient_dust](particles/ambient_dust.md) | Dust floating across the whole image |

## Lyrics and texts
- **[Texts (titles)](texts.md)**: fixed texts from one time to another at a position you choose, with all the lyrics styles
- [Synced lyrics](lyrics/README.md): `.srt`, `.lrc` and LRC with per-word timing. Karaoke, positions (top/bottom/left/right/side...), in and out animations, fire colors and audio reactivity
- [Generate the per-word `.lrc`](lyrics/generate.md) from the audio and the plain-text lyrics (Suno format supported), built into the binary · [step by step](lyrics/step-by-step.md)

## Output
- [Output video](output.md) — resolution, fps, quality, GPU/CPU

## YAML structure
```yaml
audio:              # audio.md, reactivity.md
background:         # background/README.md
background_effects: # one block per effect: background/*.md
visualizer:         # visualizer/*.md
particles:          # particles/*.md
lyrics:             # lyrics/README.md
texts:              # texts.md
output:             # output.md
```
Generate a template with everything: `audioviz --init nuevo.yaml`. Paths are relative to the YAML.
