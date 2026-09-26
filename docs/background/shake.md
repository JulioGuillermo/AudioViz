# shake — movimiento de cámara

Desplaza la imagen con un vaivén suave pseudoaleatorio (suma de senos) cuya amplitud sigue a la banda.
Se añade automáticamente el zoom justo para que nunca se vean los bordes.

```yaml
background_effects:
  shake:
    enabled: true
    amplitude: 5.0
    speed: 1.0
    react_to: "bass"
```

| Campo | Def. | Descripción |
|---|---|---|
| `enabled` | false | Activa el efecto |
| `amplitude` | — | Desplazamiento máximo en píxeles (4–10 sutil, 15+ agresivo) |
| `speed` | 1.0 | Velocidad del movimiento |
| `react_to` | bass | [Fuente](../reactividad.md). `beat` da sacudidas secas en cada golpe |
