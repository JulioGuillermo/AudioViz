package comfy

import (
	"encoding/json"
	"testing"
)

// a subgraph whose instance promotes its inputs (proxyWidgets, the last one without a value)
func TestConvertSubgraphProxy(t *testing.T) {
	ui := `{"nodes":[{"id":77,"type":"sg1","mode":0,"inputs":[
	  {"name":"text","widget":{"name":"text"},"link":null},{"name":"cfg","widget":{"name":"cfg"},"link":null}],
	  "properties":{"proxyWidgets":[["-1","text"],["73","control_after_generate"],["-1","cfg"],["73","noise_seed"]]},
	  "widgets_values":["a fox",null,3.5]},
	 {"id":9,"type":"SaveImage","mode":0,"inputs":[{"name":"images","link":1}],"widgets_values":["out"]}],
	"links":[[1,77,0,9,0,"IMAGE"]],
	"definitions":{"subgraphs":[{"id":"sg1","inputs":[{"name":"text","linkIds":[10]},{"name":"cfg","linkIds":[11]}],
	  "outputs":[{"name":"IMAGE","linkIds":[12]}],
	  "nodes":[{"id":74,"type":"CLIPTextEncode","mode":0,"inputs":[{"name":"text","widget":{"name":"text"},"link":10}],"widgets_values":[""]},
	           {"id":63,"type":"CFGGuider","mode":0,"inputs":[{"name":"cfg","widget":{"name":"cfg"},"link":11}],"widgets_values":[1]},
	           {"id":73,"type":"RandomNoise","mode":0,"inputs":[{"name":"noise_seed","widget":{"name":"noise_seed"}}],"widgets_values":[42,"randomize"]},
	           {"id":43,"type":"VAEDecode","mode":0,"inputs":[]}],
	  "links":[{"id":10,"origin_id":-10,"origin_slot":0,"target_id":74,"target_slot":0},
	           {"id":11,"origin_id":-10,"origin_slot":1,"target_id":63,"target_slot":0},
	           {"id":12,"origin_id":43,"origin_slot":0,"target_id":-20,"target_slot":0}]}]}}`
	def := func(s string) *NodeDef {
		var d NodeDef
		if err := json.Unmarshal([]byte(s), &d); err != nil {
			t.Fatal(err)
		}
		return &d
	}
	info := ObjectInfo{
		"CLIPTextEncode": def(`{"input":{"required":{"text":["STRING",{}],"clip":["CLIP"]}}}`),
		"CFGGuider":      def(`{"input":{"required":{"cfg":["FLOAT",{}],"model":["MODEL"]}}}`),
		"RandomNoise":    def(`{"input":{"required":{"noise_seed":["INT",{"control_after_generate":true}]}}}`),
		"VAEDecode":      def(`{"input":{"required":{"samples":["LATENT"]}}}`),
		"SaveImage":      def(`{"input":{"required":{"images":["IMAGE"],"filename_prefix":["STRING",{}]}},"output_node":true}`),
	}
	p, warn, err := Convert([]byte(ui), info)
	if err != nil || len(warn) > 0 {
		t.Fatal(err, warn)
	}
	if v := p["77:74"].Inputs["text"]; v != "a fox" {
		t.Errorf("text: %v", v)
	}
	if v := p["77:63"].Inputs["cfg"]; v != 3.5 {
		t.Errorf("cfg: %v", v)
	}
	if v := p["77:73"].Inputs["noise_seed"]; v != 42.0 {
		t.Errorf("seed: %v", v)
	}
	if id, slot, ok := Link(p["9"].Inputs["images"]); !ok || id != "77:43" || slot != 0 {
		t.Errorf("images: %v", p["9"].Inputs["images"])
	}
	roles := Detect(p, info)
	if len(roles) == 0 || roles[len(roles)-1].Kind != RoleOutput {
		t.Errorf("roles: %+v", roles)
	}
}

func TestConvertDynamicCombo(t *testing.T) {
	var info ObjectInfo
	_ = json.Unmarshal([]byte(`{"Render": {"input": {"required": {
		"geo": ["GEO", {}],
		"output": ["COMFY_DYNAMICCOMBO_V3", {"options": [
			{"key": "depth", "inputs": {"required": {"normalization": ["COMBO", {"options": ["v2", "minmax"]}], "sky_clip": ["BOOLEAN", {}]}}},
			{"key": "mask", "inputs": {"required": {"colored": ["BOOLEAN", {}]}}}]}],
		"scale": ["FLOAT", {}]}},
		"input_order": {"required": ["geo", "output", "scale"]}}}`), &info)
	ui := `{"nodes": [{"id": 1, "type": "Render", "inputs": [], "widgets_values": ["depth", "minmax", true, 2.5]},
		{"id": 2, "type": "Render", "inputs": [], "widgets_values": ["mask", false, 0.5]}], "links": []}`
	p, _, err := Convert([]byte(ui), info)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]map[string]any{
		"1": {"output": "depth", "output.normalization": "minmax", "output.sky_clip": true, "scale": 2.5},
		"2": {"output": "mask", "output.colored": false, "scale": 0.5},
	}
	for id, w := range want {
		for k, v := range w {
			if got := p[id].Inputs[k]; got != v {
				t.Errorf("node %s %s = %v, want %v (%v)", id, k, got, v, p[id].Inputs)
			}
		}
	}
}
