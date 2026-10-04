# Generating synced lyrics (per-word LRC)

Everything runs **inside the binary**: no Python, no Whisper, nothing to install. Only the models
are downloaded once (≈ 660 MB) to `~/.cache/audioviz/models`, and after that it works offline.

## From the studio
**Lyrics** track → *Generate synced lyrics…* (or drag the `.txt` onto the window). Choose the source
(the lyrics as a `.txt`, or *Transcribe the audio* when you have no text — see below), the language and the mode. The progress view shows an overall bar, another one for the current step (with elapsed
and remaining time) and the list of steps. When it finishes, the lyrics are saved to the project's `lyrics.lrc` and
loaded automatically; if there already was one, the previous file is kept with the date in its name.

## From the terminal
```bash
./audioviz lyrics "song.flac" lyrics.txt -o lyrics.lrc            # accurate mode
./audioviz lyrics "song.flac" lyrics.txt -o lyrics.lrc --fast     # fast mode
./audioviz lyrics "song.flac" lyrics.txt -o lyrics.lrc --lang es  # another language
./audioviz lyrics "song.flac" -o lyrics.lrc --lang auto           # no text: transcribe the song
```

## Without the lyrics text: transcription
With no text, the words Whisper hears become the lyrics. They are split into lines at the pauses in
the voice, at Whisper's phrase breaks and after punctuation (a line never exceeds 14 words); `♪`,
`[Music]` and similar marks are dropped, and so are words "heard" in silence. The language can be
detected automatically (`auto`), although choosing it is more reliable.

Then correct it in the studio:
1. **Edit text** (in the generate dialog when it finishes, in the lyrics inspector or in the line inspector):
   the whole lyrics as text, one line per row. Fix misheard words, split or join lines, and press
   **Align again**: the corrected text is aligned like a `.txt` would be, reusing the cached separation
   and recognition, so it takes seconds. Timing tweaks made by hand are replaced.
2. Fine-tune timings with the usual tools: dragging lines and words, *Resync*, and **split / join**
   lines in the line inspector (scissors on a word, *Split at cursor*, *Join next*).

On the demo song the transcription got 97 % of the words right (word-level comparison with the real
lyrics) and 63 lines against 60 in the original.

## What it does (steps)
| Step | What it does | Where it runs |
|---|---|---|
| 1. Models | Downloads Whisper large-v3-turbo (574 MB) and Demucs htdemucs (84 MB) the first time. Resumable | Network |
| 2. Prepare audio | Resamples to 16 kHz (and to 44.1 kHz for Demucs) | CPU |
| 3. Separate vocals | **Demucs** isolates the vocals from the music. It is cached: the second time with the same song this step is skipped | CPU, ≈ the length of the song |
| 4. Recognize vocals | **Whisper** transcribes the vocals with the timestamp of each word | **GPU (Vulkan)** if available, otherwise CPU |
| 5. Align with lyrics | Matches the recognized words with **your lyrics** (tolerates misheard words), discards "hallucinations" in silences, re-recognizes doubtful segments using the lyrics as a hint and places any missing words in the regions with vocals | CPU, instant |

The final text is always **your lyrics** (not what Whisper hears); only the timings are computed.
The lyrics may come in Suno format: lines like `[Verse]`, `[Chorus]`, `[Soft piano...]`,
`[...]` tags inside a line and lines that are only `( ... )` are removed.

## Modes
| Mode | When |
|---|---|
| **Accurate** (default) | Separates the vocals before recognizing. Much more reliable with dense music (orchestra, guitars, choirs) |
| **Fast** | Recognizes on the full mix. Takes seconds, but may skip whole phrases if the music drowns out the vocals |

## Accuracy
Measured on *I didn't ask to be the devil* against a reference forced alignment: median error of
**0.10 s** per word, 83 % of words within 0.25 s and 97 % within 0.5 s.

## Fine-tuning the result
- If everything is uniformly early or late: `lyrics.offset` in the YAML.
- A specific word: in the studio, click the line (*Lyric lines* track) and adjust its start; or edit
  its `<mm:ss.xx>` tag in the `.lrc`.
- If a phrase is sung more times than it appears in the `.txt`, add it to the `.txt` and generate again
  (with the vocals already cached it takes a few seconds), or add it with *Edit text*.

## Without a GPU
It works the same on the CPU (recognition takes longer). To force the CPU for recognition: `AUDIOVIZ_NO_VULKAN=1` (the video renderer has its own `--renderer cpu`).
