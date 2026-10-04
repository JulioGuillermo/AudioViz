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

- phase: taken from the log messages, since `generate_image` runs everything in one call:
  `EDIT mode` → encoding the reference images (VAE), `parse '…'` → text encoding,
  `generating image:` → sampling, `decoding … latents` → VAE decoding. The weights are loaded
  **lazily** inside each phase (`loading N/M tensors from …` … `loading tensors completed`).
- progress: the same callback reports the sampling steps, the VAE tiles **and** the tensors
  being loaded, so the C side stores them apart depending on whether a load is in progress
  (`av_get_state`). Each phase is a step in the UI (Prepare the model, Encode the reference
  images, Read the prompt, Generate the image, Decode the image), with "Loading the weights to
  the GPU · n %" while its weights load. (The first version took any total other than the
  sampling steps for the decoding, so the tensor loading showed up as "Decoding the image".)
- preview: `PREVIEW_PROJ` (a linear projection of the latents, 1/8 of the size, nearly free) at
  every step. The callback copies the frame into a buffer under a spin lock and bumps a
  sequence number; `av_preview_get` copies it out when the number changed.
- log: the last error line is kept and becomes the Go error (`generate_image` only returns a
  bool). `AUDIOVIZ_DEBUG_SD=1` prints the whole log to stderr.

Because the callbacks are global, one generation runs at a time (`gen` mutex).

The context (`sd_ctx_t`, the loaded weights, several GB) is kept in `eng` between images and
replaced only when the model changes (the key includes whether the vision encoder is loaded).
The studio frees it after 10 minutes without use (`unloadAfter`); `Unload()` frees it on demand.

Context parameters: threads = physical cores, flash attention in the diffusion model as the
user chooses (on by default; `AUDIOVIZ_SD_FA=0` changes the default), everything else at the
library defaults (weights on the GPU backend).

**GPU job timeout.** ggml-vulkan sends up to 100 operations per submission by default. With
large images (or flash attention's scalar kernel on Intel) one submission can take longer than
the driver allows a compute job — 5 s on Intel's `xe` (`/sys/class/drm/card*/device/tile0/gt0/engines/ccs/job_timeout_ms`).
The driver then resets the engine (`xe … Engine reset: engine_class=ccs` in the kernel log)
and the generation hangs forever. `internal/ggml` sets `GGML_VK_MAX_NODES_PER_SUBMIT=8` at
startup (unless already set): no measurable cost (26.6 vs 26.7 s on a Z-Image test) and each
submission stays far below the limit. Generation parameters: prompt,
size, steps, `txt_cfg`, seed, Euler when the family asks for it (otherwise the model's default
sampler and scheduler), and the reference images.

Advanced settings (`Request`): negative prompt, CFG (`txt_cfg`), sampler and scheduler by
name (`str_to_sample_method` / `str_to_scheduler`; only the names in `Samplers` / `Schedulers`
are accepted), flash attention per request (part of the context key, so changing it reloads
the model), and an initial image for img2img (`init_image` + `strength`), scaled and
center-cropped to the output size (`fitRGB`).

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
| `POST /api/images/generate` | `{model, prompt, refs[], width, height, steps, seed}` and the advanced `{negative, cfg, sampler, scheduler, flashAttn: on/off/"", init, strength}` → job `image`. Saves `images/<date>-<first words>.png` and its `.json` (prompt, model, seed, steps, size, refs, device, elapsed) |
| `GET /api/images/preview` | the latest preview as JPEG; `job.gen.preview` carries its URL with a counter so the `<img>` reloads |
| `DELETE /api/images?name=` | deletes an image of `images/` (name only) and its `.json` |

Uploads for references reuse `POST /api/upload` (into `assets/`).

## Performance

Measured at 1024×576 (Z-Image: 2,336 tokens of 3,840 channels per step, 30 layers).

| | Intel Lunar Lake iGPU (Vulkan) | Apple M4 base (Metal) |
|---|---|---|
| Z-Image Turbo, 8 steps | ~72 s (8.3 s/step) | ~125 s (15 s/step, with flash attention) |
| FLUX.2 klein, 4 steps, 1 reference | ~52 s (11.7 s/step) | — |

Where a Z-Image step goes on the Intel iGPU (`GGML_VK_PERF_LOGGER=1`, 9 s of GPU time):

| | Time | |
|---|---|---|
| Weight matmuls (Q4_K) | 3.7 s | 8–9 TFLOPS: the XMX units are used (cooperative matrix) |
| Attention, unfused | 2.1 s | QKᵀ, scale, softmax, ×V in f32 over a 30 × 2336² matrix (≈ 650 MB) per layer |
| Element-wise ops | 2.8 s | scale, norms, adds, copies: memory-bound (LPDDR5x) |

**Flash attention on Intel:** ggml-vulkan's fast flash-attention kernel (`FA_COOPMAT1`) needs
16×16×16 cooperative-matrix tiles; Intel Xe2 exposes 8×16×16, so it falls back to the scalar
kernel (≈ 0.5 TFLOPS): 5.1 s per step against 2.1 s for the unfused attention. Hence flash
attention *off* is faster on Intel for small images (Z-Image 13.8 → 8.3 s/step, klein 26 →
11.7 s/step), but the plain attention materializes heads × tokens² × 4 bytes: 2.4 GB at 5,000
tokens, 6 GB at 8,000 (1344×768 plus one reference), where a step takes minutes. So it is a
manual setting, on by default, and the dialog warns when it is off with more than 5,000 tokens.
On Metal it makes no difference (48.7 s with, 50.7 s without, M4). Q8_0 weights run at the same speed as Q4_K (compute-bound, not
dequantization-bound). Turning cooperative matrix off makes it 50 % slower.

PyTorch-based tools (ComfyUI) on Intel use oneDNN's fused bf16 attention (SDPA) on XMX, which
ggml-vulkan cannot match on this GPU today; on NVIDIA GPUs ggml-vulkan has the coopmat2 path.

Model loading takes 2–3 s from the page cache. The text encoding takes ~2 s.
Whisper recognition with the shared ggml gives the same result as before (524 words on the
demo song, 377/380 aligned).
