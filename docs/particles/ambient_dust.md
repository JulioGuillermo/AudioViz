# ambient_dust

Motas repartidas por toda la imagen que flotan lentamente en direcciones aleatorias. Van bien con
`speed` bajo (0.3–0.8), tamaños pequeños y `react_to: overall` para que "respiren" con el volumen.

```yaml
particles:
  enabled: true
  layers:
    - type: "ambient_dust"
      react_to: "overall"
      count: 150
      min_density: 0.4
      min_size: 0.8
      max_size: 2.0
      speed: 0.4
      color: "#ffe9c4"
      glow: true
```
Opciones: ver [capas](README.md#opciones-de-cada-capa).
