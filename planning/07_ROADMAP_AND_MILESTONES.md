# Implementation Roadmap and Work Phases

## Phase 1: Go Module Initialization and Audio Pipeline
- [ ] Initialize the `go.mod` module (e.g. `github.com/jg/audio-bg`).
- [ ] Implement a streaming PCM audio decoder (FLAC, MP3, WAV support at 48kHz stereo Float32).
- [ ] Implement an FFT engine with a Hann window and strict L and R channel separation.
- [ ] Create a converter to perceptual scales (Log/Mel bins) and a transient detector (*Spectral Flux / Bass Energy*).
- [ ] **Verification Test**: Test script that processes `I didnt ask to be the devil.flac` and checks the L and R magnitude output on the console or in a data dump.

## Phase 2: Background Engine and Video/Image Ingestion
- [ ] Implement a still image loader with `cover`, `contain` and `blur_padding` modes.
- [ ] Implement a streaming video reader through an FFmpeg pipe with infinite *looping* support.
- [ ] **Verification Test**: Correct loading and scaling of `"I didn't ask to be the devil"` (832x1248) to Full HD resolution (1920x1080) and vertical resolution (1080x1920).

## Phase 3: Reactive Distortion Effects
- [ ] Implement *Bass Zoom / Pulse* with smooth bilinear interpolation.
- [ ] Implement horizontal and radial RGB channel separation (*Chromatic Aberration*).
- [ ] Implement sinusoidal deformation (*Ripple / Wave*).
- [ ] Implement *Glitch / VHS displacement* controlled by a hit threshold.
- [ ] Implement reactive brightness, contrast and vignetting modulation.
- [ ] **Verification Test**: Generation of test frames with different reactive intensity levels applied to the test image.

## Phase 4: Symmetric Stereo Spectrograms (L vs R)
- [ ] Anti-aliased vector and raster 2D drawing engine.
- [ ] Implement the **Central Circular** visualizer (L semicircle on the left, R semicircle on the right).
- [ ] Implement the **Linear Horizontal** visualizer (Center-out mirrored and Dual split).
- [ ] Implement the **Lateral Vertical** visualizer (edges towards the center).
- [ ] Implement the **Waveform / Oscilloscope** visualizer.
- [ ] Implement neon glow layers (*Glow / Bloom*) and color gradient support.

## Phase 5: Reactive Particle System
- [ ] Implement 2D particle simulation in Go (velocity, lifetime, alpha fade-out, additive blending).
- [ ] Implement a radial burst emitter (*Radial Burst*) triggered by kick/bass hits.
- [ ] Implement ambient atmospheric dust (*Ambient Drift*).

## Phase 6: Concurrent Orchestrator, FFmpeg Encoder Pipe and CLI
- [ ] Design the Go worker pool (`runtime.NumCPU()`) with a sequential buffer for continuous writing to the FFmpeg encoder.
- [ ] Terminal progress bar with render FPS, percentage and estimated time (ETA).
- [ ] CLI flag parser (`--audio`, `--bg`, `--output`, `--preset`, `--preview`, `--frame-at`).
- [ ] Embed YAML presets in the binary via `embed.FS`.

## Phase 7: Validation with the Test Files and Calibration
- [ ] Generate a short 10-15 second test video using `"I didnt ask to be the devil.flac"` and `"I didn't ask to be the devil"`.
- [ ] Validate frame-accurate synchronization between audio peaks and visual effects.
- [ ] Validate that building with `go build` produces a self-contained, working binary.
