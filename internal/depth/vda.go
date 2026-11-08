package depth

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"regexp"
	"sort"
	"strings"
)

// Video Depth Anything Small (DepthAnything/Video-Depth-Anything, Apache-2.0): Depth Anything 2
// Small with four temporal modules in its head, which look at 32 frames at once so the depth
// holds steady over a video. Published only as a PyTorch checkpoint: converted here, once, to the
// GGUF depth-anything.cpp reads (DA2 Small's names and settings plus the temporal modules,
// "head.tm.N.*", for our patch scripts/ml/patches-da/vda.patch).

// gguf value kinds
const (
	ggufU32   = 4
	ggufI32   = 5
	ggufF32   = 6
	ggufBool  = 7
	ggufStr   = 8
	ggufArray = 9
)

type ggufKV struct {
	key  string
	kind int
	val  any // uint32, int32, float32, bool, string, []int32, []float32
}

// DA2 Small's settings (as in mudler's depth-anything2-small GGUF), for the video model
func vdaKVs() []ggufKV {
	p := "depthanything3."
	return []ggufKV{
		{"general.architecture", ggufStr, "depthanything3"},
		{p + "arch", ggufStr, "depthanything2"},
		{p + "checkpoint_name", ggufStr, "Video-Depth-Anything-vits"},
		{p + "patch_size", ggufU32, uint32(14)},
		{p + "vit.embed_dim", ggufU32, uint32(384)},
		{p + "vit.depth", ggufU32, uint32(12)},
		{p + "vit.num_heads", ggufU32, uint32(6)},
		{p + "vit.head_dim", ggufU32, uint32(64)},
		{p + "vit.mlp_hidden", ggufU32, uint32(1536)},
		{p + "vit.ffn_type", ggufStr, "mlp"},
		{p + "vit.num_register_tokens", ggufU32, uint32(0)},
		{p + "vit.init_values", ggufF32, float32(1)},
		{p + "vit.alt_start", ggufI32, int32(-1)},
		{p + "vit.rope_start", ggufI32, int32(-1)},
		{p + "vit.qknorm_start", ggufI32, int32(-1)},
		{p + "vit.rope_freq", ggufF32, float32(100)},
		{p + "vit.cat_token", ggufBool, false},
		{p + "vit.qkv_bias", ggufBool, true},
		{p + "vit.ln_eps", ggufF32, float32(1e-6)},
		{p + "vit.interpolate_offset", ggufF32, float32(0.1)},
		{p + "vit.interpolate_antialias", ggufBool, false},
		{p + "vit.pos_embed_grid", ggufU32, uint32(37)},
		{p + "vit.out_layers", ggufArray, []int32{2, 5, 8, 11}},
		{p + "img.mean", ggufArray, []float32{0.485, 0.456, 0.406}},
		{p + "img.std", ggufArray, []float32{0.229, 0.224, 0.225}},
		{p + "img.resize_mode", ggufStr, "lower_bound"},
		{p + "img.resize_target", ggufU32, uint32(518)},
		{p + "head.features", ggufU32, uint32(64)},
		{p + "head.out_channels", ggufArray, []int32{48, 96, 192, 384}},
		{p + "head.output_dim", ggufU32, uint32(1)},
		{p + "head.pos_embed", ggufBool, false},
		{p + "head.activation", ggufStr, "relu"},
		{p + "head.norm_type", ggufStr, "idt"},
	}
}

var (
	reBlock = regexp.MustCompile(`^pretrained\.blocks\.(\d+)\.(.+)$`)
	reProj  = regexp.MustCompile(`^head\.projects\.(\d)\.(weight|bias)$`)
	reRes   = regexp.MustCompile(`^head\.resize_layers\.(\d)\.(weight|bias)$`)
	reLrn   = regexp.MustCompile(`^head\.scratch\.layer(\d)_rn\.(weight|bias)$`)
	reRcu   = regexp.MustCompile(`^head\.scratch\.refinenet(\d)\.resConfUnit(\d)\.conv(\d)\.(weight|bias)$`)
	reRout  = regexp.MustCompile(`^head\.scratch\.refinenet(\d)\.out_conv\.(weight|bias)$`)
	reOut2  = regexp.MustCompile(`^head\.scratch\.output_conv2\.(\d)\.(weight|bias)$`)
	reTm    = regexp.MustCompile(`^head\.motion_modules\.(\d)\.temporal_transformer\.(.+)$`)
)

var vdaBlock = map[string]string{
	"norm1.weight": "norm1.weight", "norm1.bias": "norm1.bias", "norm2.weight": "norm2.weight", "norm2.bias": "norm2.bias",
	"attn.qkv.weight": "attn_qkv.weight", "attn.qkv.bias": "attn_qkv.bias", "attn.proj.weight": "attn_proj.weight",
	"attn.proj.bias": "attn_proj.bias", "ls1.gamma": "ls1", "ls2.gamma": "ls2", "mlp.fc1.weight": "mlp_fc1.weight",
	"mlp.fc1.bias": "mlp_fc1.bias", "mlp.fc2.weight": "mlp_fc2.weight", "mlp.fc2.bias": "mlp_fc2.bias",
}

var vdaTm = map[string]string{
	"norm.weight": "norm.weight", "norm.bias": "norm.bias", "proj_in.weight": "proj_in.weight", "proj_in.bias": "proj_in.bias",
	"proj_out.weight": "proj_out.weight", "proj_out.bias": "proj_out.bias",
	"transformer_blocks.0.ff.net.0.proj.weight": "ff_in.weight", "transformer_blocks.0.ff.net.0.proj.bias": "ff_in.bias",
	"transformer_blocks.0.ff.net.2.weight": "ff_out.weight", "transformer_blocks.0.ff.net.2.bias": "ff_out.bias",
	"transformer_blocks.0.ff_norm.weight": "ff_norm.weight", "transformer_blocks.0.ff_norm.bias": "ff_norm.bias",
}

// vdaName: the GGUF name of a checkpoint tensor ("" = not used)
func vdaName(n string) string {
	switch n {
	case "pretrained.cls_token":
		return "vit.cls_token"
	case "pretrained.pos_embed":
		return "vit.pos_embed"
	case "pretrained.patch_embed.proj.weight":
		return "vit.patch_embed.weight"
	case "pretrained.patch_embed.proj.bias":
		return "vit.patch_embed.bias"
	case "pretrained.norm.weight":
		return "vit.norm.weight"
	case "pretrained.norm.bias":
		return "vit.norm.bias"
	case "head.scratch.output_conv1.weight":
		return "head.scratch.out1.weight"
	case "head.scratch.output_conv1.bias":
		return "head.scratch.out1.bias"
	}
	if m := reBlock.FindStringSubmatch(n); m != nil {
		if r, ok := vdaBlock[m[2]]; ok {
			return "vit.blk." + m[1] + "." + r
		}
		return ""
	}
	if m := reProj.FindStringSubmatch(n); m != nil {
		return "head.proj." + m[1] + "." + m[2]
	}
	if m := reRes.FindStringSubmatch(n); m != nil {
		return "head.resize." + m[1] + "." + m[2]
	}
	if m := reLrn.FindStringSubmatch(n); m != nil {
		return "head.scratch.layer" + m[1] + "_rn." + m[2]
	}
	if m := reRcu.FindStringSubmatch(n); m != nil {
		return fmt.Sprintf("head.scratch.rn%s.rc%s.c%s.%s", m[1], m[2], m[3], m[4])
	}
	if m := reRout.FindStringSubmatch(n); m != nil {
		return "head.scratch.rn" + m[1] + ".out." + m[2]
	}
	if m := reOut2.FindStringSubmatch(n); m != nil {
		switch m[1] {
		case "0":
			return "head.scratch.out2a." + m[2]
		case "2":
			return "head.scratch.out2b." + m[2]
		}
		return ""
	}
	if m := reTm.FindStringSubmatch(n); m != nil {
		p := "head.tm." + m[1] + "."
		if r, ok := vdaTm[m[2]]; ok {
			return p + r
		}
		// transformer_blocks.0.attention_blocks.J.{to_q,to_k,to_v}.weight, to_out.0.{weight,bias},
		// pos_encoder.pe; transformer_blocks.0.norms.J.{weight,bias}
		rest := strings.TrimPrefix(m[2], "transformer_blocks.0.")
		var j, what string
		if _, err := fmt.Sscanf(strings.ReplaceAll(rest, ".", " "), "attention_blocks %s %s", &j, &what); err == nil {
			a := strings.TrimPrefix(rest, "attention_blocks."+j+".")
			switch a {
			case "to_q.weight":
				return p + "a" + j + ".q.weight"
			case "to_k.weight":
				return p + "a" + j + ".k.weight"
			case "to_v.weight":
				return p + "a" + j + ".v.weight"
			case "to_out.0.weight":
				return p + "a" + j + ".o.weight"
			case "to_out.0.bias":
				return p + "a" + j + ".o.bias"
			case "pos_encoder.pe":
				return p + "a" + j + ".pe"
			}
			return ""
		}
		if strings.HasPrefix(rest, "norms.") {
			parts := strings.Split(rest, ".") // norms J weight|bias
			if len(parts) == 3 {
				return p + "n" + parts[1] + "." + parts[2]
			}
		}
		return ""
	}
	return ""
}

// vdaPE: the sinusoidal frame embedding of a temporal attention block ([1, 32, C]), for a
// checkpoint that does not keep it
func vdaPE(c int) *pthTensor {
	const n = 32
	t := &pthTensor{Shape: []int{1, n, c}, Data: make([]float32, n*c)}
	for pos := 0; pos < n; pos++ {
		for i := 0; i < c; i += 2 {
			div := math.Exp(float64(i) * -math.Log(10000) / float64(c))
			t.Data[pos*c+i] = float32(math.Sin(float64(pos) * div))
			if i+1 < c {
				t.Data[pos*c+i+1] = float32(math.Cos(float64(pos) * div))
			}
		}
	}
	return t
}

// ConvertVDA writes the GGUF of a Video Depth Anything checkpoint (.pth)
func ConvertVDA(pth, out string) error {
	ts, err := readPth(pth)
	if err != nil {
		return err
	}
	named := map[string]*pthTensor{}
	for n, t := range ts {
		if g := vdaName(n); g != "" {
			named[g] = t
		}
	}
	for m := 0; m < 4; m++ { // (the frame embeddings, if the checkpoint has none)
		for j := 0; j < 2; j++ {
			k := fmt.Sprintf("head.tm.%d.a%d.pe", m, j)
			if named[k] == nil {
				if q := named[fmt.Sprintf("head.tm.%d.a%d.q.weight", m, j)]; q != nil {
					named[k] = vdaPE(q.Shape[1])
				}
			}
		}
	}
	for _, must := range []string{"vit.pos_embed", "vit.blk.11.mlp_fc2.weight", "head.scratch.out2b.weight",
		"head.tm.3.proj_out.weight", "head.tm.0.a1.q.weight", "head.tm.2.ff_in.weight"} {
		if named[must] == nil {
			return fmt.Errorf("not a Video Depth Anything Small checkpoint (%s missing)", must)
		}
	}
	names := make([]string, 0, len(named))
	for n := range named {
		names = append(names, n)
	}
	sort.Strings(names)
	return writeGGUF(out, vdaKVs(), names, named)
}

// writeGGUF: a GGUF v3 file of f32 tensors (dims reversed from PyTorch's, as ggml's)
func writeGGUF(path string, kvs []ggufKV, names []string, ts map[string]*pthTensor) error {
	f, err := os.Create(path + ".tmp")
	if err != nil {
		return err
	}
	w := bufio.NewWriterSize(f, 1<<20)
	le := binary.LittleEndian
	put := func(v any) { binary.Write(w, le, v) }
	str := func(s string) { put(uint64(len(s))); w.WriteString(s) }
	w.WriteString("GGUF")
	put(uint32(3))
	put(uint64(len(names)))
	put(uint64(len(kvs)))
	for _, kv := range kvs {
		str(kv.key)
		put(uint32(kv.kind))
		switch v := kv.val.(type) {
		case string:
			str(v)
		case []int32:
			put(uint32(ggufI32))
			put(uint64(len(v)))
			put(v)
		case []float32:
			put(uint32(ggufF32))
			put(uint64(len(v)))
			put(v)
		default:
			put(v)
		}
	}
	const align = 32
	var off uint64
	for _, n := range names {
		t := ts[n]
		str(n)
		put(uint32(len(t.Shape)))
		for i := len(t.Shape) - 1; i >= 0; i-- {
			put(uint64(t.Shape[i]))
		}
		put(uint32(0)) // f32
		put(off)
		off += (uint64(len(t.Data))*4 + align - 1) / align * align
	}
	// the data starts aligned
	pos := int64(w.Buffered())
	if st, err := f.Stat(); err == nil {
		pos += st.Size()
	}
	pad := func(n int64) {
		for ; n%align != 0; n++ {
			w.WriteByte(0)
		}
	}
	pad(pos)
	for _, n := range names {
		t := ts[n]
		put(t.Data)
		pad(int64(len(t.Data)) * 4)
	}
	if err := w.Flush(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(path+".tmp", path)
}
