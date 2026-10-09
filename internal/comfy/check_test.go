package comfy

import (
	"encoding/json"
	"testing"
)

func TestCheck(t *testing.T) {
	var info ObjectInfo
	_ = json.Unmarshal([]byte(`{
		"UNETLoader": {"input": {"required": {"unet_name": [["a.safetensors"], {}], "weight_dtype": ["COMBO", {"options": ["default", "fp8"]}]}}},
		"LoadImage": {"input": {"required": {"image": [["x.png"], {"image_upload": true}]}}},
		"SaveImage": {"input": {"required": {"filename_prefix": ["STRING", {}]}}, "output_node": true}}`), &info)
	f := &Flow{API: Prompt{
		"1": {Class: "UNETLoader", Inputs: map[string]any{"unet_name": "b.safetensors", "weight_dtype": "fp16"}},
		"2": {Class: "LoadImage", Inputs: map[string]any{"image": "mine.png"}},
		"3": {Class: "SaveImage", Inputs: map[string]any{"filename_prefix": "z"}},
		"4": {Class: "Upscaler9000", Inputs: map[string]any{}},
	}, Roles: []Role{{Kind: RoleImage, Node: "2", Input: "image"}, {Kind: RoleOutput, Node: "3"}, {Kind: RolePrompt, Node: "9", Input: "text"}}}
	got := map[string]int{}
	for _, is := range f.Check(info) {
		got[is.Kind]++
	}
	want := map[string]int{"model": 1, "value": 1, "node": 1, "role": 1}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: %d issues, want %d (%v)", k, got[k], v, f.Check(info))
		}
	}
	if got["output"] != 0 {
		t.Errorf("an output issue with an output role")
	}
}
