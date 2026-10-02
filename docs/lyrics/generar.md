# Generar la letra sincronizada (LRC por palabra)

Todo va **dentro del binario**: no hace falta Python, ni Whisper, ni nada instalado. Solo los modelos
se descargan una vez (≈ 660 MB) a `~/.cache/audioviz/models` y después funciona sin conexión.

## Desde el estudio
Pista **Letra** → *Generar letra sincronizada…* (o arrastra el `.txt` a la ventana). Eliges el `.txt`, el
idioma y el modo. El progreso muestra una barra total, otra del paso en curso (con tiempo transcurrido
y restante) y la lista de pasos. Al terminar, la letra se guarda en `letra.lrc` del proyecto y se
carga sola; si ya había una, la anterior se guarda con la fecha en el nombre.

## Desde la terminal
```bash
./audioviz lyrics "cancion.flac" letra.txt -o letra.lrc            # modo preciso
./audioviz lyrics "cancion.flac" letra.txt -o letra.lrc --fast     # modo rápido
./audioviz lyrics "cancion.flac" letra.txt -o letra.lrc --lang es  # otro idioma
```

## Qué hace (pasos)
| Paso | Qué hace | Dónde corre |
|---|---|---|
| 1. Modelos | Descarga la primera vez Whisper large-v3-turbo (574 MB) y Demucs htdemucs (84 MB). Reanudable | Red |
| 2. Preparar audio | Remuestrea a 16 kHz (y a 44,1 kHz para Demucs) | CPU |
| 3. Separar la voz | **Demucs** aísla la voz de la música. Se guarda en caché: la segunda vez con la misma canción se salta | CPU, ≈ la duración de la canción |
| 4. Reconocer la voz | **Whisper** transcribe la voz con el instante de cada palabra | **GPU (Vulkan)** si hay, si no CPU |
| 5. Alinear con la letra | Empareja las palabras reconocidas con **tu letra** (tolera palabras mal oídas), descarta "alucinaciones" en los silencios, vuelve a reconocer los tramos dudosos usando la letra como pista y coloca las palabras que falten en las zonas con voz | CPU, instantáneo |

El texto final es siempre **tu letra** (no lo que oye Whisper); solo se calculan los tiempos.
La letra puede venir en formato Suno: se quitan las líneas `[Verse]`, `[Chorus]`, `[Soft piano...]`,
las etiquetas `[...]` dentro de una línea y las líneas que son solo `( ... )`.

## Modos
| Modo | Cuándo |
|---|---|
| **Preciso** (por defecto) | Separa la voz antes de reconocer. Mucho más fiable con música densa (orquesta, guitarras, coros) |
| **Rápido** | Reconoce sobre la mezcla completa. Segundos, pero puede saltarse frases enteras si la música tapa la voz |

## Precisión
Medido con *I didn't ask to be the devil* frente a un alineado forzado de referencia: error mediano de
**0,10 s** por palabra, 83 % de las palabras a menos de 0,25 s y 97 % a menos de 0,5 s.

## Afinar el resultado
- Si todo va igual de adelantado o retrasado: `lyrics.offset` en el YAML.
- Una palabra concreta: en el estudio, clic en la línea (pista *Líneas de letra*) y ajusta su inicio; o edita
  su marca `<mm:ss.xx>` en el `.lrc`.
- Si una frase se canta más veces de las que aparece en el `.txt`, añádela al `.txt` y vuelve a generar
  (con la voz ya en caché tarda unos segundos).

## Sin GPU
Funciona igual por CPU (el reconocimiento tarda más). Para forzarlo: `AUDIOVIZ_NO_VULKAN=1`.
