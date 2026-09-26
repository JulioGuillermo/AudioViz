# Letra (lyrics)

Muestra una letra sincronizada, palabra a palabra si el archivo lo permite, con animaciones, colores y
reacción al audio. El texto se dibuja en la GPU a partir de un campo de distancias (SDF), así que las
palabras escalan nítidas y el contorno, el glow y la sombra salen muy baratos.

```yaml
lyrics:
  enabled: true
  path: "letra.lrc"
```
O desde la línea de comandos: `audioviz project.yaml --lyrics letra.lrc`.

## Formatos
| Formato | Tiempos | Ejemplo |
|---|---|---|
| `.srt` | inicio y fin de cada línea | `00:00:58,000 --> 00:01:00,500` |
| `.lrc` | inicio de cada línea | `[00:58.00]Texto de la línea` |
| `.lrc` mejorado | **inicio de cada palabra** | `[00:58.00]<00:58.00>Texto <00:58.40>de <00:58.60>la <00:58.90>línea <00:59.60>` |

- Si solo hay tiempos por línea, las palabras se reparten según su longitud.
- En LRC, una marca final sin texto (`<00:59.60>`) indica cuándo acaba la última palabra. Una línea vacía con tiempo (`[01:06.00]`) cierra la anterior.
- Sin final explícito, una línea dura hasta la siguiente, pero se oculta sola si llega un instrumental largo.
- Se respetan `[offset:+/-ms]` del LRC y varias marcas en una línea (`[00:10.00][01:20.00]Estribillo`).

## Opciones
| Campo | Def. | Descripción |
|---|---|---|
| `enabled` | false | Muestra la letra |
| `path` | — | Archivo `.lrc` / `.srt` |
| `offset` | 0 | Segundos a sumar a todos los tiempos (+ = más tarde). Útil para corregir desfases |
| `font` | Go Bold integrada | Fuente `.ttf` / `.otf` |
| `size` | 56 | Tamaño en px |
| `uppercase` | false | Pasa el texto a mayúsculas |
| `max_width` | 0.9 | Ancho máximo relativo; las líneas más largas se parten en varias filas |
| `line_spacing` | 1.15 | Interlineado entre filas |
| `word_spacing` | 0.12 | Espacio extra entre palabras (fracción de `size`) |
| `align` | center | Alineación de las filas con `position` `top`/`bottom`/`center`/`custom`: center, left, right |

## Posición
| `position` | Resultado |
|---|---|
| `top` | Centrada arriba, a `margin` px del borde |
| `bottom` (def.) | Centrada abajo, a `margin` px del borde |
| `center` | En el centro de la imagen |
| `left` | Pegada a la izquierda (a `margin` px), alineada a la izquierda, a la altura `position_y` |
| `right` | Igual, a la derecha |
| `side` | **Alterna izquierda y derecha en cada línea** |
| `custom` | En `position_x`, `position_y` (0–1) |

`margin` (def. 60 px) es la distancia a los bordes. Con `left`/`right`/`side`, `position_y` (0–1) da la
altura (0.5 = centro) y conviene un `max_width` menor (≤ 0.45 en imágenes verticales, para que los bloques izquierdo y derecho no se crucen).

## Modo de palabras (`mode`)
| Valor | Resultado |
|---|---|
| `karaoke` (def.) | La línea entera visible en `upcoming_color`; cada palabra se rellena de izquierda a derecha con el color principal mientras se canta |
| `words` | Cada palabra aparece (con un pequeño pop) justo cuando se canta |
| `line` | La línea entera en el color principal |

`word_pop` (def. 0.12) es el latido de escala de cada palabra al empezar a cantarse (0 = sin latido).

## Animaciones de entrada y salida
```yaml
  in_animation: "slide_up"
  out_animation: "zoom"
  in_duration: 0.3
  out_duration: 0.4
  slide_distance: 40
```
| Valor | Entrada | Salida |
|---|---|---|
| `fade` | aparece gradualmente | se desvanece |
| `slide_up` | sube desde abajo | se va hacia arriba |
| `slide_down` | baja desde arriba | se va hacia abajo |
| `slide_left` | se desliza hacia la izquierda | se va por la izquierda |
| `slide_right` | se desliza hacia la derecha | se va por la derecha |
| `slide_side` | entra desde su lado (ideal con `position: side`) | sale por su lado |
| `pop` | crece desde pequeña | se encoge |
| `zoom` | llega desde grande | se agranda y desaparece |
| `none` | corte seco | corte seco |

Todas incluyen fundido. `animation` sirve de atajo para poner la misma en ambas. La entrada empieza
`in_duration` segundos antes del inicio de la línea para que esté completa al cantarse.

## Colores
| Campo | Def. | Descripción |
|---|---|---|
| `color` | #ffffff | Color principal (palabra cantada) |
| `gradient` | — | Gradiente vertical en las letras, de arriba (`offset: 0`) a abajo (`1`). Sustituye a `color` |
| `upcoming_color` | #ffffff66 | Palabras aún no cantadas (karaoke); admite alfa `#rrggbbaa` |
| `outline_color`, `outline_width` | #000000, 2 | Contorno (px; 0 = sin contorno) |
| `glow.enabled`, `glow.color`, `glow.radius`, `glow.intensity` | true, color del texto, 12, 0.6 | Halo |
| `shadow.enabled`, `color`, `offset_x`, `offset_y`, `opacity`, `blur` | true, negro, 3, 4, 0.6, 4 | Sombra difuminada |

## Reacción al audio (`react`)
| Campo | Def. | Descripción |
|---|---|---|
| `react_to` | bass | [Fuente](../reactividad.md) |
| `scale` | 0.05 | Latido: aumento de escala máximo |
| `glow` | 0 | Glow extra con la banda |
| `shake` | 0 | Temblor en px |
| `chromatic` | 0 | Separación RGB en px |
| `flicker` | 0 | Parpadeo de brillo tipo llama (0–1) |
| `flash`, `flash_color` | 0, — | Vira el color hacia `flash_color` con la banda (0–1) |

## Ejemplo: letra de fuego
```yaml
lyrics:
  enabled: true
  path: "letra.lrc"
  font: "fonts/NotoSerifDisplay-CondensedBold.ttf"
  size: 58
  uppercase: true
  position: "bottom"
  margin: 280
  mode: "karaoke"
  in_animation: "slide_up"
  out_animation: "zoom"
  gradient:
    - { offset: 0.0,  color: "#fff3b0" }
    - { offset: 0.45, color: "#ffae2a" }
    - { offset: 1.0,  color: "#d4200a" }
  upcoming_color: "#5a1a0a99"
  outline_color: "#1a0000"
  outline_width: 2.5
  glow: { enabled: true, color: "#ff4a00", radius: 16, intensity: 0.7 }
  react: { react_to: bass, scale: 0.06, glow: 0.5, flicker: 0.6, flash: 0.3, flash_color: "#fff8e0", shake: 2 }
```
