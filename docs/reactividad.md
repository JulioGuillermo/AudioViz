# Reactividad y normalización

## Fuentes (`react_to`)
Casi todos los efectos y cada capa de partículas aceptan `react_to`:

| Valor | Alias | Rango de frecuencias / significado |
|---|---|---|
| `bass` | — | 20–150 Hz (bombo, bajo) |
| `mid` | `mids`, `medios` | 150–3000 Hz (voces, guitarras) |
| `treble` | `high`, `highs`, `agudos` | 3–16 kHz (platos, brillo) |
| `overall` | `rms`, `volume` | Volumen global |
| `transient` | `flux` | Subidas bruscas de graves (ataques) |
| `beat` | `kick` | Pulso de golpe detectado: 1 en el golpe y decae |

Todos valen entre 0 y 1.

Ejemplo de reparto:
```yaml
background_effects:
  bass_zoom:            { enabled: true, react_to: bass }
  shake:                { enabled: true, react_to: bass }
  chromatic_aberration: { enabled: true, react_to: mid }
  ripple:               { enabled: true, react_to: treble }
```

## Normalización adaptativa
Con `audio.normalize: true` (por defecto) cada señal (graves, medios, agudos, volumen y **cada barra**
del visualizador) se reescala a su propio rango local: suelo = percentil 10 y techo = percentil 97
dentro de una ventana de ±`normalize_window` segundos.

- Si el bajo está siempre alto, el efecto no se queda clavado al máximo: sigue "respirando".
- Si una canción es floja en agudos, los efectos de agudos igualmente se mueven.
- Las barras dejan de saturarse. Con la canción de ejemplo, antes estaban al máximo el 43.8 % del tiempo y ahora el 0.9 %.
- Cada banda se compara con su propio rango (compartido entre L y R para conservar el balance estéreo).
  Hay un mínimo relativo al resto del espectro para no inflar bandas vacías ni el ruido en silencios.
- Los picos se comprimen suavemente (rodilla suave) en vez de recortarse en seco.

Una ventana más corta (2 s) hace que todo reaccione más al detalle; una más larga (8 s) respeta más las
diferencias entre partes de la canción (estrofa floja / estribillo fuerte). Con `normalize: false` se usa
el comportamiento clásico (valores absolutos recortados a 1).
