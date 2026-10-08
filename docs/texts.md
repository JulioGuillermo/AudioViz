# Texts (titles)

Fixed texts shown from one time to another at a position you choose: titles, credits, chapter
names... They have every style option of the [lyrics](lyrics/README.md) (font, size, color or
gradient, outline, glow, shadow, in/out animations and audio reaction), but you place them.

## In the studio

- **Titles and texts** in the left panel, or the **Texts** lane of the timeline.
- **Add**: *Text at <cursor>* in the inspector, or double-click the Texts lane at that point.
  A new text copies the style of the selected one.
- **Time**: drag the block on the timeline to move it, its edges to change start / end (or type
  them in the inspector). Overlapping texts go to separate rows of the lane.
- **Position, size and rotation**: a selected text on screen has **handles** on the preview and
  in the [editor](studio.md#editor), like a clip: drag inside the box to move it, a corner to change
  its size, the round handle to rotate it (Shift: steps of 15°). It scales and turns around its
  position (the dot). If the property is animated, the change is a key at the cursor. While the
  text is not on screen, drag on the preview to place it (a crosshair marks the point), or use
  *Position X / Y* and *Rotation*.
- **Masks**: *Masks* in the inspector (see [below](#masks)).
- **Motion path**: it can travel along a path (see [below](#motion-path)).
- **Delete**: *Delete* in the inspector or the **Del** key.

## Animating a text

Its colors, outline, glow, shadow and audio reaction, and also its **position** (`position_x`,
`position_y`), **size** (it scales around its position) and **rotation** can change over time with keyframes,
from the diamond next to each property in the inspector (see [studio.md](studio.md#keyframes)). In
the YAML, `keys:` with times from the text's `start`:

```yaml
texts:
  - text: "HELLO"
    start: 2
    end: 8
    keys:
      position_x: [{ t: 0, v: 0.2, ease: ease_in_out }, { t: 4, v: 0.8 }]
      size: [{ t: 0, v: 60 }, { t: 4, v: 200 }]
      rotation: [{ t: 0, v: -15 }, { t: 4, v: 15, ease: spring }]
      color: [{ t: 0, v: "#ff3030" }, { t: 4, v: "#30ff80" }]
```

## In the YAML

```yaml
texts:
  - text: |-
      I DIDN'T ASK
      TO BE THE DEVIL
    start: 2          # s: starts entering
    end: 9            # s: has fully left
    position_x: 0.5   # 0 left … 1 right
    position_y: 0.2   # 0 top … 1 bottom
    align: center     # center | left | right
    size: 110
    gradient: [{offset: 0, color: "#ffe08a"}, {offset: 1, color: "#ff3b1f"}]
    glow: {enabled: true, color: "#ff5a1f", radius: 18, intensity: 0.9}
    in_animation: zoom
    out_animation: fade
  - text: "Chapter one"
    start: 30
    end: 36
    position_x: 0.06
    position_y: 0.9
    align: left
    size: 48
```

| Field | Default | Description |
|---|---|---|
| `text` | — | The text; line breaks make new rows |
| `start`, `end` | 0, 5 | When it starts entering and when it has fully left (seconds). The in and out animations happen inside this time |
| `label` | — | Optional name shown on the timeline |
| `enabled` | true | Show it or not |
| `position_x`, `position_y` | 0.5, 0.5 | Point where it is placed, relative to the frame |
| `rotation` | 0 | Degrees, clockwise, around its position |
| `masks` | — | Where on the screen it is seen, see [Masks](#masks) |
| `align` | center | `center`: the block is centered on the point. `left` / `right`: the text starts / ends at the point |
| `max_width` | 0.9 | Width (relative) before wrapping into more rows |
| everything else | as a new text | Same options as the lyrics: `font`, `size`, `uppercase`, `line_spacing`, `word_spacing`, `color`, `gradient`, `outline_color`, `outline_width`, `glow`, `shadow`, `in_animation`, `out_animation`, `in_duration`, `out_duration`, `slide_distance`, `react` — see [lyrics/README.md](lyrics/README.md) |

Up to 7 texts with different styles can be on screen at once together with the lyrics.

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

## In the depth

With a background that has a [depth map](depth.md), a text can have a **Depth** like a clip (its
place is its position): it goes **behind what is nearer**, moves with the
[2.5D camera](background/parallax.md) and blurs with the [depth of field](background/depth_of_field.md);
see the lyrics' [In the depth](lyrics/README.md#in-the-depth).

## Light on the scene

A text can light the scene behind it from its letters (a warm title lighting the throne): see the
lyrics' [Light on the scene](lyrics/README.md#light-on-the-scene).

## Masks

A text (and the [lyrics](lyrics/README.md)) can have masks, like an overlay [clip](layers.md#masks),
but they are on the **screen**: they stay still while the text moves, so a title can appear from
behind a line or slide out through a window. *Masks* in the inspector: **Shape mask** or **Painted
mask** opens the editor with that tool; everything else (nodes, node types, the right-click menus,
*only inside / only outside*, soft edge, strength, the shape over time) works as with clips. The
coordinates are 0..1 of the frame:

```yaml
texts:
  - text: "HELLO"
    masks:
      - mode: inside
        feather: 12
        shapes:
          - points: [[0.1, 0.3, 1], [0.6, 0.3, 1], [0.6, 0.7, 1], [0.1, 0.7, 1]]
```

For the lyrics they belong to the segment: select a segment of the lyrics track and use *Masks* in
its inspector (in the YAML, `masks:` in `lyrics:` or in a lyrics segment).
