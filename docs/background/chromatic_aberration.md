# chromatic_aberration — separación RGB

Desplaza el canal rojo y el azul en sentidos opuestos.

```yaml
background_effects:
  chromatic_aberration:
    enabled: true
    max_shift: 8.0
    mode: "radial"
    react_to: "mid"
```

| Campo | Def. | Descripción |
|---|---|---|
| `enabled` | true | Activa el efecto |
| `max_shift` | 10.0 | Desplazamiento máximo en píxeles |
| `mode` | radial | `radial` (crece hacia los bordes, como una lente) u `horizontal` |
| `react_to` | bass | [Fuente](../reactividad.md) |
