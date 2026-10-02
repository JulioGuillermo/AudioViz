# AudioViz — Documentación

Generador de videos musicales reactivos al audio. Un único binario sin dependencias:
render por **GPU (Vulkan, cualquier fabricante)** con caída automática a CPU, H.264 (x264 integrado)
y audio FLAC sin pérdidas dentro de un MP4.

## Empezar
- **[AudioViz Studio](studio.md)**: interfaz gráfica con preview y línea de tiempo (`audioviz studio`)
- [Línea de tiempo en el YAML](timeline.md): cambiar efectos, fondo, visualizador o letra en cada sección
- [Línea de comandos](cli.md) — render, previews, cuadro suelto, selección de GPU
- [Compilar](build.md)
- [Estructura del proyecto YAML](#estructura-del-yaml)

## Audio y reactividad
- [Audio](audio.md) — formatos, volumen, sensibilidad
- [Reactividad y normalización](reactividad.md) — `react_to` (bass/mid/treble/...) y normalización adaptativa

## Fondo — [visión general](background/README.md)
| Efecto | Qué hace | Reacciona por defecto a |
|---|---|---|
| [bass_zoom](background/bass_zoom.md) | Zoom pulsante desde el centro | graves |
| [shake](background/shake.md) | Temblor/movimiento de cámara | graves |
| [chromatic_aberration](background/chromatic_aberration.md) | Separación de canales RGB | graves |
| [ripple](background/ripple.md) | Ondulación tipo agua / aire caliente | medios |
| [glitch](background/glitch.md) | Franjas desplazadas (VHS) | golpes |
| [color_flash](background/color_flash.md) | Flash de brillo y contraste | graves |

## Visualizador — [visión general](visualizer/README.md)
| Tipo | Descripción |
|---|---|
| [circular](visualizer/circular.md) | Anillo central, L a la izquierda y R a la derecha |
| [horizontal](visualizer/horizontal.md) | Barras desde el centro hacia los lados |
| [vertical](visualizer/vertical.md) | Barras desde los bordes laterales hacia dentro |
| [oscilloscope](visualizer/oscilloscope.md) | Dos líneas de neón onduladas |

- [Colores y gradientes](visualizer/gradients.md) — por barra o a lo largo de la barra, relativo a la barra o al máximo
- [Glow](visualizer/README.md#glow)

## Partículas — [visión general y capas](particles/README.md)
| Tipo | Descripción |
|---|---|
| [rising_sparks](particles/rising_sparks.md) | Chispas que suben desde abajo (fuego) |
| [radial_burst](particles/radial_burst.md) | Explosión desde el centro |
| [ambient_dust](particles/ambient_dust.md) | Polvo flotando por toda la imagen |

## Letra
- [Letra sincronizada](lyrics/README.md): `.srt`, `.lrc` y LRC con tiempo por palabra. Karaoke, posiciones (top/bottom/left/right/side...), animaciones de entrada y salida, colores de fuego y reacción al audio
- [Generar el .lrc con tiempo por palabra](lyrics/generar.md) a partir del audio y la letra en texto (formato Suno admitido) · [receta paso a paso con todos los comandos](lyrics/paso-a-paso.md)

## Salida
- [Video de salida](output.md) — resolución, fps, calidad, GPU/CPU

## Estructura del YAML
```yaml
audio:              # audio.md, reactividad.md
background:         # background/README.md
background_effects: # un bloque por efecto: background/*.md
visualizer:         # visualizer/*.md
particles:          # particles/*.md
lyrics:             # lyrics/README.md
output:             # output.md
```
Genera una plantilla con todo: `audioviz --init nuevo.yaml`. Las rutas son relativas al YAML.
