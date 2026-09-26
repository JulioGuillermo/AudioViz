# Sistema de Configuración y CLI

## 1. Diseño del Archivo de Configuración (YAML / JSON)

El comportamiento de renderizado se controla mediante un esquema declarativo:

```yaml
# audio-bg-config.yaml

# 1. Parámetros de Video de Salida
output:
  width: 1920
  height: 1080
  fps: 60
  bitrate: "12M"
  codec: "libx264"            # libx264, h264_nvenc (NVIDIA), hevc, etc.
  preset: "fast"
  crf: 18

# 2. Ajustes del Fondo (Imagen o Video)
background:
  fit_mode: "cover"           # cover, contain, blur_padding
  opacity: 0.85               # 0.0 a 1.0 (permite oscurecer para resaltar espectro)
  darken: 0.20                # Viñeta oscura o tinte oscuro base
  loop_video: true            # Si el fondo es video y es más corto que el audio

# 3. Efectos Reactivos sobre el Fondo
distortion:
  enabled: true
  bass_pulse:
    enabled: true
    max_zoom: 1.08            # Zoom máximo en los bombos (1.0 = sin zoom)
    decay_speed: 0.15
  chromatic_aberration:
    enabled: true
    intensity: 12.0           # Desplazamiento máximo en píxeles de canales R y B
    react_to: "treble"        # bass, treble, overall
  ripple:
    enabled: false
    amplitude: 8.0
    frequency: 0.05
  glitch:
    enabled: true
    trigger_threshold: 0.75   # Umbral de transitorio para disparar glitch
    intensity: 0.5
  color_reactive:
    brightness_boost: 0.20    # Flash de brillo en drops
    contrast_boost: 0.15

# 4. Espectrograma de Audio
visualizer:
  enabled: true
  type: "circular"            # circular, horizontal_bottom, horizontal_center, vertical_edges, wave_oscilloscope
  stereo_symmetric: true      # L a la izquierda, R a la derecha
  bars: 64                    # Número de bandas de frecuencia por canal
  smoothing: 0.80             # Suavizado temporal entre cuadros
  min_height: 4.0
  max_height: 220.0
  bar_width: 6.0
  bar_gap: 4.0
  bar_roundness: 3.0          # Esquinas redondeadas
  center_x: 0.5               # Coordenadas relativas (0.5 = centro)
  center_y: 0.5
  radius: 200.0               # Radio base para tipo circular
  glow:
    enabled: true
    radius: 15.0
    intensity: 0.7
  color_mode: "gradient"      # gradient, solid, rainbow
  palette:
    gradient_stops:
      - offset: 0.0
        color: "#00f0ff"      # Cyan neón
      - offset: 0.5
        color: "#7928ca"      # Violeta profundo
      - offset: 1.0
        color: "#ff007f"      # Rosa eléctrico

# 5. Sistema de Partículas
particles:
  enabled: true
  count: 150
  type: "radial_burst"        # radial_burst, ambient_dust, rising_sparks
  min_size: 2.0
  max_size: 6.0
  color: "#ffffff"
  audio_boost: 2.5            # Multiplicador de velocidad ante picos de audio
```

---

## 2. Línea de Comandos (CLI Flags)

El binario `audio-bg` cuenta con una interfaz ergonómica:

```bash
# Uso estándar usando configuración automática / presets:
audio-bg --audio track.flac --bg background.png --output video.mp4

# Uso con video como fondo y preset Cyberpunk:
audio-bg --audio track.flac --bg clip.mp4 --preset cyberpunk --output output.mp4

# Modo de Previsualización Ultrarrápida (genera solo 5 segundos a partir del minuto 1:00):
audio-bg --audio track.flac --bg bg.png -ss 00:01:00 -t 5 -o preview.mp4

# Renderizado de un cuadro único como imagen para calibrar visualmente el diseño:
audio-bg --audio track.flac --bg bg.png --frame-at 00:01:15 -o preview_frame.png

# Sobrescritura de parámetros al vuelo:
audio-bg -a track.flac -b bg.png --visualizer circular --glow-intensity 1.2 --no-particles
```

---

## 3. Presets Integrados (Out-of-the-Box)

1. `cyberpunk` (Anillo circular cyan/magenta, aberración cromática agresiva, glitch sutil, destello en graves).
2. `synthwave` (Espectro horizontal reflejado en la base con gradiente atardecer, ondas suaves en fondo).
3. `dark_ambient` (Oscuro, viñeta profunda, partículas de polvo flotante, osciloscopio blanco puro minimalista).
4. `bass_monster` (Zoom de choque violento, explosiones radiales masivas de partículas, barras anchas de alta energía).
5. `clean_minimal` (Visualizador delgado y elegante sin distorsiones agresivas, ideal para podcasts musicales o lofi).
