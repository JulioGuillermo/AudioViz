# AudioViz Studio (graphical interface)

```bash
./audioviz studio                 # opens the project.yaml in the current folder (if any)
./audioviz studio path/to/project   # opens that project (folder or .yaml)
./audioviz studio --addr 0.0.0.0:8787   # reachable from another machine on the network
./audioviz studio --no-open       # don't open the browser
./audioviz studio --workspace ~/Videos/AudioViz   # where your projects live (default ~/AudioViz)
```
The studio ships **inside the same binary**: it starts a local server and opens the browser at
`http://localhost:8787`. Nothing needs to be installed. The preview is rendered by the same GPU and the
same engine as the export, so what you see is what gets exported.

> `--addr 0.0.0.0:...` allows access from the network, and anyone on it could browse your folders.
> Only use it on trusted networks.

## Working together

Several people — and agents through the [MCP server](mcp.md) — can have the same project open
(open the studio to the network: `audioviz studio --addr 0.0.0.0:8787`). Each change travels on
its own (*this property of this element*, never the whole project) and everyone sees it at once,
without losing what they are doing: if someone adds or removes an element meanwhile, your changes
still land on the right one. If two people change the same property at the same time, the last
one wins.

- **Who is here**: their initials in the top bar (🤖 an agent). Click yours to set your name (asked
  the first time).
- **Their cursors** on the preview, the timeline and the editor (E), with their names; the arrow
  button next to the initials hides or shows them.
- **What they have selected**: a dot of their color next to that row of the timeline (an agent's:
  where it changed something last).
- **Undo** (Ctrl+Z) undoes only your own changes.
- **What agents changed** (*🤖 N to review* in the top bar): each change in words (*Text 1 ·
  color → #ff0000*; clicking a line selects what it touched), to **accept** or **undo** — only
  that change: what came after it stays (it warns if someone changed the same since). Accept all
  / undo all, and *reviewed too* to see the ones already done.
- The **lyrics** change by parts too: a word or a line at a time, everyone at once, without
  overwriting each other.
- What you hide or fold on the timeline is yours alone (it stays in this browser).

### Chat

The speech-bubble button in the top bar opens the project's chat (the number: messages you haven't
read). Everything in it stays in the project — `chat/messages.jsonl` and the shared files in
`chat/files/` — and travels in its package.

- **Threads**: answer a message in its own thread (the arrow on a message); the message shows how
  many answers it has.
- **Files and images**: the clip button, or paste / drop them. The camera button attaches the
  frame of the preview with its time: clicking it takes everyone there.
- **Links**: the chain button writes a link to what you have selected (a text, a segment, a clip)
  and the time; clicking it selects that and goes there.
- **Mentions**: `@` and a name (it completes them). An agent can listen for its name (see
  [Agents](mcp.md)).
- Edit or remove your own messages; *someone is writing…* while they type.
- **Notifications** (the bell): the browser tells you of new messages while the studio's tab is
  not visible.

## Projects and the workspace
The **workspace** is the folder holding your projects (`~/AudioViz` unless you pass
`--workspace`). The start screen and **Projects** (top bar) show its projects as cards with the
background as thumbnail; click one to open it. From there you can also create a **New project**,
**Import** a package or **Open** any other `project.yaml`.

- **Package** (top bar) → **Download .zip**: one file with everything the project uses — project,
  lyrics, audio, the backgrounds of every segment, fonts and `assets/`. Files from outside the
  project go into `assets/` inside the zip and the project in the zip points to them, so it opens
  anywhere. *Compression*: none, fast, normal or maximum (audio, images and videos are already
  compressed and are stored as they are). Optionally include the exported videos.
- **Collect files into the project** (in Package): the same, but on disk — files the project uses
  from outside its folder are copied to `assets/` and the project points to the copies.
- **Import** (start screen or Projects): upload a package `.zip` from your computer, or pick one on
  the machine running the studio (*From the server…*). It is unpacked into a new folder of the
  workspace (`name`, `name-2`…) and opened. Names that would write outside that folder are refused.

## Starting a new project
1. **New** → choose the folder (the workspace by default) and the name. This creates
   `name/project.yaml`, `name/assets/` and `name/exports/`.
2. **Getting started** appears over the preview:
   - **Choose audio…**: the song. It is analyzed right away and the timeline becomes as long
     as the song (without audio it lasts 30 s, so you can start building the visuals).
   - **Choose background…**: image, GIF or video. The video resolution will be the background's (you can
     set a different one in **Output**). Until you pick one it is 1080×1920 with a black background.
   - **Lyrics** (optional): a `.lrc`/`.srt`, or *Generate…* (from a `.txt`, or transcribing the song).
3. The guide hides itself once there is audio and a background. From then on everything is adjusted in the tracks.

**Drag and drop**: drop files anywhere in the window. They are copied to `assets/`
and placed according to their type:

| File | Goes to |
|---|---|
| FLAC, WAV, MP3, OGG | Project audio |
| Image, GIF or video | Background (the segment active at the cursor; the first one if there is no background yet) |
| `.lrc`, `.srt` | Lyrics (and enables them) |
| `.txt` | Opens *Generate lyrics* with that text |
| `.ttf`, `.otf` | Lyrics font |

A new project starts with sensible defaults (circular visualizer, zoom on the
bass, aberration, glitch on beats, particles). Change whatever you like from each track.

## Screen layout

| Area | What's there |
|---|---|
| Top | Project name, save status, job progress (export / generate lyrics / generate image), **Projects**, **Package**, **Exports**, **Images**, **Export** |
| Left | Track list: Output, then three foldable zones, each with its own color: **Main** (Audio, Image / video, Lyrics, Titles and texts), **Layers** (the [overlay layers](layers.md), front first; drag to reorder) and **Effects** (zoom, shake, chromatic aberration, ripple, glitch, color flash, parallax 2.5D, depth of field, spectrum, particles). Entries off at the current instant are dimmed |
| Center | **Preview** and transport (play, frame by frame, preview quality) |
| Right | **Inspector**: all the parameters of the selected segment |
| Bottom | **Timeline**: ruler with markers and the waveform with the bass/mid/treble curves (both always at the top while the tracks scroll), then the same zones with their tracks |

**Show / hide tracks**: the eye of each entry in the left panel hides its track from the timeline
(only from the timeline: the effect keeps rendering; turn it off in the inspector for that). Click
a zone header, in the left panel or on the timeline, to fold or unfold it. Both are saved in the
project (`studio:` in `project.yaml`), so it opens as you left it; they are not undo steps.

Everything is **saved automatically** to `project.yaml` (shown as "Saved hh:mm:ss"), which remains editable
by hand and renderable with `audioviz project.yaml`.

## Segments: changing effects over the song

Each track is a list of **segments**. The first one lasts from the beginning until the next one; each
new segment starts at its own instant and replaces that track's configuration from there on.

- **Create a segment**: double-click the track at the desired point, or the inspector's **＋ Add segment at cursor**
  button (uses the cursor position), or press **N**. The new segment copies the config of the one
  that was active, so you only change what you want.
- **Move it**: drag it on the timeline. It snaps (magnet) to markers, beats, the cursor and
  other segments; hold **Alt** to move it freely. You can also type the exact instant in
  "Starts at".
- **Transition**: *Cut* or *Fade* (the background has many more) with its duration:
  - numbers and colors are interpolated (e.g. the gradient gradually changes from red to blue);
  - turning an effect on/off fades;
  - a change of visualizer type or bar count fades out and back in;
  - a background change uses its own transition: besides *Fade* there are *Dissolve*, *Dip to black*,
    *Flash*, *Wipe*, *Slide*, *Push* (with a direction), *Zoom in/out*, *Circle*, *Clock wipe*,
    *Glitch* and *Pixelate* (see [background/transitions.md](background/transitions.md));
  - particles from the previous segment stop spawning and fade out while the new ones spawn.
- **Delete**: the *Delete segment* button or the **Del** key (the first one cannot be deleted).
- **Motion** (background segments): slow zoom, pan, Ken Burns, drift or sway over the image, so it
  is not static (see [background/motion.md](background/motion.md)).
- **Name**: give segments names ("Chorus", "Bridge") to see them on the timeline.

### Tied to background segments
A clip (an image, a video, an emitter…) or a segment of a background effect (bass zoom, shake,
chromatic aberration, ripple, glitch, color flash) can be tied to one or more background segments:
*Background segments* in its inspector, one button per background segment. It then shows (or
applies) only while one of them is on screen, and during a transition only on its image: in a
fade, the fire tied to the old background fades out with it and its light never reaches the new
one; in a burn it burns with the paper; a ripple tied to the new background only ripples it. Its
own times stay free. With none chosen it is on every background, as always. A link icon marks
what is tied on the timeline. (Splitting an effect's segment keeps it tied; a new background
segment is a new one.)

**Texts (titles)**: *Titles and texts* in the left panel or the *Texts* lane of the timeline. Add
one at the cursor (or double-click the lane), drag its block to move it and its edges to change
when it starts and ends, and **drag on the preview** to place it. Each text has all the lyrics
style options. See [texts.md](texts.md).

**Lyrics** work the same way: a lyrics segment changes the style (font, position, colors,
animations…). Each line uses the layout of the segment in which it starts being sung, so it doesn't
move around mid-phrase.

## Keyframes

Inside a segment (or a text), a property can change over time. Next to each number, color or switch
of the inspector there is a **diamond**:

- **Click it** to animate the property: it puts a key at the cursor with the current value. Move the
  cursor and change the value: a new key is created there (or the key there is updated). The
  inspector always shows the value at the cursor.
- The diamond is **filled** when there is a key at the cursor (click: remove it), **outlined** when the
  property is animated but not at this instant (click: add a key). Removing the last key leaves the
  property still with that value.
- **Right-click a diamond**: add / remove the key at the cursor, show only that property's keys on
  the timeline, clear all its keys, or clear all the keys of the element (the properties keep the
  value they have at the cursor).
- On the timeline (and on the editor's bar), the keys of the element appear as diamonds on its
  track. **Click** one to select it and go there; **Shift / Ctrl + click** adds or removes keys from
  the selection. **Drag** moves the selected keys together (they snap like segments; Alt = free).
  **Del** deletes the selected keys, **Esc** clears the selection. **Right-click** a key: the curve
  to the next key (linear, hold, ease in / out / in-out, anticipation and overshoot, spring; for all
  the selected ones), delete, select all the keys, **show only** one of its properties, or clear
  keys (of that property or of the whole element).
- **Property filter**: when only one property's keys are shown (also after clicking its diamond), a
  chip with its name appears on the timeline bar and the editor's bar; its ✕ shows them all again.
- Keys belong to their segment: they move with it, and their times count from its start.

Not everything can be animated: the lyrics layout (size, position, spacing, animations), the number of
bars or of particles are built once; their diamonds do not appear.

## Markers
The **⚑ Marker** button or the **M** key at the cursor. Click a marker to jump to it, double-click to
rename it, right-click to delete it. They serve as references for placing segments.

## Images
**Images** (top bar, or *Generate an image…* in the background inspector) generates images from a
prompt, or edits images given as references, on the GPU: Z-Image Turbo, FLUX.2 klein or Qwen
Image. The image forms live in the dialog; the results are kept in the project's `images/` folder
and can be used as the background with one click. See [images.md](images.md).

## Lyrics
- Import a `.lrc` / `.srt` in the **Lyrics** track → *File*.
- **Generate synced lyrics…** (in the lyrics inspector, or by dragging in the `.txt`): choose the
  `.txt` (Suno format works) — or *Transcribe the audio* if you have no text —, the language and the
  mode (accurate / fast). It is built into the binary
  (Demucs + Whisper on the GPU); the progress shows each step with its time. See [generate.md](lyrics/generate.md).
- **Fine-tune timings on the timeline** ("Lyric lines" track):
  - click a line to select it; once selected, drag it to move it (an unselected line never moves,
    so a stray click can't shift it). **Shift+click** selects a range of lines that then move together;
  - **Edit words** in the inspector (single line) shows its words as blocks on the timeline: drag a
    word to move it, its left edge to change only its start or its right edge to change only its end
    (the last word can stretch up to the next line). **Done**, `Esc` or selecting another line leaves
    this mode;
  - the inspector shows each word with its start and end, the crosshair button sets a start at the
    cursor and ±0.05 s buttons shift the selected lines.
- **Resync**: with one or more lines selected, *Resync line(s)* recognizes that part of the song again
  (around the lines' current position, ±5 s, without crossing the neighbouring lines unless they overlap)
  and re-aligns the words. The line is matched to one compact run of recognized words (the closest one
  if the phrase repeats), so it does not stretch over words of the neighbouring lines; words that are
  not recognized at the start or end keep the rhythm the line already had. If too little is recognized
  the line is left as it was and a notice says so. If a line is far off, drag it roughly into place
  first and then resync. It uses the separated vocals from the cache when available and takes a couple
  of seconds.
- **Edit text** (lyrics or line inspector): the whole lyrics as plain text; correct it and *Align
  again* to recompute the times (seconds, it reuses the cached recognition). Ideal after a transcription.
- **Split / join lines** (line inspector): the scissors on a word start a new line there, *Split at
  cursor* cuts before the first word after the playhead, *Join next* merges the line with the next one.
  The words keep their times.
- Lyric edits are **saved automatically** to the project's `.lrc` (per-word timing).

## Files
The file pickers browse the folders of the machine where the studio runs. When choosing a background they show **thumbnails** of images, GIFs and videos (a frame near the start); the buttons next to the path switch between thumbnails and a list (remembered by the browser). With
**⬆ Upload from this computer…** you copy a file into the project's `assets/` (useful if you open the
studio from another computer).

## Export
**🎬 Export** → the full song or just a part. A copy of the project is exported as it
is, so you can keep editing in the meantime. Progress appears at the top and in the dialog
(frames, speed, elapsed and remaining time).

Videos go to the project's **`exports/`** folder by default (`exports/<project>-<date>.mp4`); you
can type another path. **Exports** (top bar) lists the videos of `exports/` with a thumbnail,
duration, size and date: click one to **play** it in the browser, **download** it or **delete** it.
(The audio of the MP4 is FLAC: Chrome and Firefox play it; the download always works.)

## Editor
**✎ Editor** (transport bar) or **E** opens the preview over the whole window: tools on the left,
the inspector on the right (it can be hidden), a small timeline below (the selection's range and
keys are marked on it).

- **Zoom**: the wheel (at the pointer), the zoom tool (click / Alt+click / drag), the buttons, **0** =
  fit, **1** = actual pixels. The frame is requested at full quality when zoomed in.
- **Pan**: the hand tool (**H**), the middle button, Shift+wheel, or two fingers.
- **Rotate the view** (like turning the paper): the rotate tool (**R**, Shift = steps of 15°) or
  two fingers; the ° button resets it.
- **Touch**: two fingers pan, zoom and rotate at the same time.
- **Pen**: the status line shows the pointer type, the pen's pressure and tilt.
- The selection tool (**V**) has the same handles as the preview (move, scale, rotate a clip or a
  text).
- **Masks** of the selected clip ([layers.md](layers.md#masks)), text or lyrics segment
  ([texts.md](texts.md#masks)): shape (**S**), nodes (**N**), shapes (**A**: select, move, scale, rotate, group), line eraser (**X**), scissors (**K**), fill (**F**), brush
  (**B**) and eraser (**E**), with the brush's size, hardness, opacity and pressure options in the
  top bar.
- **Motion path** (**P**) of the selected clip or text: draw it, then drag its nodes ([layers.md](layers.md#motion-path)).
- **Esc** closes it.

## Right-click menus
The browser's own menu is replaced by the studio's (it stays only in text boxes, to copy and paste).
The menus never go off the screen.

| Where | Options |
|---|---|
| A segment on the timeline | **Split here** (the new segment starts at that point with the same settings; its keyframes are shared out so the animation goes on from the same value), rename, copy / paste settings (onto a segment of the same track), delete |
| A keyframe | Curve to the next key (each one drawn), delete |
| A text block / the Texts lane | Duplicate, delete / new text there |
| A marker | Go there, rename, delete |
| The ruler or the waveform | Move the cursor there, marker there |
| An entry of the track list or its name on the timeline | Show / hide its track, new segment (or text) at the cursor |
| A zone header | Fold / unfold |

## Layout
The dividers between the left panel, the preview, the inspector and the timeline can be dragged
to give more room to what you are working on (they light up on hover); double-click one to go back
to its default size. The sizes are remembered by the browser.

## Language
The interface language (English / Español) is chosen in the top bar and remembered by the browser.

## Shortcuts
| Key | Action |
|---|---|
| Space | Play / pause |
| Ctrl+Z / Ctrl+Y (or Ctrl+Shift+Z) | Undo / redo (also the arrows in the top bar; up to 32 steps) |
| ← / → | Previous / next frame (with Shift: ±1 s) |
| Home / End | Beginning / end |
| N | New segment at the cursor (selected track) |
| Del | Delete the selected segment or text |
| M | Marker at the cursor |
| S | Audio while scrubbing on / off |
| Esc | Leave word editing |
| Drag on the ruler | Scroll the timeline (a click without dragging moves the cursor) |
| Ctrl + wheel | Timeline zoom (keeps the point under the pointer; the buttons keep the center) |

## Preview
- **Audio while scrubbing** (speaker button next to the clock, or `S`): with the playback stopped,
  moving through time (dragging on the ruler or the waveform, ←/→, jumping to a line…) plays a short
  snippet of the song at that point. On by default; the choice is remembered by the browser.
- *Preview quality* (1/2 by default) renders directly at a lower resolution on the GPU; the image is
  always scaled to fill the preview area, keeping the video's aspect ratio.
- On the same machine it runs at 50–60 fps. From another machine it requests several frames ahead to
  compensate for network latency.
- When jumping to another point, particles are re-simulated for a few seconds so they look like
  they do in the export.
