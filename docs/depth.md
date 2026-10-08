# Depth

A background image can have a **depth map**: how near each of its points is. With it, the light of
the [emitters](emitters.md) lights the scene in 2.5D (the side of the rock that faces the fire, not
the one behind), and clips and emitters can **go behind what is nearer** (a fire behind a throne, a
logo behind a mountain) without drawing masks.

## In the studio

- **Image / video** (a background segment) → **Depth** → *Depth from*:
  - **AI (Depth Anything)**: worked out on the GPU from the image itself. The model is downloaded
    once and the map is kept in the project's `cache/depth` folder, so it is only worked out again
    if the image changes. **Model**:

    | Model | Size | Notes |
    |---|---|---|
    | DA3 Small | 105 MB | fast, the default |
    | DA3 Base | 149 MB | |
    | **DA3 Mono Large** | 1.3 GB | made for a single image: the sharpest shapes, the sky at the very back |
    | DA3 Metric Large | 1.3 GB | real distances (also with the sky) |
    | DA3 Large | 1.4 GB | non-commercial |
    | DA3 Giant | 4.9 GB | slow, non-commercial |
    | DA3 Nested Giant + Large | 6.2 GB (two files) | the slowest, non-commercial |
    | DA2 Small | 99 MB | Depth Anything 2: fine edges |
    | DA2 Base / Large | 140 / 454 MB | non-commercial |
    | DA2 Metric indoors / outdoors | 454 MB | real distances, trained on rooms / streets; non-commercial |

    *Non-commercial*: that model's license (CC BY-NC 4.0) only allows non-commercial use; the others
    are Apache-2.0.

    **Detail**: the size the model works at — *normal* (504 px, what they were trained at), *high*
    (1008 px, the default), *very high* (1512 px) or *the image's own size*. The models see about 500 px
    at a time; above that the image is gone over in overlapping **tiles**, each one fitted to one look
    at the whole image (so the layout of the scene stays right) and blended with soft seams: every
    tile adds its fine detail (feathers, spikes, hair). DA3 Small, Base, Large and Giant do *high* and
    *very high* in one go instead. Below the image's size the result is enlarged following the
    image's edges. It is used with 16 bits of precision (smooth gradients). On an Intel iGPU, a
    1920×1088 image: Small 1 s (normal), 2 s (high), 6 s (its own size); Mono Large 1–2 s and ~20 s.
  - **Brightness**: a quick guess without AI (bright = near).
  - **A depth map of my own**: an image, white = near, black = far.
- **Work out the depth** makes it now (otherwise it is made the first time the frame needs it) and
  shows it. For a GIF or a video, its first frame is used.
- The button next to *Editor* under the preview shows **the depth of the scene** (white = near),
  as it moves with the background's motion, zoom and shake: to see where to place things.

## The 2.5D camera

With a depth map the background can be seen by a camera that moves (what is near moves more than
what is far): the [parallax](background/parallax.md) effect.

The zoom can be the camera moving in (the near grows more than the far): `depth` in
[bass_zoom](background/bass_zoom.md). And only a range of depths can be in focus, the rest blurred:
[depth_of_field](background/depth_of_field.md).

## Background effects on some depths

The flash, the chromatic aberration, the ripple and the glitch have **Only on some depths**: between
**Up to (far)** and **From (near)**, 0 far … 1 near, with a soft edge (the flash on the far
mountains only, the glitch breaking only what is behind). While you adjust them the preview shows
the depth map with what they leave out in black. They still react to their band as always.

## Clips and emitters in the depth

A clip or an emitter has a **Depth** section:

- **Its depth**: *none*, *fixed* (0 far … 1 near) or *auto* — the scene's depth where it is (its
  center, or the emitter's origin), plus *nearer / farther than the scene*: it stands on the ground
  wherever it moves. It can have keys like any number.
- **Behind what is nearer**: it is hidden where the scene is nearer than it, with a **soft edge**.
- While you adjust its depth or the soft edge, the preview shows the depth map with **what is
  behind it in red**: what it would hide (or be hidden by, if it goes behind) at a glance.

## The light in 2.5D

With a background that has depth, an emitter's light (see [emitters.md](emitters.md)) fades away
from its own depth (**Fades with depth**; 0 = flat as in 2D) and lights more the side of the
surfaces that faces it (**Surfaces shading**). Its depth is the emitter's (or the scene's at its
origin).

It can also light only a range of depths: **Lights from (near)** — what is nearer than this is in
front of it and is not lit — and **Lights up to (far)** — what is farther is too far —, both 0 (black
in the depth map, the farthest) … 1 (white, the nearest), with a **soft edge**. While you adjust
any of the three, the preview shows the depth map with what is not lit in black.

Far away it can light **only the edges** that face it (rim light): see *Only the edges, far away*
in [emitters.md](emitters.md).

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

From the command line: `audioviz depth [-source ai|brightness] [-model small|base|mono|metric|large|giant|nested|da2-small|da2-base|da2-large|da2-indoor|da2-outdoor] [-detail normal|high|max|native] [-o depth.png] image.png`.
