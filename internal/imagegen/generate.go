// Package imagegen generates images from a prompt, optionally with reference images to edit or
// combine, inside the binary: stable-diffusion.cpp on the GPU (Vulkan, or Metal on macOS) with
// Z-Image Turbo, FLUX.2 klein or Qwen Image. The models are downloaded once to ~/.cache/audioviz.
package imagegen

import (
	"context"
	"fmt"
	"image"
	"math"
	"math/rand/v2"
	"time"

	"golang.org/x/image/draw"

	"audioviz/internal/progress"
)

type Request struct {
	Model         string // family ID (zimage, klein, qwen)
	Prompt        string
	Width, Height int           // rounded to multiples of 32
	Steps         int           // 0 = the model's default
	Seed          int64         // < 0 = random
	Refs          []image.Image // reference images (models that accept them)
}

type Result struct {
	Image  *image.NRGBA
	Seed   int64
	Steps  int
	Device string
	Model  string
}

var stepWeights = map[string]float64{"models": 0.1, "load": 0.1, "generate": 0.8}

// maxRef: reference images are scaled down to at most this many pixels (about 1024×1024): more
// only costs memory and time
const maxRef = 1024 * 1024

// Generate makes one image. report receives the progress; onPreview (optional) the image as it
// forms, a rough preview at every step.
func Generate(ctx context.Context, req Request, report func(progress.Status), onPreview func(*image.NRGBA)) (*Result, error) {
	f := Get(req.Model)
	if f == nil {
		return nil, fmt.Errorf("unknown image model %q", req.Model)
	}
	if req.Prompt == "" {
		return nil, fmt.Errorf("the prompt is empty")
	}
	if len(req.Refs) > f.Refs {
		if f.Refs == 0 {
			return nil, fmt.Errorf("%s does not take reference images", f.Name)
		}
		return nil, fmt.Errorf("%s takes up to %d reference images", f.Name, f.Refs)
	}
	w, h := round32(req.Width), round32(req.Height)
	if w < 256 || h < 256 || w*h > 2048*2048 {
		return nil, fmt.Errorf("size %d×%d out of range (256 to 2048)", req.Width, req.Height)
	}
	steps := req.Steps
	if steps <= 0 {
		steps = f.Steps
	}
	seed := req.Seed
	if seed < 0 {
		seed = rand.Int64N(1 << 31)
	}

	gen.Lock()
	defer gen.Unlock()
	t := progress.NewTracker([]progress.Step{
		{ID: "models", Label: "Models"},
		{ID: "load", Label: "Load the model"},
		{ID: "generate", Label: "Generate the image"},
	}, stepWeights, report)

	// 1. Models (downloaded the first time)
	need := f.Needed(len(req.Refs) > 0)
	if missing := Missing(need); missing == 0 {
		t.End("models", "skipped", "Already downloaded")
	} else {
		t.Start("models", fmt.Sprintf("Downloading %.1f GB…", float64(missing)/1e9))
		var base int64
		for _, m := range need {
			if m.Present() {
				continue
			}
			_, err := m.Ensure(ctx, func(done, total int64, rate float64) {
				t.Progress("models", float64(base+done)/float64(missing),
					fmt.Sprintf("%s · %.0f of %.0f MB · %.1f MB/s", m.Label, float64(done)/1e6, float64(total)/1e6, rate/1e6))
			})
			if err != nil {
				t.End("models", "error", err.Error())
				return nil, err
			}
			base += m.Size
		}
		t.End("models", "done", "Downloaded")
	}

	// 2. Load (kept in memory for the next image)
	paths := map[string]string{}
	for _, m := range need {
		paths[m.Name] = m.Path()
	}
	vision := ""
	if len(req.Refs) > 0 && f.Vision != nil {
		vision = paths[f.Vision.Name]
	}
	key := f.ID + "|" + vision
	if eng.ctx != nil && eng.key == key {
		t.End("load", "skipped", "Already in memory")
	} else {
		t.Start("load", "Loading "+f.Name+"…")
		if err := eng.load(key, paths[f.Diffusion.Name], paths[f.LLM.Name], vision, paths[f.VAE.Name]); err != nil {
			t.End("load", "error", err.Error())
			return nil, err
		}
		t.End("load", "done", f.Name)
	}
	t.SetDevice(eng.device)
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// 3. Generate: sampling steps polled from the C callbacks, with a preview at each one
	refs := make([]rgb, len(req.Refs))
	for i, r := range req.Refs {
		refs[i] = toRGB(r)
	}
	t.Start("generate", fmt.Sprintf("%d×%d · %d steps · seed %d", w, h, steps, seed))
	type res struct {
		img *image.NRGBA
		err error
	}
	done := make(chan res, 1)
	go func() {
		img, err := eng.run(req.Prompt, w, h, steps, f.CFG, f.Euler, seed, refs)
		done <- res{img, err}
	}()
	buf := make([]byte, w*h*4)
	seq := 0
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	canceled := false
	for {
		select {
		case r := <-done:
			if canceled || ctx.Err() != nil {
				t.End("generate", "error", "Canceled")
				return nil, context.Canceled
			}
			if r.err != nil {
				t.End("generate", "error", r.err.Error())
				return nil, r.err
			}
			t.End("generate", "done", fmt.Sprintf("%d×%d · %d steps · seed %d", w, h, steps, seed))
			return &Result{Image: r.img, Seed: seed, Steps: steps, Device: eng.device, Model: f.ID}, nil
		case <-ctx.Done():
			if !canceled {
				canceled = true
				cancel()
			}
		case <-tick.C:
			// The same callback reports the sampling steps, then the image decoding (in tiles)
			if s, n := step(); n == steps {
				t.Progress("generate", 0.95*float64(s)/float64(n), fmt.Sprintf("Step %d of %d", s, n))
			} else if n > 0 {
				t.Progress("generate", 0.95+0.05*float64(s)/float64(n), "Decoding the image")
			}
			if onPreview != nil {
				var img *image.NRGBA
				if seq, img = preview(seq, buf); img != nil {
					onPreview(img)
				}
			}
		}
	}
}

func round32(v int) int { return (v + 16) / 32 * 32 }

// Models lists, per family, whether its files are downloaded and how much is missing
func Models() []map[string]any {
	var out []map[string]any
	for _, f := range Families {
		var size int64
		for _, m := range f.Needed(f.Vision != nil) {
			size += m.Size
		}
		out = append(out, map[string]any{"id": f.ID, "name": f.Name, "desc": f.Desc, "refs": f.Refs, "steps": f.Steps,
			"heavy": f.Heavy, "license": f.License, "size": size, "missing": Missing(f.Needed(false)),
			"missingRefs": Missing(f.Needed(true))})
	}
	return out
}

// toRGB converts a reference image to packed RGB, scaled down to maxRef pixels and to multiples of 16
func toRGB(src image.Image) rgb {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if a := w * h; a > maxRef {
		s := math.Sqrt(float64(maxRef) / float64(a))
		w, h = int(float64(w)*s), int(float64(h)*s)
	}
	w, h = max(16, w/16*16), max(16, h/16*16)
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Src, nil)
	pix := make([]byte, w*h*3)
	for i := 0; i < w*h; i++ {
		copy(pix[i*3:i*3+3], dst.Pix[i*4:i*4+3])
	}
	return rgb{w: w, h: h, pix: pix}
}
