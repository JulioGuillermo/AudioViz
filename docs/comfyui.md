# ComfyUI flows

A project can use **ComfyUI** workflows ("flows") to make its images: your own ones, the ones saved
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
| Input image | each image loader (an image of the project uploaded to ComfyUI) |
| Parameter | any other value you expose, with your name: steps, a LoRA's strength… |
| Result | the node the result is taken from (a Save Image…) |

And what it makes: an image from a prompt, an image edit (from images), a depth map, a video.

## Using it

*Images → New image → Model*: the project's image flows show as *ComfyUI · name*; the form shows
what its roles take. It runs as a job (its progress in the top bar, *Cancel* stops ComfyUI too) and
the result lands in the project's images. If ComfyUI refuses the flow — a model it doesn't have, a
wrong value — the message says which node and why.
