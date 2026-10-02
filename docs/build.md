# Compilar

```bash
./build.sh
```
Genera `./audioviz`, que solo depende de libc. x264 y un FFmpeg mínimo (solo decodificación de
vídeo) se descargan y compilan una vez como librerías estáticas (`third_party/`) y quedan dentro
del binario, igual que la interfaz web del estudio.

Requisitos **solo para compilar**: `go`, `gcc`, `nasm`, `git`, cabeceras de Vulkan (`vulkan-headers`)
y `glslc` (solo si se modifica `internal/gpu/shaders/render.comp`; el `.spv` compilado está en el repo).

Para ejecutar no hace falta nada instalado. La GPU se usa a través del driver Vulkan del sistema
(parte del driver gráfico); si no existe, el render cae a CPU automáticamente.

Licencia: x264 es GPL, por lo que el binario resultante también lo es (FFmpeg se compila en modo LGPL).
