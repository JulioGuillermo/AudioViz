# Lyrics rendering

How synced lyrics go from a `.srt`/`.lrc` file to pixels: parsing and timing, the SDF word
atlas, layout, animation, style/reactivity, and how the shader (and the CPU copy of it) samples
the atlas. User options are in [../lyrics/README.md](../lyrics/README.md); producing the
word-timed `.lrc` is in [lyrics-generation.md](lyrics-generation.md).

| File | Contents |
|---|---|
| `internal/lyrics/parse.go` | `Load`, `parseSRT`, `parseLRC`, `parseLRCBody`, `distribute`; `Line`, `Word` |
| `internal/lyrics/layout.go` | `Engine` (`newEngine`, `layoutRows`, `anchor`, `anim`, `Frame`), `buildSDF`, `edt2D`/`edt1D`, `Item`, `Atlas` |
| `internal/lyrics/style.go` | `Style`, `StyleFor` (colours, glow, shadow, reactivity), CPU compositor `DrawCPU`/`textAt` |
| `internal/lyrics/multi.go` | `Multi`: one `Engine` per timeline segment, combined atlas |
| `internal/gpu/shaders/render.comp` | `atlasByte`, `sdfAt`, `textGradient`, `drawText`, word culling in `buildTileLists` |
| `internal/engine/render.go` | `fillParams` (lyrics part), `cpuFrame` |

```mermaid
flowchart LR
  F[.srt / .lrc] --> L["Load(path, offset)<br/>[]Line{Start, End, Words, WordTimed}"]
  L --> M["NewMulti: group lines by<br/>segment active at line start"]
  M --> E["newEngine per segment:<br/>rasterize words → SDF → shelf atlas<br/>layoutRows, line end trimming"]
  E --> A["combined atlas 2048 × ΣH<br/>(uploaded once per LyrVer)"]
  E --> FR["Frame(t, feat) → []Item<br/>(≤ 96 per frame)"]
  ST["StyleFor(lerped cfg, t, feat)"] --> P[Params: TextInfo/Style/colours/items]
  FR --> P --> GPU[drawText in shade()]
  A --> GPU
```

## 1. Parsing (`parse.go`)

`Load(path, offset)`: reads the file, strips a UTF-8 BOM, normalizes `\r\n` → `\n`, picks the
parser by extension (`.srt`, `.lrc`; otherwise SRT if the text contains `-->`, else LRC), fails if
no timed line results, then adds `offset` (config `lyrics.offset`, seconds, + = later) to every
line and word time.

### Data model

```go
type Word struct { Text string; Start, End float64 }          // seconds
type Line struct { Start, End float64; Words []Word; WordTimed bool }
```

`WordTimed` = word times came from the file (enhanced LRC); otherwise they are synthesized by
`distribute`. Words are whitespace-separated tokens (`strings.Fields`), punctuation included.

### SRT

- Blocks split on `"\n\n"`. In each block the first row matching
  `(\d+):(\d+):(\d+)[,.](\d+) --> (\d+):(\d+):(\d+)[,.](\d+)` gives start/end; the fraction is read
  as `0.<digits>` (so `,5` = 0.5 s, `,500` = 0.5 s).
- All following rows of the block are joined with spaces; tags `<…>` and `{…}` (e.g. `<i>`,
  `{\an8}`) are removed. Empty text → block skipped.
- Lines sorted by start (stable), then `distribute(line, End)`.
- Edge case: a separator line containing spaces is not `"\n\n"`, so two cues merge into one block
  and the second cue's index/time rows end up in the first cue's text.

### LRC

Per non-empty row:

1. Consume all leading time tags `^\[(\d+):(\d+(?:[.:]\d+)?)\]` — several tags
   (`[00:10.00][01:20.00]Chorus`) produce one line per tag. `mm:ss:xx` is accepted (the second `:`
   becomes `.`).
2. No time tag: if it is `[offset:<ms>]` (case-insensitive) set `globalOffset = −ms/1000`
   (LRC convention: positive offset = lyrics earlier); other metadata (`[ar:…]`, `[ti:…]`) is ignored.
3. `parseLRCBody(rest, start)`.

`parseLRCBody` — word tags `<mm:ss.xx>`:

- No word tags: plain words, `WordTimed = false`.
- With tags: `WordTimed = true`. Text before the first tag starts at the line start. Each tag
  starts a segment that runs until the next tag; several words under one tag split that interval
  evenly. A last segment with no following tag lasts 0.6 s. A tag with no text after it sets the
  previous word's `End` — a pause inside the line — and, when it is the last tag, the line's
  `End` (the "end tag"). The studio (`hPutLyrics`) and the generator (`lyricsgen.LRC`) write
  such a tag after any word that ends ≥ 0.05 s before the next one.

After all rows: stable sort by start; then the end of each line is resolved:

```text
next = start of the next line, or start + 8 for the last line
if End == 0 or End > next:
    est = WordTimed ? lastWord.End + 1.2 : start + 1.5 + 0.45 * nWords
    End = min(next, max(est, start + 1))
if !WordTimed: distribute(line, End)
apply globalOffset to line and words
```

So a line without an explicit end stays until the next line but no longer than the estimate,
which hides it during instrumental breaks. Word times of `WordTimed` lines are not clamped to `End`.

> **Mismatch with the user docs.** The loop meant to make an empty timed row (`[01:06.00]`) close
> the previous line never runs: `parseLRCBody("")` returns a line with no words and `End == 0`,
> which is filtered out before that loop (`if len(l.Words) > 0 || l.End > 0`). Such rows are
> ignored and the estimate above applies. Verified with a scratch copy of `parse.go`:
> `[00:10.00]hello world` + `[00:12.00]` + `[00:30.00]…` gives the first line `10.00–12.40`
> (estimate), not `10.00–12.00`. [../lyrics/README.md](../lyrics/README.md) says the empty row closes the line.

### `distribute` (line timing only)

```text
dur = (End − Start) · 0.9   (0.5 if ≤ 0)
weight(word) = runeCount(word) + 1
word i gets dur · weight_i / Σweight, consecutively from Start
```

The 10 % left at the end is a rest before the next line; rune count (not bytes) handles accents.

## 2. Engine construction (`newEngine`)

One `Engine` per enabled timeline segment that has lines (see §6). Inputs: that segment's
`LyricsConfig`, its lines, frame size W×H.

### Font

`loadFace`: `font` path (`.ttf/.otf`) or built-in `gobold.TTF`; `opentype.NewFace(Size: size, DPI: 72, Hinting: none)` —
at 72 DPI the size is in pixels. Ascent/descent come from `face.Metrics()`.

### Constants derived from the config

| Name | Formula |
|---|---|
| `pad` | `ceil(max(outline_width + glow.radius, shadow.blur + outline_width) + 4)`, clamped to [6, 30] px (glow radius counts even if glow is disabled) |
| `boxH` | `ceil(ascent + descent) + 2·pad` — same for every word of the engine |
| `spaceAdv` | advance of `" "` + `word_spacing · size` |
| `lineH` | `(ascent + descent) · max(line_spacing, 0.5)` |
| `maxW` | `W · max_width` (≤0 → `0.9·W`) |

### Word rasterization and SDF

For every word occurrence (no de-duplication; `uppercase` applied first):

1. `adv` = `font.MeasureString`; bitmap `bw = ceil(adv) + 2·pad` (clamped to 2048) × `boxH`,
   `image.Alpha` coverage drawn with the baseline at `y = pad + ascent`, pen at `x = pad`.
2. `buildSDF(cov, maxD = pad)`:

```text
inside  = coverage ≥ 128
outside² = EDT² to nearest inside pixel ; inside² = EDT² to nearest outside pixel
d = 0.5 − a                  if 0 < a < 1   (antialiased edge: sub-pixel distance from coverage a)
d = sqrt(outside²) − 0.5     if outside pixel
d = −(sqrt(inside²) − 0.5)   if inside pixel
d = clamp(d, −pad, +pad)
byte = round(128 − d · SDFScale),  SDFScale = 4         // 0.25 px per level, + = outside
```

With pad ≤ 30 the bytes stay in [8, 248]. Unused atlas texels are 0 (= 32 px outside).

`edt2D` is the exact Euclidean distance transform of Felzenszwalb & Huttenlocher: `edt1D`
(lower envelope of parabolas) over every column, then over every row; "infinity" is `1e20`.
`edt1D` has an extra branch for `k == 0 && s <= z[0]` that resets the envelope; since
`z[0] = −∞` it is effectively unreachable (defensive).

### Atlas packing

Shelf packing into a fixed width `atlasW = 2048`: words are placed left to right in file order;
when `shelfX + bw > 2048` a new shelf starts at `shelfY += shelfH`. All words share `boxH`, so
shelves are uniform. Atlas = `2048 × (shelfY + shelfH)`, 1 byte per texel (`Atlas{W, H, Pix}`).
Each word stores `glyphWord{ax, ay, aw, ah, pad, adv, offX, offY, start, end}`.

Rough estimate: 300 words at size 56 with default outline/glow (pad 18, boxH ≈ 100, ~11 words per
shelf) ≈ 27 shelves → ~5.5 MB. The GPU text buffer is sized `max(1 MiB, 2 × atlas)` per slot.

### Line layout (`layoutRows`)

Greedy wrapping: add words while `rowWidth + space + adv ≤ maxW` (a word longer than `maxW` gets
its own row). Rows are stacked `lineH` apart and centred vertically on the line anchor:
`y_r = −totalH/2 + lineH·(r + 0.5)`. Horizontal start per row:

| align | row start x (relative to anchor) |
|---|---|
| center | `−rowW/2` |
| left | `−maxW/2` |
| right | `maxW/2 − rowW` |

Each word gets `offX = x + adv/2`, `offY = y_r` (its centre relative to the anchor). `halfH = totalH/2` is
kept for the anchor. Positions `left`/`right`/`side` force `align` left/right by side;
`side` alternates left (even line index) / right (odd).

### Line end trimming (no stacked lines)

```text
for consecutive lines cur, next:
    if cur.end > next.start − in_duration:
        cur.end = max(next.start − in_duration, cur.start + 0.2)
```

The previous line's exit animation therefore starts exactly when the next line's entry animation
starts: one line leaves while the next arrives, and there is never a second steady line on screen.
The 0.2 s floor can still let a very short line overlap when lines are closer than that.

## 3. Per-frame layout (`Engine.Frame(t, feat)`)

### Anchor (`anchor`) — centre of the line block

| position | x | y |
|---|---|---|
| top | W/2 | `margin + halfH` |
| bottom (default) | W/2 | `H − margin − halfH` |
| center | W/2 | H/2 |
| left / right / side | left: `margin + maxW/2`; right: `W − margin − maxW/2` | `H·position_y` (≤0 → H/2) |
| custom (anything else) | `W·position_x` | `H·position_y` (≤0 → H/2) |

### Visibility and animation

A line is processed for `t ∈ [start − in_duration, end + out_duration]` (durations min 0.01 s).
`easeOut(x) = 1 − (1 − clamp01(x))³`.

```text
t < start : p = easeOut((t − (start − inD)) / inD)      anim(in_animation,  p, side, out=false)
t > end   : p = 1 − easeOut((t − end) / outD)           anim(out_animation, p, side, out=true)
otherwise : p = 1 (dx = dy = 0, scale = 1, alpha = 1)
```

`in_animation`/`out_animation` fall back to `animation`. `anim(name, p)` with `q = 1 − p`,
`d = slide_distance`, default `scale = 1, alpha = p`:

| name | dx, dy (in) | dx, dy (out) | scale | alpha |
|---|---|---|---|---|
| fade (any unknown name) | 0 | 0 | 1 | p |
| none | 0 | 0 | 1 | 1 only when p ≥ 1, else 0 |
| slide_up | dy = +d·q | dy = −d·q | 1 | p |
| slide_down | dy = −d·q | dy = +d·q | 1 | p |
| slide_left | dx = +d·q | dx = −d·q | 1 | p |
| slide_right | dx = −d·q | dx = +d·q | 1 | p |
| slide_side | dx = side·2d·q (dy = d·q if centred) | same | 1 | p |
| pop | 0 | 0 | 0.5 + 0.5p | p |
| zoom | 0 | 0 | 1 + 0.6q | p |

Lines whose alpha ≤ 0.001 are skipped.

### Reactivity in layout

`band = min(feat.Band(react.react_to, "bass"), 1.2)`.
- Shake: `a = react.shake·band`; `sx = a(0.6 sin(23.1t) + 0.4 sin(37.7t + 1.3))`, `sy = a(0.6 sin(19.3t + 0.7) + 0.4 sin(41.9t + 2.1))`.
- Beat scale: `beatScale = 1 + react.scale·band`.

Line centre = anchor + shake + animation offset; line scale `ls = animScale · beatScale`.
Word centre = `(cx + offX·ls, cy + offY·ls)` (the whole block scales about its centre).

### Modes (per word)

| mode | Behaviour |
|---|---|
| karaoke (default, also any unknown value) | `fill = clamp01((t − w.start) / max(w.end − w.start, 0.05))` |
| words | hidden until `w.start`; then `p = (t − w.start)/min(inD, 0.25)`, alpha ×`easeOut(p)`, scale ×`1 + word_pop·(1 − easeOut(p))`; fill 1 |
| line | fill 1 |

Word pulse (karaoke and line): for 0.3 s after `w.start`, scale ×`1 + word_pop·sin(π·(t − w.start)/0.3)`.

Output per word: `Item{AX, AY, AW, AH (atlas rect), CX, CY, Scale, Alpha, Fill, Pad}`; at most
`maxItems = 96` (same as `gpu.MaxTextItems`), extra words are dropped.

## 4. Style and audio reaction (`StyleFor`)

Computed every frame from the lyrics config **lerped across the timeline at t** (`Session.Prepare`:
`config.Lerp(prev, cur, k)`), with `band = min(feat.Band(react_to, "bass"), 1)`:

| Field | Value |
|---|---|
| `Color` | `color` (default white) |
| `Upcoming` | `upcoming_color` (default `#ffffff66`) |
| `Outline` | `outline_color` (default black); alpha 0 if `outline_width ≤ 0` |
| `Grad` | `gradient` stops → `{Off, rgb}` (vertical, top 0 → bottom 1); replaces `color` for the fill |
| `GlowR`, `GlowI` | if glow enabled: `radius`, `intensity + react.glow·band` |
| `GlowC` | `glow.color`, default = middle gradient stop or `Color` |
| shadow | if enabled: offset x/y, opacity, blur, colour |
| `Chroma` | `react.chromatic · band` (px) |

Then, in order:

- **Flash**: if `react.flash > 0` and `flash_color` set, `k = min(flash·band, 1)`; `Color` and every
  gradient stop move toward `flash_color` by k.
- **Flicker** (fire): `n = 0.5 sin(31t) + 0.3 sin(57t + 1.1) + 0.2 sin(89t + 2.3)`,
  `b = 1 + flicker·(0.35 + 0.65·band)·n·0.5`; `Color`, gradient stops and `GlowC` are multiplied by
  b (clamped to 1) and `GlowI *= b`. Three incommensurate sines give a non-repeating flame-like
  shimmer that grows with the band.

## 5. Compositing (GPU and CPU)

### GPU sampling

`fillParams` writes, when `fs.Lyr != nil`:

| Param | Content |
|---|---|
| `textInfo` | nItems (≤ 96), atlas width (2048), any-chroma flag (some style with chroma > 0.3 px) |
| `ts[8]` (`TStyle`) | style table: `info` (outline width, glow radius, glow intensity, chroma px or 0), `color`, `upcoming`, `outline`, `glowC`, `shadow` (dx, dy, opacity, blur), `shadowC`, `gradN` (stops, stage), `mask` (screen mask: offset in `LAYERS`, w, h, on), `grad[8]` (rgb + offset) |
| `tA[i]` / `tB[i]` / `tC[i]` | atlas x, y, w, h / screen centre x, y, scale, alpha / fill, pad, **style index**, rotation (rad) |

Rotation: `local = R(−rot)·(p − centre)/scale` (the shadow offset, in screen px, is turned the
same way, so the shadow keeps falling down); the tile culling uses the rotated box's extents.
Screen masks (`LyricsConfig.Masks`, so texts and lyrics segments): the session makes one
frame-sized mask per style (`maskFor(masks, W, H)`, up to `effects.MaskSize`) when it builds the
frame (`FrameState.TextItems/TextStyles`, computed once per frame and shared by the GPU, the CPU
and the studio), packed in `LAYERS` after the clips' images; the word's alpha is multiplied by it
at the pixel (`maskBilinear` / `lyrics.maskAt`). `LayerPixels` reserves room for them.

The atlas (binding 5) is a `uint[]`; `atlasByte(x, y)` extracts byte `idx & 3` of word `idx >> 2`,
`idx = y·atlasW + x`. It is uploaded only when `Session.LyrVer` changes (per slot).

`sdfAt(a, rect)`: clamp `a` into the word's rect, bilinear between the 4 texels (the `+1`
neighbours clamped to the rect so sampling never bleeds into the next word), return
`(128 − v)/4` = signed distance in atlas px. Multiplying by the item scale gives screen px.

`drawText(col, p, ch)` for each word in the tile list, with that word's style `ts[tC.z]` (`sText`, see
[rendering.md](rendering.md#tile-culling-shared-memory)), in item order:

```glsl
local = (p − centre) / scale;  if |local| > rect/2: skip     // atlas px from word centre
a = rect.xy + rect.zw/2 + local
// 1 shadow
ds = sdfAt(a − shadowOffset/scale)·scale − outlineW
col = mix(col, shadowC, (1 − smoothstep(−blur, blur, ds)) · opacity · alpha)   // blur ≥ 0.75
// 2 glow
d  = sdfAt(a)·scale
g  = exp(−max(d − outlineW, 0) / (glowR·0.45)) · glowI · alpha               // glowR ≥ 0.5
g *= 1 − smoothstep(0.55, 0.95, (d/scale) / pad)      // fade to 0 before the rect edge
col = mix(col, glowC, clamp(g)·0.85)
// 3 outline
col = mix(col, outline.rgb, clamp(0.5 − (d − outlineW)) · outline.a · alpha)
// 4 fill (karaoke)
fillX = rect.x + pad + fill·(rect.w − 2·pad)          // atlas x of the progress edge
ty    = (a.y − rect.y − pad) / (rect.h − 2·pad)       // 0 top .. 1 bottom of the text box
k     = clamp(fillX − a.x + 0.5)                      // 1 = sung part
fc    = mix(upcoming.rgb, textGradient(ty), k);  fa = mix(upcoming.a, color.a, k)
col   = mix(col, fc, clamp(0.5 − d) · fa · alpha)
```

Notes:
- The karaoke edge is a 1-atlas-px ramp moving linearly across the word's advance box, so it
  sweeps through letters at constant speed regardless of glyph shapes.
- The shadow offset is applied in screen px (divided by scale in atlas space). Because the
  per-word rect clips everything, a shadow offset larger than `pad − blur − outline` gets cut at the rect edge.
- Coverage uses `clamp(0.5 − d)`: a 1-px anti-aliased edge at every scale (the SDF is resampled, not the bitmap).
- With chroma on in any style, `shade` calls `drawText` three times on the same base colour with
  `ch = −1, 0, +1` and keeps R, G, B respectively; each word shifts its point by `ch ·` its own
  style's chroma (0 for styles without it).
- Culling box per word: `rect/2 · scale + |chroma| + max(|shadow dx|, |shadow dy|) + 2`; at most 48
  words per 32×16 tile.

### CPU (`style.go: DrawCPU`)

`textAt` is a line-by-line port of `drawText` (same constants and order). `DrawCPU` computes the
union box of all items (scaled rect + chroma + shadow offset + 2 px), and processes its rows in
`runtime.NumCPU()` goroutines (interleaved rows). Both apply chroma only above 0.3 px (`chromaOf`).

## 6. Several lyric styles over time (`multi.go`)

The `lyrics` timeline track can change style mid-song ([config-and-timeline.md](config-and-timeline.md)).

- The file is loaded once, with **segment 0's** `path` and `offset` (later segments' path/offset are ignored).
- Each line goes to the segment active at its **start** (`config.Active(segs, line.Start)`); lines of
  disabled segments are dropped.
- Each enabled segment with lines gets its own `Engine` (font, size, position, animations, modes,
  shake/scale reactivity) and sub-atlas; sub-atlases are stacked vertically into one
  `2048 × ΣH` atlas, with `offs[i]` added to each item's `AY` in `Multi.Frame`.
- Colours, outline, glow, shadow, flash/flicker/chroma come from `StyleFor` on the lerped config
  at `t`, so they cross-fade during a `fade` transition, while layout/animation stay with the line's segment.
- `Multi.Frame` concatenates items of all engines and truncates to 96.

Rebuild policy (`engine.Session.Update`): the atlas is rebuilt only when the key
`"WxH|" + yaml(lyrics track) + mtime(segment 0 file)` changes; each rebuild increments `LyrVer`,
which makes the GPU renderer re-upload the atlas, and the studio recreates the renderer if the
new atlas exceeds the text buffer (`Renderer.Fits`).

## 7. Limits and gotchas

| Limit | Value / effect |
|---|---|
| Atlas width | 2048 px; a word wider than `2048 − 2·pad` is clipped at the bitmap edge |
| Items per frame | 96 (engine and `Multi`), extra words not drawn |
| Words per GPU tile | 48 |
| Gradient stops | 8 on the GPU |
| Padding | 6–30 px; large glow radius beyond `pad` is cut by the rect (glow is faded to 0 at 95 % of pad) |
| Duplicate words | Stored once per occurrence (simple, atlas grows linearly with the lyric length) |
| Unused code | `Engine.StyleAt`, `Engine.Band`, `lyrics.New` have no callers (the engine path uses `NewMulti` + `StyleFor`) |

## 8. Fixed texts (titles) and the style table

`config.TextConfig` = `text`, `start`, `end`, `label` + an inlined `LyricsConfig` (every style
option). `DefaultText()` fills missing YAML fields (`UnmarshalYAML`); `MarshalYAML` leaves out the
lyrics-only fields (path, offset, position, margin, mode, word_pop, upcoming_color).

`lyrics.NewMulti(lyricsSegs, texts, W, H)` builds one engine per lyrics segment (as before) and
one per enabled, non-empty text (`textEngine`), all stacked in the same atlas (`subs[:nLyr]` are
lyrics, the rest texts). A text engine is a single `Line`:

- position forced to `custom` (anchor = `position_x·W, position_y·H`), mode `line`;
- line breaks in `text` become `BreakWord` entries that force a new row in `layoutRows`;
- `buildEngine(..., edge=true)`: with `align: left/right` the row starts/ends **at** the anchor
  (lyrics align inside the max width instead);
- its line runs `[start + in_duration, end − out_duration]` so the in animation starts at `start`
  and the out animation ends at `end` (`none` animations take no time; if the text is shorter
  than its animations both meet in the middle).

`Multi.Frame(t, feat, lerpedLyricsCfg)` returns the items **and a style table**: entry 0 is the
lyrics style (`StyleFor` on the lerped lyrics config), then one `StyleFor(textCfg)` per text that
has words on screen, up to `MaxStyles = 8` (a 9th simultaneous text is not drawn). Each item's
`Style` indexes that table; `fillParams` copies it to `ts[]` and the index to `tC.z`. All texts
share the 96-items-per-frame and 48-words-per-tile limits with the lyrics.

Rebuild key (`Session.Update`): `W×H | yaml(lyrics track, texts) | mtime(lyrics file)`.

Animated texts: `ApplyKeys(text, keys, t − start)`; the change of `position_x/y` moves the items,
the change of `size` scales them and `rotation` (also without keys) turns them, all around the
anchor; each item carries the rotation (`Item.Rot` → `tC.w`). The text's style records its index,
anchor and rotation (`Style.Text/AnchorX/AnchorY/Rotation`); `lyrics.Boxes` turns that into the
studio's `X-Texts` header (per text: anchor, rotation and the box of its letters around the
anchor before the rotation) for the handles.
