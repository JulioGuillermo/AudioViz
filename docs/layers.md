# Overlay layers

Images, GIFs and videos drawn over the background: a logo, a frame, a green-screen video,
smoke, light leaks… As many layers as you want, each one a track of **clips** (a clip shows one
file from its start to its end).

## In the studio
- **+ Layer** (left panel, *Layers* zone) asks for a file and adds a layer **in front of the others**
  with a 10-second clip at the cursor.
- **Order**: the higher a layer is in the list, the more in front it is drawn. Drag the entries of the
  left panel to change it (or right-click: *Bring forward* / *Send backward*).
- **Clips**: on the layer's track, drag a clip to move it and its edges to change when it starts and
  ends; double-click the track (or right-click) for a new clip there; right-click a clip to duplicate
  or delete it. The inspector also has the exact times.
- **On the preview** the selected clip has a frame: drag inside to move it, a corner to scale it and
  the round handle to rotate it (Shift: steps of 15°). If the property has keyframes, the handles set
  a key at the cursor.
- The eye of each layer hides its track from the timeline (it keeps rendering).

## A clip's settings

| Group | Options |
|---|---|
| File | image (PNG / WebP with transparency, JPEG…), GIF or video; **base size**: inside the frame, covering it, exactly the frame, or its own pixels |
| Placement | position X / Y (center, 0..1 of the frame, can go outside), scale, rotation, flip, opacity, fade in / out |
| Blend | normal, add (light), multiply (darkens), screen (lightens: great for light, fire, smoke on black), overlay, lighten, darken |
| Filters | brightness, contrast, saturation (−1 = black and white), hue, blur |
| Chroma key | removes a color (green screen): key color, tolerance, soft edge |
| Audio reaction | opacity and/or scale pulse with a band (bass, mids…) |
| Video / GIF | start at (seconds of the file skipped), speed, loop |
| Background effects | for each one (zoom, shake, chromatic aberration, ripple, glitch, color flash): **like the background** (the clip moves with it, as if it were part of the image), **own** (its own settings and band) or **off** |

Every number and color can have **keyframes** (the diamond next to it): a logo that flies in with a
spring, a layer that fades and turns… see [studio.md](studio.md#keyframes).

## Visualizer, particles, lyrics and texts among the layers
Each of them has a **Drawing order**: above all the layers (default), above the background but under
the layers, or right above a given layer. For example, the spectrum above a smoke layer but under a
frame layer.

## In the YAML

```yaml
layers:                      # bottom to top: the last one is drawn in front
  - name: Logo
    clips:
      - path: logo.png
        start: 0
        end: 30
        x: 0.85              # center, 0..1 of the frame
        y: 0.12
        scale: 0.25
        blend: screen
        react: { react_to: bass, scale: 0.1 }
        effects:
          shake: { mode: off }
          bass_zoom: { mode: own, own: { enabled: true, max_zoom: 1.2, sensitivity: 1, react_to: treble } }
        keys:
          rotation: [{ t: 0, v: -30, ease: spring }, { t: 2, v: 0 }]
  - name: Green screen
    clips:
      - path: dancer.mp4
        start: 12
        end: 40
        fit: cover
        chroma: { enabled: true, color: "#00ff00", tolerance: 0.3, softness: 0.1 }
        video: { trim: 3.5, speed: 1, loop: true }
visualizer:
  z_index: 1                 # right above the first layer (0 = above the background; unset = above all)
```

Clip fields not given take their defaults (centered, inside the frame, scale 1, opacity 1, normal
blend, every background effect *like the background*). Up to 16 clips are drawn at the same time.
