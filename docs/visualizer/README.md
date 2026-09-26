# Visualizador

Espectro estéreo: `bars` bandas logarítmicas (25 Hz – 18 kHz) por canal, L y R separados.

```yaml
visualizer:
  enabled: true
  type: "circular"
  bars: 56
  min_height: 4.0
  max_height: 180.0
  bar_width: 5.0
  bar_gap: 3.5
  roundness: 2.5
  smoothing: 0.75
  decay_rate: 0.12
  color_mode: "gradient"
  gradient: [...]
  glow: { enabled: true, radius: 12.0, intensity: 0.65 }
```

## Tipos
| `type` | Alias | Página |
|---|---|---|
| `circular` | `circle`, `radial` | [circular](circular.md) |
| `horizontal` | `linear`, `bottom` | [horizontal](horizontal.md) |
| `vertical` | `edges` | [vertical](vertical.md) |
| `oscilloscope` | `wave`, `waveform` | [oscilloscope](oscilloscope.md) |

## Opciones comunes
| Campo | Def. | Descripción |
|---|---|---|
| `enabled` | true | Muestra el visualizador |
| `bars` | 56 | Bandas por canal (máx. 256) |
| `min_height` | 4 | Altura en silencio (px) |
| `max_height` | 200 | Altura con la banda al máximo (px) |
| `bar_width` | 5 | Grosor de barra / línea (px) |
| `bar_gap` | 4 | Separación entre barras (horizontal) |
| `roundness` | 2.5 | Radio de las esquinas (px) |
| `smoothing` | 0.75 | Velocidad de subida (0.1 lenta … 0.95 instantánea) |
| `decay_rate` | 0.12 | Velocidad de bajada por cuadro (0.05 lenta … 0.3 rápida) |
| `color_mode` | gradient | `gradient` o `solid` |
| `solid_color` | #00f0ff | Color si `color_mode: solid` (admite `#rrggbbaa`) |
| `gradient` | cian→púrpura→rosa | Paradas de color, ver [gradientes](gradients.md) |
| `gradient_axis` | index | `index` (un color por barra) o `length` (a lo largo de la barra), ver [gradientes](gradients.md) |
| `gradient_fit` | bar | Con `length`: `bar` o `max`, ver [gradientes](gradients.md) |

La altura de las barras está [normalizada por banda](../reactividad.md#normalización-adaptativa).

## Glow
```yaml
  glow:
    enabled: true
    radius: 12.0
    intensity: 0.65
```
| Campo | Def. | Descripción |
|---|---|---|
| `enabled` | true | Halo de neón alrededor de barras y líneas |
| `radius` | 12 | Alcance del halo (px) |
| `intensity` | 0.65 | Opacidad del halo (0–1+) |
