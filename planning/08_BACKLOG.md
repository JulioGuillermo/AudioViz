# Backlog

What is agreed or proposed and not done yet (newest first). The older phase plans
(`07_ROADMAP_AND_MILESTONES.md`) describe the first version.

## Lyrics
- **Several lyrics tracks** (different voices: lead, backing vocals, a second singer): today there
  is one lyrics track (its segments share one file; the project holds one `/lyrics`). Lyrics as
  layers, each with its file, its style and its place; generating / aligning per voice (the stems
  can help: vocals only).

## Jobs
- **A queue of jobs** instead of refusing a second one: two lanes (the local GPU: export, images,
  depth, lyrics, stems; and ComfyUI), queued jobs listed with who started them, their state and
  configuration; cancel / remove from the queue; recent ones with their result.

## Engines and speed
- **Demucs, phase 2**: its encoders / decoders on the GPU (ggml) as its cross-transformer already
  is (4:26 → 2:51 for a 4:30 song; with all of it on the GPU maybe 40–60 s). Multiplying only the
  convolutions' products on the GPU was tried: slower (moving the im2col matrices costs more).
- **SYCL kernel cache**: the SYCL runtime's persistent cache crashes when read back (off: each
  start compiles the kernels again, ~10 s on the first image).
- **The Intel plugin on Windows** (a DLL can't take ggml's symbols from the .exe the same way).
- **A public repository of plugins** to download them from the studio (the code repository is
  private; e.g. Hugging Face).

## ComfyUI
- Simpler rows for a flow's input files when the node is a known loader (hide the media and the
  field: they follow from the node).

## Collaboration
- **History and branches**: how much history to keep (configurable), go back to a point, undo one
  change, start a branch from a change.

## Depth
- **Masks from depth** (things passing behind objects of the image, automatically).

## Distribution
- Review the licences of the dependencies and models (x264 is GPL; some depth models are
  non-commercial) before any paid offer; open source (AGPL) with donations and a paid cloud
  (hosted collaboration first, render / AI later, by usage) was the direction discussed.
