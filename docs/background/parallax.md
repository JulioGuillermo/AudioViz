# parallax — 2.5D camera

With a background that has a [depth map](../depth.md), a camera that moves through the image: what
is near moves more than what is far, so a still picture looks filmed. What is nearer than the
**focus** goes one way and what is farther the other; near things hide what is behind them, and
what the camera uncovers is filled with what is farther. Clips and emitters with a depth (their
*Depth* section) move as that depth does, and an emitter's light moves with its fire. Just enough
zoom is added so the frame's edges are never visible.

```yaml
background_effects:
  parallax:
    enabled: true
    motion: sway      # sway, beat, still
    amount: 30
    x: 0
    y: 0
    dolly: 0
    focus: 0.5
    speed: 1.0
    react_to: bass    # the band: beat motion, or react > 0
    react: 0          # sway / still: how much the music pushes it further (0 … 1)
```

| Field | Default | Description |
|---|---|---|
| `enabled` | false | Enables the effect (needs a depth map; without one nothing moves) |
| `motion` | sway | **sway**: a slow drift around x, y, moving in and out a little; **beat**: towards x, y (and in, with `dolly`) with the music, back at rest; **still**: at x, y — move it with segments (their transitions travel) or keys |
| `amount` | 30 | How far (px) the nearest things move against the focus (20–40 subtle, 60+ strong) |
| `x`, `y` | 0 | Where the camera is, −1 … 1 (left / up … right / down) |
| `dolly` | 0 | Moving in: what is near grows more than what is far (−0.3 … 0.3; negative moves out) |
| `focus` | 0.5 | The depth that stays still: 0 far … 1 near |
| `speed` | 1.0 | Speed of the sway |
| `react_to` | bass | [Source](../reactivity.md) of the pushes (beat motion, or with `react`) |
| `react` | 0 | Sway and still: how much the music pushes the camera further, 0 … 1 (1 = up to twice its movement and moving in on the hits) |

Large moves uncover more of what was hidden: thin near things (text painted in the image,
branches) may leave a short stretch behind them. A sharp depth map (DA3 Mono Large) gives clean
edges. The fluid and liquid transitions don't move.
