# Background

```yaml
background:
  path: "fondo.png"
  fit_mode: "cover"
  opacity: 0.9
  darken: 0.12
  vignette: 0.25
  loop_video: true
```

| Field | Default | Description |
|---|---|---|
| `path` | — | PNG, JPEG, WebP, BMP image, animated GIF or **video** (mp4, mov, mkv, webm, avi; H.264, H.265, VP8, VP9, ProRes...) |
| `fit_mode` | cover | `cover` (fills and crops), `contain` (fits entirely, black bars), `stretch` (distorts) |
| `opacity` | 0.88 | Scales the background brightness (0–1) |
| `darken` | 0.15 | Constant darkening (0–1) to make the visualizer stand out |
| `vignette` | 0.25 | Darkens the edges (0–1) |
| `loop_video` | true | Loops the video/GIF if it is shorter than its segment |

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
