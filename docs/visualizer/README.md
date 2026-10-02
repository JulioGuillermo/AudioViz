# Visualizer

Stereo spectrum: `bars` logarithmic bands (25 Hz – 18 kHz) per channel, L and R separate.

```yaml
visualizer:
  enabled: true
  type: "circular"
  bars: 56
  min_height: 4.0
  max_height: 180.0
  bar_width: 5.0
  bar_gap: 3.5
  roundness: 2.5
  smoothing: 0.75
  decay_rate: 0.12
  color_mode: "gradient"
  gradient: [...]
  glow: { enabled: true, radius: 12.0, intensity: 0.65 }
```

## Types
| `type` | Aliases | Page |
|---|---|---|
| `circular` | `circle`, `radial` | [circular](circular.md) |
| `horizontal` | `linear`, `bottom` | [horizontal](horizontal.md) |
| `vertical` | `edges` | [vertical](vertical.md) |
| `oscilloscope` | `wave`, `waveform` | [oscilloscope](oscilloscope.md) |

## Common options
| Field | Default | Description |
|---|---|---|
| `enabled` | true | Shows the visualizer |
| `bars` | 56 | Bands per channel (max. 256) |
| `min_height` | 4 | Height in silence (px) |
| `max_height` | 200 | Height with the band at its maximum (px) |
| `bar_width` | 5 | Bar / line thickness (px) |
| `bar_gap` | 4 | Gap between bars (horizontal) |
| `roundness` | 2.5 | Corner radius (px) |
| `smoothing` | 0.75 | Rise speed (0.1 slow … 0.95 instant) |
| `decay_rate` | 0.12 | Fall speed per frame (0.05 slow … 0.3 fast) |
| `color_mode` | gradient | `gradient` or `solid` |
| `solid_color` | #00f0ff | Color when `color_mode: solid` (accepts `#rrggbbaa`) |
| `gradient` | cyan→purple→pink | Color stops, see [gradients](gradients.md) |
| `gradient_axis` | index | `index` (one color per bar) or `length` (along the bar), see [gradients](gradients.md) |
| `gradient_fit` | bar | With `length`: `bar` or `max`, see [gradients](gradients.md) |

Bar height is [normalized per band](../reactivity.md#adaptive-normalization).

## Glow
```yaml
  glow:
    enabled: true
    radius: 12.0
    intensity: 0.65
```
| Field | Default | Description |
|---|---|---|
| `enabled` | true | Neon halo around bars and lines |
| `radius` | 12 | Halo reach (px) |
| `intensity` | 0.65 | Halo opacity (0–1+) |
