// Package comfy connects AudioViz to a ComfyUI server (the user starts it): its saved workflows
// and templates, converted to the API format (as its own interface does when it queues one),
// the places a flow takes a prompt, images, a seed… (its roles), and running one.
package comfy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Prompt: a workflow in ComfyUI's API format: node id → its class and inputs (a value, or a link
// [node id, output slot])
type Prompt map[string]*APINode

type APINode struct {
	Class  string         `json:"class_type"`
	Inputs map[string]any `json:"inputs"`
	Meta   map[string]any `json:"_meta,omitempty"`
}

// Title: the node's title (its class without one)
func (n *APINode) Title() string {
	if t, ok := n.Meta["title"].(string); ok && t != "" {
		return t
	}
	return n.Class
}

// ObjectInfo: ComfyUI's node definitions (GET /object_info)
type ObjectInfo map[string]*NodeDef

type NodeDef struct {
	Input struct {
		Required map[string]json.RawMessage `json:"required"`
		Optional map[string]json.RawMessage `json:"optional"`
	} `json:"input"`
	InputOrder struct {
		Required []string `json:"required"`
		Optional []string `json:"optional"`
	} `json:"input_order"`
	Output     []json.RawMessage `json:"output"`
	OutputNode bool              `json:"output_node"`
	Category   string            `json:"category"`
	Display    string            `json:"display_name"`
}

// dynamicCombo: a choice whose options bring inputs of their own ("parent.child" in the prompt)
const dynamicCombo = "COMFY_DYNAMICCOMBO_V3"

// raw: an input's definition; a dynamic combo's ("mode.strategy") looked up in its options
func (d *NodeDef) raw(name string) (json.RawMessage, bool) {
	if raw, ok := d.Input.Required[name]; ok {
		return raw, true
	}
	if raw, ok := d.Input.Optional[name]; ok {
		return raw, true
	}
	k := strings.LastIndex(name, ".")
	if k < 0 {
		return nil, false
	}
	parent, ok := d.raw(name[:k])
	if !ok {
		return nil, false
	}
	for _, o := range comboOptions(parent) {
		for _, m := range []map[string]json.RawMessage{o.Inputs.Required, o.Inputs.Optional} {
			if raw, ok := m[name[k+1:]]; ok {
				return raw, true
			}
		}
	}
	return nil, false
}

type comboOption struct {
	Key    string `json:"key"`
	Inputs struct {
		Required map[string]json.RawMessage `json:"required"`
		Optional map[string]json.RawMessage `json:"optional"`
	} `json:"inputs"`
	order []string // its inputs in their order
}

// comboOptions: a dynamic combo's options (none for another input)
func comboOptions(raw json.RawMessage) []comboOption {
	var arr []json.RawMessage
	if json.Unmarshal(raw, &arr) != nil || len(arr) < 2 {
		return nil
	}
	var typ string
	if json.Unmarshal(arr[0], &typ) != nil || typ != dynamicCombo {
		return nil
	}
	var o struct {
		Options []json.RawMessage `json:"options"`
	}
	_ = json.Unmarshal(arr[1], &o)
	var out []comboOption
	for _, r := range o.Options {
		var c comboOption
		if json.Unmarshal(r, &c) != nil {
			continue
		}
		var in struct {
			Inputs struct {
				Required json.RawMessage `json:"required"`
				Optional json.RawMessage `json:"optional"`
			} `json:"inputs"`
		}
		_ = json.Unmarshal(r, &in)
		c.order = append(objectKeys(in.Inputs.Required), objectKeys(in.Inputs.Optional)...)
		out = append(out, c)
	}
	return out
}

// objectKeys: a JSON object's keys in their order
func objectKeys(raw json.RawMessage) []string {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return nil
	}
	var keys []string
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return keys
		}
		k, _ := t.(string)
		keys = append(keys, k)
		var skip json.RawMessage
		if dec.Decode(&skip) != nil {
			return keys
		}
	}
	return keys
}

// dynamicChildren: the widgets a dynamic combo's chosen option brings ("parent.child", in order)
func (d *NodeDef) dynamicChildren(name string, chosen any) []string {
	raw, ok := d.raw(name)
	if !ok {
		return nil
	}
	key := fmt.Sprint(chosen)
	for _, o := range comboOptions(raw) {
		if o.Key != key {
			continue
		}
		var out []string
		for _, c := range o.order {
			if d.IsWidget(name + "." + c) {
				out = append(out, name+"."+c)
			}
		}
		return out
	}
	return nil
}

// spec: an input's definition: its type ("INT", "IMAGE", "COMBO"…), its options; ok: it exists
func (d *NodeDef) spec(name string) (typ string, opts map[string]any, ok bool) {
	raw, ok := d.raw(name)
	if !ok {
		return "", nil, false
	}
	var arr []json.RawMessage
	if json.Unmarshal(raw, &arr) != nil || len(arr) == 0 {
		return "", nil, true
	}
	if json.Unmarshal(arr[0], &typ) != nil {
		typ = "COMBO" // a list of choices
	}
	if len(arr) > 1 {
		_ = json.Unmarshal(arr[1], &opts)
	}
	return typ, opts, true
}

// IsWidget: an input set by a value (not a link from another node's output)
func (d *NodeDef) IsWidget(name string) bool {
	typ, _, ok := d.spec(name)
	if !ok {
		return false
	}
	switch typ {
	case "INT", "FLOAT", "STRING", "BOOLEAN", "BOOL", "COMBO", dynamicCombo:
		return true
	}
	return false
}

// order: its inputs in order (required, then optional)
func (d *NodeDef) order() []string {
	if len(d.InputOrder.Required)+len(d.InputOrder.Optional) > 0 {
		return append(append([]string(nil), d.InputOrder.Required...), d.InputOrder.Optional...)
	}
	var r, o []string
	for k := range d.Input.Required {
		r = append(r, k)
	}
	for k := range d.Input.Optional {
		o = append(o, k)
	}
	sort.Strings(r)
	sort.Strings(o)
	return append(r, o...)
}

// controlled: a widget followed by its "control after generate" value (a seed)
func (d *NodeDef) controlled(name string) bool {
	typ, opts, _ := d.spec(name)
	if c, ok := opts["control_after_generate"].(bool); ok {
		return c
	}
	return typ == "INT" && (name == "seed" || name == "noise_seed")
}

// ---- the interface's format

type uiGraph struct {
	Nodes       []*uiNode         `json:"nodes"`
	Links       []json.RawMessage `json:"links"`
	Definitions struct {
		Subgraphs []*uiSubgraph `json:"subgraphs"`
	} `json:"definitions"`
}

type uiNode struct {
	ID         any             `json:"id"`
	Type       string          `json:"type"`
	Mode       int             `json:"mode"`
	Title      string          `json:"title"`
	Inputs     []uiInput       `json:"inputs"`
	Outputs    []uiOutput      `json:"outputs"`
	Widgets    json.RawMessage `json:"widgets_values"`
	Properties map[string]any  `json:"properties"`
}

type uiInput struct {
	Name   string `json:"name"`
	Type   string `json:"type"`
	Link   *int   `json:"link"`
	Widget *struct {
		Name string `json:"name"`
	} `json:"widget"`
}

type uiOutput struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Links []int  `json:"links"`
}

type uiSubgraph struct {
	ID      string            `json:"id"`
	Name    string            `json:"name"`
	Inputs  []sgIO            `json:"inputs"`
	Outputs []sgIO            `json:"outputs"`
	Nodes   []*uiNode         `json:"nodes"`
	Links   []json.RawMessage `json:"links"`
}

type sgIO struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	LinkIDs []int  `json:"linkIds"`
}

type uiLink struct {
	ID                     int
	Origin, Target         string
	OriginSlot, TargetSlot int
	Type                   string
}

// a subgraph's own input / output node (in its links)
const (
	sgInputNode  = "-10"
	sgOutputNode = "-20"
)

func idStr(v any) string {
	switch t := v.(type) {
	case float64:
		return strconv.FormatInt(int64(t), 10)
	case string:
		return t
	case json.Number:
		return t.String()
	}
	return fmt.Sprint(v)
}

// parseLinks: links as arrays [id, origin, slot, target, slot, type] or as objects
func parseLinks(raws []json.RawMessage) map[int]uiLink {
	out := map[int]uiLink{}
	for _, r := range raws {
		var a []any
		if json.Unmarshal(r, &a) == nil && len(a) >= 5 {
			l := uiLink{ID: int(num(a[0])), Origin: idStr(a[1]), OriginSlot: int(num(a[2])), Target: idStr(a[3]), TargetSlot: int(num(a[4]))}
			if len(a) > 5 {
				l.Type, _ = a[5].(string)
			}
			out[l.ID] = l
			continue
		}
		var o struct {
			ID         int `json:"id"`
			OriginID   any `json:"origin_id"`
			OriginSlot int `json:"origin_slot"`
			TargetID   any `json:"target_id"`
			TargetSlot int `json:"target_slot"`
			Type       any `json:"type"`
		}
		if json.Unmarshal(r, &o) == nil {
			l := uiLink{ID: o.ID, Origin: idStr(o.OriginID), OriginSlot: o.OriginSlot, Target: idStr(o.TargetID), TargetSlot: o.TargetSlot}
			l.Type, _ = o.Type.(string)
			out[l.ID] = l
		}
	}
	return out
}

func num(v any) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case string:
		f, _ := strconv.ParseFloat(t, 64)
		return f
	}
	return 0
}

// scope: a graph level (the top or a subgraph's instance): its id prefix, nodes, links; the
// instance it belongs to and its parent scope
type scope struct {
	prefix string
	nodes  map[string]*uiNode
	links  map[int]uiLink
	inst   *uiNode
	sg     *uiSubgraph // (its instance's subgraph)
	parent *scope
	// the values its instance shows of its nodes' widgets (proxyWidgets): node id → name → value
	proxy map[string]map[string]any
}

// IsUIFormat: whether a workflow JSON is the interface's (nodes + links) rather than the API's
func IsUIFormat(b []byte) bool {
	var g struct {
		Nodes []json.RawMessage `json:"nodes"`
	}
	return json.Unmarshal(b, &g) == nil && len(g.Nodes) > 0
}

// Convert: a workflow saved by ComfyUI's interface as an API prompt (as it queues one): its
// subgraphs flattened (their nodes "outer:inner"), bypassed nodes passed through, reroutes and
// primitives resolved, notes left out; widgets_values matched to the inputs by the node
// definitions. warnings: what was left out (nodes it doesn't know…).
func Convert(b []byte, info ObjectInfo) (Prompt, []string, error) {
	var g uiGraph
	if err := json.Unmarshal(b, &g); err != nil {
		return nil, nil, err
	}
	subs := map[string]*uiSubgraph{}
	for _, s := range g.Definitions.Subgraphs {
		subs[s.ID] = s
	}
	c := &converter{info: info, subs: subs, out: Prompt{}}
	top := &scope{nodes: map[string]*uiNode{}, links: parseLinks(g.Links)}
	for _, n := range g.Nodes {
		top.nodes[idStr(n.ID)] = n
	}
	c.walk(top, 0)
	if len(c.out) == 0 {
		return nil, c.warn, fmt.Errorf("no nodes ComfyUI knows in it")
	}
	return c.out, c.warn, nil
}

type converter struct {
	info ObjectInfo
	subs map[string]*uiSubgraph
	out  Prompt
	warn []string
}

// virtual: nodes of the interface only
func virtual(t string) bool {
	switch t {
	case "Reroute", "PrimitiveNode", "Note", "MarkdownNote", "PrimitiveString", "Label (rgthree)", "Fast Groups Bypasser (rgthree)", "Fast Groups Muter (rgthree)":
		return t != "PrimitiveString"
	}
	return false
}

func (c *converter) walk(s *scope, depth int) {
	if depth > 16 {
		c.warn = append(c.warn, "subgraphs nested too deep")
		return
	}
	ids := make([]string, 0, len(s.nodes))
	for id := range s.nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		n := s.nodes[id]
		if n.Mode == 2 || n.Mode == 4 || virtual(n.Type) {
			continue // muted, bypassed (passed through where linked), or of the interface only
		}
		if sg := c.subs[n.Type]; sg != nil {
			c.walk(c.inner(s, n, sg), depth+1)
			continue
		}
		def := c.info[n.Type]
		if def == nil {
			c.warn = append(c.warn, fmt.Sprintf("node %s (%s) is not installed in ComfyUI: left out", s.prefix+id, n.Type))
			continue
		}
		an := &APINode{Class: n.Type, Inputs: map[string]any{}, Meta: map[string]any{"title": firstNonEmpty(n.Title, def.Display, n.Type)}}
		c.widgets(n, def, an)
		for k, v := range s.proxy[id] {
			if _, _, ok := def.spec(k); ok {
				an.Inputs[k] = v // as its instance shows it
			}
		}
		for i, in := range n.Inputs {
			if in.Link == nil {
				continue
			}
			if v, ok := c.resolveInput(s, n, i); ok {
				an.Inputs[in.Name] = v
			}
		}
		c.out[s.prefix+id] = an
	}
}

func firstNonEmpty(xs ...string) string {
	for _, x := range xs {
		if x != "" {
			return x
		}
	}
	return ""
}

// inner: the scope of a subgraph's instance n (in s)
func (c *converter) inner(s *scope, n *uiNode, sg *uiSubgraph) *scope {
	in := &scope{prefix: s.prefix + idStr(n.ID) + ":", nodes: map[string]*uiNode{}, links: parseLinks(sg.Links), inst: n, sg: sg, parent: s,
		proxy: map[string]map[string]any{}}
	for _, m := range sg.Nodes {
		in.nodes[idStr(m.ID)] = m
	}
	vals, pw := proxyWidgets(n)
	for k, p := range pw {
		if k >= len(vals) || p[0] == "-1" || p[1] == "control_after_generate" || strings.Contains(p[1], ":") {
			continue
		}
		if in.proxy[p[0]] == nil {
			in.proxy[p[0]] = map[string]any{}
		}
		in.proxy[p[0]][p[1]] = vals[k]
	}
	return in
}

// proxyWidgets: an instance's widget values and which inner widget each one shows ([node id,
// name]; "-1": one of the subgraph's own inputs)
func proxyWidgets(n *uiNode) ([]any, [][2]string) {
	var vals []any
	_ = json.Unmarshal(n.Widgets, &vals)
	raw, _ := n.Properties["proxyWidgets"].([]any)
	var out [][2]string
	for _, r := range raw {
		a, _ := r.([]any)
		if len(a) == 2 {
			out = append(out, [2]string{idStr(a[0]), fmt.Sprint(a[1])})
		}
	}
	// (a value per entry, in their order; the last ones may have none: the seed it doesn't show)
	if len(out) < len(vals) {
		return vals, nil // more values than entries: not trusted
	}
	return vals, out
}

// widgets: n's widget values as its inputs: in the order of its widget inputs (the interface's),
// else of its definition; a seed's "control after generate" and an upload button's value skipped
func (c *converter) widgets(n *uiNode, def *NodeDef, an *APINode) {
	var byName map[string]any
	if json.Unmarshal(n.Widgets, &byName) == nil && len(byName) > 0 {
		for k, v := range byName {
			if def.IsWidget(k) {
				an.Inputs[k] = v
			}
		}
		return
	}
	var vals []any
	if json.Unmarshal(n.Widgets, &vals) != nil || len(vals) == 0 {
		return
	}
	// its widgets in order: its definition's (the interface makes them so), with the ones of the
	// interface only (an upload button…) where the node lists them
	var names []string
	for _, k := range def.order() {
		if def.IsWidget(k) {
			names = append(names, k)
		}
	}
	var listed []string
	for _, in := range n.Inputs {
		if in.Widget != nil {
			listed = append(listed, in.Widget.Name)
		}
	}
	for j, w := range listed {
		if _, _, ok := def.spec(w); ok {
			continue
		}
		at := 0 // after the one before it in the node's list
		for b := j - 1; b >= 0; b-- {
			if k := indexOf(names, listed[b]); k >= 0 {
				at = k + 1
				break
			}
		}
		names = append(names[:at], append([]string{w}, names[at:]...)...)
	}
	i := 0
	for x := 0; x < len(names); x++ {
		name := names[x]
		if i >= len(vals) {
			break
		}
		v := vals[i]
		i++
		typ, opts, inSpec := def.spec(name)
		if inSpec {
			an.Inputs[name] = v
		}
		if typ == dynamicCombo {
			// the chosen option's widgets come next (unless the node lists them already)
			var add []string
			for _, c := range def.dynamicChildren(name, v) {
				if indexOf(names, c) < 0 {
					add = append(add, c)
				}
			}
			names = append(names[:x+1], append(add, names[x+1:]...)...)
		}
		if def.controlled(name) && i < len(vals) {
			if s, ok := vals[i].(string); ok && (s == "fixed" || s == "increment" || s == "decrement" || s == "randomize") {
				i++
			}
		}
		// an upload button's value the node doesn't list ("image" after the file's name)
		if inSpec && indexOf(listed, "upload") < 0 && i < len(vals) {
			for _, u := range []string{"image_upload", "video_upload", "audio_upload", "animated_image_upload"} {
				if b, _ := opts[u].(bool); b {
					if s, ok := vals[i].(string); ok && (s == "image" || s == "video" || s == "audio") {
						i++
					}
					break
				}
			}
		}
	}
}

func indexOf(xs []string, x string) int {
	for i, y := range xs {
		if y == x {
			return i
		}
	}
	return -1
}

// resolveInput: what input i of node n (in s) receives: a link to a real node's output
// [id, slot], or a value (a primitive, a subgraph input given as a value)
func (c *converter) resolveInput(s *scope, n *uiNode, i int) (any, bool) {
	in := n.Inputs[i]
	if in.Link == nil {
		return nil, false
	}
	return c.resolveLink(s, *in.Link, 0)
}

func (c *converter) resolveLink(s *scope, linkID, depth int) (any, bool) {
	if depth > 64 {
		return nil, false
	}
	l, ok := s.links[linkID]
	if !ok {
		return nil, false
	}
	return c.resolveOrigin(s, l.Origin, l.OriginSlot, l.Type, depth+1)
}

// resolveOrigin: output slot of node origin (in s)
func (c *converter) resolveOrigin(s *scope, origin string, slot int, typ string, depth int) (any, bool) {
	if depth > 64 {
		return nil, false
	}
	if origin == sgInputNode {
		// the subgraph's input: what its instance receives there (a link outside, or a value)
		// (by its name: the instance may list only some of them)
		inst, ps := s.inst, s.parent
		if inst == nil || ps == nil || s.sg == nil || slot >= len(s.sg.Inputs) {
			return nil, false
		}
		name := s.sg.Inputs[slot].Name
		for _, in := range inst.Inputs {
			if in.Name != name {
				continue
			}
			if in.Link != nil {
				return c.resolveLink(ps, *in.Link, depth)
			}
			break
		}
		return c.promoted(inst, name)
	}
	n := s.nodes[origin]
	if n == nil {
		return nil, false
	}
	switch {
	case n.Mode == 2:
		return nil, false
	case n.Mode == 4:
		// bypassed: what it receives of that type (the same slot's first)
		for pass := 0; pass < 2; pass++ {
			for j, in := range n.Inputs {
				if in.Link == nil || (pass == 0 && (j != slot || (typ != "" && in.Type != typ))) || (pass == 1 && typ != "" && in.Type != typ) {
					continue
				}
				return c.resolveLink(s, *in.Link, depth)
			}
		}
		return nil, false
	case n.Type == "Reroute":
		if len(n.Inputs) > 0 && n.Inputs[0].Link != nil {
			return c.resolveLink(s, *n.Inputs[0].Link, depth)
		}
		return nil, false
	case n.Type == "PrimitiveNode":
		var vals []any
		if json.Unmarshal(n.Widgets, &vals) == nil && len(vals) > 0 {
			return vals[0], true
		}
		return nil, false
	}
	if sg := c.subs[n.Type]; sg != nil {
		// a subgraph's output: what reaches its output node inside
		if slot >= len(sg.Outputs) || len(sg.Outputs[slot].LinkIDs) == 0 {
			return nil, false
		}
		in := c.inner(s, n, sg)
		return c.resolveLink(in, sg.Outputs[slot].LinkIDs[0], depth)
	}
	return []any{s.prefix + origin, slot}, true
}

// promoted: the value an instance gives one of its subgraph's inputs (a widget on it: its
// widgets_values in the order of its widget inputs / proxyWidgets "-1")
func (c *converter) promoted(inst *uiNode, name string) (any, bool) {
	vals, pw := proxyWidgets(inst)
	for k, p := range pw {
		if p[0] == "-1" && p[1] == name && k < len(vals) {
			return vals[k], true
		}
	}
	k := 0
	for _, in := range inst.Inputs {
		if in.Widget == nil {
			continue
		}
		if in.Name == name || in.Widget.Name == name {
			if k < len(vals) {
				return vals[k], true
			}
			return nil, false
		}
		k++
	}
	return nil, false
}

// Links: the [id, slot] an input value is, if it is a link
func Link(v any) (string, int, bool) {
	a, ok := v.([]any)
	if !ok || len(a) != 2 {
		return "", 0, false
	}
	id, ok1 := a[0].(string)
	if !ok1 {
		id = idStr(a[0])
	}
	return id, int(num(a[1])), true
}

// Clean: a name for a flow from a file name
func Clean(name string) string {
	name = strings.TrimSuffix(name, ".json")
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	return strings.TrimSpace(strings.ReplaceAll(name, "_", " "))
}
