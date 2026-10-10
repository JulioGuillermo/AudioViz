# ComfyUI flows

A project can use **ComfyUI** workflows ("flows") to make images, depth maps, videos and audio: your own ones, the ones saved
in your ComfyUI, or its templates. AudioViz sends them to your ComfyUI and takes the result back
into the project. ComfyUI is yours to start (AudioViz never starts it); it can be on this machine
or another one on the network (start it with `--listen` then).

## Setting it up

*Project → ComfyUI*: its **address** (default `http://127.0.0.1:8188`) and **Test** (its version
and GPU when it answers). Below, the project's flows: they are kept in the project
(`<project>/comfy/`), so they travel with it.

## Importing a flow

*Import a flow…*:

- **Saved in ComfyUI** — the workflows in your ComfyUI (as its interface saves them).
- **ComfyUI templates** — its template library, with pictures, by category (search by name or model).
- **A file** — a workflow `.json` (as ComfyUI saves it, or exported for the API).

A flow saved by ComfyUI's interface is converted as ComfyUI itself does when it runs one:
subgraphs (also nested, with their promoted widgets), bypassed nodes, reroutes and primitives.

## Its roles

When importing, AudioViz proposes where each thing goes; check them in the flow's editor (the
pencil):

| Role | |
|---|---|
| Prompt / Negative prompt | the text node the sampler's positive / negative come from |
| Seed | the sampler's seed (a new one each run unless you give one) |
| Width / Height | the empty latent's size (the dialog's Shape and Size) |
| Input file | each loader of an image, a video or audio (a file of the project uploaded to ComfyUI) |
| Parameter | any other value you expose, with your name: steps, a LoRA's strength… (a video's length, its frame rate, a song's duration and lyrics are proposed when the flow has them) |
| Result | the node the result is taken from (a Save Image…) |

And what it makes: an image from a prompt, an image edit (from images), a depth map, a video, audio
(music, a voice…).

The editor reads as sentences ("Your prompt goes to *CLIP Text Encode #6* → *text*"), each a node of
the flow (its title in ComfyUI and number) and one of its fields, with what it holds now.

**What it lacks.** Each flow is checked against your ComfyUI: nodes not installed, model files it
doesn't have, options it no longer takes, no result node. The list shows ✓ (ready), ⚠ (what it
lacks, in the editor and the dialogs; it can't run until fixed) or ? (ComfyUI not answering).
After installing models or nodes, *Check again* (or *Test*).

## Using it

*Images → New image → Model*: the project's image flows show as *ComfyUI · name*; the form shows
what its roles take (an empty prompt: the flow's own). It runs as a job (its progress in the top bar, *Cancel* stops ComfyUI too) and
the result lands in the project's images. If ComfyUI refuses the flow — a model it doesn't have, a
wrong value — the message says which node and why.

**Any flow** (but a depth one) also runs from its ▶ in the list: its form, its progress (with the
image forming when ComfyUI sends previews), and its result, kept in the project — images in
`images/`, videos in `videos/`, audio in `audio/` — with *Use as background* (an image or a video,
for the background segment at the cursor) or *Use as the project's audio*.

## Depth maps

A background segment's *Depth → Depth from: ComfyUI* uses a depth flow of the project (*Flow*):
the segment's image (a video's first frame) goes to the flow's first input image and its result
(white = near) is the depth map, kept in the project's cache like the others (worked out again
when the image or the flow changes). Any depth node works, e.g. ComfyUI's *Depth Anything 3*
(`DA3Inference` + `DA3Render`, its model in `models/geometry_estimation`) or controlnet_aux's
preprocessors. ComfyUI must be running when the map is worked out.
