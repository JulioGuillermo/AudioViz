package imagegen

import "audioviz/internal/aimodel"

// Family is an image model: its weights (diffusion model, text encoder, VAE and, for editing
// with Qwen Image, the vision encoder) and the settings it is meant to run with.
type Family struct {
	ID      string  `json:"id"`
	Name    string  `json:"name"`
	Desc    string  `json:"desc"`
	Refs    int     `json:"refs"`  // reference images it accepts (0 = text only)
	Steps   int     `json:"steps"` // default sampling steps
	CFG     float32 `json:"cfg"`
	Euler   bool    `json:"-"`     // sample with Euler (otherwise the model default)
	Heavy   bool    `json:"heavy"` // needs a lot of memory and time
	License string  `json:"license"`

	Diffusion aimodel.Model  `json:"-"`
	LLM       aimodel.Model  `json:"-"`
	VAE       aimodel.Model  `json:"-"`
	Vision    *aimodel.Model `json:"-"` // only with reference images
}

func hf(repo, path string) string { return "https://huggingface.co/" + repo + "/resolve/main/" + path }

// Qwen3 4B: the text encoder of both Z-Image and FLUX.2 klein (downloaded once)
var qwen3_4b = aimodel.Model{Name: "qwen3-4b", Label: "Text encoder (Qwen3 4B)", File: "Qwen3-4B-Q4_K_M.gguf",
	URL: hf("unsloth/Qwen3-4B-GGUF", "Qwen3-4B-Q4_K_M.gguf"), Size: 2497281312}

var Families = []*Family{
	{ID: "zimage", Name: "Z-Image Turbo", Desc: "Fast and photorealistic, good with text in the image. From a prompt only.",
		Steps: 8, CFG: 1, License: "Apache-2.0",
		Diffusion: aimodel.Model{Name: "zimage", Label: "Z-Image Turbo", File: "z_image_turbo-Q4_K.gguf",
			URL: hf("leejet/Z-Image-Turbo-GGUF", "z_image_turbo-Q4_K.gguf"), Size: 3864250304},
		LLM: qwen3_4b,
		VAE: aimodel.Model{Name: "flux-vae", Label: "Image decoder (FLUX VAE)", File: "flux_ae.safetensors",
			URL: hf("Comfy-Org/z_image_turbo", "split_files/vae/ae.safetensors"), Size: 335304388}},
	{ID: "klein", Name: "FLUX.2 klein 4B", Desc: "Fast, generates or edits: give it reference images (up to 4) and describe the change.",
		Refs: 4, Steps: 4, CFG: 1, Euler: true, License: "Apache-2.0",
		Diffusion: aimodel.Model{Name: "klein", Label: "FLUX.2 klein 4B", File: "flux-2-klein-4b-Q4_0.gguf",
			URL: hf("leejet/FLUX.2-klein-4B-GGUF", "flux-2-klein-4b-Q4_0.gguf"), Size: 2460378560},
		LLM: qwen3_4b,
		VAE: aimodel.Model{Name: "flux2-vae", Label: "Image decoder (FLUX.2 VAE)", File: "flux2_vae.safetensors",
			URL: hf("Comfy-Org/flux2-dev", "split_files/vae/flux2-vae.safetensors"), Size: 336213556}},
	{ID: "qwen", Name: "Qwen Image 2.1", Desc: "The highest quality and best prompt following, also edits with references. Heavy: ~11 GB of models, and slow without a dedicated GPU.",
		Refs: 3, Steps: 20, CFG: 6, Euler: true, Heavy: true, License: "Apache-2.0",
		Diffusion: aimodel.Model{Name: "qwen-image", Label: "Qwen Image 2.1", File: "qwen_image_2.1-Q4_K.gguf",
			URL: hf("leejet/Qwen-Image-2.1-GGUF", "qwen_image_2.1-Q4_K.gguf"), Size: 4197494816},
		LLM: aimodel.Model{Name: "qwen3-vl-8b", Label: "Text encoder (Qwen3-VL 8B)", File: "Qwen3VL-8B-Instruct-Q4_K_M.gguf",
			URL: hf("Qwen/Qwen3-VL-8B-Instruct-GGUF", "Qwen3VL-8B-Instruct-Q4_K_M.gguf"), Size: 5027784800},
		Vision: &aimodel.Model{Name: "qwen3-vl-8b-vision", Label: "Vision encoder (Qwen3-VL 8B)", File: "mmproj-Qwen3VL-8B-Instruct-Q8_0.gguf",
			URL: hf("Qwen/Qwen3-VL-8B-Instruct-GGUF", "mmproj-Qwen3VL-8B-Instruct-Q8_0.gguf"), Size: 752289728},
		VAE: aimodel.Model{Name: "qwen-image-vae", Label: "Image decoder (Qwen Image 2.1 VAE)", File: "qwen_image_2.1_vae_bf16.safetensors",
			URL: hf("Comfy-Org/Qwen-Image-2.1", "vae/qwen_image_2.1_vae_bf16.safetensors"), Size: 675509688}},
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
