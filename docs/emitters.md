# Particle emitters

Fire, sparks, smoke, dust, floating lights, explosions, snow, water, rain, invisible light sources... An emitter is a layer of the
**Layers** zone (it goes in front of or behind the other layers, like them) whose clips throw
particles instead of showing a file. They are computed on the GPU (tens of thousands at once) and
always give the same result: any frame can be seen or rendered directly.

## In the studio

- **+ Layer** (left panel, Layers zone) → *Particle emitter* → a preset. The new layer has a 10 s
  clip at the cursor.
- **Place it**: its point on the preview (or in the editor) moves it; the tip of its arrow turns it
  (Shift: steps of 15°). Its shape (line, circle, rectangle) is outlined. With its light on, a circle
  shows the light's reach (where it is still strong) and a fainter one where it fades out; with
  *only the edges, far away*, a blue one where it switches to lighting only the edges; drag the
  handle of any of them to change it. It can also travel along a
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
| Where they are born | shape: point, circle, ring, line or rectangle, and its size (px); or **the letters of a text** (a fixed text: on its outline or all over its letters; they follow it as it moves, turns and grows, and the text can be hidden: letters made of fire). **They travel along the motion path**: with a motion path they go along it at their speed (the shape's width spreads them across it). **Particles' route** (its own section): a route drawn in the editor that each particle follows while it lives, at its speed — flames that lick along a line, a stream that turns. It starts where each one is born (it moves with the emitter) or, with *Where it is drawn*, they all go along it where it is drawn. Its nodes can have a [depth](layers.md#values-on-the-nodes): each particle takes the depth of its point on the route (the motion path's too, with *They travel along the motion path*). **How much they follow it** (0..1, it can have keys): 0 as if there were no route; 1 they go along it as far as their launch and the forces would take them (gravity pushes them along the route instead of down; turbulence stays); between, a mix of both |
| How they leave | direction (0° = up, plus the clip's rotation), spread of the cone (360° = every way), speed and its variation |
| Forces | gravity (negative: they rise), wind, drag (they slow down), turbulence (wandering) and its speed |
| Floor and the scene | a floor px below the emitter, or **the scene's** (with a depth map: where the depth below the emitter comes up to its own): with gravity they fall onto it and **bounce**, keeping part of their speed (bounce) and of their sideways speed. **They go round the scene**: with a depth map, what is in the scene at their depth (within *Solid within*, in depth, of the emitter's) is solid and they slide off it — smoke goes round a wing, fire licks a rock; what is nearer hides them and what is farther is behind them. Where they are born is left free. While you adjust it the preview shows what is solid in cyan. **Bounce off it**: 0 they slide along it (flames lick it); more, they bounce off walls and ceilings (sparks) keeping that much speed — on what faces up they still slide (the floor above bounces them) |
| Life and size | life (s) and its variation; size at birth and at the end, and its variation |
| Color and look | colors at birth, halfway and at the end (their opacity fades them), fade in / out, how they add up (*light* or *matter*), streak (stretched along their motion), soft edge, random seed |
| Look | *particles* (each as it is) or *flame*: they melt into one flame — the color comes from the heat that piles up (colors of its edges, body and core; heat), and rising noise cuts its edges into long tongues (how far, the size of the whirls, how fast they rise, how much they eat the edges) — or *smoke*: soft matter torn into wisps by noise (how much, the size of the wisps, how fast the pattern rises, how far it swirls) — or *water*: they melt into one liquid with a surface lit from the top left (where it becomes liquid: how much of them must pile up; how see-through its body is, its edges staying; its shine; how rounded it looks; ripples: how far, their size and speed; **lens**, px: what is behind it is seen through it bent like through glass — the drops magnify it, and turn it upside down when strong). **Only its light (and glow)**: the particles are not drawn, only their light on the scene and their glow — an invisible light source (preset *Light source*) |
| Glow | a halo of light around them: strength, reach (px) and color (empty = the particles' own; the flame's body color with the flame look) |
| Light | it lights up what is behind it (the background and the layers below it), from where its particles are: strength, reach (px, where it is still strong), where it fades out (px; 0 = 2.5 × the reach), color (empty = the particles' own; the flame's body), how (*soft*: lights and tints what is there; *add*: adds its color, like neon) and flicker. On by default for fire, realistic fire and explosion |
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
          preset: fire          # fire, fire_real, sparks, smoke, dust, light, explosion, lamp, water, water_clear, splash, rain, snow
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
          light: { enabled: true, intensity: 0.8, reach: 450, end: 1100, mode: soft, flicker: 0.25,
                   rim: true, rim_from: 500, rim_soft: 120, rim_width: 10, rim_gain: 2, rim_edge: 0.04,
                   shade: 0.6, gloss: 0.3, shadows: 0.5 }
```

The **Water** preset (`water`) is a jet with the water look that falls onto a floor; `splash`
throws a splash on each beat and `rain` falls across the frame; `water_clear` (*Clear water (lens)*) is almost colorless and bends what is behind it like glass:

```yaml
        emitter:
          preset: water
          water: { threshold: 0.5, clear: 0.7, shine: 1, bump: 1, ripple: 3, ripple_scale: 40, ripple_speed: 50, lens: 0 }
          route: { points: [[0.5, 0.8], [0.45, 0.6], [0.6, 0.45]], open: true }   # their own route (0..1 of the frame)
          route_fixed: false     # true: along it where it is drawn, not from where each is born
          route_strength: 1      # 0 as if there were no route … 1 only the route
```

The **Light source** preset (`lamp`) is `hidden: true` with its light on and a soft glow: set the
glow's strength to 0 for a light with nothing visible.

The light follows the particles: when the fire grows with the bass, moves along its path or
fades out, its light does too. With a background that has a [depth map](depth.md) it lights the
scene in 2.5D (see there), and it can light **only the edges** of things far away (rim light):
*Only the edges, far away* — farther than **Edges from** (px, its blue circle; 0 = everywhere),
changing over **Change over** px, it no longer lights the surfaces but the edges of things that turn
towards it: where the surface turns away from you by **What counts as an edge** or more (less = more
edges), **Edge strength** times as bright. It still ends where the light fades out.

The surfaces are shaded with **normals** worked out from the depth map (its slope taken without
crossing the jumps between objects, smoothed at a fine and a coarse scale, the outlines slightly
rounded; how round with the background depth's **Relief of the surfaces**). Where the light is
**in front** of a surface (nearer than it) it lights the side that faces it, strongest straight on
and fading as the surface turns away; where it is **behind** it, the front stays dark and only the
edges that turn towards it light up (and *Only the edges, far away* works only then):
**Surfaces shading** brightens the side that faces the light and darkens the other, **Shine**
(`gloss`, 0..1) adds highlights where the surface reflects the light towards you (wet skin, metal),
and **Shadows** (`shadows`, 0..1) darkens what has something nearer between it and the light. The
same applies to the [texts that light the scene](texts.md). The normals are an estimate from one
image: they follow the shapes the depth map sees, not the hidden side of things.

Keys of where it is (x, y, rotation, its path) and the music's push count from the moment each
particle is born; the other settings (forces, colors...) apply to all of them now. Any number of
emitter clips (and so of lights) can be on screen at once.
