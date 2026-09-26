# glitch — franjas desplazadas

Desplaza horizontalmente franjas aleatorias de la imagen durante el cuadro en que se dispara.

```yaml
background_effects:
  glitch:
    enabled: true
    threshold: 0.65
    intensity: 0.35
    react_to: ""        # vacío = golpes/transitorios
```

| Campo | Def. | Descripción |
|---|---|---|
| `enabled` | true | Activa el efecto |
| `intensity` | 0.4 | Cantidad de franjas y desplazamiento |
| `threshold` | 0.7 | Umbral de disparo |
| `react_to` | "" | Vacío: se dispara con cada golpe o cuando el transitorio supera `threshold` (clásico). Con una [fuente](../reactividad.md) (`treble`, `mid`...): se dispara mientras la banda supere `threshold` (0.9+ = solo picos) |
