# Command line

```
audioviz [project.yaml] [options]
```
If no project is given, `project.yaml` is used.

Subcommands:
- `audioviz studio [project.yaml] [--addr 127.0.0.1:8787] [--no-open] [--workspace ~/AudioViz]`: graphical interface (see [studio.md](studio.md)).
- `audioviz lyrics <audio> <lyrics.txt> [-o lyrics.lrc] [--lang en] [--fast]`: generate a per-word synced `.lrc` (see [lyrics/generate.md](lyrics/generate.md)).

| Option | Description |
|---|---|
| `-p, --preview <s>` | Render only N seconds |
| `--ss <s>` | Preview start second (with `-p`) |
| `--frame-at <s>` | Export a single frame as PNG |
| `--frame-out <path>` | PNG path (default `preview_frame.png`) |
| `-o, --output <path>` | Overrides `output.path` |
| `-a, --audio <path>` | Overrides `audio.path` |
| `-b, --bg <path>` | Overrides `background.path` |
| `-l, --lyrics <path>` | Shows that `.lrc`/`.srt` lyrics file (enables `lyrics`) |
| `--renderer auto\|gpu\|cpu` | Render backend (see [output](output.md)) |
| `--gpu-device <n>` | GPU to use |
| `--list-gpus` | List Vulkan GPUs |
| `--init <file.yaml>` | Create a YAML template |
| `-h, --help` | Help |

## Examples
```bash
audioviz project.yaml                        # full video
audioviz project.yaml -p 10 --ss 60          # 10 s starting at minute 1
audioviz project.yaml --frame-at 35          # frame at second 35
audioviz project.yaml --renderer cpu         # no GPU
```
Audio analysis is always done over the whole song, so a preview looks exactly
like that segment of the final video.
