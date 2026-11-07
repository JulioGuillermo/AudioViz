# Generating images

AudioViz can create images from a description (a *prompt*) and edit or combine existing ones, to
use them as backgrounds. Everything runs **inside the binary**, on the GPU (Vulkan, or Metal on a
Mac): nothing to install, no account, nothing leaves your machine. The models are downloaded once
to `~/.cache/audioviz/models`.

## From the studio
**Images** (top bar), or *Generate an image…* in the background inspector. The dialog opens on
the **list of every image**: the generated ones (newest first, marked with a wand) and the
project's (backgrounds and `assets/`), with filters *All / Generated / Project* and *Upload…*.

Click an image to see it big, with:
- **Use as background**: puts it in the background segment at the cursor.
- **Edit with AI**: opens the generator with it as a reference image (switches to FLUX.2 klein
  if the model doesn't take references); describe the change.
- **Use as initial image**: img2img from it (see *Initial image* below).
- **Reuse settings** (generated): loads its prompt, model, seed and advanced settings.
- **Download**, and **Delete** (only generated images).

**New image** opens the generator; *All images* goes back to the list:
1. Choose the **model** (see below).
2. Write the **prompt**: what the image shows — subject, place, light, style, framing. English
   works best (e.g. *"a neon-lit synthwave city skyline at night seen from a rooftop, rain,
   cinematic wide shot"*).
3. With FLUX.2 klein or Qwen Image you can add **reference images** (from the project, uploaded
   or from any folder) and describe the change: *"make it a sunset"*, *"the singer from image 1 on
   the stage of image 2"*.
4. **Shape** (like the video, 16:9, square, vertical…) and **size**: *fast* (≈0.6 megapixels),
   *detailed* (≈1 MP) or *video size*.
5. **Generate**. The image appears roughly as it forms, step by step. You can go back to the
   list (a bar shows the progress), close the dialog and keep working, or cancel. When it
   finishes, the new image opens.

**Advanced settings** (collapsible, remembered; empty = the model's default):

| Setting | What it does |
|---|---|
| Flash attention | On (default, recommended) or off. On keeps the memory small at any size. Off is a little faster for small images (~8 % at 1024×576), but its memory grows with the square of the size: with large images or references it needs several GB per step and can stall (the dialog warns). |
| CFG | How strictly the image follows the prompt. Z-Image Turbo and klein are distilled for 1; Qwen Image uses 6. |
| Negative prompt | What to avoid. Only has an effect with CFG above 1 (and makes each step twice as slow). |
| Sampler / scheduler | The sampling algorithm and the noise schedule (`euler`, `dpm++2m`, `karras`, `simple`…). |
| Initial image + denoise | img2img: start from an image instead of noise; the result keeps its composition. Denoise 0.3 changes little (style, colors), 0.8 keeps only the rough layout, 1 ignores it. Works with every model. |

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
| **Z-Image Turbo** | Fast, photorealistic, text inside the image | — | 6.7 GB | ≈ 1 min 20 s (8 steps); 1920×1088: ≈ 5 min 30 s |
| **FLUX.2 klein 4B** | Fast generation **and editing** | up to 4 | 5.3 GB (the text encoder is shared with Z-Image: 2.8 GB if you already have it) | ≈ 1 min (4 steps, 1 reference) |
| **Qwen Image 2.1** | Highest quality and prompt following, editing | up to 3 | 9.9 GB (+0.75 GB to edit) | several minutes; meant for a dedicated GPU |
| **Qwen Image 2.1 Turbo** | Qwen Image distilled to 8 steps: nearly its quality, several times faster; editing | up to 3 | 4.2 GB more if you have Qwen Image 2.1 (shares its encoders and decoder), else 9.9 GB | ≈ 2 min at 1024×1024 |
| **FLUX.3 Image Klein** | Generation and editing; **places elements with boxes** in the prompt (below) | up to 4 | 6 GB (shares the FLUX.2 decoder) | ≈ 2 min at 1024×1024 (16 steps) |
| **MOSS-Image** | Text inside the image (posters, signs, also Chinese); experimental **box layout** | — | 9.3 GB | ≈ 10 min at 1024×1024 (30 steps, two passes each) |

Licenses: Z-Image, FLUX.2 klein 4B and MOSS-Image are Apache-2.0. Qwen Image 2.1 and its Turbo
use the Qwen Research license (**non-commercial**). FLUX.3 Image Klein uses the FLUX Kommunity
license (commercial use of the images only below US$5M yearly revenue). A dedicated GPU or an
Apple Silicon Mac is much faster than an integrated GPU; without any GPU it works on the CPU,
very slowly.

### Placing elements with boxes
**FLUX.3 Image Klein**: write the description, name each element with an id in angle brackets,
then a space and a JSON list with one box per id, `[top, left, bottom, right]` from 0 to 1000:
```text
A poster: a red bicycle <bicycle_1> against a blue wall <wall_1>, the word SUMMER <title_1> at the top.
[{"id": "wall_1", "bbox": [0, 0, 1000, 1000], "desc": "A pale blue plaster wall."},
 {"id": "title_1", "bbox": [60, 200, 260, 800], "desc": "SUMMER in large white letters."},
 {"id": "bicycle_1", "bbox": [430, 330, 950, 980], "desc": "A vintage red bicycle."}]
```
**MOSS-Image**: the whole prompt is a JSON layout; boxes are `[y_min, x_min, y_max, x_max]` in
pixels with the canvas centre as the origin (a 1024×1024 canvas spans −512…512):
```json
{"summary": {"meta": ["poster"], "canvas": "single", "caption": "A travel poster, cream background."},
 "items": [{"label": "text", "caption": "Dark-blue title at the top.", "text": "EXPLORE", "bbox": [-416, -384, -288, 384]},
           {"label": "object", "caption": "A blue mountain in the centre.", "bbox": [-192, -288, 384, 288]}],
 "bbox_coord_space": "abs_centralized", "bbox_format": "yxyx"}
```

## From the terminal
```bash
./audioviz image -p "a lone guitarist on a cliff at sunset, cinematic" -o guitarist.png
./audioviz image --model klein -r guitarist.png -p "make it a snowy night with northern lights" -o winter.png
./audioviz image -p "..." -W 1920 -H 1088 --steps 12 --seed 42 -o big.png
./audioviz image -p "an oil painting" --init photo.png --strength 0.6 -o painting.png   # img2img
./audioviz image -p "..." --cfg 4 --neg "blurry, text" --sampler dpm++2m --scheduler karras --fa off
./audioviz image --list          # the models and what is downloaded
```
Ctrl+C cancels. `AUDIOVIZ_DEBUG_SD=1` prints the engine's log.
