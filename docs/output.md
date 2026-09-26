# Salida

```yaml
output:
  path: "output.mp4"
  width: 0
  height: 0
  fps: 60
  renderer: "auto"
  gpu_device: -1
  preset: "fast"
  crf: 20
```

| Campo | Def. | Descripción |
|---|---|---|
| `path` | output.mp4 | Archivo MP4 (H.264 + FLAC, `moov` al principio) |
| `width`, `height` | 0 | Resolución; 0 = la del fondo. Se redondea a par |
| `fps` | 60 | Cuadros por segundo |
| `renderer` | auto | `auto`: GPU Vulkan y si falla CPU · `gpu`: obliga GPU · `cpu`: obliga CPU |
| `gpu_device` | -1 | Índice de GPU (`audioviz --list-gpus`), -1 = la mejor (dedicada > integrada) |
| `preset` | fast | Preset x264: `ultrafast`, `superfast`, `veryfast`, `faster`, `fast`, `medium`, `slow`, `slower`, `veryslow`. Más lento = archivo más pequeño a igual calidad |
| `crf` | 18 | Calidad: 18 muy alta, 23 estándar. Cada +6 ≈ la mitad de tamaño |

## Rendimiento
La GPU dibuja cada cuadro en 1–2 ms. El resto del tiempo es la compresión H.264, que va por CPU
(x264 con todos los núcleos). Para ir más rápido usa `preset: veryfast`/`superfast`; para archivos más
pequeños sube `crf` o usa `medium`/`slow`.

## Compatibilidad
El audio va en FLAC dentro del MP4. Lo reproducen mpv, VLC, Chrome, Firefox, Android, ffmpeg y
YouTube (subida), pero no QuickTime/Safari de Apple.
