# Background motion

Each background segment can move the camera slowly over its image, so a still picture is not
static (the "Ken Burns" effect). In the studio: *Image / video* → *Motion*.

```yaml
background:
  path: "cover.png"
  motion:
    type: ken_burns   # see the table
    amount: 0.15      # strength
    easing: linear    # linear | ease
    speed: 1          # drift and sway only
    direction: auto   # ken_burns only: auto, top_left, top_right, bottom_left, bottom_right, top, bottom, left, right
```

| `type` | What it does |
|---|---|
| `none` | Still image (default) |
| `zoom_in` / `zoom_out` | Slow zoom towards / away from the center, from 1 to 1 + `amount` |
| `pan_left` / `pan_right` / `pan_up` / `pan_down` | The image travels in that direction (zoomed in by `amount` to have room) |
| `ken_burns` | Zooms in while the camera travels across the image, from the opposite corner (or side) to the `direction` one (`auto`: a different corner per segment) |
| `drift` | Slow floating around the image, without stopping |
| `sway` | Gentle rotation back and forth |

| Field | Default | Description |
|---|---|---|
| `amount` | 0.15 | Strength: extra zoom (0.15 = 15 %) or how far it pans. 0.05 subtle … 0.4 strong |
| `easing` | linear | `linear`: constant speed. `ease`: starts and ends softly (zoom and pan types) |
| `speed` | 1 | `drift` and `sway`: cycles every 10 seconds |
| `direction` | auto | `ken_burns`: corner or side the camera moves towards: `top_left`, `top_right`, `bottom_left`, `bottom_right`, `top`, `bottom`, `left`, `right` (from the opposite side, straight), or `auto` (a different corner per segment) |

- The motion lasts the whole time the segment is visible: from its start until the next segment
  has fully entered (its transition included), so the image keeps moving during transitions.
- The image is scaled up just enough that its edges never show.
- Like any field, it can change per segment: one image can zoom in and the next one pan.
- It combines with the audio effects ([bass_zoom](bass_zoom.md), [shake](shake.md)...), which
  are applied on top.
