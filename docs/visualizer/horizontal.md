# horizontal

Vertical bars rising from a baseline: the L channel from the center to the left, R from the center to the
right (bass in the center). If they don't fit, the width adjusts automatically.

```yaml
visualizer:
  type: "horizontal"
  position_y: 0.85
  mirror_bottom: true
```

| Field | Default | Description |
|---|---|---|
| `position_y` | 0.85 | Relative height of the baseline (1.0 = bottom edge) |
| `mirror_bottom` | true | Downward reflection (half height, 35 % opacity) |
| `center_gap` | 2 × `bar_gap` | Gap in px between the first L bar and the first R bar, in the center (0 = touching) |

Plus the [common options](README.md#common-options) (`bar_width`, `bar_gap`, `roundness`...).
