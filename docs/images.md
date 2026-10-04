# Generating images

AudioViz can create images from a description (a *prompt*) and edit or combine existing ones, to
use them as backgrounds. Everything runs **inside the binary**, on the GPU (Vulkan, or Metal on a
Mac): nothing to install, no account, nothing leaves your machine. The models are downloaded once
to `~/.cache/audioviz/models`.

## From the studio
**Images** (top bar), or *Generate an image…* in the background inspector.

1. Choose the **model** (see below).
2. Write the **prompt**: what the image shows — subject, place, light, style, framing. English
   works best (e.g. *"a neon-lit synthwave city skyline at night seen from a rooftop, rain,
   cinematic wide shot"*).
3. With FLUX.2 klein or Qwen Image you can add **reference images** (from the project, uploaded
   or from any folder) and describe the change: *"make it a sunset"*, *"the singer from image 1 on
   the stage of image 2"*.
4. **Shape** (like the video, 16:9, square, vertical…) and **size**: *fast* (≈0.6 megapixels),
   *detailed* (≈1 MP) or *video size* (slow). The background is scaled to the video anyway, so the
   fast size is usually enough.
5. **Generate**. The image appears roughly as it forms, step by step; you can close the dialog
   and keep working (the job bar shows the progress) or cancel.

Each image is saved in the project's `images/` folder as a PNG, with a `.json` next to it
holding the prompt, model and seed. From an image:
- **Use as background**: puts it in the background segment at the cursor.
- **Edit / vary**: it becomes the reference of a new image (switches to FLUX.2 klein if needed).
- **Reuse settings**: loads its prompt, model and seed. With the same seed and a slightly
  different prompt you get a variation of the same image.
- **Download** / **Delete**.

The loaded model stays in memory between images (the next one starts sooner) and is freed after
10 minutes without use.

## Models
| Model | Good for | References | Download | Time (1024×576, Intel Lunar Lake iGPU) |
|---|---|---|---|---|
| **Z-Image Turbo** | Fast, photorealistic, text inside the image | — | 6.7 GB | ≈ 1 min 10 s (8 steps) |
| **FLUX.2 klein 4B** | Fast generation **and editing** | up to 4 | 5.3 GB (the text encoder is shared with Z-Image: 2.8 GB if you already have it) | ≈ 50 s (4 steps, 1 reference) |
| **Qwen Image 2.1** | Highest quality and prompt following, editing | up to 3 | 9.9 GB (+0.75 GB to edit) | several minutes; meant for a dedicated GPU |

All three have permissive licenses (Apache-2.0): the images are yours to use. A dedicated GPU
or an Apple Silicon Mac is much faster than an integrated GPU; without any GPU it works on the
CPU, very slowly.

## From the terminal
```bash
./audioviz image -p "a lone guitarist on a cliff at sunset, cinematic" -o guitarist.png
./audioviz image --model klein -r guitarist.png -p "make it a snowy night with northern lights" -o winter.png
./audioviz image -p "..." -W 1920 -H 1088 --steps 12 --seed 42 -o big.png
./audioviz image --list          # the models and what is downloaded
```
Ctrl+C cancels. `AUDIOVIZ_DEBUG_SD=1` prints the engine's log.
