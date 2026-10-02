# chromatic_aberration — RGB separation

Shifts the red and blue channels in opposite directions.

```yaml
background_effects:
  chromatic_aberration:
    enabled: true
    max_shift: 8.0
    mode: "radial"
    react_to: "mid"
```

| Field | Default | Description |
|---|---|---|
| `enabled` | true | Enables the effect |
| `max_shift` | 10.0 | Maximum shift in pixels |
| `mode` | radial | `radial` (grows towards the edges, like a lens) or `horizontal` |
| `react_to` | bass | [Source](../reactivity.md) |
