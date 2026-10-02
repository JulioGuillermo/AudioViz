# shake — camera movement

Moves the image with a smooth pseudo-random sway (a sum of sines) whose amplitude follows the band.
Just enough zoom is added automatically so the edges are never visible.

```yaml
background_effects:
  shake:
    enabled: true
    amplitude: 5.0
    speed: 1.0
    react_to: "bass"
```

| Field | Default | Description |
|---|---|---|
| `enabled` | false | Enables the effect |
| `amplitude` | — | Maximum displacement in pixels (4–10 subtle, 15+ aggressive) |
| `speed` | 1.0 | Movement speed |
| `react_to` | bass | [Source](../reactivity.md). `beat` gives sharp jolts on every beat |
