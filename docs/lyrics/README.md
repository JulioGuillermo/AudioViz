# Lyrics

Shows synced lyrics, word by word if the file allows it, with animations, colors and
audio reactivity. The text is drawn on the GPU from a signed distance field (SDF), so
words scale crisply and the outline, glow and shadow are very cheap.

```yaml
lyrics:
  enabled: true
  path: "lyrics.lrc"
```
Or from the command line: `audioviz project.yaml --lyrics lyrics.lrc`.

Don't have the `.lrc`? [Generate it from the audio and the plain-text lyrics](generate.md).

## Formats
| Format | Timing | Example |
|---|---|---|
| `.srt` | start and end of each line | `00:00:58,000 --> 00:01:00,500` |
| `.lrc` | start of each line | `[00:58.00]Text of the line` |
| enhanced `.lrc` | **start of each word** | `[00:58.00]<00:58.00>Text <00:58.40>of <00:58.60>the <00:58.90>line <00:59.60>` |

- If there are only per-line timings, words are distributed according to their length.
- In LRC, a tag with no text marks when the previous word ends: at the end of the line
  (`<00:59.60>`), or in the middle when there is a pause before the next word
  (`<00:58.00>Text <00:58.30> <00:58.90>line`), so the karaoke fill stops during the silence.
  Without it a word lasts until the next one starts. The studio and the generator write it
  whenever a word ends at least 0.05 s before the next one. An empty line with a timestamp (`[01:06.00]`) closes the previous one.
- Without an explicit end, a line lasts until the next one, but it hides itself if a long instrumental comes along.
- `[offset:+/-ms]` in the LRC and multiple tags on one line (`[00:10.00][01:20.00]Chorus`) are honored.

## Options
| Field | Default | Description |
|---|---|---|
| `enabled` | false | Shows the lyrics |
| `path` | — | `.lrc` / `.srt` file |
| `offset` | 0 | Seconds added to all timings (+ = later). Useful to correct offsets |
| `font` | built-in Go Bold | `.ttf` / `.otf` font |
| `size` | 56 | Size in px |
| `uppercase` | false | Converts the text to uppercase |
| `max_width` | 0.9 | Relative maximum width; longer lines are wrapped onto several rows |
| `line_spacing` | 1.15 | Spacing between rows |
| `word_spacing` | 0.12 | Extra space between words (fraction of `size`) |
| `align` | center | Row alignment with `position` `top`/`bottom`/`center`/`custom`: center, left, right |

## Position
| `position` | Result |
|---|---|
| `top` | Centered at the top, `margin` px from the edge |
| `bottom` (default) | Centered at the bottom, `margin` px from the edge |
| `center` | In the center of the image |
| `left` | Against the left side (at `margin` px), left-aligned, at height `position_y` |
| `right` | Same, on the right |
| `side` | **Alternates left and right on each line** |
| `custom` | At `position_x`, `position_y` (0–1) |

`margin` (default 60 px) is the distance to the edges. With `left`/`right`/`side`, `position_y` (0–1) sets the
height (0.5 = center) and a smaller `max_width` is advisable (≤ 0.45 on vertical images, so the left and right blocks don't overlap).

## Word mode (`mode`)
| Value | Result |
|---|---|
| `karaoke` (default) | The whole line is visible in `upcoming_color`; each word fills from left to right with the main color as it is sung |
| `words` | Each word appears (with a small pop) right when it is sung |
| `line` | The whole line in the main color |

`word_pop` (default 0.12) is the scale pulse of each word when it starts being sung (0 = no pulse).

## In and out animations
```yaml
  in_animation: "slide_up"
  out_animation: "zoom"
  in_duration: 0.3
  out_duration: 0.4
  slide_distance: 40
```
| Value | In | Out |
|---|---|---|
| `fade` | fades in | fades out |
| `slide_up` | rises from below | leaves upwards |
| `slide_down` | drops from above | leaves downwards |
| `slide_left` | slides in to the left | leaves to the left |
| `slide_right` | slides in to the right | leaves to the right |
| `slide_side` | enters from its side (ideal with `position: side`) | leaves through its side |
| `pop` | grows from small | shrinks |
| `zoom` | arrives from large | grows and disappears |
| `none` | hard cut | hard cut |

All of them include a fade. `animation` is a shortcut to set the same one for both. The in animation starts
`in_duration` seconds before the line's start so it is complete when sung.

## Colors
| Field | Default | Description |
|---|---|---|
| `color` | #ffffff | Main color (sung word) |
| `gradient` | — | Vertical gradient across the letters, from top (`offset: 0`) to bottom (`1`). Replaces `color` |
| `upcoming_color` | #ffffff66 | Words not yet sung (karaoke); accepts alpha `#rrggbbaa` |
| `outline_color`, `outline_width` | #000000, 2 | Outline (px; 0 = no outline) |
| `glow.enabled`, `glow.color`, `glow.radius`, `glow.intensity` | true, text color, 12, 0.6 | Halo |
| `shadow.enabled`, `color`, `offset_x`, `offset_y`, `opacity`, `blur` | true, black, 3, 4, 0.6, 4 | Blurred drop shadow |

## Audio reactivity (`react`)
| Field | Default | Description |
|---|---|---|
| `react_to` | bass | [Source](../reactivity.md) |
| `scale` | 0.05 | Pulse: maximum scale increase |
| `glow` | 0 | Extra glow with the band |
| `shake` | 0 | Shake in px |
| `chromatic` | 0 | RGB separation in px |
| `flicker` | 0 | Flame-like brightness flicker (0–1) |
| `flash`, `flash_color` | 0, — | Shifts the color towards `flash_color` with the band (0–1) |

## Example: fire lyrics
```yaml
lyrics:
  enabled: true
  path: "lyrics.lrc"
  font: "fonts/NotoSerifDisplay-CondensedBold.ttf"
  size: 58
  uppercase: true
  position: "bottom"
  margin: 280
  mode: "karaoke"
  in_animation: "slide_up"
  out_animation: "zoom"
  gradient:
    - { offset: 0.0,  color: "#fff3b0" }
    - { offset: 0.45, color: "#ffae2a" }
    - { offset: 1.0,  color: "#d4200a" }
  upcoming_color: "#5a1a0a99"
  outline_color: "#1a0000"
  outline_width: 2.5
  glow: { enabled: true, color: "#ff4a00", radius: 16, intensity: 0.7 }
  react: { react_to: bass, scale: 0.06, glow: 0.5, flicker: 0.6, flash: 0.3, flash_color: "#fff8e0", shake: 2 }
```
