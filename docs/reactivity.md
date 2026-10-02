# Reactivity and normalization

## Sources (`react_to`)
Almost every effect and every particle layer accepts `react_to`:

| Value | Aliases | Frequency range / meaning |
|---|---|---|
| `bass` | — | 20–150 Hz (kick drum, bass) |
| `mid` | `mids`, `medios` | 150–3000 Hz (vocals, guitars) |
| `treble` | `high`, `highs`, `agudos` | 3–16 kHz (cymbals, brightness) |
| `overall` | `rms`, `volume` | Overall volume |
| `transient` | `flux` | Sudden rises in the bass (attacks) |
| `beat` | `kick` | Detected beat pulse: 1 on the beat, then decays |

All of them range from 0 to 1.

Example assignment:
```yaml
background_effects:
  bass_zoom:            { enabled: true, react_to: bass }
  shake:                { enabled: true, react_to: bass }
  chromatic_aberration: { enabled: true, react_to: mid }
  ripple:               { enabled: true, react_to: treble }
```

## Adaptive normalization
With `audio.normalize: true` (the default) each signal (bass, mids, treble, volume and **every bar**
of the visualizer) is rescaled to its own local range: floor = 10th percentile and ceiling = 97th percentile
within a window of ±`normalize_window` seconds.

- If the bass is always loud, the effect doesn't get stuck at the maximum: it keeps "breathing".
- If a song is weak in the treble, treble effects still move.
- Bars no longer saturate. With the example song, they used to be at the maximum 43.8 % of the time and now 0.9 %.
- Each band is compared with its own range (shared between L and R to preserve the stereo balance).
  There is a minimum relative to the rest of the spectrum so empty bands and noise during silences aren't inflated.
- Peaks are compressed smoothly (soft knee) instead of being hard-clipped.

A shorter window (2 s) makes everything react more to detail; a longer one (8 s) better preserves the
differences between parts of the song (quiet verse / loud chorus). With `normalize: false` the
classic behavior is used (absolute values clipped to 1).
