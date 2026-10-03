# Studio internals

How `audioviz studio` works inside: the Go HTTP server, its API, background jobs and events,
and the browser app (Preact + htm, no build step). What the studio *does* for the user is in
[../studio.md](../studio.md); this page explains *how* and *why*, for whoever maintains or ports it.

| Part | Files |
|---|---|
| Entry point | `cmd/audioviz/main.go` → `runStudio` |
| Server, API, jobs, SSE | `internal/studio/server.go` |
| Project ⇄ JSON conversion | `internal/studio/project.go` |
| Web app (embedded) | `internal/studio/web/` |
| Render session used by the preview and export | `internal/engine` (see [rendering.md](rendering.md)) |
| Lyrics generation / Resync | `internal/lyricsgen` (see [lyrics-generation.md](lyrics-generation.md)) |

---

## 1. Process and startup

`main()` checks whether `os.Args[1] == "studio"` and calls `runStudio(args)` before the normal
CLI flags are parsed. `runStudio`:

- parses `--addr` (default **`127.0.0.1:8787`**) and `--no-open`. The project path can go before
  or after the flags (it takes `args[0]` if it isn't a flag, else `fs.Arg(0)`);
- calls `studio.Run(Options{Addr, Project, Version}, ready)`. `ready(url)` prints the URL and,
  unless `--no-open`, starts the first of `xdg-open` / `open` that exists in `PATH`.

`studio.Run` (server.go):

1. Creates `Server{hub: newHub()}` and stores the working directory as `startDir`.
2. With no project argument, opens `./project.yaml` if it exists. A folder argument becomes
   `<folder>/project.yaml`. A missing file is silently ignored (the app shows the welcome screen);
   a file that fails to load aborts startup.
3. Builds an `http.ServeMux` (Go 1.22+ method patterns such as `"GET /api/state"`), listens with
   `net.Listen` and serves with plain `http.Serve` — no TLS, no auth, no timeouts.
4. The URL printed for `:port` or `0.0.0.0:port` is rewritten to `http://localhost:port` so it
   opens locally.

### Server state

```go
type Server struct {
    mu       sync.Mutex        // guards projPath, cfg, sess, job, logs
    projPath string            // absolute path of project.yaml
    cfg      *config.ProjectConfig
    sess     *engine.Session   // the loaded project: audio, analysis, backgrounds, lyrics
    renderMu sync.Mutex        // serializes preview rendering and session updates
    rend     *engine.Renderer  // preview renderer (GPU or CPU), created lazily
    hub      *hub              // SSE clients
    job      *job              // the current/last background job (only one at a time)
    ...
}
```

Two locks on purpose: `mu` is held only for short reads/writes of pointers, while `renderMu` is
held for the whole duration of a frame render or `Session.Update`. A slow render therefore never
blocks `/api/state`, file browsing or SSE.

`Server.open(path)` loads the config (`config.LoadConfig`), opens a new `engine.Session`, swaps
it in under `mu`, closes the preview renderer under `renderMu` (it belongs to the old session),
closes the old session, logs and broadcasts a `project` event.

### Embedded web app

```go
//go:embed web
var webFS embed.FS
...
sub, _ := fs.Sub(webFS, "web")
mux.Handle("/", http.FileServer(http.FS(sub)))
```

Every file under `internal/studio/web/` (HTML, JS modules, CSS, the vendored Preact bundle) is
compiled into the binary. There is no bundler: the browser loads `index.html`, which loads
`app.js` as an ES module, which imports the other modules by relative path. Editing a `.js`
file and rebuilding the Go binary is the whole workflow. (While developing, note the files are
served from the embedded copy, so a Go rebuild is needed to see JS changes.)

---

## 2. HTTP API

All JSON. Errors are returned by `fail(w, code, err)` as `{"error": "..."}` with the HTTP status;
the client helper `api()` (util.js) throws `Error(data.error)` for any non-2xx. Request bodies
are read through `readJSON`, capped at **64 MiB** (`io.LimitReader`).

| Method | Path | Request | Response | Handler / what it does |
|---|---|---|---|---|
| GET | `/` (and any static path) | — | files from `web/` | `http.FileServer` over the embedded FS |
| GET | `/api/state` | — | `{version, project: meta\|null, startDir, home, lyricsTool: true, models: {whisper, demucs}, logs: [..200], gpu: string, job?: snapshot}` | `hState`. `models` = `lyricsgen.WhisperModel.Present()` / `DemucsModel.Present()` (file exists with the expected size). `gpu` = `gpu.ListDevices()` (text listing of Vulkan devices). `home` = `$HOME` |
| POST | `/api/project/new` | `{dir, name}` | same as `/api/state` | `hNew`: creates `<dir>/<name>/assets/`, refuses (409) if `project.yaml` exists, writes `config.DefaultConfig()` with `output.path = <dir>/output.mp4`, opens it |
| POST | `/api/project/open` | `{path}` (file or folder) | same as `/api/state` | `hOpen`: folder → `<folder>/project.yaml`, then `open()` |
| GET | `/api/project` | — | `{meta, project: ProjectJSON}` | `hGetProject`, see §3 |
| PUT | `/api/project` | `ProjectJSON` | `{meta, saved: "hh:mm:ss"}` | `hPutProject`: rebuild config, `Session.Update`, save `project.yaml` |
| GET | `/api/analysis` | — | `{fps, frames, bass, mid, treble, rms, wave, beats: [frame...]}` | `hAnalysis`: per-frame curves for the timeline, each as **base64 of one byte per frame** (`b64u8`, value×255 clamped). `rms` is `min(OverallRMS*3, 1)`. `wave` is the stereo peak per frame column |
| GET | `/api/lyrics` | — | `{path, lines: [{start, end, words: [{text, start, end}]}]}` | `hGetLyrics`: parses the lyrics file of the first lyrics segment with offset **0** (offset is applied only when rendering) |
| PUT | `/api/lyrics` | `{lines, path?}` | `{path}` | `hPutLyrics`: writes a word-timed `.lrc`, see §6 |
| GET | `/api/frame?f=<frame>&scale=1..4` | — | `image/jpeg` (+ `X-Render-Ms`, `X-Total-Ms`, `Cache-Control: no-store`) | `hFrame`: renders one preview frame, see §5 |
| GET | `/api/fs?path=&kind=` | — | `{path, parent, entries: [{name, path, dir, size}]}` | `hFS`: file browser, see §7 |
| POST | `/api/upload` | multipart, field `file` | `{path}` | `hUpload`: copies into `<project>/assets/<basename>` |
| GET | `/api/media?path=` | — | file bytes | `hMedia`: `http.ServeFile` (Range, `Last-Modified`, content type) |
| GET | `/api/thumb?path=&s=` | — | JPEG | `hThumb` (`thumbs.go`): image/GIF/video scaled to fit `s`×`s` (64–480, default 240), a video's frame at min(1 s, 10 % of its length); in-memory cache of 400 keyed by path, size, mtime and `s`; at most 3 generated at once |
| GET | `/api/probe?path=` | — | `video.ProbeMedia` result | `hProbe`. Not used by the current web app |
| POST | `/api/export` | `{path?, start?, end?}` (seconds; `end` 0 = to the end) | job snapshot | `hExport`: starts an `export` job, see §4 |
| POST | `/api/lyrics/generate` | `{txt, lang, fast}` | job snapshot | `hGenLyrics`: starts a `lyrics` job |
| POST | `/api/lyrics/resync` | `{lines, targets: [lineIdx], lang}` | `{lines, source}` | `hResync`: synchronous, see §6 |
| POST | `/api/job/cancel` | — | `{ok: true}` | `hCancel`: cancels the current job's context |
| GET | `/api/events` | — | `text/event-stream` | `hub.serve`, see §4 |

`meta` (`projectMeta`, from the session, not the YAML): `{path, dir, name (folder name), width,
height, fps, frames, duration, hasAudio, trackNames}`. The client takes resolution, fps and
duration from here because the session resolves `0 = background size` and the song length.

There is no separate GPU or models endpoint: both live in `/api/state`.

---

## 3. Project load and save (`project.go`)

The UI never edits YAML. It edits a JSON view in which **every track is an explicit list of
segments**:

```go
type ProjectJSON struct {
    Audio   map[string]any       `json:"audio"`   // same keys as YAML `audio:`
    Output  map[string]any       `json:"output"`
    Tracks  map[string][]SegJSON `json:"tracks"`  // background, bass_zoom, shake, chromatic_aberration,
                                                  // ripple, glitch, color_flash, visualizer, particles, lyrics
    Markers []MarkerJSON         `json:"markers"`
}
type SegJSON struct { At float64; Label string; Transition {Mode, Duration}; Cfg map[string]any }
```

- **`ProjectToJSON`**: each config block is converted with `toMap` = `yaml.Marshal` →
  `yaml.Unmarshal` into a map → `normalize` (turns nested `map[any]any` into `map[string]any`
  so `encoding/json` accepts it). Going through YAML means the JSON keys are exactly the YAML
  keys (`fit_mode`, `react_to`...), so the UI schema (`schema.js`) uses the documented names.
- **`ProjectFromJSON(p, dir)`**: starts from `config.DefaultConfig()` and, for every track,
  `segsFromJSON` builds each segment from a **fresh deep copy of the default** (marshal/unmarshal,
  so slices such as gradient stops are never shared between segments) and overlays the map with
  `fromMap`. Missing keys therefore keep their defaults — old or hand-trimmed projects load fine.
  Then: first segment forced to `at = 0`; any transition mode other than `fade` becomes `cut`;
  segments 2..n sorted by `at` (insertion sort that never moves index 0); per-track fix-ups
  (`config.FixVisualizer`, `config.FixParticles`); the lyrics **path and offset are copied from
  the first lyrics segment to all** (they are global); finally `SyncFromTimeline` and `Validate`.
  An empty track becomes one default segment.

**Save path** (`hPutProject`): decode JSON → `ProjectFromJSON` (400 on error) →
under `renderMu`: `sess.Update(cfg)` (reuses audio, analysis and decoded backgrounds when
unchanged; rebuilds the lyric atlas only when the lyrics track or the lyrics file's mtime
changed) → if the preview renderer no longer fits (`Renderer.Fits()`: output size changed or the
lyric atlas outgrew the GPU text buffer) it is closed and recreated on the next frame →
`cfg.Save(path)`.

`ProjectConfig.Save` (internal/config) marshals the config, rewrites absolute paths inside the
project folder as relative ones, writes `project.yaml.tmp` and renames it over `project.yaml`
(atomic). It prepends one fixed header comment:

```yaml
# AudioViz project (hand-editable; see docs/index.md)
```

**User comments are not preserved.** The file is regenerated from the struct on every save, so
comments, key order and formatting of a hand-written `project.yaml` are lost the first time the
studio saves it, and an edit made by hand while the studio is open is overwritten by the next
autosave (there is no mtime check or reload). Only the values survive.

---

## 4. Jobs and server-sent events

### Jobs

Long operations (export, lyrics generation) run as a **single** background job:

```go
type job struct {
    mu       sync.Mutex
    ID       string            // UnixNano
    Kind     string            // "export" | "lyrics"
    Status   string            // running → done | error | canceled
    Progress float64           // 0..1
    Message  string
    Output   string            // file written
    Log      []string
    Started  time.Time
    Gen      *lyricsgen.Status // lyrics only: per-step progress
    cancel   context.CancelFunc
}
```

`startJob(kind, run)` refuses with 409 `a job is already running` if the current job is running
(one at a time: export and generation both saturate the GPU). Otherwise it creates a
`context.WithCancel(context.Background())` — not the request context, so the job outlives the
POST — stores the job and runs `run(ctx, j)` in a goroutine. When `run` returns:

| `run` result | Status | Message |
|---|---|---|
| `errors.Is(err, context.Canceled)` | `canceled` | `Canceled` |
| other error | `error` | `err.Error()` |
| nil | `done` (progress = 1) | whatever `run` set |

and a final `job` event is broadcast. The last job stays in `s.job` (and in `/api/state`) so a
reloaded page still shows it.

`snapshot()` is what both the HTTP responses and the events carry:

```json
{"id", "kind", "status", "progress", "message", "output", "log": [last 40], "elapsed": s,
 "gen": null | {"steps": [{"id","label","state","progress","detail","elapsed","eta"}],
                "current": i, "progress": 0..1, "device": "GPU name", "elapsed": s}}
```

`gen` is a pointer to the last `lyricsgen.Status` copy received from the pipeline. Step ids are
`models`, `prepare`, `separate`, `recognize`, `align`; the UI maps them to translated labels
with `STEP_LABEL` (dialogs.js). `state` is `pending | running | done | skipped | error`; `eta`
is -1 when unknown.

**Export job** (`hExport`): output path = request `path` or `output.path`, made absolute against
the project folder. The job calls `sess.Fork()` — an independent session with a copy of the
current config that reuses the decoded audio and computed analyses — and runs
`fork.ExportRange(ctx, renderer, startFrame, endFrame, out, progress)`. Editing during an export
therefore changes the live session but not the export. Progress updates set
`Frame d/t · N FPS · N s left` (or `Writing MP4...` in the `mux` stage) and are broadcast at most
every **300 ms** (always for `mux`).

**Lyrics job** (`hGenLyrics`): reads the `.txt`, converts the session's integer PCM to
interleaved float32 (`1 / 2^(bits-1)` scale), and calls `lyricsgen.Generate` with
`AudioKey = audioKey(audio path)` = `"path|size|mtime"` (key for the separated-vocals cache) and
`Separate = !fast`. Each status callback (the pipeline throttles itself to ~150 ms) updates
`Gen`, `Progress`, `Message` = current step label + detail, and broadcasts. On success the
output is **always `<project>/lyrics.lrc`**; an existing file is first renamed to
`lyrics-YYYYMMDD-HHMMSS.lrc`. The client, on the `done` event, points every lyrics segment to
`output`, enables the first one and reloads the lines (`onLyricsGenerated` in app.js, with an
800 ms delay so the session update lands first).

**Cancel** (`hCancel`) calls the job's `cancel()`. Export and the lyrics pipeline check the
context between frames / steps and Whisper has an abort callback, so cancellation is
cooperative; the final status arrives as an event.

### SSE `/api/events`

`hub` keeps a set of client channels (buffer **64** messages each). `hub.send(type, data)`
marshals `{"type": type, "data": data}` and does a **non-blocking** send to every client — a
slow client drops messages instead of stalling the server. Each connection:

- sends `: hello\n\n` immediately (forces headers through proxies and lets `EventSource` fire
  `open`);
- writes every message as `data: <json>\n\n` and flushes;
- sends a `: ping\n\n` comment every **20 s** as keep-alive (idle connections through proxies or
  browsers would otherwise be dropped);
- unregisters on `r.Context().Done()`.

| Event `type` | `data` | Sent by | Used by the client |
|---|---|---|---|
| `job` | job snapshot | job progress and completion | yes: top bar, Export / Generate dialogs, `onLyricsGenerated` |
| `log` | string | `Server.logf` (also kept in the last 200 `logs` of `/api/state`) | no |
| `project` | `"open"` | `Server.open` | no |

`EventSource` reconnects by itself; because the full job snapshot is sent each time, a missed
message is harmless.

---


**Export progress** (`job.export`, `exportStatus`): `stage` (`render` / `mux`), `done` / `total`
frames, `fps` (current render speed), `eta` and `elapsed` (s), `seconds` (length being exported).
The dialog (`ExportProgress`) shows stage, bar, frames, speed (and × real time against the
project fps), elapsed and remaining; once finished, the average speed of the whole export.

## 5. Preview frames

`GET /api/frame?f=<idx>&scale=<1..4>` (`hFrame`):

1. Reads `sess` and `output.renderer` under `mu`; `scale` is clamped to `[1, 4]`.
2. Under `renderMu` (frames are rendered one at a time, in request order):
   - creates the preview renderer on first use: `engine.NewRenderer(sess, mode, 1)` — **one GPU
     slot**, falls back to CPU unless the mode is `gpu`;
   - `sess.Prepare(idx)` resolves the frame state on the timeline (active segments, fades,
     background frame, visualizer features). Particles are simulated **incrementally** when
     frames arrive in order; on a backward jump or a jump of more than 600 frames they are
     re-simulated from 5 s earlier (particles live < 5 s), which is why a seek looks like the
     export;
   - `rend.Preview(fs, scale)`: on the GPU, the shader is told the preview scale and writes a
     `ceil(W/scale) × ceil(H/scale)` RGBA image directly (no full-size render + downscale); on
     the CPU it renders full size and `downscale`s.
3. Encodes JPEG quality **80** and replies with `Cache-Control: no-store` and the timing headers.

There is **no server-side frame cache**: every request renders. Caching is unnecessary because
rendering is a single compute dispatch (a few ms on a GPU) and would be invalidated by every
edit anyway. On the client the request key includes `rev` (bumped after each save), so a changed
project always fetches fresh frames.

---

## 6. Lyrics: GET/PUT and Resync

**GET** (`hGetLyrics`) loads the file of the first lyrics segment with `lyrics.Load(path, 0)`.
Times are returned *without* the project's lyrics offset; PUT writes them back without it too, so
the offset stays a render-time setting.

**PUT** (`hPutLyrics`) target file: request `path`, else the current lyrics path, else
`<project>/lyrics.lrc`; any other extension (e.g. `.srt`) is replaced by `.lrc` — editing an SRT
on the timeline produces a sibling `.lrc`. Lines are sorted by start, lines without words are
dropped, and each line is written as an enhanced LRC line with per-word tags plus a closing tag
for the end of the last word:

```
[re:audioviz studio]
[00:12.34]<00:12.34>First <00:12.80>words <00:13.40>here <00:14.10>
```

(`lrcTime` = `mm:ss.xx`, negative times clamped to 0.) The line start comes from its first word;
the `start`/`end` fields of the line object are ignored.

**Resync** (`POST /api/lyrics/resync`, `hResync`) is synchronous (it uses the request context, so
it is **not** a job and `/api/job/cancel` does not affect it; closing the page aborts it). The
client sends **all** its current lines (including unsaved edits) and the indices to re-align.
The server converts the PCM to float32 and calls `lyricsgen.Resync` with the lines, targets,
language and `AudioKey`. It uses the cached separated vocals when available, otherwise the full
mix, and returns `source` describing which (the UI ignores it). Lines that could not be
recognized are returned unchanged. The algorithm (±5 s window, compact-run fitting, keeping the
line's rhythm for unrecognized edge words) is documented in
[lyrics-generation.md](lyrics-generation.md). If the Whisper model is missing it is downloaded
inside this request (no progress shown).

---

## 7. Files: browser, uploads, media

- **`/api/fs`** (`hFS`): `path` defaults to the project folder (or `startDir`); `~` and `~/...`
  expand to the home folder. Hidden entries (leading `.`) are skipped. `kind` filters files by
  extension using `kindExts` (`audio`, `bg`, `lyrics`, `txt`, `font`, `project`); folders are
  always listed. For `bg`, files **without an extension** are kept if `video.ProbeMedia` can open
  them. Sorted folders first, then case-insensitive name. The client asks for kind `none-` when
  choosing a folder (an unknown kind = no filter, then it keeps only `dir` entries).
- **`/api/upload`** (`hUpload`): multipart field `file`, saved as
  `<project>/assets/<filepath.Base(filename)>` (Base prevents path traversal). An existing file
  with the same name is **overwritten**. Used by drag-and-drop and *Upload from this computer…*,
  which is how a remote browser gets files onto the studio machine.
- **`/api/media`** (`hMedia`): `http.ServeFile`, so `Range`, `If-Modified-Since` and MIME
  detection are handled by the standard library. Range support is what lets the `<audio>` element
  seek in long songs without downloading them first.

**Security model**: none beyond the bind address. `/api/fs` lists and `/api/media` serves *any*
path readable by the user, and there is no auth, CSRF or `Origin` check. That is acceptable on
`127.0.0.1` for a single-user tool (with the usual caveat that any web page open in the same
browser can send requests to localhost), and is why the user docs warn about `--addr 0.0.0.0`.

---

## 8. Web app architecture

### Stack

- **Preact + htm**, vendored as one standalone ES module `web/vendor/preact-htm.js` (~13 KB; exports
  `h, html, render, Component, createContext` and the hooks). `html` is htm's tagged template, so
  components are written as `` html`<div class=${x}>...</div>` `` — JSX without a compiler.
- **No build step, no npm**: the files are plain ES modules served as they are. The choice keeps
  the studio embeddable as static files and editable without a JS toolchain.
- Icons: `icons.js` holds the inline SVG bodies of the Lucide icons used (ISC license);
  `<${Icon} name size cls title />` renders them via `dangerouslySetInnerHTML`.
- Styles: `styles.css`, one stylesheet with design tokens on `:root` (`--bg`, `--surface*`,
  `--text*`, `--accent`, band colours `--band-bass/mid/treble`, `--font`, `--mono`). The timeline
  canvases read tokens at draw time through `getComputedStyle`. The layout is a CSS grid:
  rows `44px / 1fr / var(--tl-h)` and columns `220px / 1fr / 340px` (areas top, tracks,
  preview, inspector, timeline); `--tl-h` is set by the timeline resize handle.

### Modules

| File | Contents |
|---|---|
| `index.html` | `<div id="app">`, `styles.css`, `<script type="module" src="app.js">`, inline SVG favicon |
| `app.js` | `App` (all state), `Preview`, `Clock`, `Setup` (getting-started guide), `SegHeader`, `TrackInspector`, `LineEditor`, `Welcome`, `LangSelect` |
| `timeline.js` | `Timeline`, `ViewCanvas`, `Ruler`, `AudioLane`, `Playhead`, `segSummary` |
| `fields.js` | Schema-driven `Form`, `Field` (num/int/optnum/bool/enum/color/gradient/text/file/group/layers), `Select`, `Switch`, `Num`, `Color`, `Gradient`, `Layers`, `Section`, `FILE_ICON` |
| `dialogs.js` | `Modal`, `FilePicker`, `NewProject`, `ExportDialog`, `JobView`, `GenSteps`, `GenLyricsDialog`, `STEP_LABEL`, `LYRIC_LANGS` |
| `schema.js` | `TRACKS` (id, group, label, icon, fields), `AUDIO_FIELDS`, `OUTPUT_FIELDS`, `trackById`. Field types and `showIf`, `firstOnly`, `def`, `help` |
| `store.js` | `createStore`, `useStore`, `useThrottledStore` |
| `i18n.js` / `lang-es.js` | `t()`, `setLang`, `langStore`, `LANGS`; Spanish dictionary |
| `scrub.js` | `createScrubber()` (audio scrubbing) |
| `util.js` | `api`, `clone` (JSON deep copy), `fmtTime`/`parseTime`, `b64u8`, `basename`, `debounce` (with `.flush`), hex/alpha helpers, `gradientCSS` |

### Component map

```
App
├─ Welcome (no project) ─ LangSelect
├─ top bar: project name, save status, job bar, New, Open, LangSelect, Export
├─ track list (Audio, Output, TRACKS grouped; struck through when off at the current time)
├─ Preview ─ (subscribes to timeStore)        Setup (guide over the preview)
├─ transport: buttons, Clock (timeStore), scrub toggle, preview scale <Select>
├─ inspector:
│   ├─ Form(AUDIO_FIELDS) | Form(OUTPUT_FIELDS)
│   ├─ TrackInspector ─ segment tabs, SegHeader, Form(def.fields), "Generate lyrics" (lyrics track)
│   └─ LineEditor (selected lyric line[s]) ─ shift / crosshair / Edit words / Resync / words table
├─ Timeline ─ Ruler(ViewCanvas), AudioLane(ViewCanvas), lyric lines / word blocks,
│             one lane per track with segment blocks, markers, Playhead (timeStore)
└─ dialogs: NewProject, ExportDialog(JobView), GenLyricsDialog(GenSteps), FilePicker (own layer,
            above any dialog), toast, drop zone
```

### State management

All application state lives in `App` with `useState`; there is no global store library. Key
pieces:

| State | Meaning |
|---|---|
| `st` | `/api/state` (models, GPU text, home...) |
| `proj` / `meta` | `ProjectJSON` being edited / session metadata from the server |
| `ana` | analysis curves decoded with `b64u8` into `Uint8Array`s |
| `lyr` | `{path, lines}` from `/api/lyrics`, edited locally |
| `sel` | inspector selection: `{kind: 'audio'\|'output'\|'line'\|'track', track, seg}` |
| `lineSel` / `wordEdit` | selected lyric line indices (contiguous range) / word-edit mode |
| `playing`, `scale`, `rev` | transport, preview scale (default 2), preview revision |
| `save`, `lyrSave`, `job`, `dlg`, `pick`, `toast` | status and overlays |
| `timeStore` | **playback time, outside React state** (see §9) |

Because event handlers registered once (SSE, drag & drop, debounced savers) would otherwise see
stale closures, the latest values are mirrored into refs: `projRef`, `lyrRef`, `scrubRef`.
Every edit goes through:

```js
const update = (fn) => {           // app.js
  const p = clone(projRef.current); fn(p);
  setProj(p); projRef.current = p;
  pushRef.current(p, oldAudioPath, oldLyricsPath);   // debounced PUT /api/project (250 ms)
};
```

The debounced push (`pushRef`, **250 ms**) PUTs the whole project, updates `meta`, shows
"Saved hh:mm:ss", reloads the analysis and the `<audio>` source if the audio path changed, reloads
lyrics if the lyrics path changed, and bumps `rev` so the preview re-renders. Segment drags
update `proj` locally on every mouse move (`onMoveSeg(..., commit=false)` does not save) and save
once on mouse-up (`commit=true`, which also re-sorts segments and keeps the moved one selected).

The UI schema drives the inspector: `Form` walks `fields`, honours `showIf(cfg)` and `firstOnly`
(fields shown only in the first segment, e.g. global lyric settings), and `Field` picks the
widget by `type`. Adding a parameter to the editor is usually one line in `schema.js` plus its
translation.

---

## 9. Performance design

The first versions kept the playback time in `App` state: every animation frame re-rendered the
whole tree (inspector forms, track list, every timeline block), which cost CPU and, worse,
rewrote the `<option>`s of open dropdowns so they flickered or closed during playback. The
current design isolates what changes at 60 fps.

### Time store (`store.js`)

```js
createStore(initial)        // {get, set, sub}; set() ignores identical values, notifies subscribers
useStore(store)             // component re-renders on every change
useThrottledStore(store, ms, active)
```

`App` creates one `timeStore` (`useRef(createStore(0)).current`). While playing, a
`requestAnimationFrame` loop sets it from `audio.currentTime` (or from `performance.now()` when
there is no audio) and stops at the end.

- Only **`Preview`, `Clock` and `Playhead`** call `useStore(timeStore)` and re-render every frame.
  They are small.
- `App` itself reads `time = useThrottledStore(timeStore, 250, playing)`: while `playing`, at most
  one update every **250 ms** (leading update, then a trailing timer that reads the *latest*
  value, so the final position is never lost); when stopped, every change passes immediately (so
  scrubbing and stepping feel instant). This `time` feeds everything else: track "off" state,
  current lyric line highlight, inspector "Segment at …", snapping (`slowTime`).
- Handlers that need the exact time call `timeStore.get()` instead of using the throttled value.

### `Select` that doesn't re-render needlessly (`fields.js`)

Even at 4 renders/s, rebuilding a `<select>` closes or flickers it in some browsers. `Select` is
a **class component** with `shouldComponentUpdate`:

```js
function selectSig(p) {   // value, cls, title and the *translated* option labels
  const tr = p.translate !== false;
  return [String(p.value ?? ''), p.cls, p.title, ...p.options.map(([v, l]) => v + '\u0001' + (tr ? t(l) : l))].join('\u0002');
}
shouldComponentUpdate(next) { return selectSig(next) !== this.sig; }
render(...) { this.sig = selectSig(this.props); ... }
```

- The signature is compared with the one stored **at the last render**, not with `this.props`
  (Preact updates `this.props` even when a render is skipped, so comparing props would miss
  changes that arrived during skipped renders).
- Labels are compared **after translation**, so switching language changes the signature and
  re-renders the select, although `value`/`options` are identical.
- `onChange` is not part of the signature; the handler reads `this.props.onChange` at event time,
  so a skipped render never calls a stale closure.
- Each `<option>` carries `selected` itself because Preact sets `select.value` before the options
  exist; a value not present in `options` is shown as an extra first option.

### Preview pipelining (`Preview` in app.js)

Per-component mutable state in a ref: `inflight`, `lat` (moving average latency, starts at 40 ms,
`lat = 0.8·lat + 0.2·sample`), `shownSeq/shownF/shownKey` (what is on screen), `seq`, `lastReq`,
`lastT`.

- **While playing**: target frame = `round((time + lat/1000) · fps)` — ask for the frame that
  will be current when the answer arrives. A request is sent if fewer than **3** are in flight,
  the key (`frame|scale|rev`) differs from the last request and the frame is ahead of the one
  shown. Over a network with 100 ms latency this keeps several frames in the pipe; locally it
  runs at 50–60 fps.
- Responses are decoded with `createImageBitmap` and drawn only if their sequence number is
  newer than what is on screen (out-of-order answers are dropped).
- **Jump detection**: if time goes back by more than **0.05 s** or forward by more than **1 s**
  between two updates, `shownF` and `lastReq` are reset. Without this, seeking backwards while
  playing would never request anything (every new frame would be "behind" `shownF`), and the
  preview would stall.
- **While stopped**: request exactly the frame at the cursor; if one is in flight, poll every
  15 ms and request when it finishes (only the latest wanted key). Stopping playback resets
  `shownF`.
- The HUD shows resolution, scale, average latency and, while playing, frames received in the
  last second.

Since the server renders under one mutex, more than ~3 requests in flight would only queue up
there and add latency.

### Timeline canvases (`ViewCanvas`)

The ruler and the waveform/band lanes are canvases that **cover only the visible width** of the
scroller, not the whole song. A full-width canvas at high zoom (e.g. a 4-minute song at
600 px/s = 144 000 px × devicePixelRatio) exceeds the browser's maximum canvas size and renders
blank. `ViewCanvas`:

- sizes the canvas to `clientWidth × height` × `devicePixelRatio`;
- moves it with `transform: translateX(scrollLeft)` inside the wide `.tl-inner`;
- sets `g.setTransform(dpr, 0, 0, dpr, -x0·dpr, 0)` so `draw(g, x0, x1)` paints in timeline
  coordinates;
- repaints on `scroll` (passive listener, coalesced with `requestAnimationFrame`) and on
  `ResizeObserver`, and whenever its `deps` change (`[ana, pps]`, `[pps, duration]`).

`AudioLane` draws, per pixel column, the peak of `wave` over the frames in that column, then the
bass/mid/treble curves (average per column) and beat ticks; `Ruler` picks the first step from
`[0.1 … 60] s` that is at least 70 px wide, with 3 sub-ticks.

Everything else (segment blocks, lyric lines, word blocks, markers, playhead) is absolutely
positioned DOM at `left = t · pps`, which has no size limit.

### Zoom with a fixed point

`pps` (pixels per second) is clamped to **[1, 600]**; it starts at 12 and is set to fit the song
(`(clientWidth − 40) / duration`, min 2) on open and whenever the duration changes.

```js
zoomTo(np, offset)  // offset: px from the visible left edge; default = centre
  zoomAnchor.current = { tt: (scrollLeft + offset) / oldPps, offset };  setPps(np);
useLayoutEffect(() => { scrollLeft = anchor.tt * newPps - anchor.offset }, [effPps]);
```

The time under the anchor is recorded before the state change and the scroll is corrected in a
**layout effect**, i.e. after the DOM has the new width but before the browser paints, so there is
no visible jump. Buttons zoom ×1.5 around the centre; **Ctrl/⌘ + wheel** zooms ×1.18 around the
pointer; "show whole song" fits without an anchor.

### Playhead follow

`Playhead` re-renders every frame but only re-checks auto-scroll when `floor(time·pps/100)`
changes (every 100 px). While playing, if the playhead leaves the visible area (or gets within
30 px of the right edge) the view scrolls so it sits at 20 % of the width.

---


**Editable fields keep what you type** (`fields.js: Edit`): text and number boxes render
`value` only while they are not focused. The app re-renders several times a second while
playing, and a controlled `value` would otherwise reset the text being typed on every render.
The value is applied on `change` (Enter or leaving the field); Esc restores it.

## 10. Lyrics editing on the timeline

Lines come from `/api/lyrics` into `lyr.lines` (`{start, end, words: [{text, start, end}]}`) and
are drawn in the "Lyric lines" lane. CSS classes: `now` (being sung), `on` (selected), `multi`
(part of a multi-selection), `edit` (in word-edit mode: dashed outline, `pointer-events: none`,
text hidden).

### Selection before move

`lineDrag` (timeline.js): a mouse-down on a line that is **not already selected** only selects it
(`onSelectLine`); only a mouse-down on an already selected line starts a drag. This prevents a
stray click — usually meant to seek or select — from shifting a line's timing. The `drag` helper
also ignores movements under **3 px**.

`selectLine(i, multi)` (app.js): **Shift+click** selects the contiguous range from the current
selection to `i`; a plain click on a line of an existing multi-selection keeps the selection
(so the group can be dragged). Selection sets `sel = {kind: 'line'}` (LineEditor in the
inspector) and leaves word-edit mode when the selection changes.

### Line drag with snapping

All selected lines move by the same shift. The first selected line's start is snapped:

```js
snap(tt, ev): if (ev.altKey) return tt;        // Alt = free
  nearest of snaps within 8 px (8 / pps seconds)
snaps = [slowTime (cursor), markers, every segment start of every track, every beat]
```

Words are clamped to ≥ 0 and the line `start`/`end` recomputed from its words. During the drag,
`onLyrics(lines)` updates the state **without saving** (`editLyrics(fn, false)`); mouse-up with
movement calls `onLyricsCommit()` → save. Segment drags use the same `snap` (segments are clamped
to `[0.01, duration − 0.01]`; the first segment can't move).

### "Edit words" mode

Enabled from LineEditor (single line only) → `wordEdit = true`. The timeline then renders the
line's words as `.lyrword` blocks with a left grip (`start`) and right grip (`end`):

| Drag | Effect (all snapped unless Alt) |
|---|---|
| block body | moves the word keeping its length, between previous word start + 0.03 s and next word start − 0.03 s |
| left grip | start only, ≥ previous start + 0.03 s, ≤ own end − 0.03 s |
| right grip | end only, ≥ own start + 0.03 s, ≤ next word's start — or, for the last word, the next **line's** first word (so the last word can stretch to the next line) |

After each move the previous word's end is trimmed if it overlaps, and the line bounds are
recomputed. Entering the mode **auto-zooms** so the line fills about half the view:
`want = min(600, 0.5·clientWidth / max(0.5 s, lineLength))`; if that is more than 5 % above the
current zoom it zooms with the anchor at the line centre, otherwise it just scrolls the line to
the centre. The mode exits on **Done** (same button), **Esc** (global keydown), or selecting
another line.

LineEditor also offers: go to line start − 0.5 s, shift selected lines ±0.05 s, "start at
cursor" (single line), a per-word table (text/start/end inputs; changing a start trims the
previous word's end) and the crosshair "start at cursor" per word.

### Autosave

```js
editLyrics(fn, save = true)            // clone lyrRef, apply, setLyr, maybe schedule save
lyrSaveRef = debounce(async () => {    // 600 ms
  await api('PUT', '/api/lyrics', {lines, path});
  await api('PUT', '/api/project', projRef.current);   // makes the session reload the lyrics
  setLyrSave('saved'); setRev(+1);                      // fresh preview frames
}, 600);
```

The second PUT is needed because the session only rebuilds the lyric atlas inside
`Session.Update`; its change detection includes the `.lrc` file's mtime, so an unchanged
project with a rewritten lyrics file still triggers the rebuild. Status ("Saving…/Saved/Save
error") is shown in the LineEditor header.

### Resync flow

1. LineEditor → *Resync line(s)* calls `resync()` with the current `lineSel` and the lyrics
   language (`lyricLang`, persisted in localStorage, also set when generating lyrics).
2. `POST /api/lyrics/resync {lines: all current lines, targets: lineSel, lang}`.
3. The returned lines replace only the targeted indices via `editLyrics` (→ autosave).
4. **"Left untouched" notice**: the client compares each target's words before and after
   (same word count and every start within 1e-6 s). Lines that came back identical were not
   recognized clearly by the server, which returns them unchanged; their 1-based numbers are
   shown in a toast: *"Could not recognize line {n} clearly; it was left as it was. Drag it closer
   to where it is sung and try again."* (A line that genuinely needed no change would also
   trigger the message, which is acceptable.)

---

## 11. Audio scrubbing (`scrub.js`)

When playback is stopped, `seek()` calls `scrubber.play(t)` if scrubbing is on, so dragging on
the ruler/waveform, arrow keys, clicking a line or marker all play a short snippet.

| Constant | Value | Why |
|---|---|---|
| `RATE` | 24 000 Hz | `AudioContext({sampleRate: 24000})`; enough for recognizing the spot, half the memory of a 48 kHz decode (falls back to the default rate if the browser refuses) |
| `LEN` | 0.12 s | snippet length |
| `FADE` | 0.008 s | linear fade in/out to avoid clicks |
| `MIN_GAP` | 60 ms | at most one snippet per 60 ms while dragging |

- The song (`/api/media?path=…`) is fetched and decoded **once** with `decodeAudioData` into an
  `AudioBuffer`; concurrent `load()` calls share the same promise; a source change drops the
  buffer. Decoding starts eagerly when scrubbing is switched on or the audio is loaded with
  scrubbing on (`preload()`), so the first snippet isn't delayed.
- `fire(t)` stops the current snippet (ramping its gain to 0 over `FADE`), creates a new
  `AudioBufferSourceNode` → `GainNode`, schedules fade-in/out and `src.start(now, t, LEN)`.
- **Throttle with trailing call**: if the last snippet started less than `MIN_GAP` ago, the
  position is stored in `pending` and a timer plays the *latest* pending position when the gap
  expires — so the position where the drag stops is always heard.
- Starting playback calls `scrubber.stop()`.
- Toggle: speaker button next to the clock or the **S** key; stored in `localStorage`
  `audioviz.scrub` (`'0'` = off; default on). The `AudioContext` is resumed on use because
  browsers start it suspended until a user gesture.

---

## 12. Internationalization

- Source strings are **English, in the code**. `t(s, vars)` (i18n.js) looks up `s` in the
  dictionary of the current language, falls back to `s`, then replaces each `{name}` with
  `vars.name` (`split/join`, all occurrences).
- `lang-es.js` exports `ES = { 'English text': 'Spanish text', ... }` — one flat object keyed by
  the exact English string (including punctuation and placeholders).
- `DICTS = { en: null, es: ES }`, `LANGS = [['en','English'], ['es','Español']]`. The language is
  stored in `localStorage` key **`audioviz.lang`** (validated against `DICTS`, default `en`).
  `setLang` saves it, sets `<html lang>` and updates `langStore`.
- `App` calls `useStore(langStore)`, so a language change re-renders the whole tree; `Select`
  re-renders because its signature includes translated labels (§9).
- Schema labels, help texts and enum option labels are English in `schema.js` and translated at
  render time (`t(f.label)`, `Select` translates options unless `translate={false}`).
- Server messages (job messages such as `Frame 10/200 …`, `Exported to …`, error strings) are
  **not** translated; only the step labels of lyrics generation are mapped client-side via
  `STEP_LABEL`.

**Adding a language** (e.g. French):

1. Create `web/lang-fr.js` exporting `FR = { ... }` with the same keys as `ES` (copy `lang-es.js`
   and translate the values; keep `{placeholders}` intact).
2. In `i18n.js`: `import { FR } from './lang-fr.js'`, add `fr: FR` to `DICTS` and
   `['fr', 'Français']` to `LANGS`.
3. Rebuild the binary (the file is embedded). Missing keys simply show in English.

**Pitfall: identical English keys share one translation.** The dictionary is keyed by text, so
every use of the same English string gets the same translation. E.g. `'Fade'` is both a
transition mode and a lyric animation, `'on'`/`'off'`, `'Mode'`, `'Speed'`... If a language needs
different words for two uses of the same English text, the English strings must be made
different (e.g. add context to one of them). Conversely, changing an English string in the code
silently drops its translation until the dictionary key is updated as well.

---

## 13. Other client details

- **Keyboard** (global `keydown`, ignored when focus is in `input/select/textarea`): Space
  play/pause; ←/→ one frame (Shift: 1 s); Home/End; M marker (via `prompt()`); S scrubbing;
  Esc leaves word edit; N new segment at the cursor in the selected track; Delete removes the
  selected segment (not the first). Modals close on Esc, only the topmost one (`Modal` checks it
  is the last `.modal-bg`).
- **Drag and drop**: window-level `dragenter/over/leave/drop` with a depth counter (to ignore
  child enter/leave pairs); files are uploaded one by one and assigned by extension (`kindOf`):
  audio → `audio.path`; image/video → background segment active at the cursor (or the first one if
  there is no background yet); `.lrc/.srt` → all lyrics segments + enable; `.ttf/.otf` → lyric
  font for all segments; `.txt` → opens the Generate dialog with that file.
- **New segment**: copies the config of the segment active at that time, `cut` transition,
  inserted after it.
- **Markers**: stored in the project (`markers`), sorted on add; click = seek, double-click =
  rename, right-click = delete (with `confirm`).
- **Model download notice** in the Generate dialog: shows ≈574 MB (Whisper) and/or ≈84 MB
  (Demucs, precise mode only) when `st.models` reports them missing.

## 14. localStorage keys

All accesses are wrapped in `try/catch` (private mode or blocked storage falls back to defaults).

| Key | Values | Default | Set by |
|---|---|---|---|
| `audioviz.lang` | `en`, `es` | `en` | `setLang` (i18n.js) |
| `audioviz.scrub` | `'1'` / `'0'` | on (anything but `'0'`) | `setScrub` (app.js) |
| `audioviz.lyricLang`, `audioviz.layout` (panel widths and timeline height), `audioviz.pickerView` | Whisper language code (`en`, `es`, `fr`, `de`, `it`, `pt`, `ja`) | `en` | `setLyricLang` (app.js): LineEditor selector and on starting a generation |

Nothing else is persisted in the browser; the project, lyrics and timeline zoom state are either
on the server or reset on reload (zoom fits the song again).

## Undo / redo

`app.js` keeps `hist = {past, future, cur, at}` with snapshots `{proj, lyr}` (the immutable objects
the app already produces: every change clones). `update()` and committed lyric edits call
`record()`, which pushes the previous `cur` onto `past` (max 32) unless the last record was less
than 0.6 s ago (so a slider drag is one step). Live drags (segments, texts, lyric lines/words) only
call `histBase()` (make sure the pre-drag state is the base) and are recorded when released.
`applySnap` restores a snapshot: saves the project (`pushRef`) and, if the lyrics differ, writes
them back (`lyrSaveRef`), then clamps the selection. A reloaded lyrics file replaces `cur.lyr`
without adding a step; opening a project resets the history. Ctrl/Cmd+Z, Ctrl+Y and
Ctrl+Shift+Z work outside text fields (which keep their own undo).
