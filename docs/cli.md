# Línea de comandos

```
audioviz [proyecto.yaml] [opciones]
```
Si no se indica proyecto se usa `project.yaml`. Para la interfaz gráfica: `audioviz studio` (ver [studio.md](studio.md)).

| Opción | Descripción |
|---|---|
| `-p, --preview <s>` | Renderiza solo N segundos |
| `--ss <s>` | Segundo de inicio del preview (con `-p`) |
| `--frame-at <s>` | Exporta un único cuadro a PNG |
| `--frame-out <ruta>` | Ruta del PNG (def. `preview_frame.png`) |
| `-o, --output <ruta>` | Sustituye `output.path` |
| `-a, --audio <ruta>` | Sustituye `audio.path` |
| `-b, --bg <ruta>` | Sustituye `background.path` |
| `-l, --lyrics <ruta>` | Muestra esa letra `.lrc`/`.srt` (activa `lyrics`) |
| `--renderer auto\|gpu\|cpu` | Backend de render (ver [output](output.md)) |
| `--gpu-device <n>` | GPU a usar |
| `--list-gpus` | Lista las GPUs Vulkan |
| `--init <archivo>` | Crea una plantilla YAML |
| `-h, --help` | Ayuda |

## Ejemplos
```bash
audioviz project.yaml                        # video completo
audioviz project.yaml -p 10 --ss 60          # 10 s desde el minuto 1
audioviz project.yaml --frame-at 35          # cuadro del segundo 35
audioviz project.yaml --renderer cpu         # sin GPU
```
El análisis de audio siempre se hace sobre la canción entera, así que un preview se ve exactamente
igual que ese tramo del video final.
