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
	"os"
	"time"

	"golang.org/x/image/draw"

	"audioviz/internal/progress"
)

type Request struct {
	Model         string // family ID (zimage, klein, klein3, qwen, qwen-turbo, moss; +"-int8" on SYCL)
	Prompt        string
	Width, Height int           // rounded to multiples of 32
	Steps         int           // 0 = the model's default
	Seed          int64         // < 0 = random
	Refs          []image.Image // reference images (models that accept them)

	// Advanced (zero values = the model's defaults)
	Negative  string      // negative prompt (only has an effect with CFG > 1)
	CFG       float32     // guidance scale
	Sampler   string      // stable-diffusion.cpp names: euler, euler_a, dpm++2m, …
	Scheduler string      // discrete, karras, simple, …
	FlashAttn string      // "on", "off", "" = DefaultFlashAttn (on)
	Init      image.Image // starting image (img2img): the result keeps its composition
	Strength  float32     // with Init: how much it changes (denoise, 0..1; 0 = 0.75)
}

// Samplers and Schedulers accepted in Request (stable-diffusion.cpp names)
var (
	Samplers   = []string{"euler", "euler_a", "heun", "dpm2", "dpm++2s_a", "dpm++2m", "dpm++2mv2", "ipndm", "ipndm_v", "lcm", "ddim_trailing", "tcd", "res_multistep", "res_2s", "er_sde", "lms"}
	Schedulers = []string{"discrete", "karras", "exponential", "ays", "gits", "sgm_uniform", "simple", "smoothstep", "kl_optimal", "beta", "flux", "flux2"}
)

func valid(v string, list []string) bool {
	if v == "" {
		return true
	}
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

type Result struct {
	Image     *image.NRGBA
	Seed      int64
	Steps     int
	CFG       float32
	FlashAttn bool
	Device    string
	Model     string
}

var stepWeights = map[string]float64{"models": 0.05, "load": 0.02, "refs": 0.04, "prompt": 0.04, "generate": 0.8, "decode": 0.05}

// vaeTileAbove: from this many pixels on, the VAE works in tiles. On Vulkan from 512×512 (in one
// piece its buffers outgrow what Vulkan can map: at 1024×1024 on Intel 40 s instead of 12 s in
// tiles); on SYCL in one piece is faster (7 s instead of 27 s) until it no longer fits
func vaeTileAbove() int {
	if onSYCL() {
		return 1536 * 1024
	}
	return 512 * 512
}

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
	if f.SYCLOnly && !onSYCL() {
		return nil, fmt.Errorf("%s needs the Intel GPU engine (SYCL plugin): Settings › Plugins", f.Name)
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
	if !valid(req.Sampler, Samplers) || !valid(req.Scheduler, Schedulers) {
		return nil, fmt.Errorf("unknown sampler %q or scheduler %q", req.Sampler, req.Scheduler)
	}
	cfg := req.CFG
	if cfg <= 0 {
		cfg = f.CFG
	}
	strength := req.Strength
	if strength <= 0 || strength > 1 {
		strength = 0.75
	}
	fa := DefaultFlashAttn()
	switch req.FlashAttn {
	case "on":
		fa = true
	case "off":
		fa = false
	}
	seed := req.Seed
	if seed < 0 {
		seed = rand.Int64N(1 << 31)
	}

	gen.Lock()
	defer gen.Unlock()
	steps0 := []progress.Step{{ID: "models", Label: "Models"}, {ID: "load", Label: "Prepare the model"}}
	if len(req.Refs) > 0 {
		steps0 = append(steps0, progress.Step{ID: "refs", Label: "Encode the reference images"})
	}
	steps0 = append(steps0, progress.Step{ID: "prompt", Label: "Read the prompt"},
		progress.Step{ID: "generate", Label: "Generate the image"}, progress.Step{ID: "decode", Label: "Decode the image"})
	t := progress.NewTracker(steps0, stepWeights, report)

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
	key := fmt.Sprintf("%s|%s|fa=%v", f.ID, vision, fa)
	if eng.ctx != nil && eng.key == key {
		t.End("load", "skipped", "Already in memory")
	} else {
		t.Start("load", f.Name+"…")
		tok := ""
		if f.Tokenizer != nil {
			tok = paths[f.Tokenizer.Name]
		}
		diff := paths[f.Diffusion.Name]
		// (development: another diffusion file for the family, e.g. its INT8 convrot weights)
		if d := os.Getenv("AUDIOVIZ_SD_DIFFUSION"); d != "" {
			diff = d
		}
		if err := eng.load(key, diff, paths[f.LLM.Name], vision, paths[f.VAE.Name], tok, f.Conv, onSYCL() && !f.Heavy, fa); err != nil {
			t.End("load", "error", err.Error())
			return nil, err
		}
		t.End("load", "done", f.Name)
	}
	t.SetDevice(eng.device)
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// 3. Generate: stable-diffusion.cpp goes through its phases (references, prompt, sampling,
	// decoding) inside one call; they are polled from the C callbacks, with a preview at each step
	refs := make([]rgb, len(req.Refs))
	for i, r := range req.Refs {
		refs[i] = toRGB(r)
	}
	var init *rgb
	if req.Init != nil {
		r := fitRGB(req.Init, w, h)
		init = &r
	}
	neg := req.Negative
	if neg == "" && cfg > 1 {
		neg = f.Negative
	}
	sp := sampling{negative: neg, w: w, h: h, steps: steps, cfg: cfg, sampler: req.Sampler, scheduler: req.Scheduler,
		euler: f.Euler, seed: seed, strength: strength, tile: w*h > vaeTileAbove()}
	// a distilled model's own schedule, unless the steps or the scheduler were changed
	if len(f.Sigmas) > 0 && steps == f.Steps && req.Scheduler == "" {
		sp.sigmas = f.Sigmas
	}
	type res struct {
		img *image.NRGBA
		err error
	}
	done := make(chan res, 1)
	go func() {
		img, err := eng.run(req.Prompt, sp, refs, init)
		done <- res{img, err}
	}()
	phaseStep := map[int]string{phRefs: "refs", phText: "prompt", phSample: "generate", phDecode: "decode"}
	cur := ""
	doneDetail := map[string]string{"refs": fmt.Sprintf("%d image(s)", len(req.Refs)), "prompt": "Done",
		"generate": fmt.Sprintf("%d steps · %d×%d · seed %d", steps, w, h, seed), "decode": fmt.Sprintf("%d×%d", w, h)}
	// advance ends the current step and starts the next one (the phases only go forward)
	advance := func(id string) {
		if id == cur {
			return
		}
		if cur != "" {
			t.End(cur, "done", doneDetail[cur])
		}
		cur = id
		t.Start(id, "")
	}
	buf := make([]byte, w*h*4)
	seq := 0
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	canceled := false
	for {
		select {
		case r := <-done:
			if canceled || ctx.Err() != nil {
				if cur != "" {
					t.End(cur, "error", "Canceled")
				}
				return nil, context.Canceled
			}
			if r.err != nil {
				if cur != "" {
					t.End(cur, "error", r.err.Error())
				}
				return nil, r.err
			}
			advance("decode")
			t.End("decode", "done", doneDetail["decode"])
			return &Result{Image: r.img, Seed: seed, Steps: steps, CFG: cfg, FlashAttn: fa, Device: eng.device, Model: f.ID}, nil
		case <-ctx.Done():
			if !canceled {
				canceled = true
				cancel()
			}
		case <-tick.C:
			st := getState()
			id, ok := phaseStep[st.phase]
			if !ok {
				continue
			}
			advance(id)
			// (a model too big for the GPU at once has its weights staged block by block at every
			// step: once the steps have started they stay shown, the loading only noted after them)
			loadNote := ""
			if st.loading {
				loadNote = " · " + "loading weights"
			}
			switch {
			case id == "generate" && st.steps > 0:
				t.Progress(id, float64(st.step)/float64(st.steps), fmt.Sprintf("Step %d of %d · %d×%d · seed %d", st.step, st.steps, w, h, seed)+loadNote)
			case id == "decode" && st.steps > 0:
				t.Progress(id, float64(st.step)/float64(st.steps), fmt.Sprintf("Part %d of %d", st.step, st.steps)+loadNote)
			case st.loading:
				p := 0.0
				if st.tensorsTotal > 0 {
					p = float64(st.tensors) / float64(st.tensorsTotal)
				}
				t.Progress(id, 0, fmt.Sprintf("Loading the weights to the GPU · %.0f%%", p*100))
			case id == "generate":
				t.Progress(id, 0, fmt.Sprintf("%d steps · %d×%d · seed %d", steps, w, h, seed))
			default:
				t.Progress(id, 0, "")
			}
			if onPreview != nil && id == "generate" {
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
	sycl := onSYCL()
	for _, f := range Families {
		if f.SYCLOnly && !sycl {
			continue
		}
		var size int64
		for _, m := range f.Needed(f.Vision != nil) {
			size += m.Size
		}
		out = append(out, map[string]any{"id": f.ID, "name": f.Name, "desc": f.Desc, "refs": f.Refs, "steps": f.Steps, "cfg": f.CFG,
			"heavy": f.Heavy, "license": f.License, "size": size, "missing": Missing(f.Needed(false)), "layout": f.Layout,
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

// fitRGB: the image covering w×h (scaled and cropped at the center), as packed RGB
func fitRGB(src image.Image, w, h int) rgb {
	b := src.Bounds()
	sw, sh := float64(b.Dx()), float64(b.Dy())
	s := math.Max(float64(w)/sw, float64(h)/sh)
	cw, ch := float64(w)/s, float64(h)/s
	crop := image.Rect(b.Min.X+int((sw-cw)/2), b.Min.Y+int((sh-ch)/2), b.Min.X+int((sw+cw)/2), b.Min.Y+int((sh+ch)/2))
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, crop, draw.Src, nil)
	pix := make([]byte, w*h*3)
	for i := 0; i < w*h; i++ {
		copy(pix[i*3:i*3+3], dst.Pix[i*4:i*4+3])
	}
	return rgb{w: w, h: h, pix: pix}
}
