# Configuration System and CLI

## 1. Configuration File Design (YAML / JSON)

Rendering behavior is controlled through a declarative schema:

```yaml
# audio-bg-config.yaml

# 1. Output Video Parameters
output:
  width: 1920
  height: 1080
  fps: 60
  bitrate: "12M"
  codec: "libx264"            # libx264, h264_nvenc (NVIDIA), hevc, etc.
  preset: "fast"
  crf: 18

# 2. Background Settings (Image or Video)
background:
  fit_mode: "cover"           # cover, contain, blur_padding
  opacity: 0.85               # 0.0 to 1.0 (lets you darken it to make the spectrum stand out)
  darken: 0.20                # Dark vignette or base dark tint
  loop_video: true            # If the background is a video and is shorter than the audio

# 3. Reactive Effects on the Background
distortion:
  enabled: true
  bass_pulse:
    enabled: true
    max_zoom: 1.08            # Maximum zoom on kicks (1.0 = no zoom)
    decay_speed: 0.15
  chromatic_aberration:
    enabled: true
    intensity: 12.0           # Maximum shift in pixels of the R and B channels
    react_to: "treble"        # bass, treble, overall
  ripple:
    enabled: false
    amplitude: 8.0
    frequency: 0.05
  glitch:
    enabled: true
    trigger_threshold: 0.75   # Transient threshold to trigger the glitch
    intensity: 0.5
  color_reactive:
    brightness_boost: 0.20    # Brightness flash on drops
    contrast_boost: 0.15

# 4. Audio Spectrogram
visualizer:
  enabled: true
  type: "circular"            # circular, horizontal_bottom, horizontal_center, vertical_edges, wave_oscilloscope
  stereo_symmetric: true      # L on the left, R on the right
  bars: 64                    # Number of frequency bands per channel
  smoothing: 0.80             # Temporal smoothing between frames
  min_height: 4.0
  max_height: 220.0
  bar_width: 6.0
  bar_gap: 4.0
  bar_roundness: 3.0          # Rounded corners
  center_x: 0.5               # Relative coordinates (0.5 = center)
  center_y: 0.5
  radius: 200.0               # Base radius for the circular type
  glow:
    enabled: true
    radius: 15.0
    intensity: 0.7
  color_mode: "gradient"      # gradient, solid, rainbow
  palette:
    gradient_stops:
      - offset: 0.0
        color: "#00f0ff"      # Neon cyan
      - offset: 0.5
        color: "#7928ca"      # Deep violet
      - offset: 1.0
        color: "#ff007f"      # Electric pink

# 5. Particle System
particles:
  enabled: true
  count: 150
  type: "radial_burst"        # radial_burst, ambient_dust, rising_sparks
  min_size: 2.0
  max_size: 6.0
  color: "#ffffff"
  audio_boost: 2.5            # Speed multiplier on audio peaks
```

---

## 2. Command Line (CLI Flags)

The `audio-bg` binary has an ergonomic interface:

```bash
# Standard usage with automatic configuration / presets:
audio-bg --audio track.flac --bg background.png --output video.mp4

# Usage with a video background and the Cyberpunk preset:
audio-bg --audio track.flac --bg clip.mp4 --preset cyberpunk --output output.mp4

# Ultra-fast Preview Mode (renders only 5 seconds starting at 1:00):
audio-bg --audio track.flac --bg bg.png -ss 00:01:00 -t 5 -o preview.mp4

# Render a single frame as an image to visually calibrate the design:
audio-bg --audio track.flac --bg bg.png --frame-at 00:01:15 -o preview_frame.png

# Override parameters on the fly:
audio-bg -a track.flac -b bg.png --visualizer circular --glow-intensity 1.2 --no-particles
```

---

## 3. Built-in Presets (Out-of-the-Box)

1. `cyberpunk` (Cyan/magenta circular ring, aggressive chromatic aberration, subtle glitch, flash on the bass).
2. `synthwave` (Horizontal spectrum mirrored at the bottom with a sunset gradient, gentle ripples on the background).
3. `dark_ambient` (Dark, deep vignette, floating dust particles, minimalist pure-white oscilloscope).
4. `bass_monster` (Violent impact zoom, massive radial particle bursts, wide high-energy bars).
5. `clean_minimal` (Thin, elegant visualizer without aggressive distortions, ideal for music podcasts or lofi).
