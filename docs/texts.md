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
- **Position**: with a text selected, **drag on the preview** to place it (a crosshair marks the
  point), or use *Position X / Y*.
- **Delete**: *Delete* in the inspector or the **Del** key.

## Animating a text

Its colors, outline, glow, shadow and audio reaction, and also its **position** (`position_x`,
`position_y`) and **size** (it scales around its position) can change over time with keyframes,
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
| `align` | center | `center`: the block is centered on the point. `left` / `right`: the text starts / ends at the point |
| `max_width` | 0.9 | Width (relative) before wrapping into more rows |
| everything else | as a new text | Same options as the lyrics: `font`, `size`, `uppercase`, `line_spacing`, `word_spacing`, `color`, `gradient`, `outline_color`, `outline_width`, `glow`, `shadow`, `in_animation`, `out_animation`, `in_duration`, `out_duration`, `slide_distance`, `react` — see [lyrics/README.md](lyrics/README.md) |

Up to 7 texts with different styles can be on screen at once together with the lyrics.
