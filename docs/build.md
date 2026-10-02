# Compilar

```bash
./build.sh
```
Genera `./audioviz`, que solo depende de libc. x264 y un FFmpeg mínimo (solo decodificación de
vídeo) se descargan y compilan una vez como librerías estáticas (`third_party/`) y quedan dentro
del binario, igual que la interfaz web del estudio.

Requisitos **solo para compilar**: `go`, `gcc`/`g++`, `cmake`, `nasm`, `git`, cabeceras de Vulkan
(`vulkan-headers`) y `glslc` (para los shaders de Whisper y, si se modifica, `internal/gpu/shaders/render.comp`).

Se compilan como librerías estáticas, una sola vez:
- `scripts/build-x264.sh`: x264 (codificación H.264)
- `scripts/build-ffmpeg.sh`: FFmpeg mínimo (solo decodificar fondos de vídeo)
- `scripts/build-ml.sh`: whisper.cpp con backend Vulkan (reconocimiento de voz) y demucs.cpp (separación de voz)

`libstdc++` y `libgcc` se enlazan estáticos y `libvulkan` se carga en ejecución solo si existe, así que el
binario resultante solo depende de libc.

Para ejecutar no hace falta nada instalado. La GPU se usa a través del driver Vulkan del sistema
(parte del driver gráfico); si no existe, el render cae a CPU automáticamente.

Licencia: x264 es GPL, por lo que el binario resultante también lo es (FFmpeg se compila en modo LGPL).
