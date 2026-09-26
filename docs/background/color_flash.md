# color_flash — flash de brillo y contraste

Sube brillo y contraste del fondo proporcionalmente a la banda.

```yaml
background_effects:
  color_flash:
    enabled: true
    brightness_boost: 0.15
    contrast_boost: 0.10
    react_to: "bass"
```

| Campo | Def. | Descripción |
|---|---|---|
| `enabled` | true | Activa el efecto |
| `brightness_boost` | 0.15 | Brillo extra máximo (0–1) |
| `contrast_boost` | 0.10 | Contraste extra máximo (0–1) |
| `react_to` | bass | [Fuente](../reactividad.md) |
