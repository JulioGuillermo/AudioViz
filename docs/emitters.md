# Particle emitters

Fire, sparks, smoke, dust, floating lights, explosions, snow... An emitter is a layer of the
**Layers** zone (it goes in front of or behind the other layers, like them) whose clips throw
particles instead of showing a file. They are computed on the GPU (tens of thousands at once) and
always give the same result: any frame can be seen or rendered directly.

## In the studio

- **+ Layer** (left panel, Layers zone) → *Particle emitter* → a preset. The new layer has a 10 s
  clip at the cursor.
- **Place it**: its point on the preview (or in the editor) moves it; the tip of its arrow turns it
  (Shift: steps of 15°). Its shape (line, circle, rectangle) is outlined. It can also travel along a
  [motion path](layers.md#motion-path): its particles stay behind as a trail.
- **More clips**: *Clip at <cursor>* in the inspector (or double-click its lane) copies the last one.
- **Preset**: the buttons at the top of the inspector start again from another preset.
- Like any clip: opacity, fades, **blend** (*Add* for light: fire, sparks; *Normal* for smoke),
  filters, the background effects it follows, [masks](layers.md#masks) (on the screen: it can go
  behind something, or only show in a zone) and keys for every number and color.

## Settings

| Group | Settings |
|---|---|
| Amount | per second, a burst at the start, the most alive at once |
| Where they are born | shape: point, circle, ring, line or rectangle, and its size (px) |
| How they leave | direction (0° = up, plus the clip's rotation), spread of the cone (360° = every way), speed and its variation |
| Forces | gravity (negative: they rise), wind, drag (they slow down), turbulence (wandering) and its speed |
| Life and size | life (s) and its variation; size at birth and at the end, and its variation |
| Color and look | colors at birth, halfway and at the end (their opacity fades them), fade in / out, how they add up (*light* or *matter*), streak (stretched along their motion), soft edge, random seed |
| Look | *particles* (each as it is) or *flame*: they melt into one flame — the color comes from the heat that piles up (colors of its edges, body and core; heat), and rising noise cuts its edges into long tongues (how far, the size of the whirls, how fast they rise, how much they eat the edges) |
| Glow | a halo of light around them: strength, reach (px) and color (empty = the particles' own; the flame's body color with the flame look) |
| Audio reaction | the band (bass, mids, treble, volume, transients, beats) makes more of them, faster, bigger, and a burst on each beat |

Lengths are pixels of a 1080-line video (they scale with the output) and with the clip's scale.

## In the YAML

```yaml
layers:
  - name: "Fire"
    kind: emitter
    clips:
      - start: 0
        end: 30
        x: 0.5
        y: 0.95
        blend: add
        emitter:
          preset: fire          # fire, fire_real, sparks, smoke, dust, light, explosion, snow
          rate: 650             # anything not written comes from the preset
          react: { react_to: bass, rate: 1, size: 0.35 }
        keys:
          emitter.rate: [{ t: 0, v: 100 }, { t: 5, v: 900, ease: ease_in }]
```

The **Realistic fire** preset (`fire_real`) is the fire with the flame look and an orange glow.
Any emitter can use them:

```yaml
        emitter:
          preset: fire
          look: flame
          flame: { color_cool: "#b8200a", color_mid: "#ff8a1e", color_hot: "#fff4d0", heat: 1, noise: 22, noise_scale: 60, noise_speed: 260, edge: 0.55 }
          glow: { intensity: 0.8, radius: 90, color: "#ff6a1a" }
```

Keys of where it is (x, y, rotation, its path) and the music's push count from the moment each
particle is born; the other settings (forces, colors...) apply to all of them now. At most 4
emitter clips are drawn at once.
