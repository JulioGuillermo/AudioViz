# depth_of_field — focus by depth

With a background that has a [depth map](../depth.md), only a range of depths is in focus and the
rest is blurred the farther it is from it, as a camera's lens: the throne sharp and the mountains
behind it soft, or the other way round. Clips and emitters with a depth (their *Depth* section) are
blurred as their depth is. A sharp thing in front doesn't bleed onto the blurred background.

```yaml
background_effects:
  depth_of_field:
    enabled: true
    from: 1        # in focus from (near) …
    to: 0.5        # … up to (far)
    soft: 0.2
    blur: 8
    react: 0
    focus_move: 0
    react_to: bass
```

| Field | Default | Description |
|---|---|---|
| `enabled` | false | Enables the effect (needs a depth map) |
| `from`, `to` | 1, 0.5 | The depths in focus, 0 far … 1 near. While you adjust them in the studio the preview shows the depth map with what is out of focus in black |
| `soft` | 0.2 | How far (in depth) past the focus it reaches its full blur |
| `blur` | 8 | Blur at most (px) |
| `react` | 0 | With the music: how much of the blur the band brings (0 = always all of it; 1 = all of it only at full band) |
| `focus_move` | 0 | With the music: how far the focus moves at full band (+ nearer, − farther) — a rack focus on the beat |
| `react_to` | bass | [Source](../reactivity.md) |

Its focus can also be moved with segments (their transitions travel) or keys.
