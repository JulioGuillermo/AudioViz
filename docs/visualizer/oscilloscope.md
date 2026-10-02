# oscilloscope

Two neon lines crossing the screen (L upwards, R downwards). The height of each point is its
band's level times a sine wave, so it is a wavy spectrum, not the actual audio waveform.

```yaml
visualizer:
  type: "oscilloscope"
  position_y: 0.5
  max_height: 120.0
  bar_width: 3.0
```

| Field | Default | Description |
|---|---|---|
| `position_y` | 0.5 | Relative height of the center line |
| `max_height` | — | Maximum amplitude (px) |
| `bar_width` | 3 | Line thickness |

Colors: with `gradient_axis: index` the L line uses the first gradient color and R the last one.
With `length` the color depends on the distance from the center line.
