# glitch — displaced stripes

Shifts random stripes of the image horizontally during the frame in which it fires.

```yaml
background_effects:
  glitch:
    enabled: true
    threshold: 0.65
    intensity: 0.35
    react_to: ""        # empty = beats/transients
```

| Field | Default | Description |
|---|---|---|
| `enabled` | true | Enables the effect |
| `intensity` | 0.4 | Number of stripes and amount of displacement |
| `threshold` | 0.7 | Trigger threshold |
| `react_to` | "" | Empty: fires on every beat or when the transient exceeds `threshold` (classic). With a [source](../reactivity.md) (`treble`, `mid`...): fires while the band exceeds `threshold` (0.9+ = peaks only) |
| `depth_only`, `depth_from`, `depth_to`, `depth_soft` | off, 1, 0, 0.05 | With a [depth map](../depth.md#background-effects-on-some-depths): only between `depth_to` (far) and `depth_from` (near), 0 far … 1 near, with a soft edge |
