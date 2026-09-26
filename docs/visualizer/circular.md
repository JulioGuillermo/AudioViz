# circular

Anillo central: el semicírculo izquierdo es el canal L y el derecho el R, ambos de graves (arriba) a
agudos (abajo). Las barras de los dos canales están desplazadas medio paso para que no se solapen.
El radio pulsa con los graves y hay un anillo fino de base.

```yaml
visualizer:
  type: "circular"
  radius: 200.0
  center_x: 0.5
  center_y: 0.5
```

| Campo | Def. | Descripción |
|---|---|---|
| `radius` | 220 | Radio base en px (0 = 22 % del lado menor) |
| `center_x`, `center_y` | 0.5 | Centro relativo (0–1) |

Más las [opciones comunes](README.md#opciones-comunes). `bar_gap` y `roundness` no se usan (barras con puntas redondas de grosor `bar_width`).
