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

## Masks
A mask limits where a clip is seen; it belongs to the clip (moves, scales and turns with it). In
the clip inspector, *Masks*: **Shape mask** or **Painted mask** opens the [editor](studio.md#editor)
with its tool.

- **Shape** (S): draw a closed shape by hand over the clip (pen, mouse or finger); it is smoothed and
  simplified, and always closed. Several shapes add up.
- **Nodes** (N): click a node to select it (**Shift+click** adds more), drag to move the selected
  ones; **Del** (or Alt+click) removes them; **C** changes the type of the selected node.
  - **Right-click the line**: *add a node here* (exactly on the curve: the shape does not change),
    *delete this segment* (a closed shape opens there; an open one splits in two), *close the
    shape*, *delete shape*. Double-clicking the line also adds a node.
  - **Right-click a node**: its type, delete the node(s), close the shape, delete shape; with the two
    ends selected, **join** (**J**): the two ends of one open shape close it; ends of two open shapes
    join them into one.
  - An **open** shape (orange, dotted) is a line, not an area: it does not mask until it is closed. Node types: **smooth** (automatic curve,
  circle), **corner** (a sharp point, square), **symmetric handles** (diamond: drag a handle and the
  other mirrors it), **aligned handles** (hexagon: always in line, so no point, but each keeps its
  own length) and **free handles** (triangle: each handle on its own). **C** goes through them in
  that order. Turning a node into
  handles starts from the curve as it is.
- **Brush** (B) / **eraser** (E): paint the mask; size, hardness and opacity in the bar above, and
  the pen's **pressure** can set the size and / or the opacity. Saved as a PNG in the project's
  `masks/` folder.
- Each mask: *only inside* or *only outside*, **soft edge** (px), **strength**, on / off. The
  *inside* masks of a clip add up (it is seen in their union: a shape plus painted areas), the
  *outside* ones cut holes.
- The handles and the mask outlines follow the clip as it is shown, with the background effects
  it follows (zoom, shake) and its audio pulse.
- **Shape over time**: key the shape (its diamond), move the cursor and edit the nodes: the shape
  morphs between keys with the key's curve (right-click its diamond on the timeline: ease in / out,
  spring...). The keys need the same number of points (otherwise it switches); a node that changes
  type (e.g. smooth → corner) moves all the way and changes type halfway, and its handles move
  when it has them in both keys.

```yaml
        masks:
          - kind: vector                  # or raster
            mode: inside                  # outside: hidden inside the shape
            feather: 20
            shapes:
              - points: [[0.5, 0.05], [0.95, 0.5, 1], [0.5, 0.95, 2, -0.2, 0, 0.2, 0], [0.05, 0.5]]
                open: false               # true: a line (does not mask)
                # 0..1 of the clip's image: [x, y] smooth · [x, y, 1] corner ·
                # [x, y, 2 | 4 | 3, in x, in y, out x, out y] symmetric / aligned / free handles (offsets)
          - kind: raster
            path: masks/abc123.png        # white / opaque = inside
```

## Motion path

A clip or a text can travel along a path drawn on the screen (like a motion path in a video
editor). *Motion path* in the inspector → **Draw a path** opens the [editor](studio.md#editor)
with the path tool (**P**): draw it by hand from where it starts to where it ends (the ring marks
the start), then drag its nodes; the node types, the right-click menus (add a node, delete a
segment, close it...) are the same as for the masks. *Draw again* replaces it.

- **Progress along the path**: 0 = start, 1 = end. A new path gets two keys, 0 at the start and 1
  at the end (ease in / out): change them, add more, give them curves (it can stop, go back, bounce
  with a spring...). A closed path goes round again past 1 (2 = two laps).
- **Turn with the path**: its rotation follows the direction of the path (added to its own
  rotation; 0° = moving right).
- While it has a path its *Position* is not used; dragging it on the preview moves the whole path.

```yaml
    motion:
      path: { points: [[0.1, 0.8], [0.5, 0.4], [0.9, 0.8]], open: true }   # 0..1 of the frame
      progress: 0
      orient: true
    keys:
      motion.progress: [{ t: 0, v: 0, ease: ease_in_out }, { t: 6, v: 1 }]
```

## Particle emitters
A layer can also be a **particle emitter** (fire, sparks, smoke...): *+ Layer* → *Particle emitter*. See
[emitters.md](emitters.md).

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
