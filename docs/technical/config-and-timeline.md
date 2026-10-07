# Configuration and timeline

Package `internal/config`: the YAML project model, its defaults, the **timeline** (how settings
change over the song) and the interpolation used by fade transitions.

| File | Contents |
|---|---|
| `internal/config/config.go` | `ProjectConfig` and all block structs, `DefaultConfig`, `LoadConfig`/`ParseConfig`, `Validate`, path mapping, `Marshal`/`Save` |
| `internal/config/timeline.go` | `Transition`, `Seg[T]`, `Timeline`, `Marker`, `Active`, `Blend`, `BuildTimeline`, `SyncFromTimeline` |
| `internal/config/lerp.go` | `Lerp[T]`: reflection-based interpolation of two config blocks |
| `internal/config/timeline_test.go` | `TestTimeline` |

User view: [../index.md](../index.md#yaml-structure) (sections), [../timeline.md](../timeline.md)
(writing segments by hand), [../studio.md](../studio.md#segments-changing-effects-over-the-song)
(transition rules as seen in the studio). Each option is documented in the per-feature pages.

## 1. Project model (`config.go`)

`ProjectConfig` mirrors the YAML one-to-one (`yaml.v3` struct tags):

| YAML key | Go field / type | Timeline track(s) | User doc |
|---|---|---|---|
| `audio` | `Audio AudioConfig` | — (not time-varying) | [audio.md](../audio.md) |
| `background` | `Background BackgroundConfig` | `background` | [background/](../background/README.md) |
| `background_effects` | `BackgroundEffects` with `BassZoom`, `Shake`, `ChromaticAberration`, `Ripple`, `Glitch`, `ColorFlash` | one track per effect: `bass_zoom`, `shake`, `chromatic_aberration`, `ripple`, `glitch`, `color_flash` | [background/*.md](../background/README.md) |
| `visualizer` | `Visualizer VisualizerConfig` | `visualizer` | [visualizer/](../visualizer/README.md) |
| `particles` | `Particles ParticlesConfig` (`Layers []ParticleLayer` + an inlined single `ParticleLayer`) | `particles` | [particles/](../particles/README.md) |
| `lyrics` | `Lyrics LyricsConfig` | `lyrics` | [lyrics/](../lyrics/README.md) |
| `output` | `Output OutputConfig` | — | [output.md](../output.md) |
| `timeline` | `TimelineRaw yaml.Node` (raw, decoded later) | — | [timeline.md](../timeline.md) |
| `studio` | `Studio StudioView` (`hidden`, `collapsed`: the studio's track view; no effect on the video, omitted when empty) | — | [studio.md](../studio.md) |

Fields not in the YAML (`yaml:"-"`): `Timeline` (built from the blocks + `timeline:`),
`Markers`, and `Dir` (project folder, base for relative paths).

**`ParticlesConfig`** accepts both shapes: a list in `layers:` or, for old projects, the fields of
one layer directly under `particles:` (`ParticleLayer` is embedded with `yaml:",inline"`). If
`layers` is empty, the inline layer becomes the only layer (`Validate`, `FixParticles`).

**Unused fields** (kept so older YAML files still parse): `visualizer.stereo_symmetric`,
`visualizer.draw_mode`, `output.codec` (marked deprecated: always H.264) and
`output.audio_bitrate` (audio is always lossless FLAC). Nothing reads them.

### Defaults and the `--init` template

`DefaultConfig()` returns the full set of defaults. Main values (the rest are in the user docs):

| Block | Defaults |
|---|---|
| `audio` | volume 1.0, sensitivity 1.0, normalize true, normalize_window 4.0 |
| `background` | fit cover, opacity 0.88, darken 0.15, vignette 0.25, loop_video true |
| `background_effects` | bass_zoom on (1.06, sens 1.2); chromatic_aberration on (10 px, radial, bass); ripple **off**; glitch on (threshold 0.70, intensity 0.4); color_flash on (0.15 / 0.10); shake off (zero value) |
| `visualizer` | circular, 56 bars, smoothing 0.75, decay 0.12, radius 220, cyan→purple→magenta gradient, glow on |
| `particles` | on, one inline `radial_burst` layer of 120 |
| `lyrics` | **disabled** (no `enabled` default), karaoke, bottom, size 56, slide_up in / fade out |
| `output` | `output.mp4`, auto size (0×0), 60 fps, preset fast, CRF 18, renderer auto, gpu_device −1 |

`audioviz --init file.yaml` (`createTemplateProject` in `cmd/audioviz/main.go`) is simply
`yaml.Marshal(DefaultConfig())` with placeholder paths `audio.flac`, `background.png`,
`output.mp4`. Because it is a plain marshal it has **no comments** (the CLI help calls it "a full
commented YAML template") and it includes the unused fields listed above.

### Loading (`LoadConfig` → `ParseConfig`)

```
cfg := DefaultConfig()
yaml.Unmarshal(data, &cfg)     // YAML overrides defaults field by field; lists are replaced whole
cfg.Dir = dir of the YAML
cfg.Validate()                 // fills zero values
cfg.BuildTimeline()            // segments per track (section 2)
cfg.mapPaths(relative → filepath.Join(dir, p))   // base blocks AND every segment
```

`Validate` fixes values the user set to zero or left inconsistent: `output.path` → `output.mp4`,
`fps ≤ 0` → 60, `visualizer.bars ≤ 0` → 48 (note: 48, while `DefaultConfig` uses 56) and `> 256`
→ 256, `volume`/`sensitivity ≤ 0` → 1, `normalize_window ≤ 0` → 4, empty particle layers → the
inline layer, empty renderer → `auto`, visualizer type lower-cased. It never returns an error.

Paths are made absolute **after** the timeline is built, so segments that inherit a path from the
previous one are mapped too. Unknown YAML keys are ignored silently (yaml.v3 non-strict decoding),
inside segments as well.

### Saving (`Marshal`, `Save`)

The studio edits `cfg.Timeline` directly, so saving has to regenerate the YAML from it:

1. `SyncFromTimeline` copies segment 0 of every track back into the base blocks and rebuilds
   `TimelineRaw`: one sequence per track with each later segment written as
   `at`, `label` (if any), `transition`, followed by **every field** of the block (not only the
   ones that changed — a hand-written minimal segment becomes verbose after a studio save; the
   meaning is the same). Markers go under `timeline.markers`.
2. The result is marshalled and re-parsed into a fresh `DefaultConfig()`, the timeline is rebuilt,
   and paths **inside** `Dir` are made relative (`filepath.Rel`, rejected if it starts with `..`);
   paths outside stay absolute. This round trip is also the deep copy that keeps the in-memory
   config's absolute paths intact.
3. `Save` writes `path.tmp` with a header comment and renames it over `path` (atomic replace).

## 2. Timeline (`timeline.go`)

### Model

A project is a set of independent **tracks**, each a time-sorted list of segments of one config
block type:

```go
type Transition struct {
    Mode      string  // "cut", "fade" or a background transition (config.TransitionModes)
    Duration  float64 // seconds (not for cut)
    Direction string  // left/right/up/down for wipe, slide, push
}

type Seg[T any] struct {
    At         float64    // start time in seconds
    Label      string     // name shown in the studio
    Transition Transition // how this segment is ENTERED
    Cfg        T          // the complete block in effect from At on
}

type Timeline struct {
    Background []Seg[BackgroundConfig]
    BassZoom   []Seg[BassZoomConfig]
    Shake      []Seg[ShakeConfig]
    Chromatic  []Seg[ChromaticAberrationConfig]
    Ripple     []Seg[RippleConfig]
    Glitch     []Seg[GlitchConfig]
    ColorFlash []Seg[ColorFlashConfig]
    Visualizer []Seg[VisualizerConfig]
    Particles  []Seg[ParticlesConfig]
    Lyrics     []Seg[LyricsConfig]
}
```

`TrackNames` lists the YAML keys in that order (also used by the studio API). `Markers` are
separate named time marks (`{at, label}`, sorted), not tied to any track.

Invariants after `BuildTimeline`: every track has **at least one segment**, segment 0 has
`At = 0` and `Mode = "cut"`, segments are sorted by `At`, and every `Cfg` is a **complete** block
(not a diff). Code can therefore index `track[0]` safely and never needs to merge at render time.

### Building a track (`buildTrack`)

```
segs = [ {At: 0, Cfg: fix(base block), Transition: cut} ]
parse each node of timeline.<track>: read the header {at (required), label, transition}
stable-sort the nodes by at
for each node, in time order:
    cfg = deepCopy(previous segment's Cfg)      // inherit everything
    decode the node, minus at/label/transition, ON TOP of cfg
    fix(&cfg)                                   // FixVisualizer / FixParticles / no-op
    transition = cut if absent; mode defaults to "fade" if a transition is given without mode
    if at <= 0: replace segment 0's Cfg and Label (its transition is ignored)
    else:       append
```

Consequences:

- Inheritance follows **time order**, not file order (the test writes the 90 s segment before
  the 30 s one; the 90 s one still inherits the 30 s changes).
- Decoding onto an existing struct overwrites scalars but **replaces lists whole** (yaml.v3
  allocates a new slice), so `particles.layers` or `visualizer.gradient` in a segment replace the
  previous list rather than merging element by element.
- A segment without `at` is an error (with the YAML line). An unknown track name under
  `timeline:` is an error. Fixers: `FixVisualizer` (bars 48 default / 256 cap, type lower-case),
  `FixParticles` (inline layer → `layers`).
- Two segments with the same `at` are both kept; the later one in the file wins (see `Active`).
- `deepCopy` is a YAML marshal + unmarshal, so it copies slices and pointers deeply but drops
  `yaml:"-"` fields; errors are ignored.

### Resolving time `t`

```go
// index of the last segment with At <= t (0 if t is before everything)
func Active[T any](segs []Seg[T], t float64) int

// prev, cur and blend factor k
func Blend[T any](segs []Seg[T], t float64) (prev, cur *T, k float64)
```

`Active` is a binary search (`sort.Search` for the first `At > t`, minus one, clamped to 0).
`Blend` returns `k = 1` and `prev = cur` unless the active segment `i > 0` has
`Transition.Animated()` (any mode but `cut`, with `Duration > 0`) and `t < At + Duration`; then `prev` = segment `i−1` and
`k = (t − At) / Duration`, rising 0 → 1.

So a fade **starts at `at`** and ends at `at + duration`; the previous segment is shown
unchanged up to `at`. Fades do not chain: if a segment starts while the previous one is still
fading in, the new fade starts from the previous segment's *final* config, so the image jumps.

### Who uses `Blend` / `Active` (`internal/engine/session.go`)

Each consumer decides *what* to blend; `Lerp` is only one of the strategies.

| Track | Strategy |
|---|---|
| `background` | `Lerp` for numeric fields (darken, opacity, vignette); both images are always drawn during the transition, each with its own motion layer, and mixed by the transition mode (see [rendering.md](rendering.md#background-layers-motion-and-transitions)). Video time is `t − At` of the respective segment |
| `visualizer` | If `type`, `bars` or `enabled` differ: no interpolation — the old one fades out over `k < 0.5`, the new one fades in over `k ≥ 0.5` (`VizAlpha`). Otherwise `Lerp` |
| `bass_zoom`, `shake`, `ripple`, `chromatic_aberration`, `color_flash` | Compute the effect **output** for both configs and lerp the outputs (so `enabled: false` → `true` fades in). Chromatic mode switches instead of blending; ripple frequency/phase are taken from the enabled side when one side is disabled (`effectsAt`) |
| `glitch` | No blend: uses `prev` while `k < 0.5`, `cur` after |
| `particles` | Own logic in `stepParticle`: `Active` picks the segment; a new particle system is created per segment, older ones are set `Draining` and their alpha goes `1 − k` during a fade (stay 1 on a cut until their particles die) |
| `lyrics` | `Lerp` of the style. Layout is fixed per line: `lyrics.NewMulti` assigns each line to `Active(segs, line.Start)`; path and offset always come from segment 0 |

### Keyframes (`keys.go`)

`Seg.Keys` (`Keys = map[path][]Key`, `Key{T, V any, Ease}`) and `TextConfig.Keys`. Path = YAML keys
joined by dots, list indices as numbers. Parsed from a `keys:` header field of each timeline entry
(stripped from the body like `at` / `label` / `transition`); the first segment's label and keys
go in an entry `at: 0` that only has them (written by `trackNodes` when needed, read by the
"segment at 0 replaces the base" rule). Lists are sorted on load (`Keys.Sort`; also in
`segsFromJSON` and `lyrics.NewMulti` for texts).

- `ValueAt(list, u)`: holds before the first / after the last key; between keys `Ease(a.Ease, k)`
  (linear, step = hold, ease_in k³, ease_out, ease_in_out cubic, back = easeInOutBack with
  c = 1.70158·1.525, spring = 1 − e^(−6k)·cos(4.5πk), exactly 1 at k = 1); numbers and hex colors (same
  `parseHex` / `formatHex` as `Lerp`) are interpolated, anything else switches at the next key.
- `ApplyKeys(cfg, keys, u)` returns a shallow copy with each path set (`setPath` via reflection,
  field lookup by YAML tag with inlined structs, cached per type); lists and pointers on a path are
  copied first, so the segment's config is never modified. Unknown paths are ignored.
- `Seg.CfgAt(t)` = `ApplyKeys(&Cfg, Keys, t - At)` (or `&Cfg` without keys). `Blend` returns
  `CfgAt` of both segments, so every caller of `Blend` gets animated values and a fade mixes two
  animated configs. Other users: `motionAt` (background motion), `stepParticle` (calls
  `ParticleSystem.SetConfig`, which updates the layers' settings but keeps their particle count),
  `lyrics.Multi.Frame` (fixed texts: style from the keyed config; `position_x/y` and `size`
  changes move and scale the laid-out items around the text's anchor).
- The studio mirrors `ValueAt` in `web/keys.js` (display only; the render always uses Go).

## 3. Interpolation (`lerp.go`)

```go
func Lerp[T any](a, b *T, k float64) T
```

- `k ≤ 0` → deep copy of `a`; `k ≥ 1` → deep copy of `b` (via `deepCopy`, i.e. YAML).
- Otherwise: deep copy of `a`, then `lerpValue` walks the value with **reflection**:

| Kind | Behavior |
|---|---|
| `float32`, `float64` | linear: `a + (b − a)·k` |
| `int`, `int32`, `int64` | linear, rounded (`math.Round`) |
| `string` that parses as a hex color in **both** `a` and `b` (`#rgb`, `#rrggbb`, `#rrggbbaa`) | per channel R, G, B, A (0–255, sRGB values, no gamma/linear conversion), written as `#rrggbb`, or `#rrggbbaa` if either input had 8 digits |
| other `string` | snaps to `b` at `k ≥ 0.5` |
| `struct` | recursive over settable (exported) fields |
| slice, **same length** | element by element (e.g. gradient stops: offsets and colors both blend) |
| slice, different length | snaps to `b` (shallow copy of the slice) at `k ≥ 0.5` |
| pointer, both non-nil | the pointed values are interpolated into a new allocation (`*float64` like `center_gap`, `min_density`, `direction`) |
| pointer, one nil | snaps to `b` at `k ≥ 0.5` |
| `bool`, unsigned ints, everything else | snaps to `b` at `k ≥ 0.5` |

The "switch at half-way" rule for non-numeric fields matches the visualizer crossfade, which also
swaps at `k = 0.5`.

Cost: every call does at least one YAML marshal + unmarshal (`deepCopy`), **also outside
transitions** (`k = 1`). `Session.Prepare` calls `Lerp` for background, visualizer and lyrics on
every frame, so that is three YAML round trips per frame on the CPU. It has not shown up as a
bottleneck next to x264, but it is the first thing to replace (e.g. return `*cur` directly when
`k ≥ 1`) if frame preparation ever needs to be faster.

## 4. Tests (`timeline_test.go`)

`TestTimeline` parses one YAML (with base dir `/proj`) and checks:

| Area | Assertion |
|---|---|
| Ordering | Visualizer segments written as 90 s then 30 s come out as `[0, 30, 90]` |
| Inheritance | 30 s inherits `type: horizontal` and sets 64 bars; 90 s inherits the 64 bars and the 2-stop gradient and changes the type |
| Transitions | Missing transition → `cut`; `{mode: fade, duration: 2}` kept |
| Paths | Base and segment paths resolved against the project dir (`/proj/bg1.png`, `/proj/sub/bg2.png`); `darken` inherited by the background segment |
| `Blend` | At 91 s (1 s into a 2 s fade) → `prev` horizontal, `cur` circular, `k = 0.5` |
| Markers | One marker parsed |
| Round trip | `Marshal` writes relative paths (`sub/bg2.png`, no `/proj/`), and re-parsing yields the same segments, paths and markers |

Not covered: `Lerp` (colors, pointers, slices of different length), `at ≤ 0` replacing the base,
duplicate `at`, unknown track errors, `Validate` defaults.
