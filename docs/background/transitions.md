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

- `duration` (seconds) is how long it takes; the transition starts at the segment's `at`.
- `direction` (`left`, `right`, `up`, `down`) is where the new image moves, for `wipe`, `slide`
  and `push`. `left` = it enters from the right side.
- Each image keeps its own [motion](motion.md) during the transition.
- If one side has no image, black is used.
- Other tracks (visualizer, effects, particles, lyrics) only have `cut` and `fade`; any other
  mode on them behaves as `fade`.
