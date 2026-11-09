# Background

```yaml
background:
  path: "fondo.png"
  fit_mode: "cover"
  opacity: 0.9
  darken: 0.12
  vignette: 0.25
  loop_video: true
  motion: { type: ken_burns, amount: 0.15 }   # see motion.md
```

- **[Motion](motion.md)**: slow zoom, pan, Ken Burns, drift or sway over each image, so it is not static.
- **[Transitions](transitions.md)**: how one background segment replaces the previous one (fade, slide, wipe, circle, glitch...).

| Field | Default | Description |
|---|---|---|
| `path` | — | PNG, JPEG, WebP, BMP image, animated GIF or **video** (mp4, mov, mkv, webm, avi; H.264, H.265, VP8, VP9, ProRes...) |
| `fit_mode` | cover | `cover` (fills and crops), `contain` (fits entirely, black bars), `stretch` (distorts) |
| `opacity` | 0.88 | Scales the background brightness (0–1) |
| `darken` | 0.15 | Constant darkening (0–1) to make the visualizer stand out |
| `vignette` | 0.25 | Darkens the edges (0–1) |
| `loop_video` | true | Loops the video/GIF if it is shorter than its segment |
| `motion` | none | Camera motion over the image: see [motion.md](motion.md) |

## Effects (`background_effects`)
They are applied in this order: glitch → chromatic aberration → ripple → zoom/shake → color.
Each one can react to a different band with [`react_to`](../reactivity.md).

| Effect | Description |
|---|---|
| [bass_zoom](bass_zoom.md) | Pulsing zoom |
| [shake](shake.md) | Camera movement |
| [chromatic_aberration](chromatic_aberration.md) | RGB separation |
| [ripple](ripple.md) | Ripple |
| [glitch](glitch.md) | Displaced stripes |
| [color_flash](color_flash.md) | Brightness/contrast flash |

Any of these segments can be **tied to background segments** (`bind`, see
[studio.md](../studio.md#tied-to-background-segments)): it applies only while one of them is on
screen, and during a transition only to its image (parallax and depth of field too: the camera
moves, or the lens blurs, only that image and what is in depth on it).
