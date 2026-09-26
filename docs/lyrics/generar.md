# Generar la letra sincronizada (LRC por palabra)

Herramienta **aparte del binario** (`tools/lyrics/`), solo para preparar el `.lrc` una vez.
El binario no la necesita para renderizar.

```bash
tools/lyrics/run.sh "cancion.flac" "letra.txt" letra.lrc
```
La primera vez crea un entorno Python propio en `tools/lyrics/.venv` (torch CPU, Demucs y stable-ts)
y descarga los modelos: ~3 GB para Whisper large-v3 y ~80 MB para Demucs.

**Receta completa con todos los comandos (revisar, detectar fallos y corregir): [paso a paso](paso-a-paso.md).**

## Qué hace
1. **Limpia la letra**. Acepta el formato de Suno/Udio: quita `[Verse 1]`, `[Chorus]`,
   `[Soft piano, whispered vocals]`, `(...)` y las líneas vacías. Cada línea que queda es una línea del LRC.
2. **Separa la voz** con Demucs (`htdemucs`), que mejora mucho la precisión con música densa (orquesta, batería).
3. **Alinea** tu texto contra la voz con stable-ts (Whisper). No transcribe: el texto es exactamente el tuyo
   y solo calcula cuándo se canta cada palabra.
4. Escribe un **LRC mejorado**: `[00:58.00]<00:58.00>I <00:58.31>didn't <00:58.62>ask ... <01:00.90>`.

## Opciones
| Opción | Def. | Descripción |
|---|---|---|
| `--model` | large-v3 | Modelo Whisper (`medium` es más rápido pero menos preciso) |
| `--lang` | en | Idioma de la letra (`es`, `en`...) |
| `--no-separate` | — | No separar la voz (si ya es una pista a capela) |
| `--vocals <wav>` | — | Usar una voz ya separada |

## Herramientas de diagnóstico
```bash
tools/lyrics/py energy.py tools/lyrics/cache/vocals.wav 100 132      # dónde hay voz y dónde silencio
tools/lyrics/py transcribe.py tools/lyrics/cache/vocals.wav 235 270  # qué oye Whisper en un tramo
```
La voz separada queda en `tools/lyrics/cache/vocals.wav` y se reutiliza con `--vocals`.

## Afinar el resultado
- Si todo va adelantado o retrasado lo mismo, usa `lyrics.offset` en el YAML (p. ej. `0.1`).
- Para una palabra concreta, edita su marca `<mm:ss.xx>` en el `.lrc` (es texto plano).
- Repeticiones que no se cantan o partes improvisadas confunden al alineado: quítalas o añádelas al `.txt`.
