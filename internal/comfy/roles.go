package comfy

import (
	"sort"
	"strconv"
	"strings"
)

// A flow's roles: where AudioViz puts what it is given (the prompt, the negative prompt, the
// seed, the size, the input images, any other value the user exposes) and where it takes the
// result from (an output node). Detect proposes them; the user corrects them in the studio.

type Role struct {
	Kind  string `json:"kind"` // prompt, negative, seed, width, height, image, param, output
	Node  string `json:"node"`
	Input string `json:"input,omitempty"`
	Label string `json:"label,omitempty"`
	Media string `json:"media,omitempty"` // an input file's: image, video, audio
	// what it holds in the flow (the default when AudioViz gives none)
	Value any `json:"value,omitempty"`
}

// Role kinds
const (
	RolePrompt   = "prompt"
	RoleNegative = "negative"
	RoleSeed     = "seed"
	RoleWidth    = "width"
	RoleHeight   = "height"
	RoleImage    = "image"
	RoleParam    = "param"
	RoleOutput   = "output"
)

// output nodes AudioViz takes results from, the preferred first (a video's or a song's before
// an image, which in such a flow is mostly a frame or a cover)
var outputClasses = []string{"VHS_VideoCombine", "SaveVideo", "SaveWEBM", "SaveAnimatedWEBP", "SaveAnimatedPNG",
	"SaveAudio", "SaveAudioMP3", "SaveAudioOpus", "SaveImage", "PreviewImage", "PreviewAudio"}

// file loaders: their input (the file's name in ComfyUI's input folder) and what it takes
var fileLoaders = map[string]struct{ input, media string }{
	"LoadImage": {"image", "image"}, "LoadImageMask": {"image", "image"}, "LoadImageOutput": {"image", "image"},
	"VHS_LoadVideo": {"video", "video"}, "LoadVideo": {"file", "video"},
	"LoadAudio": {"audio", "audio"}, "VHS_LoadAudioUpload": {"audio", "audio"}}

// values offered in the form when a flow has them (videos' and songs' length…)
var commonParams = []struct{ input, label string }{
	{"length", "Length (frames)"}, {"num_frames", "Frames"}, {"frame_rate", "Frames per second"}, {"fps", "Frames per second"},
	{"seconds", "Duration (s)"}, {"duration", "Duration (s)"}, {"lyrics", "Lyrics"}, {"steps", "Steps"}}

func sortedIDs(p Prompt) []string {
	ids := make([]string, 0, len(p))
	for id := range p {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return idLess(ids[i], ids[j]) })
	return ids
}

// idLess: node ids in numeric order ("3" < "12" < "12:4")
func idLess(a, b string) bool {
	as, bs := strings.Split(a, ":"), strings.Split(b, ":")
	for i := 0; i < len(as) && i < len(bs); i++ {
		x, e1 := strconv.Atoi(as[i])
		y, e2 := strconv.Atoi(bs[i])
		if e1 == nil && e2 == nil && x != y {
			return x < y
		}
		if as[i] != bs[i] {
			return as[i] < bs[i]
		}
	}
	return len(as) < len(bs)
}

// Detect: the roles a flow most likely has
func Detect(p Prompt, info ObjectInfo) []Role {
	var roles []Role
	ids := sortedIDs(p)
	// the prompt and the negative: from the samplers' positive / negative back to the text
	seenText := map[string]bool{}
	for _, id := range ids {
		n := p[id]
		for _, side := range []struct{ input, kind string }{{"positive", RolePrompt}, {"negative", RoleNegative}, {"conditioning", RolePrompt}} {
			v, ok := n.Inputs[side.input]
			if !ok {
				continue
			}
			src, _, isLink := Link(v)
			if !isLink {
				continue
			}
			if tid, tin, val, ok := findText(p, src, 0); ok && !seenText[tid+"|"+tin] && !hasRole(roles, side.kind) {
				seenText[tid+"|"+tin] = true
				roles = append(roles, Role{Kind: side.kind, Node: tid, Input: tin, Value: val})
			}
		}
	}
	if !hasRole(roles, RolePrompt) {
		// no sampler found: the first text there is
		for _, id := range ids {
			for _, k := range []string{"text", "prompt", "text_g", "positive"} {
				if s, ok := p[id].Inputs[k].(string); ok && !seenText[id+"|"+k] {
					roles = append(roles, Role{Kind: RolePrompt, Node: id, Input: k, Value: s})
					break
				}
			}
			if hasRole(roles, RolePrompt) {
				break
			}
		}
	}
	// the seed (a sampler's or a noise's first), the size of the empty latent
	for pass := 0; pass < 2 && !hasRole(roles, RoleSeed); pass++ {
		for _, id := range ids {
			n := p[id]
			c := strings.ToLower(n.Class)
			if pass == 0 && !strings.Contains(c, "sampler") && !strings.Contains(c, "noise") {
				continue
			}
			for _, k := range []string{"seed", "noise_seed"} {
				if v, ok := n.Inputs[k]; ok && !isLinkValue(v) && !hasRole(roles, RoleSeed) {
					roles = append(roles, Role{Kind: RoleSeed, Node: id, Input: k, Value: v})
				}
			}
		}
	}
	for _, id := range ids {
		n := p[id]
		if strings.Contains(n.Class, "Latent") && !hasRole(roles, RoleWidth) {
			w, okw := n.Inputs["width"]
			h, okh := n.Inputs["height"]
			if okw && okh && !isLinkValue(w) && !isLinkValue(h) {
				roles = append(roles, Role{Kind: RoleWidth, Node: id, Input: "width", Value: w},
					Role{Kind: RoleHeight, Node: id, Input: "height", Value: h})
			}
		}
	}
	// the input files, in order
	for _, id := range ids {
		n := p[id]
		if l, ok := fileLoaders[n.Class]; ok {
			label := n.Title()
			if label == n.Class {
				label = "" // (the studio names it: Image 1…)
			}
			roles = append(roles, Role{Kind: RoleImage, Node: id, Input: l.input, Label: label, Media: l.media, Value: n.Inputs[l.input]})
		}
	}
	// common values (a video's length, a song's lyrics…), the first of each
	taken := map[string]bool{}
	for _, r := range roles {
		taken[r.Node+"|"+r.Input] = true
	}
	for _, cp := range commonParams {
		for _, id := range ids {
			v, ok := p[id].Inputs[cp.input]
			if !ok || isLinkValue(v) || taken[id+"|"+cp.input] || hasLabel(roles, cp.label) {
				continue
			}
			taken[id+"|"+cp.input] = true
			roles = append(roles, Role{Kind: RoleParam, Node: id, Input: cp.input, Label: cp.label, Value: v})
			break
		}
	}
	// the output: the preferred kind of save node there is
	for _, cls := range outputClasses {
		for _, id := range ids {
			if p[id].Class == cls {
				roles = append(roles, Role{Kind: RoleOutput, Node: id, Label: p[id].Title()})
				return roles
			}
		}
	}
	for _, id := range ids {
		if d := info[p[id].Class]; d != nil && d.OutputNode {
			roles = append(roles, Role{Kind: RoleOutput, Node: id, Label: p[id].Title()})
			break
		}
	}
	return roles
}

func hasLabel(rs []Role, label string) bool {
	for _, r := range rs {
		if r.Kind == RoleParam && r.Label == label {
			return true
		}
	}
	return false
}

func hasRole(rs []Role, kind string) bool {
	for _, r := range rs {
		if r.Kind == kind {
			return true
		}
	}
	return false
}

func isLinkValue(v any) bool {
	_, _, ok := Link(v)
	return ok
}

// findText: upstream of node id (through conditionings, guiders, string nodes…), the first input
// holding a prompt: its node, input and text; a zeroed conditioning has none
func findText(p Prompt, id string, depth int) (string, string, any, bool) {
	if depth > 12 {
		return "", "", nil, false
	}
	n := p[id]
	if n == nil || n.Class == "ConditioningZeroOut" {
		return "", "", nil, false
	}
	keys := []string{"text", "prompt", "text_g", "positive", "tags", "string", "value"}
	// then any text input called so (input_string, text_a…)
	var more []string
	for k := range n.Inputs {
		lk := strings.ToLower(k)
		if strings.Contains(lk, "text") || strings.Contains(lk, "string") || strings.Contains(lk, "prompt") {
			more = append(more, k)
		}
	}
	sort.Strings(more)
	for _, k := range append(keys, more...) {
		v, ok := n.Inputs[k]
		if !ok {
			continue
		}
		if s, isStr := v.(string); isStr {
			return id, k, s, true
		}
		if src, _, isLink := Link(v); isLink {
			if t, ti, tv, ok := findText(p, src, depth+1); ok {
				return t, ti, tv, true
			}
		}
	}
	// keep going up its conditioning / guider inputs
	names := make([]string, 0, len(n.Inputs))
	for k := range n.Inputs {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		if k == "negative" || k == "clip" || k == "model" || k == "vae" {
			continue
		}
		if src, _, isLink := Link(n.Inputs[k]); isLink {
			if t, ti, tv, ok := findText(p, src, depth+1); ok {
				return t, ti, tv, true
			}
		}
	}
	return "", "", nil, false
}
