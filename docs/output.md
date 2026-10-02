# Output

```yaml
output:
  path: "output.mp4"
  width: 0
  height: 0
  fps: 60
  renderer: "auto"
  gpu_device: -1
  preset: "fast"
  crf: 20
```

| Field | Default | Description |
|---|---|---|
| `path` | output.mp4 | MP4 file (H.264 + FLAC, `moov` at the start) |
| `width`, `height` | 0 | Resolution; 0 = the background's. Rounded to an even number |
| `fps` | 60 | Frames per second |
| `renderer` | auto | `auto`: Vulkan GPU, falling back to CPU · `gpu`: force GPU · `cpu`: force CPU |
| `gpu_device` | -1 | GPU index (`audioviz --list-gpus`), -1 = the best one (discrete > integrated) |
| `preset` | fast | x264 preset: `ultrafast`, `superfast`, `veryfast`, `faster`, `fast`, `medium`, `slow`, `slower`, `veryslow`. Slower = smaller file at the same quality |
| `crf` | 18 | Quality: 18 very high, 23 standard. Each +6 ≈ half the size |

## Performance
The GPU draws each frame in 1–2 ms. The rest of the time is H.264 compression, which runs on the CPU
(x264 using all cores). To go faster use `preset: veryfast`/`superfast`; for smaller
files raise `crf` or use `medium`/`slow`.

## Compatibility
The audio is FLAC inside the MP4. It plays in mpv, VLC, Chrome, Firefox, Android, ffmpeg and
YouTube (uploads), but not in Apple's QuickTime/Safari.
