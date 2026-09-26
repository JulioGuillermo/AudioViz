# oscilloscope

Dos líneas de neón que cruzan la pantalla (L hacia arriba, R hacia abajo). La altura de cada punto es la de
su banda por una onda seno, así que es un espectro ondulado, no la forma de onda real del audio.

```yaml
visualizer:
  type: "oscilloscope"
  position_y: 0.5
  max_height: 120.0
  bar_width: 3.0
```

| Campo | Def. | Descripción |
|---|---|---|
| `position_y` | 0.5 | Altura relativa de la línea central |
| `max_height` | — | Amplitud máxima (px) |
| `bar_width` | 3 | Grosor de línea |

Colores: con `gradient_axis: index` la línea L usa el primer color del gradiente y la R el último.
Con `length` el color depende de la distancia a la línea central.
