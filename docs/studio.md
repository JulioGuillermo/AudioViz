# AudioViz Studio (interfaz gráfica)

```bash
./audioviz studio                 # abre el project.yaml de la carpeta actual (si hay)
./audioviz studio ruta/proyecto   # abre ese proyecto (carpeta o .yaml)
./audioviz studio --addr 0.0.0.0:8787   # accesible desde otro equipo de la red
./audioviz studio --no-open       # no abrir el navegador
```
El estudio va **dentro del mismo binario**: arranca un servidor local y abre el navegador en
`http://localhost:8787`. No hace falta instalar nada. El preview lo renderiza la misma GPU y el
mismo motor que la exportación, así que lo que ves es lo que se exporta.

> `--addr 0.0.0.0:...` permite entrar desde la red, y cualquiera en ella podría ver tus carpetas.
> Úsalo solo en redes de confianza.

## Pantalla

| Zona | Qué hay |
|---|---|
| Arriba | Nombre del proyecto, estado de guardado, progreso de trabajos (exportar / generar letra), **Nuevo**, **Abrir**, **Exportar** |
| Izquierda | Lista de pistas: Audio, Salida, Fondo, cada efecto del fondo, Visualizador, Partículas y Letra. Tachadas si están apagadas en el instante actual |
| Centro | **Preview** y transporte (reproducir, cuadro a cuadro, calidad del preview) |
| Derecha | **Inspector**: todos los parámetros del tramo seleccionado |
| Abajo | **Línea de tiempo**: regla con marcas, forma de onda con las curvas de graves/medios/agudos, líneas de la letra y una pista por efecto |

Todo se **guarda solo** en `project.yaml` (lo indica "Guardado hh:mm:ss"), que sigue siendo editable
a mano y renderizable con `audioviz project.yaml`.

## Tramos: cambiar efectos a lo largo de la canción

Cada pista es una lista de **tramos**. El primero dura desde el principio hasta el siguiente; cada
tramo nuevo empieza en su instante y sustituye la configuración de esa pista desde ahí.

- **Crear un tramo**: doble clic en la pista en el punto deseado, o botón **＋ Tramo en …** del
  inspector (usa la posición del cursor), o tecla **N**. El tramo nuevo copia la config del que
  estaba activo, así que solo cambias lo que quieras.
- **Moverlo**: arrástralo en la línea de tiempo. Se pega (imán) a marcas, golpes, al cursor y a
  otros tramos; mantén **Alt** para moverlo libre. También puedes escribir el instante exacto en
  "Empieza en".
- **Transición**: *De golpe* o *Suave (fundido)* con su duración:
  - números y colores se interpolan (p. ej. el gradiente cambia de rojo a azul poco a poco);
  - activar/desactivar un efecto se funde;
  - un cambio de tipo de visualizador o de número de barras hace un fundido de salida y entrada;
  - un cambio de fondo hace un fundido cruzado;
  - las partículas del tramo anterior dejan de nacer y se apagan mientras nacen las nuevas.
- **Eliminar**: botón *Eliminar tramo* o tecla **Supr** (el primero no se puede borrar).
- **Nombre**: pon nombres ("Estribillo", "Puente") para verlos en la línea de tiempo.

La **letra** funciona igual: un tramo de letra cambia el estilo (fuente, posición, colores,
animaciones…). Cada línea usa la maquetación del tramo en que empieza a cantarse, para que no
cambie de sitio a mitad de frase.

## Marcas
Botón **⚑ Marca** o tecla **M** en el cursor. Clic en una marca para ir a ella, doble clic para
renombrarla, clic derecho para borrarla. Sirven de referencia para colocar los tramos.

## Letra
- Importa un `.lrc` / `.srt` en la pista **Letra** → *Archivo*.
- **Generar letra sincronizada…** (en el inspector de la letra): eliges el `.txt` (vale el formato
  de Suno) y el idioma; lanza `tools/lyrics/run.sh` (Demucs + Whisper) y al terminar la carga sola.
  Necesita `tools/lyrics` junto al binario (ver [generar.md](lyrics/generar.md)).
- **Retocar tiempos**: clic en una línea de la pista "Letra (líneas)". El inspector muestra cada
  palabra con su inicio y fin; ⇤ pone el inicio en el cursor, y hay botones para mover la línea
  entera. Luego **Guardar letra** (escribe un `.lrc` con tiempo por palabra).

## Archivos
Los selectores de archivo navegan por las carpetas del equipo donde corre el estudio. Con
**⬆ Subir desde este equipo…** copias un archivo a `assets/` del proyecto (útil si abres el
estudio desde otro ordenador).

## Exportar
**🎬 Exportar** → canción completa o solo una parte. Se exporta una copia del proyecto tal como
está, así que puedes seguir editando mientras tanto. El progreso aparece arriba.

## Atajos
| Tecla | Acción |
|---|---|
| Espacio | Reproducir / pausa |
| ← / → | Cuadro anterior / siguiente (con Mayús: ±1 s) |
| Inicio / Fin | Principio / final |
| N | Nuevo tramo en el cursor (pista seleccionada) |
| Supr | Borrar el tramo seleccionado |
| M | Marca en el cursor |
| Ctrl + rueda | Zoom de la línea de tiempo |

## Preview
- *Calidad preview* (1/2 por defecto) renderiza directamente a menos resolución en la GPU.
- En el mismo equipo va a 50–60 fps. Desde otro equipo pide varios cuadros por adelantado para
  compensar la latencia de la red.
- Al saltar a otro punto, las partículas se re-simulan unos segundos para que se vean como
  en la exportación.
