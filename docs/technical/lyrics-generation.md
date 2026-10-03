# Lyrics generation: internals

This document covers how synced-lyrics generation and line re-sync are implemented. It is for
developers who maintain or port the code. For usage, see [../lyrics/generate.md](../lyrics/generate.md)
and [../lyrics/step-by-step.md](../lyrics/step-by-step.md).

Everything lives in `internal/lyricsgen`. It runs inside the single AudioViz binary: whisper.cpp
(with ggml) and demucs.cpp are linked statically through cgo, and only the model weights are
downloaded at runtime.

| File | Contents |
|---|---|
| `pipeline.go` | `Generate` (the five-step pipeline), `Status`/`Step` progress model, separated-vocals cache |
| `models.go` | `Model`, `WhisperModel`, `DemucsModel`, `CacheDir`, resumable download (`Ensure`) |
| `demucs.go`, `dmx.cpp`, `dmx.h` | Vocal separation: Go wrapper `SeparateVocals` and a threaded C++ wrapper around demucs.cpp |
| `whisper.go` | Whisper wrapper (cgo preamble with C helpers), `OpenWhisper`, `Transcribe`, `DTWBias` |
| `vkshim.c` | The four Vulkan symbols ggml-vulkan imports, resolved by `dlopen`ing libvulkan lazily |
| `resample.go` | `Resample` (Kaiser-windowed sinc) and `ToMono` |
| `align.go` | Lyrics cleaning, word similarity, voice-activity model, `alignNW`, `fillUnanchored`, `finish`, `LRC` |
| `resync.go` | `Resync` (re-align selected lines), `alignFit`, `placeEdges` |

Callers:

- **CLI**: `runLyrics` in `cmd/audioviz/main.go` (`audioviz lyrics <audio> <lyrics.txt> [-o] [--lang] [--fast]`).
  It decodes the audio with `audio.DecodeAudio(path, 1)`, builds the cache key from the
  *absolute* path, and prints one progress line per `Status` update.
- **Studio**: `hGenLyrics` (`POST /api/lyrics/generate`) and `hResync` (`POST /api/lyrics/resync`)
  in `internal/studio/server.go`, plus `audioKey`.

---

## 1. Pipeline overview

`Generate(ctx, Options, report func(Status)) (*Result, error)` in `pipeline.go`:

```
CleanLyrics(text) ──► lines
                         │
 1 models     WhisperModel (+ DemucsModel if separation is needed and the vocals are not cached)
 2 prepare    stereo@SR ─► Resample ─► mono 16 kHz (mix16)      [+ stereo 44.1 kHz for Demucs]
 3 separate   stereo44 ─► Demucs htdemucs ─► vocals stereo44 ─► mono 16 kHz (vocals16) ─► cache
              (fast mode: vocals16 = mix16; cache hit: step skipped)
 4 recognize  Whisper large-v3-turbo (Vulkan or CPU), DTW token timestamps ─► []Word
 5 align      dropSilent ─► alignNW ─► repair doubtful lines ─► fillUnanchored ─► finish ─► LRC
```

`Options`:

| Field | Meaning |
|---|---|
| `Stereo []float32` | Interleaved stereo PCM, normalized to [-1, 1) by the caller (`int / 2^(bits-1)`) |
| `SampleRate` | Sample rate of `Stereo` |
| `AudioKey` | Identifies the audio file for the vocals cache. Empty disables the cache. |
| `Lyrics` | Plain text. Suno format is accepted (see `CleanLyrics`). |
| `Lang` | Whisper language code. Defaults to `"en"`. |
| `Separate` | `true` = accurate mode (Demucs). `false` = fast mode (`--fast` / `"fast": true`). |

`Result` holds `Lines []LLine`, the `LRC` text, and the `Device` used for recognition.

### 1.1 Progress model (`Status` / `Step`)

The studio sends this structure to the UI as `job.gen` over SSE (`hub.send("job", …)`). The CLI
prints it as text.

```go
type Step struct {
    ID       string  // models | prepare | separate | recognize | align
    Label    string
    State    string  // pending, running, done, skipped, error
    Progress float64 // 0..1 within the step
    Detail   string  // free text ("Whisper large-v3-turbo · Intel(R) Graphics", "312 MB of 574 MB · 21.3 MB/s"...)
    Elapsed  float64 // s
    ETA      float64 // estimated remaining s, -1 = unknown
}
type Status struct {
    Steps    []Step
    Current  int     // index of the step last started
    Progress float64 // global 0..1
    Device   string  // GPU description or "CPU"; set once Whisper is loaded
    Elapsed  float64 // s since Generate started
}
```

The `tracker` type implements it with `start`, `progress` and `end`. Each of these takes the mutex
and calls `emit`.

- **Global progress** is a weighted average. The weights are `stepWeights` = models 0.05,
  prepare 0.03, separate 0.62, recognize 0.22, align 0.08, chosen to be roughly proportional to
  duration in accurate mode. **Skipped steps are removed from both numerator and denominator**, so
  in fast mode, or on a vocals-cache hit, the bar still runs from 0 to 100 %. Done steps count
  their full weight, and running steps count `weight × Progress`.
- **ETA** is computed only for the running step, with linear extrapolation:
  `ETA = Elapsed × (1 − p) / p`. It applies only when `p > 0.02` and `Elapsed > 1 s`. Otherwise
  `ETA = -1`, and the UI hides it. `end()` sets `ETA = 0`.
- **Throttling**: `progress()` emits at most every 150 ms. `start()` and `end()` always emit
  (`force`). `emit` sends a copy of `Status`, including a copy of `Steps`, so the callback may keep
  it.
- A skipped step goes straight from `pending` to `skipped` through `end()`, without `start()`.
  `Current` keeps pointing at the last started step.

The step's own progress comes from different sources:

| Step | Progress source |
|---|---|
| models | Bytes downloaded / bytes missing across all needed models (callback every 200 ms, includes MB/s) |
| prepare | None. It jumps from 0 to done. |
| separate | Average of the per-thread demucs progress floats, polled every 250 ms |
| recognize | whisper.cpp `progress_callback` (an int percentage), polled every 200 ms |
| align | Index of the doubtful section being re-recognized / number of sections |

In the studio, `hGenLyrics` also copies `st.Progress` into `job.Progress` and builds `job.Message`
as `Label · Detail` for the compact job bar. `GenSteps` in `web/dialogs.js` renders the overall bar
(`gen.progress`, `gen.elapsed`), the current step bar (`progress`, `elapsed`, `−eta` if `eta > 0`),
the list of steps with state icons, and `gen.device`.

### 1.2 Cancellation

- Studio: `startJob` creates a `context.WithCancel`, and `POST /api/job/cancel` (`hCancel`) calls it.
  Only one job (export or lyrics) can run at a time (HTTP 409 otherwise). A `context.Canceled` error
  turns the job status into `canceled`.
- `Generate` checks `ctx` after the models step and before each repair section.
- Download: the HTTP request is built with `NewRequestWithContext`, so cancelling aborts it. The
  `.part` file is kept, and the next run resumes it.
- Demucs: the Go loop (`SeparateVocals`) polls `ctx` every 250 ms and sets a C `int *cancel` flag.
  The demucs progress callback in `dmx.cpp` throws a private `Cancelled` exception when the flag is
  set. Cancellation therefore only takes effect at the next progress report from the model, which
  is frequent but not instant. All threads are joined, and `dmx_vocals` returns 2 ("cancelled").
- Whisper: the same pattern with a 200 ms poll. The flag is checked by both `abort_callback`
  (during decoding) and `encoder_begin_callback` (before each encoder run).
- Resampling (prepare) and alignment are not interruptible, but they are short.
- The CLI uses `context.Background()`. Ctrl-C simply kills the process, and a partially downloaded
  model is resumed next time.

---

## 2. Models and caches

### 2.1 Model files (`models.go`)

| Var | File | Size (bytes) | Source |
|---|---|---|---|
| `WhisperModel` | `ggml-large-v3-turbo-q5_0.bin` | 574 041 195 (~574 MB) | `huggingface.co/ggerganov/whisper.cpp` |
| `DemucsModel` | `ggml-model-htdemucs-4s-f16.bin` | 83 994 361 (~84 MB) | `huggingface.co/datasets/Retrobear/demucs.cpp` |

large-v3-turbo q5_0 was chosen because it is nearly as accurate as large-v3 and much faster: the
turbo decoder has 4 layers, and q5_0 quantization reduces size and memory bandwidth.

**Cache dir**: `CacheDir()` returns `$AUDIOVIZ_CACHE` if set. Otherwise it returns
`os.UserCacheDir()/audioviz`, which is `~/.cache/audioviz` on Linux and falls back to
`os.TempDir()/audioviz`. Models go to `<cache>/models/<File>`.

**Presence check**: `Present()` checks that the file exists **and that its size equals the
hard-coded `Size`**. There is no checksum. If the upstream file ever changes, `Size` must be updated,
or the download will fail with "incomplete download".

**Download** (`Ensure`):
1. Writes to `<file>.part`. If the file already exists, sends `Range: bytes=<have>-`.
2. A `206` response appends to the `.part` file. A `200` means the server ignored the range, so the
   `.part` file is truncated and the download restarts. Any other status is an error.
3. Copies the body in 1 MiB chunks and reports `(done, total, bytes/s)` at most every 200 ms.
4. Verifies `done == Size`, then renames `.part` to the final name.

The studio's `/api/state` exposes `models: {whisper, demucs}` (from `Present()`), so the UI can warn
about the first-time download.

### 2.2 Separated-vocals cache (`pipeline.go`)

Demucs is the expensive step: roughly the song's duration on CPU. Its output is cached, after
downmixing and resampling, as raw little-endian `float32` **mono at 16 kHz** with no header:

```
<cache>/vocals/<hex(sha1(key)[:10])>.f32          (20 hex chars)
key = AudioKey + "|" + SampleRate + "|" + len(Stereo)
AudioKey = "<path>|<size>|<mtime unix>"           (studio: audioKey(); CLI: same format with the absolute path)
```

- Size and mtime invalidate the entry when the file changes. Sample rate and sample count protect
  against a different decode of the same file.
- `loadF32` only checks that the length is a multiple of 4. A corrupt or truncated file would be
  accepted. Delete `<cache>/vocals` to reset the cache.
- Fast mode neither reads nor writes the cache. `Resync` reads it but never writes it.
- On a cache hit, Demucs is not needed, so its model is not downloaded, and the 44.1 kHz resample
  is skipped.

---

## 3. Audio preparation and resampling (`resample.go`)

`Resample(in, ch, srcRate, dstRate)` is a pure-Go polyphase windowed-sinc resampler:

| Parameter | Value |
|---|---|
| Taps | `rsTaps = 32` per side (64-tap kernel, k ∈ [-31, 32]) |
| Phase table | `rsPhases = 512` (+1) fractional positions, nearest phase (no interpolation between phases) |
| Window | Kaiser, `β = 8.6`, I₀ computed by a 30-term series (`besselI0`) |
| Cutoff | `min(1, dst/src) × 0.94` of Nyquist (anti-aliasing margin) |
| Normalization | Each phase row is normalized to unit DC gain |
| Parallelism | The output is split into `runtime.NumCPU()` contiguous ranges |

Samples outside the input are treated as zero. If `srcRate == dstRate`, it returns a copy.
`ToMono` averages L and R.

The pipeline uses it three times: input to 16 kHz (Whisper, voice-activity model, fast mode), input
to 44.1 kHz stereo (Demucs input), and Demucs output from 44.1 kHz to 16 kHz.

---

## 4. Vocal separation (`demucs.go`, `dmx.cpp`)

- Engine: [demucs.cpp](https://github.com/sevagh/demucs.cpp), a C++ port of Hybrid Transformer
  Demucs (htdemucs). It uses Eigen and runs **on the CPU only**.
- Model: htdemucs 4-source (drums, bass, other, vocals), f16 weights. Only the **vocals** stem
  (index 3) is kept.
- Input and output: interleaved stereo at 44.1 kHz.

`SeparateVocals(ctx, modelPath, stereo44, progress)`:
1. `dmx_load` loads the model (`demucscpp::load_demucs_model`).
2. Allocates C buffers for input and output, a `float progress[threads]` array, and an `int cancel`.
3. Runs `dmx_vocals` in a goroutine locked to an OS thread, and polls progress and `ctx` every
   250 ms. The progress average only counts the threads that `dmx_vocals` actually uses. The Go
   side mirrors its clamp: `min(NumCPU, n/(44100·10)+1)`.

`dmx_vocals` (C++) parallelizes demucs.cpp, which is single-threaded per call:
- `threads = clamp(requested, 1, n/(10 s)+1)`, so each thread gets at least about 10 s of audio.
- The song is cut into `threads` equal segments. Each segment is extended by `OVERLAP = 0.75 s`
  on each side, and each thread runs `demucs_inference` on its chunk with its own progress callback.
- The chunks are blended with linear cross-fade ramps of `2 × OVERLAP` samples over the shared
  regions, then normalized by the sum of the weights. The ramps hide edge artifacts, because the
  model has less context near a chunk boundary.
- Errors: the first `std::exception` message is reported (return 1). Cancellation returns 2.

The 44.1 kHz stereo vocals are then downmixed and resampled to 16 kHz mono.

**Fast mode** (`Separate = false`) skips all of this and feeds the 16 kHz mono mix to Whisper and to
the voice-activity model. It is much faster, but dense arrangements can mask whole phrases, and the
energy-based voice detection then measures the music instead of the voice.

---

## 5. Recognition (`whisper.go`, `vkshim.c`)

### 5.1 whisper.cpp and backends

whisper.cpp is linked from static libs: `libwhisper.a`, `libggml.a`, `libggml-vulkan.a`,
`libggml-cpu.a`, `libggml-base.a`, plus `-lm -lpthread -ldl`. ggml registers the Vulkan backend and
the CPU backend. With `use_gpu = true`, whisper picks the first GPU device and otherwise runs on the
CPU.

`OpenWhisper(path, aheads, gpu)`:
- Silences whisper and ggml logging once (`wh_silence`).
- `wh_open` sets `use_gpu`, **`flash_attn = false`** (DTW needs the materialized cross-attention
  weights, which flash attention does not produce), `dtw_token_timestamps = true`, and
  `dtw_aheads_preset = 14`. The value 14 is `aheadsLargeV3Turbo` in `models.go`, which must equal
  `WHISPER_AHEADS_LARGE_V3_TURBO` in `whisper.h`. Check this if whisper.cpp is upgraded, because
  the enum is not imported, only the number. These alignment heads are the cross-attention heads
  that correlate with timing for this exact model.
- Device detection (`wh_device`) returns the description of the first ggml backend device whose
  type is neither `CPU` nor `ACCEL`, for example `Intel(R) Graphics (LNL)`, or `"CPU"`. ACCEL is
  excluded because that device type covers CPU-side accelerators (such as BLAS), which are not a
  GPU.

### 5.2 Vulkan without a link-time dependency (`vkshim.c`)

`libggml-vulkan.a` imports only four Vulkan symbols directly. It loads everything else through
`vkGetInstanceProcAddr` and `vkGetDeviceProcAddr`. `vkshim.c` defines exactly those four:

| Symbol | Behaviour |
|---|---|
| `vkGetInstanceProcAddr` | On first call (`pthread_once`), `dlopen("libvulkan.so.1")` or `libvulkan.so` with `RTLD_LOCAL`, then forwards to the real loader. Without libvulkan, it returns stubs for the global functions: `vkCreateInstance` returns `VK_ERROR_INITIALIZATION_FAILED`, `vkEnumerateInstanceVersion` returns 1.0, and the extension and layer enumerations return 0 items. Anything else returns NULL. |
| `vkGetDeviceProcAddr`, `vkCmdCopyBuffer`, `vkGetPhysicalDeviceFeatures2` | Resolved with `dlsym` on first use. They are only reachable after an instance exists, which means libvulkan was loaded. |

As a result, the binary **starts without libvulkan**. When libvulkan is missing, instance creation
fails cleanly, ggml's Vulkan backend reports no devices, and whisper runs on the CPU backend.

`AUDIOVIZ_NO_VULKAN=1` (any non-empty value other than `0`) skips the `dlopen` and simulates a
missing libvulkan, which forces CPU recognition. This only affects Whisper. The renderer's own
Vulkan loader in `internal/gpu/vk.c` is separate and ignores this variable.

If ggml-vulkan is upgraded and starts importing another `vk*` symbol, the link fails with an
undefined reference. Add a forwarding function to `vkshim.c` in the same style.

### 5.3 Transcription and word timestamps

`Transcribe(ctx, pcm16k, offset, lang, prompt, progress)`:
- Returns nothing for inputs shorter than 1600 samples (0.1 s).
- Serialized by a mutex, because a `whisper_context` holds a single default state.
- Parameters: greedy sampling, `n_threads = NumCPU`, `translate = false`,
  `token_timestamps = true`, `suppress_blank = true`, and `initial_prompt = prompt` when it is not
  empty. All `print_*` options are off.
- Times from whisper are in centiseconds (`/100`). `offset` is added so that windowed calls return
  absolute song times.

**Per-token start**:
```
at = t_dtw/100 − DTWBias              (DTWBias = 0.40 s)
if t_dtw < 0 || t_dtw/100 > segment_t1 + 0.5:   at = t0/100   (fallback)
```
- `DTWBias` (a package `var`, tunable in tests) exists because the DTW timestamps were measured to
  be systematically **about 0.44 s late** against a reference forced alignment. Subtracting 0.40 s
  (slightly conservative) removed most of that bias.
- DTW is considered invalid when there is no value (`-1`), or when the value falls well past the
  end of its segment. The second case happens at the end of the audio. The fallback is the token's
  `t0`, whisper's own timestamp-token estimate, which is coarser but never absurd.

**Words from tokens**: within each segment, tokens with `id >= eot` (special and timestamp tokens)
and empty texts are skipped. A token whose text starts with a space, or the very first token,
opens a new `Word` that takes its start from that token. Other tokens are appended to the previous
word. `Prob` is a running pairwise average (`(prev + p)/2`), not a true mean. It is only used for
debugging output. `T0` and `DTW` keep the raw values for debugging.

**Word end**: the initial `End` is the segment's `t1`. Afterwards, each word's `End` is set to **the
next recognized word's `Start`**, if that start is later. So `Word.End` is not a measured word end.
It only bounds the time before Whisper heard something else. The alignment stage trims it with the
voice energy (section 6.6).

---

## 6. Alignment (`align.go`, rest of `Generate`)

The output text is always the user's lyrics. Whisper only provides timing anchors.

### 6.1 Lyrics cleaning (`CleanLyrics`)

Line by line, after removing `\r`:
- Drops empty lines, lines that are entirely `[...]` (Suno section tags such as `[Verse]` or
  `[Soft piano intro]`), and lines that are entirely `(...)`.
- Removes `[...]` tags inside a line. Parentheses inside a line are kept, because they are usually
  sung backing lines.
- Collapses whitespace. If a line becomes empty, it is dropped.

`lyricWords` splits the lines into `LWord{Text, Line, n: norm(Text)}`.

### 6.2 Normalization and similarity

- `norm(w)`: maps typographic apostrophes (’ ‘) to `'`, lowercases, keeps letters, digits and `'`,
  and trims leading and trailing `'`. Unicode letters are kept, so accented languages work.
- `similarity(a, b) = 1 − Levenshtein(a, b) / max(len(a), len(b))` on runes. It returns 1 for equal
  strings and 0 if either string is empty.

### 6.3 Global alignment (`alignNW`)

This is a Needleman-Wunsch alignment of all lyric words (rows) against all recognized words
(columns).

| Event | Score |
|---|---|
| Match, similarity ≥ 0.8 | `3 × s` (2.4–3) |
| Match, 0.5 ≤ s < 0.8 | `+1` (misheard but plausible) |
| Match, s < 0.5 | `−2` |
| Gap (skip a lyric word or a recognized word) | `−1` |

Borders are `dp[i][0] = −i`, `dp[0][j] = −j` (global).

**Tie-break**: `left` (skip a recognized word) wins over `diag`, and `diag` wins over `up`. The
backtrace runs from the end, so preferring to skip recognized words at the end pushes matches as
early as possible. When a lyric line is repeated in the audio (an echo, a hallucinated repeat, or a
chorus sung more times than written), the line is matched to its **earliest** occurrence, and the
extra recognized words are skipped, instead of to the last one.

On the backtrace, a diagonal step anchors the lyric word only if `similarity ≥ 0.5`. Anchoring
copies the recognized `Start` and `End` and sets `anchored = true`. The function returns the number
of anchored words.

### 6.4 Hallucination filter and voice activity

Whisper is known to "hallucinate" lyrics during instrumentals, often by repeating lines.
`dropSilent` removes recognized words with little voice right after their start:

```
keep w  iff  voicedFrac(w.Start − 0.1, w.Start + 0.6) ≥ 0.15
```

It runs before alignment, both on the full pass and on every windowed pass.

The voice-activity model (`newVoice`) is computed from the signal fed to Whisper, which is the
separated vocals, or the mix in fast mode:
- RMS over 20 ms frames (320 samples at 16 kHz).
- Threshold = `max(0.06 × p95(RMS), 1e-5)`, where p95 is the 95th percentile of all frames. Using a
  relative threshold makes it independent of the loudness of the master and of the project volume.
- `on[i] = rms[i] > thr`.

The model provides `voicedSpans(a, b)` (runs of voiced frames), `voicedFrac(a, b)`,
`trim(a, b)` (the first to last voiced instant ±0.25 s), and `decayEnd` (section 6.6).

### 6.5 Re-recognizing doubtful lines and filling the gaps

After the first `alignNW`, the rest of `Generate` does the following:

1. `lineCoverage` computes the fraction of anchored words per line. Runs of consecutive lines with
   coverage `< 0.5` form "doubtful groups".
2. `fillUnanchored` gives every unanchored word a provisional time, which is needed to delimit the
   windows in the next step.
3. For each group:
   - Window: from `prev.Start + min(prev duration, 1.5 s)` (the word before the group) to the start
     of the word after the group. The bounds are 0 and the song length at the edges. The window is
     then `trim`med to voice and skipped if it is shorter than 0.6 s.
   - Whisper runs again on that window, **with the group's lyric text as `initial_prompt`**. Then
     `dropSilent` runs, and `alignNW` runs on a copy of the group's words with their anchors
     cleared. Only the words that get anchored in that pass overwrite the originals.
4. Every still-unanchored word is reset, and `fillUnanchored` runs again using only the real
   anchors.
5. `finish`, then `LRC`.

The final `align` detail reports the anchored words / total, the number of sections reviewed, and
the number of words dropped by `dropSilent`.

**`fillUnanchored(lw, v, total)`** handles each maximal run `[i, j)` of unanchored words (`cnt`
words) and picks an interval `[a, b]`:

| Case | Interval |
|---|---|
| All words unanchored | `[0, total]` |
| Run at the start | `b = next anchor start`, `a = max(0, b − 0.6·cnt)` |
| Run at the end | `a = prev.Start + min(prev duration, 1 s)`, `b = min(total, a + 0.6·cnt)` |
| Between anchors | `a = prev.Start + min(prev duration, 1 s)`, `b = next anchor start` |

The interval is widened to at least `0.05 s × cnt`. The words are then laid out **over the voiced
spans only**, concatenated as if the gaps did not exist, so that words do not land in
instrumentals. If there is less than `0.15 s × cnt` of voiced time, the whole interval is used
instead. Each word gets a share proportional to `runes + 1`, since longer words take longer to
sing. Each word's start and end are mapped from that cumulative share back to real time.

The `0.6 s/word` at the edges approximates a typical sung word rate, and the `1 s` cap stops the
run from starting after a long held note.

### 6.6 Final timing (`finish`)

1. **Monotonic starts**: `Start[i] ≥ Start[i−1] + 0.02`.
2. **Ends**:
   - A word that is not the last in its line ends at `min(next.Start, Start + 2 s)`.
   - The **last word of a line** ends at `decayEnd`:
     ```
     limit = min(next word start (or total), Start + 3 s)
     if anchored and End > Start + 0.1:  limit = min(limit, End)   // End = next recognized word's start
     e   = v.decayEnd(Start, peakWin = 0.5, limit)
     End = max(e, min(Start + 0.25, next))
     ```
     `decayEnd` finds the word's peak RMS within its first 0.5 s, sets
     `thr = max(0.25 × peak, global voice thr)` (−12 dB from its own peak), and scans forward from
     the peak. The word ends where the level stays below `thr` for 5 frames (**0.1 s**). The
     returned time is the start of that quiet run. If the level never falls, the result is `limit`.
     The minimum length is 0.25 s, unless the next word starts sooner.
3. The words are grouped back into `LLine`s by `Line`.

**Why decayEnd.** The previous approach ended the word at the first 0.3 s of silence under the
global voice threshold. That threshold is very low (6 % of p95) by design, because it must detect
quiet singing. Reverb tails and backing vocals in the separated stem stay above it long after the
word is over, so last words routinely reached the 3 s cap. A threshold relative to the word's own
peak follows the actual note, including held notes, which stay near their peak. Capping at the next
recognized word adds a second, independent bound: whatever Whisper heard next was not part of this
word.

The end of the last word matters because it is the line's end in the LRC file and in the studio
(how long the line stays highlighted).

### 6.7 LRC writer (`LRC`)

```
[re:audioviz]
[00:12.34]<00:12.34>First <00:12.80>words <00:13.10>here <00:14.02>
```

Each line has a line tag (first word's start), a `<mm:ss.xx>` tag before each word, and a
**trailing end tag** with the last word's `End`. That end tag is how the line end survives the
round trip. Times are clamped at 0, and the format is `%02d:%05.2f`, so minutes are not limited to
two digits.

### 6.8 Debugging: `AUDIOVIZ_DEBUG_REC`

If set to a file path:
- `Generate` **creates or truncates** the file and writes one line per recognized word of the full
  pass, *before* `dropSilent`: `start  t0  dtw  prob  text`, tab-separated, in seconds. This shows
  the raw DTW vs t0 timings and the bias correction.
- `Resync` **appends** a header per group,
  `lines A-B window LO-HI anchored N/M`, followed by `  start  end  text` for the recognized words of
  that window, after `dropSilent`.

---

## 7. Re-sync of selected lines (`resync.go`)

In the studio, the user can select one or more lines in the *Lyric lines* track and click
re-sync. The client sends all its current, possibly hand-edited lines and the selected indices.
`hResync` passes them to `Resync`, and the client replaces only the targets with the returned
lines. The goal is to fix a few bad lines without regenerating the entire lyric, and without
touching lines the user already corrected.

`POST /api/lyrics/resync`:
```json
{ "lines": [{ "words": [{ "text": "...", "start": 1.2, "end": 1.5 }, ...] }, ...],
  "targets": [12, 13], "lang": "en" }
→ { "lines": [...same shape, with start/end per line...], "source": "separated vocals (cache)" }
```
The request runs synchronously on `r.Context()`, so it is not a job and has no progress reporting.
If the Whisper model is missing, it is downloaded silently inside the request.

### 7.1 Algorithm

1. Audio: the cached separated vocals if present (same key as `Generate`). Otherwise the full mix
   at 16 kHz. `source` tells the client which one was used.
2. The targets are sorted and grouped into runs of **consecutive** line indices. Each group is
   processed independently.
3. **Window**: `[start − 5 s, end + 5 s]`, where `start` is the group's first word start and `end`
   is its last word end, clamped to the song. A neighbouring line restricts the window only if it
   does **not overlap** the target: the previous line's end when `≤ start`, and the next line's
   start when `≥ end`. When the user has dragged a line on top of its neighbour, the neighbour
   still lets the search reach the real audio. A window shorter than 0.4 s skips the group.
4. Whisper runs on the window **without a prompt**, followed by `dropSilent`.
5. `alignFit(sub, rec, near = (start+end)/2)` (section 7.2).
6. **Minimum anchors**: if `anchored < min(len, max(2, ceil(0.4 × len)))`, the group is left
   untouched. That is at least 2 words, or 40 % of them, or all of them for a 1-word line. The UI
   detects unchanged lines (same word starts within 1e-6) and shows "Could not recognize line N
   clearly; it was left as it was. Drag it closer to where it is sung and try again."
7. `placeEdges` (section 7.3), then `fillUnanchored(sub, v, hi)` for the internal gaps.
8. `finish(sub, v, limit)`, with `limit = hi`, lowered to the next line's start if that start is
   after the group's last word. The last word may therefore fade naturally, but never into the next
   line. When the next line overlaps, it is ignored.

**Why no prompt.** whisper.cpp feeds `initial_prompt` as previous context, meaning text that has
*already been said*. Two variants were tried and both failed:
- With the **target text** as the prompt, Whisper treated the line as already transcribed and often
  skipped it in the window, which left nothing to anchor.
- With the **previous line** as the prompt (whose audio is often inside the ±5 s window), Whisper
  skipped that audio as already said, and its duration was lumped into the timestamp of the first
  word it did emit. The target's first word then started too early.

Without a prompt, everything in the window is transcribed, including words of the neighbouring
lines, and `alignFit` picks the right run.

### 7.2 `alignFit`: semi-global alignment

This is the same scoring as `alignNW` (≥0.8 → `3s`, ≥0.5 → 1, else −2, gap −1), but every lyric word
must be placed in **one compact run** of recognized words:
- `dp[0][j] = 0`: recognized words *before* the run are free.
- The end is chosen as `argmax_j dp[n][j]`: recognized words *after* the run are free.
- Skipping a recognized word *inside* the run costs a normal gap (−1).
- Tie-break between equally good ends: the end whose recognized word start is **closest to
  `near`**, the line's current centre. The user usually drags the line roughly into place, so the
  nearest of two identical repeats is the intended one.
- Inner ties prefer `diag`, then `up`, then `left`.

**Why.** With global NW in a window full of neighbouring words, a target line could anchor its
common words ("I", "the", "my") to matching words of the previous or next line and stretch over
several seconds. Global alignment pays the same per-word gap whether the skipped words are outside
or inside the run, so scattered matches cost nothing extra. The semi-global form makes the outside
free and the inside costly, which forces a compact match.

### 7.3 `placeEdges`

Unanchored words *before the first* or *after the last* anchored word have no anchor on one side.
`fillUnanchored` would spread them toward the window edge, which is up to 5 s away. `placeEdges`
instead places them using the line's **existing rhythm**:

```
step(q) = clamp(orig[q+1].Start − orig[q].Start, 0.12, 0.8)
leading : Start[k] = max(lo,        Start[k+1] − step(k)),  End[k] = Start[k+1]
trailing: Start[k] = min(hi − 0.05, Start[k−1] + step(k−1))
```

These words are then marked `anchored`, so that `fillUnanchored` only fills the internal gaps
between recognized words. The clamp keeps a previously broken line, with zero or huge gaps, from
propagating its own error.

### 7.4 Evaluation

Measured on the 62 lines of the demo song (*I didn't ask to be the devil*), with the generated
lyric as reference. Each line was resynced on its own in three conditions: in place, shifted
+1.5 s, and shifted −1.5 s. The table shows the mean per-word start error against the reference.

| Condition | Mean error per word | Lines off by > 0.3 s | Mean line-duration change |
|---|---|---|---|
| **Before** the fixes, in place | 0.17 s | 7 / 62 | +0.32 s |
| Before, shifted +1.5 s | 2.05 s | 39 / 62 | +3.69 s |
| Before, shifted −1.5 s | 0.47 s | 26 / 62 | +1.70 s |
| **After**, in place | 0.17 s | 5 / 62 | +0.25 s |
| After, shifted +1.5 s | 0.18 s | 8 / 62 | +0.17 s |
| After, shifted −1.5 s | 0.29 s | 9 / 62 | +0.31 s |

"The fixes" are: no prompt, `alignFit` instead of `alignNW`, `placeEdges`, the overlap-aware
window, and the `decayEnd`-based last-word end.

**Known limits**
- Whisper can lengthen a word that precedes a held note. For example, "their" before a 3 s "lies"
  gets part of the held note's onset, so the next word starts late.
- Repeated ad-libs or hooks ("oh", "yeah", a repeated phrase) can match a different occurrence when
  the line is far from where it is sung. The `near` tie-break only helps when the scores are equal.
- On the full mix (no cached vocals), the voice-activity model and `dropSilent` are much less
  reliable.

**Reproducing the evaluation.** Open the project in the studio with the generated `lyrics.lrc`,
which is the reference, and with the vocals cached. Parse the LRC into the `lines` JSON shape. For
each line index `i` and each shift in {0, +1.5, −1.5}, build a copy of all lines in which only line
`i`'s word starts and ends are shifted. Send it to `POST /api/lyrics/resync` with
`targets: [i]`, and compare the returned `lines[i]` word starts with the reference: the mean
absolute error, whether the line's mean error exceeds 0.3 s, and the line's duration (last end −
first start) against the reference. Average over the lines. Set `AUDIOVIZ_DEBUG_REC` to inspect
the windows and recognized words of the outliers.

---

## 8. Building the native libraries (`scripts/build-ml.sh`)

`build.sh` calls `scripts/build-ml.sh`, which builds static libraries into `third_party/build/`
(`lib/*.a` and `include/*.h`) only if they are not there yet. Delete `third_party/build/lib/libwhisper.a`
or `libdemucs.a` to force a rebuild.

**whisper.cpp**, pinned at `WHISPER_REV=60c0be6ac8fa71b1a2ae2dd938a31a34a508e774` (github.com/ggml-org/whisper.cpp):
```
cmake -DCMAKE_BUILD_TYPE=Release -DBUILD_SHARED_LIBS=OFF -DGGML_OPENMP=OFF -DGGML_NATIVE=OFF \
      -DGGML_AVX=ON -DGGML_AVX2=ON -DGGML_FMA=ON -DGGML_F16C=ON -DGGML_VULKAN=ON \
      -DWHISPER_BUILD_TESTS=OFF -DWHISPER_BUILD_EXAMPLES=OFF -DWHISPER_BUILD_SERVER=OFF
cmake --build ... --target whisper
```
- `GGML_NATIVE=OFF` with explicit AVX, AVX2, FMA and F16C targets the x86-64-v3 level instead of
  the build machine's CPU. The binary runs on any x86-64 CPU from roughly the last 10 years, and
  not on older ones.
- `GGML_OPENMP=OFF` avoids a dependency on libgomp, so ggml uses its own thread pool.
- `GGML_VULKAN=ON` compiles the Vulkan compute shaders to SPIR-V at build time with **`glslc`**
  and embeds them in `libggml-vulkan.a`. It also needs the Vulkan headers. Neither `glslc` nor the
  headers are needed at runtime.
- The script copies `libwhisper.a`, `libggml.a`, `libggml-base.a`, `libggml-cpu.a`,
  `libggml-vulkan.a`, `whisper.h` and the `ggml/include/*.h` headers.

**demucs.cpp**, pinned at `DEMUCS_REV=f1206e9adeea103aef4a636b9e62297cf1f8e34e` (github.com/sevagh/demucs.cpp),
with the `vendor/eigen` submodule:
```
g++ -O3 -march=x86-64-v3 -ffast-math -fno-finite-math-only -DNDEBUG -std=c++17 -fPIC -w -c src/*.cpp   (in parallel)
ar rcs libdemucs.a *.o
```
`-fno-finite-math-only` keeps most of `-ffast-math`'s speed but preserves inf and NaN semantics,
so that checks such as `isinf` and `isnan` and `-inf` masking are not optimized away. `dmx.cpp` itself is compiled by cgo with the flags in `demucs.go`
(`-std=c++17 -O3 -march=x86-64-v3 -DNDEBUG`) and the same include paths.

**Linking**: `demucs.go` links `libdemucs.a -l:libstdc++.a`, and `build.sh` passes
`-extldflags '-static-libstdc++ -static-libgcc'`. libstdc++ and libgcc are therefore static, and
the binary depends only on libc. libvulkan is loaded with `dlopen` (section 5.2), and `-ldl` is
part of libc on current glibc. `vkshim.c` is an ordinary cgo C file in the package and needs
`<vulkan/vulkan.h>` from the system's Vulkan headers.

**Porting notes**
- Other architectures (arm64): change `-march=x86-64-v3` in both `build-ml.sh` and the cgo
  `CXXFLAGS` in `demucs.go`, and replace the `GGML_AVX*` flags.
- macOS: ggml-vulkan would need MoltenVK, and the `vkshim.c` library names are Linux-only. The
  natural backend there is Metal (`GGML_METAL`), which requires changing the cgo `LDFLAGS` in
  `whisper.go`.
- Upgrading whisper.cpp: check the value of `WHISPER_AHEADS_LARGE_V3_TURBO`, the set of `vk*`
  symbols imported by ggml-vulkan (`nm -u libggml-vulkan.a | grep ' vk'`), the static lib paths
  copied by the script, and re-measure `DTWBias`.
