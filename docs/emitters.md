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
          preset: fire          # fire, sparks, smoke, dust, light, explosion, snow
          rate: 650             # anything not written comes from the preset
          react: { react_to: bass, rate: 1, size: 0.35 }
        keys:
          emitter.rate: [{ t: 0, v: 100 }, { t: 5, v: 900, ease: ease_in }]
```

Keys of where it is (x, y, rotation, its path) and the music's push count from the moment each
particle is born; the other settings (forces, colors...) apply to all of them now. At most 4
emitter clips are drawn at once.
