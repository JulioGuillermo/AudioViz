# Image generation on Intel iGPUs: optimization log

How image generation was made faster on an Intel Lunar Lake iGPU (Arc 140V, Xe2, 32 GB shared
memory), what each of our patches does, how to measure, and what is still slow. Kept up to date as
work goes on; the newest entries are at the end of the log.

Background: [image-generation.md](image-generation.md) (stable-diffusion.cpp on the shared ggml,
models, the Vulkan path). The SYCL backend ships as an optional plugin
([plugins.md](../plugins.md), `scripts/build-sycl-plugin.sh`).

## Where the time goes (Z-Image Turbo, 1024×1024, 8 steps, SYCL)

One DiT layer of one step, after the work below (≈ 124 ms; 30 layers + 2 refiners per step):

| Part | ms | |
|---|---|---|
| Matrix multiplications (q4_K weights, oneDNN `jit:gemm` on XMX) | ≈ 80 | 17–24 TFLOPS: close to the hardware, **not** the bottleneck |
| Attention (oneDNN SDPA, our mask-less patch) | ≈ 21.5 | |
| Norm chains, gates, SwiGLU, V preparation (fused) | ≈ 11 | was ≈ 36 before the fusions |
| Host gaps between graph runs | ≈ 4.7 | ggml-ext kernels (the rope) split the graph |

Outside the DiT: the VAE decode (≈ 7 s, its `IM2COL` kernel runs at ≈ 10 GB/s of ≈ 90 possible)
and ≈ 4 s of SYCL kernel JIT compilation the first time each kernel runs in a process.

## Results

Z-Image Turbo, 1024×1024, 8 steps, seed 7, SYCL plugin (single runs; this machine varies by a few
seconds between processes, so close numbers were A/B'd alternately):

| Stage | Total | Prompt | Generate | Decode |
|---|---|---|---|---|
| stable-diffusion.cpp 5a1ba5d, SYCL as it came | 73–89 s | 3–5 s | 54–67 s | 14 s |
| Whole model on the GPU, no graph segments, iGPU free memory fixed | 61 s | 2 s | 49 s | 7 s |
| + rope on SYCL (ggml-ext) + norm-chain fusion | 49 s | 2 s | 38 s | 7 s |
| + V preparation, SCALE·SCALE, SwiGLU·SCALE fusions | **46 s** | 3 s | **35 s** | 7 s |
| INT8 weights (W8A8 "convrot", `zimage-int8`) | **40 s** | 3 s | **27 s** | 7 s |

Every image was compared with the unfused one: PSNR ≈ 55 dB (mean difference < 0.1 of 255),
visually identical.

Vulkan (default engine), same image: VAE decode went from ≈ 40 s to 10 s with the update to
stable-diffusion.cpp 5a1ba5d (direct convolution chosen automatically).

Qwen Image 2.1's VAE (Vulkan, 1024×1024): convolution `off` 34 s, `auto` 46 s, `on` 63 s, so the
Qwen families force `off` (`Family.Conv`).

## Our patches

Applied by `scripts/build-ml.sh` to clean checkouts (it resets and removes untracked files
first); the build stamp includes them, so editing a patch rebuilds the libraries. The SYCL plugin
must be rebuilt with them too (`scripts/build-sycl-plugin.sh`): a plugin built for another stamp
is refused.

| Patch | Applies to | What it does |
|---|---|---|
| `patches/ggml-vulkan-intel-fa.patch` | ggml | Vulkan flash attention with 8×16×16 cooperative matrices (Xe2's DPAS shape) |
| `patches/ggml-sycl-sdpa-nomask.patch` | ggml | oneDNN SDPA without a mask (flash attention on XMX) |
| `patches/ggml-sycl-audioviz.patch` | ggml | In `ggml-sycl.cpp`: the profiler, iGPU free memory, the ggml-ext entry point, the DiT fusion hooks (below) |
| `patches/ggml-sycl-sd-ext.patch` | ggml | New files: `sd-ext.cpp` (stable-diffusion.cpp's operators on SYCL), `dit-fuse.cpp/.hpp` (DiT fusions) |
| `patches-sd/sycl-ext.patch` | stable-diffusion.cpp | Hands its custom operators to a backend that asks for them (the SYCL plugin) |

### iGPU free memory (`ggml-sycl-audioviz.patch`)
Level Zero reports as used the memory the kernel can reclaim at once (the page cache: the model
files just read), so on the 32 GB machine "free" jumped between 5 and 11 GB of 12.6.
stable-diffusion.cpp sizes its work by it: it cut the DiT into 36 segments staged block by block
every step, kept the text encoder and VAE weights on the CPU, or refused to load. On a device with
host-unified memory the plugin now reports `MemAvailable` (up to the device total).
`GGML_SYCL_UMA_FREE=0` restores the old value.

AudioViz also asks for the whole model on the device and no graph segments on SYCL for the
non-heavy families (`AUDIOVIZ_SD_NOSEG=0/1` overrides it; the Qwen families and MOSS keep the
automatic fit).

### stable-diffusion.cpp operators on SYCL (`sd-ext.cpp`, `patches-sd/sycl-ext.patch`)
stable-diffusion.cpp defines fused operators through ggml-ext (`sd.rope_apply`, `sd.modulate`,
INT8 matmuls…) with kernels for CPU, CUDA and Vulkan only, so SYCL ran the generic graphs: RoPE
through a table (REPEAT + MUL + ADD + CONT over [2, 64, tokens, heads]), plus a permute and a cast
before the attention. The plugin now implements `sd.rope_apply` (its output already in the
attention's layout), alone or fused as RMS_NORM + MUL + rope (+ SCALE) (+ CPY to f16): the q/k
norm, rotation, attention scale and cast in one pass. stable-diffusion.cpp gets the kernels
registered through the plugin's `ggml_ext_sycl_register_v1`. `GGML_SYCL_SD_EXT=0` turns them off.
Used by every model that rotates through `Rope::apply_rope` (Z-Image, FLUX.2/3, Qwen Image…).

### DiT fusions (`dit-fuse.cpp`)
Inside the SYCL graph loop (no graph split), each a row-based kernel:

| Chain | Before | After |
|---|---|---|
| [SCALE] → RMS_NORM → MUL(weight) → … → MUL(gate) → ADD(residual) | 6.45 ms (4 launches) | 2.1 ms |
| RMS_NORM → MUL(weight) → MUL(scale) → ADD(self): modulation m·(1+s) | 5.2 ms (3 launches) | 1.5 ms |
| CONT(strided V) → SCALE → permute → CPY to f16 | 7.15 ms | 1.7 ms |
| SCALE → SCALE | 2.4 ms | 1.4 ms |
| SWIGLU → SCALE | 7.9 ms | 5.0 ms |

(per launch, 3840 × 4128 Z-Image rows). The tiny nodes between the two MULs of a chain (the
gate's TANH, a CONT of the modulation vector, the adaLN projection) are independent of it and run
first; the matcher checks that none writes where the chain reads. In-place results (views of
their own input) count as single-use. `GGML_SYCL_DIT_FUSE=0` turns the fusions off;
`GGML_SYCL_DIT_FUSE_DEBUG=1` logs why a wide norm chain is not fused.

Lesson: a first version with one work-item per element and 64-bit division/modulo for the indices
was *slower* than the unfused ops (SwiGLU·SCALE 12 ms); one work-group per row with the row's
coordinates computed once fixed it.

### INT8 on XMX (`sd-ext.cpp`)
stable-diffusion.cpp runs INT8 "convrot" checkpoints (Comfy-Org publishes them for Z-Image Turbo,
Qwen Image 2.1 and its Turbo, MOSS-Image SFT): int8 weights with a scale per output channel,
activations rotated in groups (scaled by 1/√G, radix-4 butterflies) and quantized per row at run
time (`sd.quantize_i8_convrot`), then `sd.linear_i8`. The plugin implements both: the rotation in
local memory per (row, group), the per-row quantization, an s8 × s8 → s32 oneDNN matmul on the
XMX units, and an epilogue (× row scale × column scale + bias). Z-Image's DiT step goes from
≈ 4.4 s (q4_K) to ≈ 3.4 s. Upstream's Vulkan INT8 kernels take ≈ 2 min per step on this GPU, so
the INT8 variants (`<model>-int8`) are offered only on the SYCL engine and refused elsewhere.
Checked against the Vulkan INT8 path: the 2-step difference (PSNR 32.5 dB) is of the order of
Vulkan vs SYCL with q4_K (36.3 dB); activation rounding amplifies tiny upstream differences.

### The VAE decode (`dit-fuse.cpp`, `generate.go`)
ggml's 2D convolution is IM2COL (a patch matrix of IC·KH·KW values per output pixel: gigabytes at
1920×1088 with 128 channels, written by a gather at ≈ 10 GB/s) then MUL_MAT. The plugin runs the
pair as one oneDNN convolution on the XMX units (the product is [OC][OH·OW], NCHW for one image;
weights to f32, f32 → f16 down-conversion allowed): FLUX.2 klein's decode at 1920×1088 went from
29 s to 11 s (PSNR 49.5 dB against im2col). `GGML_SYCL_ONEDNN_CONV=0` turns it off. ggml-sycl's own
direct convolution took 20 minutes there: never use it. On SYCL the VAE no longer tiles up to
2.4 MP (the tiles' overlap is work done twice: 38 → 28 s before the convolution);
`AUDIOVIZ_SD_VAE_TILE=<pixels>` (0 = never) overrides it. Qwen's VAE (3D convolutions) is not
covered. Optional *Fast decode* uses madebyollin's tiny decoders (taef1 for Z-Image, taef2 for the
FLUX.2 VAE models, taeqi2_1 for Qwen): under a second.

## How to measure

- `GGML_SYCL_OP_PROF=1`: after every launch (a node or a fused group) the plugin waits for the
  queue and adds the time to its label (ops + output shape); the table is rewritten to
  `/tmp/ggml-sycl-op-prof.txt` after every graph (`GGML_SYCL_OP_PROF_FILE` changes the path). Time
  between graphs is counted apart as `(host, before a graph of N nodes)`. Fusion is kept, unlike a
  per-node eval callback.
- `GGML_SYCL_OP_PROF=2`: the same plus every launch in order with its node names
  (`/tmp/ggml-sycl-op-trace.txt`). First launches include the kernel's JIT compilation: compare
  later steps.
- `AUDIOVIZ_SD_OPTIME=1`: per-op time through stable-diffusion.cpp's eval callback (any backend;
  no fusion).
- `AUDIOVIZ_SD_CONV=on|off|auto`: the convolution mode, to compare.
- `ONEDNN_VERBOSE=1`: which oneDNN primitive each matmul uses and its time (FLOPs = 2·M·K·N).
- A test cache keeps the user's install untouched: `AUDIOVIZ_CACHE=/tmp/avc` with `models`
  symlinked to the real cache and the freshly built plugin in `plugins/intel-sycl`, `plugins/engine`
  = `sycl`.
- VTune `gpu-hotspots` gives only totals here (the plugin carries its own SYCL runtime);
  `xpu-offload` collected nothing.

## Still slow, next ideas

- **JIT of SYCL kernels** (≈ 4 s per process; in the studio only the first image): AOT for Xe2
  (`lnl-m`) crashed in October because the build dropped the >4 GB buffer option.
- INT8: only Z-Image was measured; Qwen and MOSS INT8 are offered but untested. A CPU-reference
  check of the INT8 kernels at a small size would make the correctness proof exact.
- Qwen Image's VAE spends ≈ 5 s in CONT permutes around its 3D convolutions.
- The w2 matmul (K = 10240) takes 23.5 ms against 16 ms for w1/w3 with the same FLOPs; the
  f32 → f16 conversion of its input is a suspect.
- Other models: the rope, the memory fix and the V preparation are generic. The norm-chain and
  SwiGLU·SCALE fusions match Z-Image's graph; FLUX and Qwen Image use LayerNorm + `sd.modulate`,
  which need their own kernels. Profile each with `GGML_SYCL_OP_PROF`.
