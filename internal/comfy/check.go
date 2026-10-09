package comfy

import (
	"encoding/json"
	"path"
	"sort"
	"strings"
)

// Issue: something that keeps a flow from running in a ComfyUI: a node it doesn't have, a model
// file it doesn't have, a choice it doesn't take, a role pointing at nothing, no output
type Issue struct {
	Kind  string `json:"kind"` // node, model, value, role, output
	Node  string `json:"node,omitempty"`
	Title string `json:"title,omitempty"` // the node's title
	Class string `json:"class,omitempty"`
	Input string `json:"input,omitempty"`
	Value string `json:"value,omitempty"`
}

// model files (what loaders choose from)
var modelExts = map[string]bool{".safetensors": true, ".sft": true, ".gguf": true, ".ckpt": true, ".pt": true, ".pth": true,
	".bin": true, ".onnx": true}

// Choices: the values a choice input takes (nil: it isn't one)
func (d *NodeDef) Choices(name string) []string {
	raw, ok := d.Input.Required[name]
	if !ok {
		raw, ok = d.Input.Optional[name]
	}
	if !ok {
		return nil
	}
	var arr []json.RawMessage
	if json.Unmarshal(raw, &arr) != nil || len(arr) == 0 {
		return nil
	}
	var list []string // [["a", "b"], {…}]
	if json.Unmarshal(arr[0], &list) == nil {
		return list
	}
	var typ string // ["COMBO", {"options": […]}]
	if json.Unmarshal(arr[0], &typ) == nil && typ == "COMBO" && len(arr) > 1 {
		var o struct {
			Options []string `json:"options"`
		}
		if json.Unmarshal(arr[1], &o) == nil {
			return o.Options
		}
	}
	return nil
}

// Check: what keeps the flow from running in the ComfyUI info describes (nothing: it can)
func (f *Flow) Check(info ObjectInfo) []Issue {
	var out []Issue
	// the inputs AudioViz fills itself (an input image is uploaded each run)
	given := map[string]bool{}
	for _, r := range f.Roles {
		if r.Kind != RoleOutput && r.Kind != RoleParam {
			given[r.Node+"|"+r.Input] = true
		}
	}
	missing := map[string]bool{}
	for _, id := range sortedIDs(f.API) {
		n := f.API[id]
		d := info[n.Class]
		if d == nil {
			out = append(out, Issue{Kind: "node", Node: id, Title: n.Title(), Class: n.Class})
			continue
		}
		for _, k := range sortedKeys(n.Inputs) {
			s, isStr := n.Inputs[k].(string)
			if !isStr || given[id+"|"+k] {
				continue
			}
			ch := d.Choices(k)
			if ch == nil || contains(ch, s) || (len(ch) == 0 && s == "") {
				continue
			}
			kind := "value"
			if modelExts[strings.ToLower(path.Ext(s))] {
				kind = "model"
				if missing[s] { // (once, though several loaders want it)
					continue
				}
				missing[s] = true
			}
			out = append(out, Issue{Kind: kind, Node: id, Title: n.Title(), Class: n.Class, Input: k, Value: s})
		}
	}
	hasOut := false
	for _, r := range f.Roles {
		n := f.API[r.Node]
		if r.Kind == RoleOutput {
			hasOut = hasOut || n != nil
		}
		if n == nil {
			out = append(out, Issue{Kind: "role", Node: r.Node, Input: r.Kind})
			continue
		}
		if r.Kind != RoleOutput {
			if _, ok := n.Inputs[r.Input]; !ok {
				out = append(out, Issue{Kind: "role", Node: r.Node, Title: n.Title(), Input: r.Kind, Value: r.Input})
			}
		}
	}
	if !hasOut {
		out = append(out, Issue{Kind: "output"})
	}
	return out
}

func sortedKeys(m map[string]any) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
