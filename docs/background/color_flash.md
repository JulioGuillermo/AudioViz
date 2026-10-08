# color_flash — brightness and contrast flash

Raises the background's brightness and contrast in proportion to the band.

```yaml
background_effects:
  color_flash:
    enabled: true
    brightness_boost: 0.15
    contrast_boost: 0.10
    react_to: "bass"
```

| Field | Default | Description |
|---|---|---|
| `enabled` | true | Enables the effect |
| `brightness_boost` | 0.15 | Maximum extra brightness (0–1) |
| `contrast_boost` | 0.10 | Maximum extra contrast (0–1) |
| `react_to` | bass | [Source](../reactivity.md) |
| `depth_only`, `depth_from`, `depth_to`, `depth_soft` | off, 1, 0, 0.05 | With a [depth map](../depth.md#background-effects-on-some-depths): only between `depth_to` (far) and `depth_from` (near), 0 far … 1 near, with a soft edge |
