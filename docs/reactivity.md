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
| `beat` | — | Detected beat pulse: 1 on the beat, then decays |

All of them range from 0 to 1.

### The song separated into its instruments

In the studio, **Audio → Instruments → Separate the song** splits it with Demucs (on the CPU, a few
minutes, once per song; the model is the same as the lyrics' and is downloaded the first time) into
drums, bass, vocals and the rest, kept as WAV files in the project's `cache/stems/<song>/`
(`drums.wav`, `bass.wav`, `other.wav`, `vocals.wav`): mono at 22 kHz, enough for the effects, or with
**Keep the parts in full quality** (`audio.stems_full: true`) stereo at 44.1 kHz, to listen to them or
use them elsewhere (**Open the folder**). Generating the lyrics uses the separated vocals when they
are there, and otherwise separates the four parts on its way (the same work) and keeps them. Then
these sources follow one instrument only, without the others getting in:

| Value | Aliases | Meaning |
|---|---|---|
| `drums` | — | The drums' energy |
| `kick` | — | A pulse at each kick (the drums' low hits), fading in 0.15 s |
| `snare` | — | A pulse at each snare hit (the drums' middle), fading in 0.12 s |
| `hats` | `hihat` | A pulse at each hi-hat / cymbal hit (the drums' highs), fading in 0.06 s |
| `bassline` | — | The bass line's energy |
| `vocals` | `voice` | The vocals' energy |
| `instruments` | `other` | The rest (guitars, synths, keys...) |

Each energy is rescaled to the instrument's own loud moments within ±`normalize_window` seconds
(silence stays silent); a hit is a sudden rise above the rises around it, at least 70 ms apart.
Until the song is separated they use the closest thing in the mix (`kick` the beat, `vocals` the
mids, `drums` and `snare` the transients, `hats` the treble, `bassline` the bass, `instruments` the
volume), so a project works either way. Rendering from the command line uses the stems if they are
in the cache.

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
