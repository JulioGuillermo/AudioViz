# Línea de tiempo en el YAML

Lo que el estudio guarda al crear tramos también se puede escribir a mano. Los bloques de siempre
(`background`, `visualizer`, `background_effects.*`, `particles`, `lyrics`) son el **primer tramo**.
Los cambios posteriores van en `timeline:`, una lista por pista. Cada tramo solo necesita los campos
que cambian: el resto se hereda del tramo anterior.

```yaml
timeline:
  markers:
    - { at: 86.4, label: "Estribillo" }
  visualizer:
    - at: 86.4
      label: "Estribillo"
      transition: { mode: fade, duration: 1.5 }   # o mode: cut (por defecto)
      type: circular
      radius: 220
  background:
    - at: 120
      transition: { mode: fade, duration: 2 }
      path: "assets/puente.mp4"
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

| Pista | Bloque que cambia |
|---|---|
| `background` | `background` (imagen, GIF o vídeo, ajuste, oscurecer...) |
| `bass_zoom`, `shake`, `chromatic_aberration`, `ripple`, `glitch`, `color_flash` | `background_effects.<efecto>` |
| `visualizer` | `visualizer` |
| `particles` | `particles` (las capas se sustituyen completas) |
| `lyrics` | estilo de `lyrics` (la ruta y el desfase son siempre los del primer tramo) |

Cada tramo admite `at` (segundos, obligatorio), `label` (nombre) y `transition`. Las reglas de las
transiciones están en [studio.md](studio.md#tramos-cambiar-efectos-a-lo-largo-de-la-canción).

Los fondos de vídeo empiezan desde su principio al empezar su tramo y se repiten si `loop_video: true`.
