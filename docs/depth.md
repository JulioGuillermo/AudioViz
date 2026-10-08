# Depth

A background image can have a **depth map**: how near each of its points is. With it, the light of
the [emitters](emitters.md) lights the scene in 2.5D (the side of the rock that faces the fire, not
the one behind), and clips and emitters can **go behind what is nearer** (a fire behind a throne, a
logo behind a mountain) without drawing masks.

## In the studio

- **Image / video** (a background segment) → **Depth** → *Depth from*:
  - **AI (Depth Anything 3)**: worked out on the GPU from the image itself. The model is downloaded
    once (*Small*: 105 MB, fast; *Base*: 149 MB, more detail; both Apache-2.0) and the map is kept
    in the project's `cache/depth` folder, so it is only worked out again if the image changes.
    **Detail**: the size the model works at — *normal* (504 px, ~0.3 s), *high* (1008 px, ~1 s,
    the default) or *very high* (1512 px, ~3 s); higher keeps finer shapes (feathers, spikes). The
    result is then enlarged to the image's size following its edges, and used with 16 bits of
    precision (smooth gradients).
  - **Brightness**: a quick guess without AI (bright = near).
  - **A depth map of my own**: an image, white = near, black = far.
- **Work out the depth** makes it now (otherwise it is made the first time the frame needs it) and
  shows it. For a GIF or a video, its first frame is used.
- The button next to *Editor* under the preview shows **the depth of the scene** (white = near),
  as it moves with the background's motion, zoom and shake: to see where to place things.

## Clips and emitters in the depth

A clip or an emitter has a **Depth** section:

- **Its depth**: *none*, *fixed* (0 far … 1 near) or *auto* — the scene's depth where it is (its
  center, or the emitter's origin), plus *nearer / farther than the scene*: it stands on the ground
  wherever it moves. It can have keys like any number.
- **Behind what is nearer**: it is hidden where the scene is nearer than it, with a **soft edge**.

## The light in 2.5D

With a background that has depth, an emitter's light (see [emitters.md](emitters.md)) fades away
from its own depth (**Fades with depth**; 0 = flat as in 2D) and lights more the side of the
surfaces that faces it (**Surfaces shading**). Its depth is the emitter's (or the scene's at its
origin).

It can also light only a range of depths: **Lights from (near)** — what is nearer than this is in
front of it and is not lit — and **Lights up to (far)** — what is farther is too far —, both 0 (black
in the depth map, the farthest) … 1 (white, the nearest), with a **soft edge**. While you adjust
any of the three, the preview shows the depth map with what is not lit in black.

## In the YAML

```yaml
background:
  path: throne.png
  depth: { source: ai, model: small, detail: high }    # ai, brightness, file (+ path)
layers:
  - name: Fire
    kind: emitter
    clips:
      - x: 0.5
        y: 0.8
        depth: { mode: fixed, z: 0.45, behind: true, soft: 0.04 }   # or mode: auto, z: offset
        emitter:
          preset: fire_real
          light: { enabled: true, depth: 2.5, shade: 0.6, from: 0.9, to: 0.55, range_soft: 0.05 }
```

From the command line: `audioviz depth [-source ai|brightness] [-model small|base] [-detail normal|high|max] [-o depth.png] image.png`.
