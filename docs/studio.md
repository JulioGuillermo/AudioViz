# AudioViz Studio (graphical interface)

```bash
./audioviz studio                 # opens the project.yaml in the current folder (if any)
./audioviz studio path/to/project   # opens that project (folder or .yaml)
./audioviz studio --addr 0.0.0.0:8787   # reachable from another machine on the network
./audioviz studio --no-open       # don't open the browser
```
The studio ships **inside the same binary**: it starts a local server and opens the browser at
`http://localhost:8787`. Nothing needs to be installed. The preview is rendered by the same GPU and the
same engine as the export, so what you see is what gets exported.

> `--addr 0.0.0.0:...` allows access from the network, and anyone on it could browse your folders.
> Only use it on trusted networks.

## Starting a new project
1. **New** → choose the folder and the name. This creates `name/project.yaml` and `name/assets/`.
2. **Getting started** appears over the preview:
   - **Choose audio…**: the song. It is analyzed right away and the timeline becomes as long
     as the song (without audio it lasts 30 s, so you can start building the visuals).
   - **Choose background…**: image, GIF or video. The video resolution will be the background's (you can
     set a different one in **Output**). Until you pick one it is 1080×1920 with a black background.
   - **Lyrics** (optional): a `.lrc`/`.srt`, or *Generate from .txt…*.
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
| Top | Project name, save status, job progress (export / generate lyrics), **New**, **Open**, **Export** |
| Left | Track list: Audio, Output, Background, each background effect, Visualizer, Particles and Lyrics. Struck through if they are off at the current instant |
| Center | **Preview** and transport (play, frame by frame, preview quality) |
| Right | **Inspector**: all the parameters of the selected segment |
| Bottom | **Timeline**: ruler with markers, waveform with the bass/mid/treble curves, lyric lines and one track per effect |

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
- **Transition**: *Cut* or *Fade* with its duration:
  - numbers and colors are interpolated (e.g. the gradient gradually changes from red to blue);
  - turning an effect on/off fades;
  - a change of visualizer type or bar count fades out and back in;
  - a background change cross-fades;
  - particles from the previous segment stop spawning and fade out while the new ones spawn.
- **Delete**: the *Delete segment* button or the **Del** key (the first one cannot be deleted).
- **Name**: give segments names ("Chorus", "Bridge") to see them on the timeline.

**Lyrics** work the same way: a lyrics segment changes the style (font, position, colors,
animations…). Each line uses the layout of the segment in which it starts being sung, so it doesn't
move around mid-phrase.

## Markers
The **⚑ Marker** button or the **M** key at the cursor. Click a marker to jump to it, double-click to
rename it, right-click to delete it. They serve as references for placing segments.

## Lyrics
- Import a `.lrc` / `.srt` in the **Lyrics** track → *File*.
- **Generate synced lyrics…** (in the lyrics inspector, or by dragging in the `.txt`): choose the
  `.txt` (Suno format works), the language and the mode (accurate / fast). It is built into the binary
  (Demucs + Whisper on the GPU); the progress shows each step with its time. See [generate.md](lyrics/generate.md).
- **Fine-tune timings on the timeline** ("Lyric lines" track):
  - drag a line to move it; **Shift+click** selects a range of lines that then move together;
  - with a single line selected, its words appear as blocks: drag a word to move it, or its
    left edge to change only its start;
  - the inspector shows each word with its start and end, the crosshair button sets a start at the
    cursor and ±0.05 s buttons shift the selected lines.
- **Resync**: with one or more lines selected, *Resync line(s)* recognizes that part of the song again
  (around the lines' current position, ±5 s, without crossing the neighbouring lines unless they overlap) and re-aligns the
  words. If a line is far off, drag it roughly into place first and then resync. It uses the separated
  vocals from the cache when available and takes a couple of seconds.
- Lyric edits are **saved automatically** to the project's `.lrc` (per-word timing).

## Files
The file pickers browse the folders of the machine where the studio runs. With
**⬆ Upload from this computer…** you copy a file into the project's `assets/` (useful if you open the
studio from another computer).

## Export
**🎬 Export** → the full song or just a part. A copy of the project is exported as it
is, so you can keep editing in the meantime. Progress appears at the top.

## Language
The interface language (English / Español) is chosen in the top bar and remembered by the browser.

## Shortcuts
| Key | Action |
|---|---|
| Space | Play / pause |
| ← / → | Previous / next frame (with Shift: ±1 s) |
| Home / End | Beginning / end |
| N | New segment at the cursor (selected track) |
| Del | Delete the selected segment |
| M | Marker at the cursor |
| Ctrl + wheel | Timeline zoom |

## Preview
- *Preview quality* (1/2 by default) renders directly at a lower resolution on the GPU.
- On the same machine it runs at 50–60 fps. From another machine it requests several frames ahead to
  compensate for network latency.
- When jumping to another point, particles are re-simulated for a few seconds so they look like
  they do in the export.
