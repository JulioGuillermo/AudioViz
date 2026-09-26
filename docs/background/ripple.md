# ripple — ondulación

Deformación sinusoidal de la imagen (agua, calor). La amplitud va del 30 % (en silencio) al 100 % según la banda.

```yaml
background_effects:
  ripple:
    enabled: true
    amplitude: 3.0
    frequency: 0.03
    speed: 1.2
    react_to: "treble"
```

| Campo | Def. | Descripción |
|---|---|---|
| `enabled` | false | Activa el efecto |
| `amplitude` | 6.0 | Amplitud máxima en píxeles (2–3 = aire caliente sutil) |
| `frequency` | 0.04 | Frecuencia espacial (más alto = ondas más juntas) |
| `speed` | 1.5 | Velocidad de avance de la onda |
| `react_to` | mid | [Fuente](../reactividad.md) |
