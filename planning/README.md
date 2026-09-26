# Índice del Plan de Desarrollo: Audio-Bg Generator

Esta carpeta contiene la especificación y planificación arquitectónica completa para el desarrollo del generador de videos musicales reactivos en **Go**:

| Documento | Descripción |
| :--- | :--- |
| [00_OVERVIEW.md](file:///home/jg/audio_bg/planning/00_OVERVIEW.md) | Resumen ejecutivo, alcance, metas y activos de prueba detectados. |
| [01_ARCHITECTURE.md](file:///home/jg/audio_bg/planning/01_ARCHITECTURE.md) | Diagrama de flujo, pipeline Zero-CGO con FFmpeg, concurrencia de workers y estructura de paquetes. |
| [02_AUDIO_ANALYSIS.md](file:///home/jg/audio_bg/planning/02_AUDIO_ANALYSIS.md) | Decodificación PCM, STFT/FFT estéreo (L & R), escala log/mel, attack/decay y detección de beats. |
| [03_VISUAL_EFFECTS_AND_BACKGROUND.md](file:///home/jg/audio_bg/planning/03_VISUAL_EFFECTS_AND_BACKGROUND.md) | Ingesta de imagen y video, bass zoom, separación cromática RGB, ondulación ripple y glitch VHS. |
| [04_SPECTROGRAM_VISUALIZERS.md](file:///home/jg/audio_bg/planning/04_SPECTROGRAM_VISUALIZERS.md) | Espectrogramas simétricos: Circular radial (L izq / R der), Horizontal lineal, Vertical y Osciloscopio con glow. |
| [05_PARTICLE_SYSTEM.md](file:///home/jg/audio_bg/planning/05_PARTICLE_SYSTEM.md) | Simulación física de partículas, explosión radial en beats y polvo ambiental cinemático. |
| [06_CONFIGURATION_AND_CLI.md](file:///home/jg/audio_bg/planning/06_CONFIGURATION_AND_CLI.md) | Esquema completo de configuración YAML/JSON, presets predefinidos y comandos CLI. |
| [07_ROADMAP_AND_MILESTONES.md](file:///home/jg/audio_bg/planning/07_ROADMAP_AND_MILESTONES.md) | Fases paso a paso de desarrollo, pruebas intermedias y criterios de aceptación. |
