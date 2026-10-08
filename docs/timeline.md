# Timeline in the YAML

What the studio saves when you create segments can also be written by hand. The usual blocks
(`background`, `visualizer`, `background_effects.*`, `particles`, `lyrics`) are the **first segment**.
Later changes go in `timeline:`, one list per track. Each segment only needs the fields
that change: the rest is inherited from the previous segment.

```yaml
timeline:
  markers:
    - { at: 86.4, label: "Estribillo" }
  visualizer:
    - at: 86.4
      label: "Estribillo"
      transition: { mode: fade, duration: 1.5 }   # or mode: cut (default)
      type: circular
      radius: 220
  background:
    - at: 120
      transition: { mode: slide, duration: 2, direction: left }   # see background/transitions.md
      path: "assets/puente.mp4"
      motion: { type: zoom_in, amount: 0.2 }                     # see background/motion.md
  chromatic_aberration:
    - at: 150
      enabled: false
  particles:
    - at: 200
      layers:
        - { type: rising_sparks, direction: 0, spread: 15, color: "#ffffff", react_to: treble }
  lyrics:
    - at: 200
      position: top
```

| Track | Block it changes |
|---|---|
| `background` | `background` (image, GIF or video, fit, darken...) |
| `bass_zoom`, `shake`, `chromatic_aberration`, `ripple`, `glitch`, `color_flash`, `parallax` | `background_effects.<effect>` |
| `visualizer` | `visualizer` |
| `particles` | `particles` (layers are replaced as a whole) |
| `lyrics` | `lyrics` style (the path and the offset are always those of the first segment) |

Each segment accepts `at` (seconds, required), `label` (name) and `transition`. The transition rules
are in [studio.md](studio.md#segments-changing-effects-over-the-song). Background segments have many
visual transitions (slide, wipe, circle, glitch...): see [background/transitions.md](background/transitions.md);
the other tracks use `cut` or `fade`.

Video backgrounds start from their beginning when their segment starts, and loop if `loop_video: true`.

## Keyframes

Inside a segment, any number or color (and switches) can change over time with **keys**. Each key
has `t` (seconds from the start of its segment, so they move with it), `v` (the value) and
optionally `ease`, the curve towards the next key: `linear` (default), `step` (holds until the next
key), `ease_in` (starts softly), `ease_out` (arrives softly), `ease_in_out`, `back` (anticipation
and overshoot: it first moves a little the other way, goes past the target and comes back) and
`spring` (goes past the target and settles on it bouncing, like a spring). With `back` and
`spring` the value goes beyond the two keys for a moment. Before the first key
and after the last one the value stays. Paths use the YAML keys with dots, and list positions as
numbers:

```yaml
timeline:
  visualizer:
    - at: 0                      # keys of the first segment (the top-level block) go in an entry at 0
      keys:
        radius: [{ t: 0, v: 200 }, { t: 8, v: 320, ease: ease_in_out }, { t: 16, v: 200 }]
        glow.intensity: [{ t: 0, v: 0.4 }, { t: 4, v: 1.5 }]
        gradient.1.color: [{ t: 0, v: "#ff0040" }, { t: 30, v: "#00e0ff" }]
  particles:
    - at: 60
      keys:
        layers.0.speed: [{ t: 0, v: 1 }, { t: 10, v: 4 }]
```

During a segment transition each side keeps its own animation and both are mixed. What is built
once is not animated: the lyrics layout (size, position, spacing, animations), the number of bars
and of particles. Fixed texts can also move and scale: see [texts.md](texts.md).
