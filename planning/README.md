# Development Plan Index: Audio-Bg Generator

This folder contains the complete specification and architectural plan for developing the audio-reactive music video generator in **Go**:

| Document | Description |
| :--- | :--- |
| [00_OVERVIEW.md](file:///home/jg/audio_bg/planning/00_OVERVIEW.md) | Executive summary, scope, goals and detected test assets. |
| [01_ARCHITECTURE.md](file:///home/jg/audio_bg/planning/01_ARCHITECTURE.md) | Flow diagram, Zero-CGO pipeline with FFmpeg, worker concurrency and package structure. |
| [02_AUDIO_ANALYSIS.md](file:///home/jg/audio_bg/planning/02_AUDIO_ANALYSIS.md) | PCM decoding, stereo STFT/FFT (L & R), log/mel scale, attack/decay and beat detection. |
| [03_VISUAL_EFFECTS_AND_BACKGROUND.md](file:///home/jg/audio_bg/planning/03_VISUAL_EFFECTS_AND_BACKGROUND.md) | Image and video ingestion, bass zoom, RGB chromatic separation, ripple and VHS glitch. |
| [04_SPECTROGRAM_VISUALIZERS.md](file:///home/jg/audio_bg/planning/04_SPECTROGRAM_VISUALIZERS.md) | Symmetric spectrograms: radial Circular (L left / R right), linear Horizontal, Vertical and Oscilloscope with glow. |
| [05_PARTICLE_SYSTEM.md](file:///home/jg/audio_bg/planning/05_PARTICLE_SYSTEM.md) | Physical particle simulation, radial burst on beats and cinematic ambient dust. |
| [06_CONFIGURATION_AND_CLI.md](file:///home/jg/audio_bg/planning/06_CONFIGURATION_AND_CLI.md) | Full YAML/JSON configuration schema, predefined presets and CLI commands. |
| [07_ROADMAP_AND_MILESTONES.md](file:///home/jg/audio_bg/planning/07_ROADMAP_AND_MILESTONES.md) | Step-by-step development phases, intermediate tests and acceptance criteria. |
