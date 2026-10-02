# Visualization Engines and Symmetric Stereo Spectrograms

## 1. Stereo Symmetry Principle (L vs R)

To reflect the real spatial image of the music mix:
- The **Left Channel (L)** drives the left half of the screen or visualizer.
- The **Right Channel (R)** drives the right half.
- Both streams are fed from their own FFT spectra independently, making it obvious when an instrument or effect moves from one earphone/speaker to the other (*pan / stereo ping-pong*).

---

## 2. Designed Visualizer Types

### A. Central Circular Spectrogram (Radial Center)
A ring or circle centered in the frame:
- **Angular Layout**:
  - **Left Semicircle ($90^\circ$ to $270^\circ$)**: Projects the L Channel frequencies. From the top or bottom pole towards the sides, mapping bass near the pole and treble towards the equator (or vice versa).
  - **Right Semicircle ($-90^\circ$ to $90^\circ$)**: Projects the R Channel frequencies in an exactly symmetric way.
- **Style Variants**:
  1. *Radial Bars*: Bars radiating outwards (or inwards) from the base radius $R_0$.
  2. *Smooth Bezier Ring*: A continuous closed path that deforms a circular band like a living membrane.
  3. *Filled Polygon with Glow*: Semi-transparent inner fill with a neon gradient and a glowing outline.
  4. *Double Ring (Bass Pulse)*: Inner ring that pulses with the bass and outer ring that dances with the full spectrum.

```
                  Top Pole (Bass)
                       . - ~ ~ - .
                   .-'      |      '-.
                .-'    L    |    R    '-.
              .'            |            '.
             /              |              \
            |   L Channel   (C)  R Channel  |
             \              |              /
              '.            |            .'
                '-.         |         .-'
                   '-. _    |    _ .-'
                         ~ - - ~
                  Bottom Pole (Treble)
```

### B. Linear Horizontal Spectrogram (Bottom / Center)
- **Position**: Usually placed at the bottom of the screen ($y \approx 0.85 \cdot H$) or centered horizontally.
- **Symmetry Modes**:
  1. *Center-Out Mirrored*: The center of the screen is the starting point ($x = W/2$). The L Channel unfolds to the left and the R Channel to the right. Low frequencies start from the center, creating an expansive visual impact on every kick.
  2. *Dual Channel Bottom Split*: Screen split into two symmetric halves:
     - $[0 \dots W/2]$: L Channel (bass in the center or at the edge).
     - $[W/2 \dots W]$: R Channel.
  3. *Reflected Mirror*: Bars that grow upwards and are reflected, attenuated, downwards with transparency (*Water Reflection Effect*).

### C. Lateral Vertical Spectrogram (Edge Inward)
- The bars are aligned vertically along the left and right margins of the screen:
  - Left margin: L Channel projecting towards the center.
  - Right margin: R Channel projecting towards the center.
- Leaves the whole center of the image free to highlight the main subject or character.

### D. Stereo Oscilloscope (Waveform Neon Lines)
- Draws the raw time-domain waveform with attenuation and persistence:
  - Bright line trace with an overlaid Gaussian blur for a neon effect (*bloom*).
  - Two interlaced lines (Cyan for L, Magenta for R, blending into additive White in the center).

---

## 3. Color Palettes and Glow Effect

- **Preconfigured Color Gradients**:
  - *Cyberpunk*: Cyan `#00f0ff` $\rightarrow$ Deep Purple `#7928ca` $\rightarrow$ Hot Pink `#ff007f`.
  - *Fire / Inferno*: Dark Red `#8b0000` $\rightarrow$ Orange `#ff4500` $\rightarrow$ Gold Yellow `#ffd700`.
  - *Synthwave / Sunset*: Orange Sunset `#ff5e36` $\rightarrow$ Violet `#9b51e0` $\rightarrow$ Electric Blue `#00c9ff`.
  - *Matrix / Toxic*: Dark Emerald `#004d40` $\rightarrow$ Neon Green `#00e676` $\rightarrow$ Electric Lime `#ccff00`.
  - *Monochrome Pure*: White `#ffffff` with decreasing opacities and soft brightness.
  - *Custom Hex Palette*: User-definable in the configuration file.
- **Anti-Aliased Rendering & Glow**:
  - High-precision vector rendering with smooth curves.
  - The neon glow is simulated with multiple passes of increasing radius and decreasing opacity (*Multi-pass Alpha Convolution*).
