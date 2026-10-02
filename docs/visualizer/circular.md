# circular

Central ring: the left semicircle is the L channel and the right one is R, both going from bass (top) to
treble (bottom). The bars of the two channels are offset by half a step so they don't overlap.
The radius pulses with the bass and there is a thin base ring.

```yaml
visualizer:
  type: "circular"
  radius: 200.0
  center_x: 0.5
  center_y: 0.5
```

| Field | Default | Description |
|---|---|---|
| `radius` | 220 | Base radius in px (0 = 22 % of the shorter side) |
| `center_x`, `center_y` | 0.5 | Relative center (0–1) |

Plus the [common options](README.md#common-options). `bar_gap` and `roundness` are not used (bars with round caps, `bar_width` thick).
