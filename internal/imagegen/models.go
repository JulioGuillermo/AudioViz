package imagegen

import "audioviz/internal/aimodel"

// Family is an image model: its weights (diffusion model, text encoder, VAE and, for editing
// with Qwen Image, the vision encoder) and the settings it is meant to run with.
type Family struct {
	ID    string  `json:"id"`
	Name  string  `json:"name"`
	Desc  string  `json:"desc"`
	Refs  int     `json:"refs"`  // reference images it accepts (0 = text only)
	Steps int     `json:"steps"` // default sampling steps
	CFG   float32 `json:"cfg"`
	Euler bool    `json:"-"` // sample with Euler (otherwise the model default)
	// Sigmas: the fixed noise schedule it was distilled for (one more than its steps); used while
	// the steps are the default and no scheduler is picked
	Sigmas  []float32 `json:"-"`
	Heavy   bool      `json:"heavy"` // needs a lot of memory and time
	License string    `json:"license"`

	Diffusion aimodel.Model  `json:"-"`
	LLM       aimodel.Model  `json:"-"`
	VAE       aimodel.Model  `json:"-"`
	Vision    *aimodel.Model `json:"-"` // only with reference images
	Tokenizer *aimodel.Model `json:"-"` // an external tokenizer.json (its text encoder's vocabulary is not the embedded one)
	Negative  string         `json:"-"` // the negative prompt when none is given (CFG > 1)
	// Conv: direct convolution or im2col (stable-diffusion.cpp's conv_direct_mode; "" = auto, a small
	// benchmark on Vulkan). Qwen Image 2.1's VAE decodes 1024² in 34 s with off, 46 s with auto and
	// 63 s with on (Vulkan, Lunar Lake)
	Conv string `json:"-"`
	// Int8: the same model's INT8 "convrot" weights (W8A8), offered as a variant (ID + "-int8") on
	// the SYCL engine, whose plugin runs them on the XMX units (≈ 25 % faster than q4_K there;
	// very slow on Vulkan). SYCLOnly marks that variant.
	Int8     *aimodel.Model `json:"-"`
	SYCLOnly bool           `json:"sycl"`
	// Layout: how the prompt places elements in boxes ("flux3": <ids> in the text + a JSON list of
	// [top, left, bottom, right] 0..1000; "moss": a JSON layout in centred pixels, yxyx; "" none).
	// The studio's box editor writes it.
	Layout string `json:"layout"`
}

func hf(repo, path string) string { return "https://huggingface.co/" + repo + "/resolve/main/" + path }

// Qwen3 4B: the text encoder of both Z-Image and FLUX.2 klein (downloaded once)
var qwen3_4b = aimodel.Model{Name: "qwen3-4b", Label: "Text encoder (Qwen3 4B)", File: "Qwen3-4B-Q4_K_M.gguf",
	URL: hf("unsloth/Qwen3-4B-GGUF", "Qwen3-4B-Q4_K_M.gguf"), Size: 2497281312}

// The FLUX.2 VAE: FLUX.2 klein's, FLUX.3 Image Klein's and MOSS-Image's
var flux2VAE = aimodel.Model{Name: "flux2-vae", Label: "Image decoder (FLUX.2 VAE)", File: "flux2_vae.safetensors",
	URL: hf("Comfy-Org/flux2-dev", "split_files/vae/flux2-vae.safetensors"), Size: 336213556}

// Qwen Image 2.1's text encoder, vision encoder and VAE: shared by the full model and Turbo
var (
	qwen3vl8b = aimodel.Model{Name: "qwen3-vl-8b", Label: "Text encoder (Qwen3-VL 8B)", File: "Qwen3VL-8B-Instruct-Q4_K_M.gguf",
		URL: hf("Qwen/Qwen3-VL-8B-Instruct-GGUF", "Qwen3VL-8B-Instruct-Q4_K_M.gguf"), Size: 5027784800}
	qwen3vl8bVision = aimodel.Model{Name: "qwen3-vl-8b-vision", Label: "Vision encoder (Qwen3-VL 8B)", File: "mmproj-Qwen3VL-8B-Instruct-Q8_0.gguf",
		URL: hf("Qwen/Qwen3-VL-8B-Instruct-GGUF", "mmproj-Qwen3VL-8B-Instruct-Q8_0.gguf"), Size: 752289728}
	qwenImageVAE = aimodel.Model{Name: "qwen-image-vae", Label: "Image decoder (Qwen Image 2.1 VAE)", File: "qwen_image_2.1_vae_bf16.safetensors",
		URL: hf("Comfy-Org/Qwen-Image-2.1", "vae/qwen_image_2.1_vae_bf16.safetensors"), Size: 675509688}
)

var Families = []*Family{
	{ID: "zimage", Name: "Z-Image Turbo", Desc: "Fast and photorealistic, good with text in the image. From a prompt only.",
		Steps: 8, CFG: 1, License: "Apache-2.0",
		Diffusion: aimodel.Model{Name: "zimage", Label: "Z-Image Turbo", File: "z_image_turbo-Q4_K.gguf",
			URL: hf("leejet/Z-Image-Turbo-GGUF", "z_image_turbo-Q4_K.gguf"), Size: 3864250304},
		Int8: &aimodel.Model{Name: "zimage-int8", Label: "Z-Image Turbo (INT8)", File: "z_image_turbo_int8_convrot.safetensors",
			URL: hf("Comfy-Org/z_image_turbo", "split_files/diffusion_models/z_image_turbo_int8_convrot.safetensors"), Size: 6201001296},
		LLM: qwen3_4b,
		VAE: aimodel.Model{Name: "flux-vae", Label: "Image decoder (FLUX VAE)", File: "flux_ae.safetensors",
			URL: hf("Comfy-Org/z_image_turbo", "split_files/vae/ae.safetensors"), Size: 335304388}},
	{ID: "klein", Name: "FLUX.2 klein 4B", Desc: "Fast, generates or edits: give it reference images (up to 4) and describe the change.",
		Refs: 4, Steps: 4, CFG: 1, Euler: true, License: "Apache-2.0",
		Diffusion: aimodel.Model{Name: "klein", Label: "FLUX.2 klein 4B", File: "flux-2-klein-4b-Q4_0.gguf",
			URL: hf("leejet/FLUX.2-klein-4B-GGUF", "flux-2-klein-4b-Q4_0.gguf"), Size: 2460378560},
		LLM: qwen3_4b,
		VAE: flux2VAE},
	{ID: "qwen", Name: "Qwen Image 2.1", Desc: "The highest quality and best prompt following, also edits with references. Heavy: ~11 GB of models, and slow without a dedicated GPU.",
		Refs: 3, Steps: 20, CFG: 6, Euler: true, Heavy: true, License: "Qwen Research (non-commercial)", Conv: "off",
		Diffusion: aimodel.Model{Name: "qwen-image", Label: "Qwen Image 2.1", File: "qwen_image_2.1-Q4_K.gguf",
			URL: hf("leejet/Qwen-Image-2.1-GGUF", "qwen_image_2.1-Q4_K.gguf"), Size: 4197494816},
		Int8: &aimodel.Model{Name: "qwen-image-int8", Label: "Qwen Image 2.1 (INT8)", File: "qwen_image_2.1_int8_convrot.safetensors",
			URL: hf("Comfy-Org/Qwen-Image-2.1", "diffusion_models/qwen_image_2.1_int8_convrot.safetensors"), Size: 7256783064},
		LLM: qwen3vl8b, Vision: &qwen3vl8bVision, VAE: qwenImageVAE},
	// The 8-step distilled Qwen Image 2.1: the same encoders and VAE, CFG 1 (one pass per step)
	// and its own sigma schedule (Qwen/Qwen-Image-2.1-Turbo model_index.json sample_sigmas)
	{ID: "qwen-turbo", Name: "Qwen Image 2.1 Turbo", Desc: "Qwen Image 2.1 distilled to 8 steps: nearly its quality, several times faster. Also edits with references. Shares the text encoder and decoder with Qwen Image 2.1 (~10 GB in all).",
		Refs: 3, Steps: 8, CFG: 1, Euler: true, Heavy: true, License: "Qwen Research (non-commercial)", Conv: "off",
		Sigmas: []float32{1.0, 0.978453, 0.95418, 0.926626, 0.89508, 0.845148, 0.704534, 0.414568, 0.0},
		Diffusion: aimodel.Model{Name: "qwen-image-turbo", Label: "Qwen Image 2.1 Turbo", File: "qwen-image-2.1-turbo-Q4_K_M.gguf",
			URL: hf("unsloth/Qwen-Image-2.1-Turbo-GGUF", "qwen-image-2.1-turbo-Q4_K_M.gguf"), Size: 4199565120},
		Int8: &aimodel.Model{Name: "qwen-image-turbo-int8", Label: "Qwen Image 2.1 Turbo (INT8)", File: "qwen_image_2.1_turbo_int8_convrot.safetensors",
			URL: hf("Comfy-Org/Qwen-Image-2.1", "diffusion_models/qwen_image_2.1_turbo_int8_convrot.safetensors"), Size: 7256787829},
		LLM: qwen3vl8b, Vision: &qwen3vl8bVision, VAE: qwenImageVAE},
	// FLUX.3 Image Klein (5.6B): 16 steps at CFG 1, edits with references (the VAE encodes them);
	// boxes in the prompt place each element
	{ID: "klein3", Name: "FLUX.3 Image Klein", Desc: "Generates or edits with references (up to 4). Places elements where you say with boxes in the prompt. Better than FLUX.2 klein, somewhat slower.",
		Refs: 4, Steps: 16, CFG: 1, Euler: true, License: "FLUX Kommunity (commercial use under US$5M revenue)", Layout: "flux3",
		Diffusion: aimodel.Model{Name: "klein3", Label: "FLUX.3 Image Klein", File: "flux3-image-klein-Q4_K.gguf",
			URL: hf("leejet/flux-3-image-klein-GGUF", "flux3-image-klein-Q4_K.gguf"), Size: 3514023104},
		LLM: aimodel.Model{Name: "qwen3-vl-4b", Label: "Text encoder (Qwen3-VL 4B)", File: "Qwen3VL-4B-Instruct-Q4_K_M.gguf",
			URL: hf("Qwen/Qwen3-VL-4B-Instruct-GGUF", "Qwen3VL-4B-Instruct-Q4_K_M.gguf"), Size: 2497281664},
		VAE: flux2VAE},
	// MOSS-Image SFT (5B): CFG 3 (two passes per step; upstream uses 50 steps), Qwen3.5 9B as the
	// text encoder with its own tokenizer.json; JSON layouts in the prompt (experimental)
	{ID: "moss", Name: "MOSS-Image", Desc: "Very good with text in the image (posters, signs), also in Chinese. Accepts a layout of boxes in the prompt (experimental). From a prompt only; slow (two passes per step).",
		Steps: 30, CFG: 3, Heavy: true, License: "Apache-2.0", Negative: "worst quality, bad anatomy, watermark, jpeg artifact", Layout: "moss",
		Diffusion: aimodel.Model{Name: "moss-image", Label: "MOSS-Image SFT", File: "MOSS-Image-SFT-Q4_K.gguf",
			URL: hf("leejet/MOSS-Image-SFT-GGUF", "MOSS-Image-SFT-Q4_K.gguf"), Size: 3327400800},
		Int8: &aimodel.Model{Name: "moss-image-int8", Label: "MOSS-Image SFT (INT8)", File: "moss_image_sft_int8_convrot.safetensors",
			URL: hf("Comfy-Org/MOSS-Image", "diffusion_models/moss_image_sft_int8_convrot.safetensors"), Size: 5413698958},
		LLM: aimodel.Model{Name: "qwen3.5-9b", Label: "Text encoder (Qwen3.5 9B)", File: "Qwen3.5-9B-Q4_K_M.gguf",
			URL: hf("unsloth/Qwen3.5-9B-GGUF", "Qwen3.5-9B-Q4_K_M.gguf"), Size: 5680522464},
		Tokenizer: &aimodel.Model{Name: "moss-tokenizer", Label: "Tokenizer (Qwen3.5)", File: "moss-image-tokenizer.json",
			URL: hf("OpenMOSS-Team/MOSS-Image-Base", "tokenizer/tokenizer.json"), Size: 19989325},
		VAE: flux2VAE},
}

// The INT8 variants (after each family that has INT8 weights) and every file in the model
// catalog (aimodel.Register)
func init() {
	var all []*Family
	for _, f := range Families {
		all = append(all, f)
		if f.Int8 != nil {
			v := *f
			v.ID, v.Name, v.Diffusion, v.Int8, v.SYCLOnly = f.ID+"-int8", f.Name+" · INT8 (Intel XMX)", *f.Int8, nil, true
			v.Desc = "The same model with INT8 weights, run on the XMX units by the SYCL engine: about a quarter faster. A separate download."
			all = append(all, &v)
		}
	}
	Families = all
	for _, f := range Families {
		aimodel.Register("Images · "+f.Name, f.Needed(true)...)
	}
}

// Get returns the family with that ID (nil if unknown)
func Get(id string) *Family {
	for _, f := range Families {
		if f.ID == id {
			return f
		}
	}
	return nil
}

// Needed lists the files to generate (with refs: also the vision encoder, if the family has one)
func (f *Family) Needed(refs bool) []aimodel.Model {
	m := []aimodel.Model{f.Diffusion, f.LLM, f.VAE}
	if f.Tokenizer != nil {
		m = append(m, *f.Tokenizer)
	}
	if refs && f.Vision != nil {
		m = append(m, *f.Vision)
	}
	return m
}

// Missing: bytes still to download for these files
func Missing(ms []aimodel.Model) int64 {
	var n int64
	for _, m := range ms {
		if !m.Present() {
			n += m.Size
		}
	}
	return n
}
