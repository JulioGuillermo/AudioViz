# Project Executive Summary: Audio-Reactive Music Video Generator

## 1. Project Vision
Build a high-performance CLI tool in **Go (Golang)** that compiles into a self-contained, cross-platform binary. The tool takes an audio track (FLAC, MP3, WAV, etc.) and a visual background (static image or video file), and generates a music video with dynamic visual effects that react mathematically to the music in real time.

## 2. Key Features
- **Native Go Binary**: Packageable without complex CGO dependencies for easy distribution, leveraging the FFmpeg pipeline for hardware- or software-accelerated decoding and encoding.
- **Dual Background Support**:
  - **Static Image**: Smart scaling (*cover*, *contain*, *blur padding*), continuous reactive animation.
  - **Background Video**: Frame-by-frame decoding, infinite *looping* support if the audio is longer than the video, reactive processing on every frame.
- **Audio-Reactive Distortion Effects**:
  - *Bass Pulse / Zoom*: Zoom hit reacting to low frequencies and percussion transients.
  - *Chromatic Aberration (RGB Separation)*: Color channel offset driven by audio intensity and peaks.
  - *Wave Distortion / Ripple*: Fluid background deformation based on mid/high frequencies.
  - *Glitch / VHS Shakes*: Line jumps, noise and cuts triggered by sharp transients or drops.
  - *Color and Contrast Adjustment*: Reactive variation of brightness, contrast, saturation, vignetting and color tints.
- **Symmetric Stereo Spectrograms and Visualizers**:
  - **L Channel (Left) and R Channel (Right)**: Independent stereo FFT processing for a faithful symmetric visualization.
  - **Central Circular Mode**: Circle or ring in the center where the left semicircle represents the L channel and the right one the R channel, with radial bars or smooth lines.
  - **Linear Horizontal Mode**: Layout at the bottom or center (L to the left, R to the right, or split side by side).
  - **Lateral Vertical Mode**: Reactive bars coming out from the side edges towards the center.
  - **Oscilloscope / Wave Mode**: Continuous stereo waveform trace with neon *glow*.
- **Reactive Particle System (Optional)**:
  - Generation of ambient dust, sparks or radial *bursts* on every beat kick.
  - Speed, size, lifetime and turbulence driven by the audio energy.
- **100% Configurable and Modular**:
  - Configuration files in YAML or JSON format.
  - Ready-to-use *Presets* system (Cyberpunk, Synthwave, Dark Ambient, Minimalist, Bass Monster).
  - CLI flag support to override any parameter on the fly.
  - *Preview* mode: rendering of a single frame (`--frame N`) or a short clip (`--preview -ss 30 -t 5`) for ultra-fast iteration.

## 3. Detected Environment and Test Files
- **Background image**: `I didn't ask to be the devil` (PNG, 832x1248, vertical / poster format).
- **Test audio**: `I didnt ask to be the devil.flac` (stereo FLAC, 48000 Hz, 16-bit, duration: 4 min 29 s).
- **System environment**:
  - OS: Linux x86_64
  - Go: `go1.26.6-X:nodwarf5`
  - FFmpeg: `Lavf62.12.102` available at `/usr/bin/ffmpeg` and `/usr/bin/ffprobe`.

---
*See the detailed documents in the `planning/` folder for in-depth technical specifications.*
