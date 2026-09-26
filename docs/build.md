# Compilar

```bash
./build.sh
```
Genera `./audioviz`, que solo depende de libc. x264 se descarga y compila una vez como
librería estática (`third_party/`) y queda dentro del binario.

Requisitos **solo para compilar**: `go`, `gcc`, `nasm`, `git`, cabeceras de Vulkan (`vulkan-headers`)
y `glslc` (solo si se modifica `internal/gpu/shaders/render.comp`; el `.spv` compilado está en el repo).

Para ejecutar no hace falta nada instalado. La GPU se usa a través del driver Vulkan del sistema
(parte del driver gráfico); si no existe, el render cae a CPU automáticamente.

Licencia: x264 es GPL, por lo que el binario resultante también lo es.
