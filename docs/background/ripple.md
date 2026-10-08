# ripple — wave distortion

Sinusoidal distortion of the image (water, heat). The amplitude goes from 30 % (in silence) to 100 % depending on the band.

```yaml
background_effects:
  ripple:
    enabled: true
    amplitude: 3.0
    frequency: 0.03
    speed: 1.2
    react_to: "treble"
```

| Field | Default | Description |
|---|---|---|
| `enabled` | false | Enables the effect |
| `amplitude` | 6.0 | Maximum amplitude in pixels (2–3 = subtle heat haze) |
| `frequency` | 0.04 | Spatial frequency (higher = waves closer together) |
| `speed` | 1.5 | Wave travel speed |
| `react_to` | mid | [Source](../reactivity.md) |
| `depth_only`, `depth_from`, `depth_to`, `depth_soft` | off, 1, 0, 0.05 | With a [depth map](../depth.md#background-effects-on-some-depths): only between `depth_to` (far) and `depth_from` (near), 0 far … 1 near, with a soft edge |
