# Partículas

Una o varias **capas** de partículas. Cada capa tiene su tipo, colores y [banda](../reactividad.md): su
cantidad, brillo, tamaño y velocidad siguen a esa banda.

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

Sin `layers`, los campos se ponen directamente bajo `particles:` y forman una única capa (formato clásico).

## Tipos
| `type` | Página |
|---|---|
| `rising_sparks` | [rising_sparks](rising_sparks.md) |
| `radial_burst` | [radial_burst](radial_burst.md) |
| `ambient_dust` | [ambient_dust](ambient_dust.md) |

## Opciones de cada capa
| Campo | Def. | Descripción |
|---|---|---|
| `type` | radial_burst | Tipo de emisor |
| `react_to` | bass | [Fuente](../reactividad.md) que controla la capa |
| `count` | 100 | Número **máximo** de partículas (total de todas las capas ≤ 1024) |
| `min_density` | 1 | Fracción de partículas vivas con la banda en silencio. 1 = siempre todas (clásico); 0.1 = casi ninguna en silencio y todas con la banda al máximo. Si es < 1 el brillo también sigue a la banda |
| `min_size`, `max_size` | 2, 6 | Radio en px (crece hasta +50 % con la banda) |
| `color` | #ffffff | Color al nacer |
| `color_end` | — | Color al morir (se interpola durante la vida) |
| `speed` | 1.0 | Velocidad base |
| `audio_boost` | 0 | Aceleración extra con la banda (px/cuadro × banda × audio_boost) |
| `turbulence` | 0 | Serpenteo lateral (0.5–1 = llama) |
| `min_life`, `max_life` | 1, 3 | Vida en segundos |
| `glow` | false | Halo suave (doble radio, 30 % de opacidad) |

En las capas que reaccionan a `bass` o `beat`, cada golpe da un acelerón extra.
