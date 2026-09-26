# Colores y gradientes

```yaml
visualizer:
  color_mode: "gradient"
  gradient_axis: "length"
  gradient_fit: "max"
  gradient:
    - { offset: 0.0, color: "#7a0000" }
    - { offset: 0.5, color: "#e0220a" }
    - { offset: 1.0, color: "#ffc04d" }
```

`gradient` es una lista de paradas (`offset` 0–1, `color` hex `#rrggbb` o `#rrggbbaa`) interpoladas linealmente.

## `gradient_axis`: dirección
| Valor | Resultado |
|---|---|
| `index` (def.) | **A lo ancho**: cada barra tiene un color sólido según su posición (graves = primer color, agudos = último). En `circular` recorre el anillo de arriba abajo |
| `length` | **A lo largo de cada barra**: el primer color en la base y el último hacia la punta |

## `gradient_fit`: referencia (solo con `length`)
| Valor | Resultado |
|---|---|
| `bar` (def.) | El gradiente se estira a la altura de cada barra: **el último color siempre está en la punta** y se mueve con la gráfica. Todas las barras muestran todos los colores |
| `max` | El gradiente va de la base a `max_height`: **el último color solo aparece cuando la barra llega al máximo**. Las barras bajas solo muestran los primeros colores (efecto termómetro / llama) |

Por tipo de visualizador, "a lo largo" significa:
- `horizontal`: de la línea base hacia arriba (el reflejo, hacia abajo).
- `vertical`: desde el borde lateral hacia el centro.
- `circular`: desde el anillo hacia fuera.
- `oscilloscope`: según la distancia a la línea central (`max_height` = último color).

## Ejemplos
Fuego (rojo abajo, amarillo solo en los picos):
```yaml
gradient_axis: length
gradient_fit: max
gradient:
  - { offset: 0.0, color: "#7a0000" }
  - { offset: 0.45, color: "#e0220a" }
  - { offset: 0.8, color: "#ff7a1a" }
  - { offset: 1.0, color: "#ffc04d" }
```
Neón con punta siempre blanca:
```yaml
gradient_axis: length
gradient_fit: bar
gradient:
  - { offset: 0.0, color: "#00f0ff" }
  - { offset: 0.85, color: "#9b51e0" }
  - { offset: 1.0, color: "#ffffff" }
```
