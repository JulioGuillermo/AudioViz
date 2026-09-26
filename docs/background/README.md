# Fondo

```yaml
background:
  path: "fondo.png"
  fit_mode: "cover"
  opacity: 0.9
  darken: 0.12
  vignette: 0.25
  loop_video: true
```

| Campo | Def. | Descripción |
|---|---|---|
| `path` | — | Imagen PNG, JPEG, WebP, BMP o **GIF animado**. Los videos (.mp4...) no están soportados |
| `fit_mode` | cover | `cover` (rellena y recorta), `contain` (cabe entera, bandas negras), `stretch` (deforma) |
| `opacity` | 0.88 | Multiplica el brillo del fondo (0–1) |
| `darken` | 0.15 | Oscurecimiento constante (0–1) para resaltar el visualizador |
| `vignette` | 0.25 | Oscurece los bordes (0–1) |
| `loop_video` | true | Repite el GIF si dura menos que la canción |

## Efectos (`background_effects`)
Se aplican en este orden: glitch → aberración cromática → ripple → zoom/shake → color.
Cada uno puede reaccionar a una banda distinta con [`react_to`](../reactividad.md).

| Efecto | Descripción |
|---|---|
| [bass_zoom](bass_zoom.md) | Zoom pulsante |
| [shake](shake.md) | Movimiento de cámara |
| [chromatic_aberration](chromatic_aberration.md) | Separación RGB |
| [ripple](ripple.md) | Ondulación |
| [glitch](glitch.md) | Franjas desplazadas |
| [color_flash](color_flash.md) | Flash de brillo/contraste |
