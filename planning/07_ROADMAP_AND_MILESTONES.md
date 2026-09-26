# Hoja de Ruta de Implementación y Fases de Trabajo

## Fase 1: Inicialización del Módulo Go y Pipeline de Audio
- [ ] Inicializar módulo `go.mod` (ej. `github.com/jg/audio-bg`).
- [ ] Implementar decodificador de audio vía streaming PCM (soporte FLAC, MP3, WAV a 48kHz estéreo Float32).
- [ ] Implementar motor FFT con ventana Hann y separación estricta de canales L y R.
- [ ] Crear convertidor a escalas perceptivas (Log/Mel bins) y detector de transitorios (*Spectral Flux / Bass Energy*).
- [ ] **Test de Verificación**: Script de prueba que procese `I didnt ask to be the devil.flac` y verifique la salida de magnitudes L y R en consola o volcado de datos.

## Fase 2: Motor de Fondos e Ingesta de Video/Imagen
- [ ] Implementar cargador de imagen fija con modos `cover`, `contain` y `blur_padding`.
- [ ] Implementar lector de video en streaming mediante pipe de FFmpeg con soporte de *looping* infinito.
- [ ] **Test de Verificación**: Carga y escalado correcto de `"I didn't ask to be the devil"` (832x1248) a resolución Full HD (1920x1080) y resolución vertical (1080x1920).

## Fase 3: Efectos de Distorsión Reactivos
- [ ] Implementar *Bass Zoom / Pulse* con interpolación bilineal suave.
- [ ] Implementar separación de canales RGB (*Chromatic Aberration*) horizontal y radial.
- [ ] Implementar deformación senoidal (*Ripple / Wave*).
- [ ] Implementar *Glitch / VHS displacement* controlado por umbral de golpe.
- [ ] Implementar modulación de brillo, contraste y viñeteado reactivo.
- [ ] **Test de Verificación**: Generación de cuadros de prueba con diferentes niveles de intensidad reactiva aplicados a la imagen de prueba.

## Fase 4: Espectrogramas Estéreo Simétricos (L vs R)
- [ ] Motor de dibujo 2D anti-aliased vectorial y rasterizado.
- [ ] Implementar visualizador **Circular Central** (Semicírculo L a la izquierda, Semicírculo R a la derecha).
- [ ] Implementar visualizador **Horizontal Lineal** (Center-out mirrored y Dual split).
- [ ] Implementar visualizador **Vertical Lateral** (bordes hacia el centro).
- [ ] Implementar visualizador de **Forma de Onda / Osciloscopio**.
- [ ] Implementar capas de resplandor neón (*Glow / Bloom*) y soporte de gradientes de color.

## Fase 5: Sistema de Partículas Reactivo
- [ ] Implementar simulación de partículas 2D en Go (velocidad, vida, desvanecimiento alfa, blending aditivo).
- [ ] Implementar emisor de explosión radial (*Radial Burst*) accionado por golpes de bombo/bajos.
- [ ] Implementar polvo atmosférico ambiental (*Ambient Drift*).

## Fase 6: Orquestador Concurrente, FFmpeg Encoder Pipe y CLI
- [ ] Diseñar el worker pool de Go (`runtime.NumCPU()`) con búfer secuencial para escritura continua en el encoder FFmpeg.
- [ ] Barra de progreso en terminal con FPS de renderizado, porcentaje y tiempo estimado (ETA).
- [ ] Parser de banderas CLI (`--audio`, `--bg`, `--output`, `--preset`, `--preview`, `--frame-at`).
- [ ] Integrar presets YAML en el binario mediante `embed.FS`.

## Fase 7: Validación con los Archivos de Prueba y Calibración
- [ ] Generar un video de prueba corto de 10-15 segundos usando `"I didnt ask to be the devil.flac"` y `"I didn't ask to be the devil"`.
- [ ] Validar sincronización milimétrica entre picos de audio y efectos visuales.
- [ ] Validar que la compilación con `go build` genere un binario autónomo y funcional.
