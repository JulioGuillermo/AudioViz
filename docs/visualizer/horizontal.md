# horizontal

Barras verticales que salen de una línea base: canal L del centro hacia la izquierda, R del centro hacia la
derecha (graves en el centro). Si no caben, el ancho se ajusta solo.

```yaml
visualizer:
  type: "horizontal"
  position_y: 0.85
  mirror_bottom: true
```

| Campo | Def. | Descripción |
|---|---|---|
| `position_y` | 0.85 | Altura relativa de la línea base (1.0 = borde inferior) |
| `mirror_bottom` | true | Reflejo hacia abajo (mitad de altura, 35 % de opacidad) |

Más las [opciones comunes](README.md#opciones-comunes) (`bar_width`, `bar_gap`, `roundness`...).
