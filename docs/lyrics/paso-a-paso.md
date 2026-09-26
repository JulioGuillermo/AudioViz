# Letra sincronizada paso a paso (receta completa)

Cómo se hizo `letra.lrc` de *I didn't ask to be the devil*, con los comandos exactos para repetirlo
con cualquier canción. Todos los comandos se lanzan desde la carpeta del proyecto (`~/audio_bg`).

> **Si trabajas por SSH**: lanza los pasos largos dentro de `tmux` o con `systemd-run --user`
> (ver [el final](#trabajar-por-ssh)). Si no, el sistema mata el proceso al cerrar la sesión.

---

## 0. Requisitos (una sola vez)

Hace falta `uv` (gestor de Python). En Arch/Manjaro:
```bash
sudo pacman -S uv
```
El entorno se crea solo la primera vez que ejecutas `tools/lyrics/run.sh`. Si quieres crearlo a mano:
```bash
uv venv -p 3.12 tools/lyrics/.venv
VIRTUAL_ENV=tools/lyrics/.venv uv pip install --index-url https://download.pytorch.org/whl/cpu torch==2.5.1 torchaudio==2.5.1
VIRTUAL_ENV=tools/lyrics/.venv uv pip install demucs stable-ts soundfile
```
Se instala torch para CPU (no hace falta GPU NVIDIA). La primera ejecución descarga los modelos:
Whisper large-v3 (~3 GB, en `~/.cache/whisper`) y Demucs htdemucs (~80 MB, en `~/.cache/torch`).

---

## 1. Preparar la letra en texto

Copia la letra en un `.txt`. El formato de Suno/Udio vale tal cual: las etiquetas `[Verse 1]`,
`[Chorus]`, `[Soft piano, whispered vocals]`... se eliminan solas. Cada línea que queda será una línea del LRC.

Consejo: si sabes que una frase se repite al cantarla (p. ej. un estribillo final o un eco),
escríbela tantas veces como se canta. Si no está en el texto, no se puede alinear.

---

## 2. Separar la voz y alinear (paso principal)

```bash
tools/lyrics/run.sh "I didnt ask to be the devil.flac" "I didn't ask to be the devil.txt" letra.lrc
```
Qué hace, en orden:
1. **Demucs** separa la voz de la música (~1,5 min con 8 núcleos) y la guarda en `tools/lyrics/cache/vocals.wav`.
2. **stable-ts / Whisper large-v3** alinea el texto contra la voz (~2 min).
3. **Reparación automática**: detecta líneas mal alineadas (palabras amontonadas con duración ~0, o
   un silencio real de más de 3 s dentro de la línea, típico de un instrumental) y las vuelve a
   alinear solo en el tramo de voz correcto. Lo verás en la salida como:
   ```
   🔧 Realineando líneas 23-26 en 01:46.20–02:07.80
   ```
4. Escribe `letra.lrc` con tiempo por palabra.

Opciones útiles:
```bash
# Volver a alinear sin repetir la separación de voz (mucho más rápido)
tools/lyrics/run.sh "cancion.flac" "letra.txt" letra.lrc --vocals tools/lyrics/cache/vocals.wav

# Otro idioma / modelo más rápido
tools/lyrics/run.sh "cancion.flac" "letra.txt" letra.lrc --lang es --model medium
```

---

## 3. Revisar el resultado

Ver solo el inicio de cada línea (sin las marcas de palabra):
```bash
sed -E 's/<[0-9:.]+>//g' letra.lrc
```
Qué buscar:
- Líneas con **el mismo tiempo o casi** (varias seguidas en el mismo segundo) → están amontonadas.
- **Saltos raros**: una línea que empieza mucho más tarde de lo esperado, o antes de terminar un instrumental.
- En una estrofa normal, las líneas suelen ir a ritmo regular (en esta canción, cada ~2,5 s).

Ver una línea concreta con los tiempos de cada palabra (p. ej. las líneas 23 a 27):
```bash
sed -n '23,27p' letra.lrc
```

---

## 4. Comprobar dónde se canta de verdad (energía de la voz)

Si una zona parece mal, mira la energía de la voz separada segundo a segundo:
```bash
tools/lyrics/py energy.py tools/lyrics/cache/vocals.wav 100 132
```
```
1:46.0 0.113 #################################
1:47.0 0.005 #
1:48.0 0.000            <- silencio: instrumental
...
1:58.0 0.092 ###########################   <- vuelve la voz
```
Argumentos: `archivo inicio_s fin_s [paso_s]`. Con paso 0.25 ves más detalle:
```bash
tools/lyrics/py energy.py tools/lyrics/cache/vocals.wav 235 270 0.25
```
Así se detectó que la estrofa 2 empieza en 1:58 y no en 1:46.

---

## 5. Ver qué oye Whisper en un tramo (transcripción libre)

Para zonas dudosas (susurros, repeticiones, outros), transcribe el tramo **sin** la letra y compara:
```bash
tools/lyrics/py transcribe.py tools/lyrics/cache/vocals.wav 235 270
```
```
03:55.82 - 03:55.92  I
03:55.92 - 03:56.60  didn't
...
04:05.16 - 04:06.06  I
04:06.10 - 04:06.46  didn't
04:06.46 - 04:06.78  ask
04:14.78 - 04:15.68  I
...
04:27.52 - 04:27.62  I
```
Así se descubrió que en el outro se canta "I didn't ask…" **tres veces** (4:05, 4:14 y 4:27),
aunque el `.txt` solo lo tenía una vez.

---

## 6. Corregir a mano (si hace falta)

El `.lrc` es texto plano. Formato de cada línea:
```
[inicio_línea]<inicio_palabra1>palabra1 <inicio_palabra2>palabra2 ... <fin_última_palabra>
```
Ejemplo (el outro corregido con los tiempos del paso 5):
```
[03:55.82]<03:55.82>I <03:55.92>didn't <03:56.60>ask <03:57.12>to <03:58.34>be <03:58.66>the <03:58.96>devil <03:59.50>
[04:05.16]<04:05.16>I <04:06.10>didn't <04:06.46>ask... <04:06.90>
[04:14.78]<04:14.78>I <04:15.74>didn't <04:16.00>ask... <04:16.50>
[04:27.52]<04:27.52>I <04:27.62>didn't <04:27.90>ask... <04:28.40>
```
Edítalo con cualquier editor (`nano letra.lrc`). Alternativa: añade la repetición al `.txt` y repite el paso 2
con `--vocals` para que lo alinee automáticamente.

Si **toda** la letra va igual de adelantada o retrasada, no toques el archivo: usa `offset` en el YAML
(segundos; positivo = más tarde):
```yaml
lyrics:
  offset: 0.1
```

---

## 7. Comprobar en el video

Un cuadro exacto (para ver si una palabra está rellenada cuando toca):
```bash
./audioviz project.yaml --lyrics letra.lrc --frame-at 120.2 --frame-out prueba.png
```
Un clip de 15 s de la zona corregida:
```bash
./audioviz project.yaml --lyrics letra.lrc -p 15 --ss 110 -o prueba.mp4
```
Cuando esté bien, activa la letra en `project.yaml` (`lyrics.enabled: true`, `path: "letra.lrc"`) y renderiza:
```bash
./audioviz project.yaml
```

---

## Trabajar por SSH

Opción 1, `tmux` (puedes cerrar la sesión y volver):
```bash
tmux new -s letra
tools/lyrics/run.sh "cancion.flac" "letra.txt" letra.lrc
# Ctrl+B y luego D para salir sin cortarlo; volver con: tmux attach -t letra
```
Opción 2, como servicio de usuario con un log:
```bash
systemd-run --user --unit=alinear --working-directory="$PWD" \
  bash -c 'tools/lyrics/run.sh "cancion.flac" "letra.txt" letra.lrc > alinear.log 2>&1'
systemctl --user is-active alinear     # active mientras trabaja
tail -f alinear.log                    # ver el progreso
```
Lo mismo sirve para un render largo:
```bash
systemd-run --user --unit=render --working-directory="$PWD" bash -c './audioviz project.yaml > render.log 2>&1'
```

---

## Resumen: todo lo que se ejecutó para esta canción
```bash
cd ~/audio_bg
# 2. separar voz + alinear + reparación automática
tools/lyrics/run.sh "I didnt ask to be the devil.flac" "I didn't ask to be the devil.txt" letra.lrc
# 3. revisar
sed -E 's/<[0-9:.]+>//g' letra.lrc
# 4. energía de la voz donde había dudas (estrofa 2 y outro)
tools/lyrics/py energy.py tools/lyrics/cache/vocals.wav 100 132
tools/lyrics/py energy.py tools/lyrics/cache/vocals.wav 228 270
# 5. qué se canta en el outro
tools/lyrics/py transcribe.py tools/lyrics/cache/vocals.wav 235 270
# 6. editar las 4 últimas líneas de letra.lrc con esos tiempos (ver ejemplo del paso 6)
nano letra.lrc
# 7. comprobar y renderizar
./audioviz project.yaml --lyrics letra.lrc --frame-at 120.2 --frame-out prueba.png
./audioviz project.yaml
```
