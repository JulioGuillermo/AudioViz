# Background transitions

How a background segment replaces the previous one. In the studio: select the segment →
*Transition*. In the YAML it is the `transition` of the segment in `timeline.background`:

```yaml
timeline:
  background:
    - at: 120
      transition: { mode: slide, duration: 1.5, direction: left }
      path: "assets/chorus.png"
```

| `mode` | Effect |
|---|---|
| `cut` | Instant change (default) |
| `fade` | Crossfade |
| `dissolve` | The new image appears grain by grain |
| `dip_black` | Fade to black, then from black |
| `flash` | Flash to white, then to the new image |
| `wipe` | A soft edge sweeps the new image in (`direction`) |
| `slide` | The new image slides in over the old one (`direction`) |
| `push` | The new image pushes the old one out (`direction`) |
| `zoom_in` | Flies into the old image while the new one appears |
| `zoom_out` | The new image arrives from very close and settles |
| `circle` | Iris opening from the center |
| `clock` | Radial sweep from 12 o'clock, clockwise |
| `glitch` | Bands switch to the new image with shifts and RGB split |
| `pixelate` | Pixelates, swaps at the middle, sharpens again |
| `burn` | The old image burns like paper from ignition points; the new one shows through the holes (`burn`) |
| `fluid` | The old image flows like a liquid, with eddies, into the new one; each part seeks similar colors (`fluid`) |

- `duration` (seconds) is how long it takes; the transition starts at the segment's `at`.
- `direction` (`left`, `right`, `up`, `down`) is where the new image moves, for `wipe`, `slide`
  and `push`. `left` = it enters from the right side.
- Each image keeps its own [motion](motion.md) during the transition.

### Fluid options
```yaml
      transition:
        mode: fluid
        duration: 4
        fluid:
          turbulence: 0.5     # 0..1 eddies and swirls
          viscosity: 0.3      # 0..1 thicker = calmer
          color_weight: 0.6   # 0..1 low: flows nearby; high: travels to the most similar color
          seed: 0             # 0 = a different flow per segment; another number = another flow
```
Each part of the old image looks for a place of similar color in the new one, and a simulated
incompressible fluid carries it there: the parts push each other, which turns into eddies.
Both images are drawn warped by that flow and blend while swirling, so it starts exactly as the
old image and ends exactly as the new one, sharp all along. The flow depends on the two images,
so every transition is different. It is simulated once (a few seconds, the first time the
transition is shown or exported), then each frame only draws it (~80 ms at 1080p, on the CPU).

### Burn options
```yaml
timeline:
  background:
    - at: 42
      path: forest.png
      transition:
        mode: burn
        duration: 3
        burn:
          origins: random      # random (default) or points
          count: 3             # random: ignition points, 1..8
          seed: 0              # random: 0 = different per segment; another number = other points
          points: [[0.3, 0.6], [0.8, 0.25]]   # points: x, y in 0..1 of the frame (up to 8)
          edge: 0.5            # 0..1 width of the charred edge and the ember
          roughness: 0.5       # 0..1 how irregular the fire front is
          color: "#ff7a1f"     # ember color
```
The fire grows from each point with an irregular front (the paper fibers): ahead of it the
paper browns and blackens, a thin ember line flickers along the edge with sparks, and the new
image appears through the hole, sooty near the edge. In the studio, with *Points I choose*, click
on the preview to add a point, drag one to move it, Shift+click to remove it.
- If one side has no image, black is used.
- Other tracks (visualizer, effects, particles, lyrics) only have `cut` and `fade`; any other
  mode on them behaves as `fade`.
