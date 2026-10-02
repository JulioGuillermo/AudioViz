# Colors and gradients

```yaml
visualizer:
  color_mode: "gradient"
  gradient_axis: "length"
  gradient_fit: "max"
  gradient:
    - { offset: 0.0, color: "#7a0000" }
    - { offset: 0.5, color: "#e0220a" }
    - { offset: 1.0, color: "#ffc04d" }
```

`gradient` is a list of stops (`offset` 0–1, `color` hex `#rrggbb` or `#rrggbbaa`) interpolated linearly.

## `gradient_axis`: direction
| Value | Result |
|---|---|
| `index` (default) | **Across**: each bar has a solid color based on its position (bass = first color, treble = last). In `circular` it runs around the ring from top to bottom |
| `length` | **Along each bar**: the first color at the base and the last towards the tip |

## `gradient_fit`: reference (only with `length`)
| Value | Result |
|---|---|
| `bar` (default) | The gradient is stretched to each bar's height: **the last color is always at the tip** and moves with the graph. Every bar shows all the colors |
| `max` | The gradient runs from the base to `max_height`: **the last color only appears when the bar reaches the maximum**. Low bars only show the first colors (thermometer / flame effect) |

Per visualizer type, "along" means:
- `horizontal`: from the baseline upwards (the reflection, downwards).
- `vertical`: from the side edge towards the center.
- `circular`: from the ring outwards.
- `oscilloscope`: by distance from the center line (`max_height` = last color).

## Examples
Fire (red at the bottom, yellow only at the peaks):
```yaml
gradient_axis: length
gradient_fit: max
gradient:
  - { offset: 0.0, color: "#7a0000" }
  - { offset: 0.45, color: "#e0220a" }
  - { offset: 0.8, color: "#ff7a1a" }
  - { offset: 1.0, color: "#ffc04d" }
```
Neon with an always-white tip:
```yaml
gradient_axis: length
gradient_fit: bar
gradient:
  - { offset: 0.0, color: "#00f0ff" }
  - { offset: 0.85, color: "#9b51e0" }
  - { offset: 1.0, color: "#ffffff" }
```
