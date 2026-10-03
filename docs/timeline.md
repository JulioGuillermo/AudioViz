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
| `bass_zoom`, `shake`, `chromatic_aberration`, `ripple`, `glitch`, `color_flash` | `background_effects.<effect>` |
| `visualizer` | `visualizer` |
| `particles` | `particles` (layers are replaced as a whole) |
| `lyrics` | `lyrics` style (the path and the offset are always those of the first segment) |

Each segment accepts `at` (seconds, required), `label` (name) and `transition`. The transition rules
are in [studio.md](studio.md#segments-changing-effects-over-the-song). Background segments have many
visual transitions (slide, wipe, circle, glitch...): see [background/transitions.md](background/transitions.md);
the other tracks use `cut` or `fade`.

Video backgrounds start from their beginning when their segment starts, and loop if `loop_video: true`.
