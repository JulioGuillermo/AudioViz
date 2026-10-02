# rising_sparks

They spawn along the bottom edge, spread across its width, and rise with some sideways drift. With `turbulence`
and `color_end` they mimic fire sparks.

Fire example by band (bass = large red embers, mids = orange sparks, treble = tiny yellow sparks):
```yaml
particles:
  enabled: true
  layers:
    - { type: rising_sparks, react_to: bass,   count: 90,  min_density: 0.15, min_size: 1.8, max_size: 4.0,
        color: "#ff3a0a", color_end: "#6e0a00", speed: 0.9, audio_boost: 1.5, turbulence: 0.6,
        min_life: 1.5, max_life: 3.5, glow: true }
    - { type: rising_sparks, react_to: mid,    count: 110, min_density: 0.15, min_size: 1.5, max_size: 4.0,
        color: "#ff8c1a", color_end: "#b82a00", speed: 1.3, audio_boost: 1.5, turbulence: 0.8,
        min_life: 1.2, max_life: 3.0, glow: true }
    - { type: rising_sparks, react_to: treble, count: 140, min_density: 0.1,  min_size: 0.8, max_size: 2.2,
        color: "#fff0a0", color_end: "#ff9500", speed: 1.8, audio_boost: 2.0, turbulence: 1.0,
        min_life: 0.8, max_life: 2.2, glow: true }
```
Options: see [layers](README.md#per-layer-options). `speed` controls the rise speed.
