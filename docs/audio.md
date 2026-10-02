# Audio

```yaml
audio:
  path: "cancion.flac"
  volume: 1.0
  sensitivity: 1.0
  normalize: true
  normalize_window: 4.0
```

| Field | Default | Description |
|---|---|---|
| `path` | — | Audio file. Formats: **FLAC, WAV** (PCM 8/16/24/32, float), **MP3**, **OGG Vorbis** |
| `volume` | 1.0 | Scales the signal *for analysis* (does not change the video's audio) |
| `sensitivity` | 1.0 | Scales the bar height |
| `normalize` | true | [Adaptive normalization](reactivity.md#adaptive-normalization) |
| `normalize_window` | 4.0 | Seconds on each side of the normalization window |

The final video's audio is the lossless original (FLAC inside the MP4), trimmed to the rendered segment.
