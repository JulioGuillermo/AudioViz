# bass_zoom — pulsing zoom

Zoom from the center proportional to the chosen band, plus an extra push on every detected beat.

```yaml
background_effects:
  bass_zoom:
    enabled: true
    max_zoom: 1.06
    sensitivity: 1.2
    react_to: "bass"
```

| Field | Default | Description |
|---|---|---|
| `enabled` | true | Enables the effect |
| `max_zoom` | 1.06 | Maximum zoom (1.06 = +6 %) |
| `sensitivity` | 1.2 | Reaction multiplier |
| `react_to` | bass | [Source](../reactivity.md) |
| `decay_speed` | 0.15 | Reserved (currently unused) |

zoom = 1 + (max_zoom − 1) · band · sensitivity + (max_zoom − 1) · beat_pulse · 0.5
