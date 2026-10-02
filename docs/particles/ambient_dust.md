# ambient_dust

Specks spread over the whole image that drift slowly in random directions. They work well with a
low `speed` (0.3–0.8), small sizes and `react_to: overall` so they "breathe" with the volume.

```yaml
particles:
  enabled: true
  layers:
    - type: "ambient_dust"
      react_to: "overall"
      count: 150
      min_density: 0.4
      min_size: 0.8
      max_size: 2.0
      speed: 0.4
      color: "#ffe9c4"
      glow: true
```
Options: see [layers](README.md#per-layer-options).
