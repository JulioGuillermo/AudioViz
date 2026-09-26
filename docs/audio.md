# Audio

```yaml
audio:
  path: "cancion.flac"
  volume: 1.0
  sensitivity: 1.0
  normalize: true
  normalize_window: 4.0
```

| Campo | Def. | Descripción |
|---|---|---|
| `path` | — | Archivo de audio. Formatos: **FLAC, WAV** (PCM 8/16/24/32, float), **MP3**, **OGG Vorbis** |
| `volume` | 1.0 | Multiplica la señal *para el análisis* (no cambia el audio del video) |
| `sensitivity` | 1.0 | Multiplica la altura de las barras |
| `normalize` | true | [Normalización adaptativa](reactividad.md#normalización-adaptativa) |
| `normalize_window` | 4.0 | Segundos a cada lado de la ventana de normalización |

El audio del video final es el original sin pérdidas (FLAC dentro del MP4), recortado al tramo renderizado.
