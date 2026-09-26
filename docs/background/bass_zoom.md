# bass_zoom — zoom pulsante

Zoom desde el centro proporcional a la banda elegida, más un empujón extra en cada golpe detectado.

```yaml
background_effects:
  bass_zoom:
    enabled: true
    max_zoom: 1.06
    sensitivity: 1.2
    react_to: "bass"
```

| Campo | Def. | Descripción |
|---|---|---|
| `enabled` | true | Activa el efecto |
| `max_zoom` | 1.06 | Zoom máximo (1.06 = +6 %) |
| `sensitivity` | 1.2 | Multiplicador de la reacción |
| `react_to` | bass | [Fuente](../reactividad.md) |
| `decay_speed` | 0.15 | Reservado (sin uso actualmente) |

zoom = 1 + (max_zoom − 1) · banda · sensitivity + (max_zoom − 1) · pulso_de_golpe · 0.5
