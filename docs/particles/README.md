# Particles

One or more particle **layers**. Each layer has its own type, colors and [band](../reactivity.md): its
amount, brightness, size and speed follow that band.

```yaml
particles:
  enabled: true
  layers:
    - type: "rising_sparks"
      react_to: "bass"
      count: 90
      min_density: 0.15
      color: "#ff3a0a"
      color_end: "#6e0a00"
    - type: "rising_sparks"
      react_to: "treble"
      count: 140
      color: "#fff0a0"
```

Without `layers`, the fields go directly under `particles:` and form a single layer (classic format).

## Types
| `type` | Page |
|---|---|
| `rising_sparks` | [rising_sparks](rising_sparks.md) |
| `radial_burst` | [radial_burst](radial_burst.md) |
| `ambient_dust` | [ambient_dust](ambient_dust.md) |

## Per-layer options
| Field | Default | Description |
|---|---|---|
| `type` | radial_burst | Emitter type |
| `react_to` | bass | [Source](../reactivity.md) that drives the layer |
| `count` | 100 | **Maximum** number of particles (total across all layers ≤ 1024) |
| `min_density` | 1 | Fraction of particles alive with the band silent. 1 = always all of them (classic); 0.1 = almost none in silence and all of them with the band at its maximum. If < 1, brightness also follows the band |
| `min_size`, `max_size` | 2, 6 | Radius in px (grows by up to +50 % with the band) |
| `color` | #ffffff | Color at birth |
| `color_end` | — | Color at death (interpolated over the lifetime) |
| `speed` | 1.0 | Base speed |
| `audio_boost` | 0 | Extra acceleration with the band (px/frame × band × audio_boost) |
| `turbulence` | 0 | Sideways wiggle (0.5–1 = flame) |
| `min_life`, `max_life` | 1, 3 | Lifetime in seconds |
| `glow` | false | Soft halo (double radius, 30 % opacity) |

In layers that react to `bass` or `beat`, each beat gives an extra burst of speed.
