# Resumen Ejecutivo del Proyecto: Audio-Reactive Music Video Generator

## 1. Visión del Proyecto
Desarrollar una herramienta CLI de alto rendimiento en **Go (Golang)** capaz de compilarse en un binario autónomo y multiplataforma. La herramienta toma una pista de audio (FLAC, MP3, WAV, etc.) y un fondo visual (imagen estática o archivo de video), y genera un video musical con efectos visuales dinámicos que reaccionan matemáticamente a la música en tiempo real.

## 2. Características Clave
- **Binario Nativo en Go**: Empaquetable sin dependencias complejas de CGO para facilitar la distribución, aprovechando el pipeline de FFmpeg para la decodificación y codificación acelerada por hardware o software.
- **Soporte de Fondos Duales**:
  - **Imagen Estática**: Escalado inteligente (*cover*, *contain*, *blur padding*), animación continua reactiva.
  - **Video de Fondo**: Decodificación frame a frame, soporte de *looping* infinito si el audio excede la duración del video, procesamiento reactivo en cada cuadro.
- **Efectos de Distorsión Reactivos al Audio**:
  - *Bass Pulse / Zoom*: Golpe de zoom reactivo a las frecuencias graves y transitorios de percusión.
  - *Chromatic Aberration (Separación RGB)*: Desfase de canales de color en función de la intensidad y picos de audio.
  - *Distorsión de Ondas / Ripple*: Deformación fluida del fondo basada en frecuencias medias/altas.
  - *Glitch / VHS Shakes*: Salto de líneas, ruido y cortes activados por transitorios agudos o drops.
  - *Ajuste de Color y Contraste*: Variación reactiva de brillo, contraste, saturación, viñeteado y tintes de color.
- **Espectrogramas y Visualizadores Estéreo Simétricos**:
  - **Canal L (Izquierda) y Canal R (Derecha)**: Procesamiento FFT estéreo independiente para visualización simétrica fidedigna.
  - **Modo Circular Central**: Círculo o anillo en el centro donde el semicírculo izquierdo representa el canal L y el derecho el canal R, con barras radiales o líneas suaves.
  - **Modo Horizontal Lineal**: Disposición en la parte inferior o central (L hacia la izquierda, R hacia la derecha, o divididos lado a lado).
  - **Modo Vertical Lateral**: Barras reactivas saliendo desde los bordes laterales hacia el centro.
  - **Modo Osciloscopio / Onda**: Trazo continuo de forma de onda estéreo con resplandor neón (*glow*).
- **Sistema de Partículas Reactivo (Opcional)**:
  - Generación de polvo ambiental, chispas o explosiones radiales (*bursts*) en cada beat kick.
  - Velocidad, tamaño, tiempo de vida y turbulencia guiadas por la energía de audio.
- **100% Configurable y Modular**:
  - Archivos de configuración en formato YAML o JSON.
  - Sistema de *Presets* listos para usar (Cyberpunk, Synthwave, Dark Ambient, Minimalist, Bass Monster).
  - Soporte de banderas CLI para sobreescribir cualquier parámetro al instante.
  - Modo *Preview*: Renderizado de un cuadro único (`--frame N`) o un clip corto (`--preview -ss 30 -t 5`) para iteración ultrarrápida.

## 3. Entorno y Archivos de Prueba Detectados
- **Imagen de fondo**: `I didn't ask to be the devil` (PNG, 832x1248, vertical / formato póster).
- **Audio de prueba**: `I didnt ask to be the devil.flac` (FLAC estéreo, 48000 Hz, 16-bit, duración: 4 min 29 seg).
- **Entorno del sistema**:
  - SO: Linux x86_64
  - Go: `go1.26.6-X:nodwarf5`
  - FFmpeg: `Lavf62.12.102` disponible en `/usr/bin/ffmpeg` y `/usr/bin/ffprobe`.

---
*Consulte los documentos detallados en la carpeta `planning/` para especificaciones técnicas profundas.*
