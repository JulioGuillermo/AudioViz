# Image generation: internals

`internal/imagegen` generates images with [stable-diffusion.cpp](https://github.com/leejet/stable-diffusion.cpp)
linked statically, on the same ggml as whisper.cpp (see [build.md](build.md)). The studio
(`internal/studio/images.go`, `web/images.js`) and the CLI (`cmd/audioviz/image.go`) use it.

| File | Role |
|---|---|
| `imagegen/models.go` | `Families`: the three models with their weight files (diffusion model, text encoder, VAE, optional vision encoder), default steps, CFG and sampler |
| `imagegen/sd.go` | cgo binding: context load/free, `generate_image`, the C callbacks (progress, preview, log) |
| `imagegen/generate.go` | `Generate`: download → load → sample, with progress, preview and cancel |
| `aimodel/model.go` | Resumable downloads to `~/.cache/audioviz/models` (shared with lyrics generation) |
| `progress/progress.go` | `Step` / `Status` / `Tracker`: the step-based progress the UI renders (shared with lyrics generation) |
| `ggml/vkshim.c` | The Vulkan loader bridge ggml-vulkan links against (shared) |

## Models

| ID | Diffusion | Text encoder | VAE | Refs | Defaults |
|---|---|---|---|---|---|
| `zimage` | Z-Image Turbo Q4_K (3.9 GB) | Qwen3 4B Q4_K_M (2.5 GB) | FLUX.1 `ae` (335 MB) | 0 | 8 steps, CFG 1 |
| `klein` | FLUX.2 klein 4B Q4_0 (2.5 GB) | **same** Qwen3 4B | FLUX.2 VAE (336 MB) | 4 | 4 steps, CFG 1, Euler |
| `qwen` | Qwen Image 2.1 Q4_K (4.2 GB) | Qwen3-VL 8B Q4_K_M (5.0 GB) + vision mmproj (752 MB, only with refs) | Qwen Image 2.1 VAE (676 MB) | 3 | 20 steps, CFG 6, Euler |

Z-Image's documentation uses Qwen3-4B-Instruct-2507 as its text encoder; the base Qwen3-4B
that FLUX.2 klein needs gives the same quality with Z-Image (checked on the same prompt and
seed), so one 2.5 GB file serves both. The files come from public Hugging Face repositories
(GGUF quantizations by leejet, unsloth and Qwen; VAEs from Comfy-Org, which mirrors them
without the gating of the original repositories). All are Apache-2.0.

## Binding (`sd.go`)

stable-diffusion.cpp reports through **global** callbacks (`sd_set_progress_callback`,
`sd_set_preview_callback`, `sd_set_log_callback`). As in `whisper.go`, the C side only stores
state and Go polls it; nothing calls from C into Go:

- progress: `av_step` / `av_steps`, read with `av_get_step`. The same callback reports the
  sampling steps and then the VAE decoding (in tiles, a different total): `Generate` tells them
  apart by comparing the total with the requested steps (sampling = 0–95 % of the step,
  decoding = the rest, detail "Decoding the image").
- preview: `PREVIEW_PROJ` (a linear projection of the latents, 1/8 of the size, nearly free) at
  every step. The callback copies the frame into a buffer under a spin lock and bumps a
  sequence number; `av_preview_get` copies it out when the number changed.
- log: the last error line is kept and becomes the Go error (`generate_image` only returns a
  bool). `AUDIOVIZ_DEBUG_SD=1` prints the whole log to stderr.

Because the callbacks are global, one generation runs at a time (`gen` mutex).

The context (`sd_ctx_t`, the loaded weights, several GB) is kept in `eng` between images and
replaced only when the model changes (the key includes whether the vision encoder is loaded).
The studio frees it after 10 minutes without use (`unloadAfter`); `Unload()` frees it on demand.

Context parameters: `diffusion_flash_attn = true`, threads = physical cores, everything else at
the library defaults (weights on the GPU backend; mmap off). Generation parameters: prompt,
size, steps, `txt_cfg`, seed, Euler when the family asks for it (otherwise the model's default
sampler and scheduler), and the reference images.

Cancel: `sd_cancel_generation(ctx, SD_CANCEL_ALL)` from `Generate` when the context is done;
the generation returns at the next step. `SD_CANCEL_RESET` is called before each generation.

## `Generate`

Steps (weights in the global progress): `models` 0.1 (skipped if everything is downloaded),
`load` 0.1 (skipped if already in memory), `generate` 0.8. Sizes are rounded to multiples of 32
(Qwen Image needs it; the others are fine with it) and limited to 256…2048 per side. A seed < 0
picks a random one (returned in the result, saved in the `.json`).

Reference images are decoded by the caller (PNG, JPEG, GIF, WebP), scaled down to at most
1024×1024 pixels of area and to multiples of 16 (CatmullRom), and passed as packed RGB.

## Studio

| Endpoint | |
|---|---|
| `GET /api/images` | models (with bytes missing, with and without refs), generated images (`images/*.png` + `.json`, newest first), project images usable as references (backgrounds, `assets/`) |
| `POST /api/images/generate` | `{model, prompt, refs[], width, height, steps, seed}` → job `image`. Saves `images/<date>-<first words>.png` and its `.json` (prompt, model, seed, steps, size, refs, device, elapsed) |
| `GET /api/images/preview` | the latest preview as JPEG; `job.gen.preview` carries its URL with a counter so the `<img>` reloads |
| `DELETE /api/images?name=` | deletes an image of `images/` (name only) and its `.json` |

Uploads for references reuse `POST /api/upload` (into `assets/`).

## Measured (Intel Lunar Lake iGPU, Vulkan, 1024×576)

| | Total | Sampling |
|---|---|---|
| Z-Image Turbo, 8 steps | 106–112 s | ~12 s/step |
| FLUX.2 klein, 4 steps, 1 reference | 110 s | ~26 s/step (the reference doubles the tokens) |

Model loading takes 2–3 s from the page cache. The text encoding takes ~2.5 s.
Whisper recognition with the shared ggml gives the same result as before (524 words on the
demo song, 377/380 aligned).
