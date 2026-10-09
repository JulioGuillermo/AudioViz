package comfy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"sync"
	"time"
)

// Flow: a ComfyUI workflow kept in a project: as ComfyUI's interface saved it (when it came so),
// in the API format, and its roles
type Flow struct {
	ID       string          `json:"id"`
	Name     string          `json:"name"`
	Kind     string          `json:"kind"`             // image, edit, depth, video
	Source   string          `json:"source,omitempty"` // where it came from (workflows/…, template:…, a file)
	UI       json.RawMessage `json:"ui,omitempty"`
	API      Prompt          `json:"api"`
	Roles    []Role          `json:"roles"`
	Warnings []string        `json:"warnings,omitempty"`
}

// Kinds of flows (what AudioViz does with what they make)
const (
	KindImage = "image" // an image from a prompt
	KindEdit  = "edit"  // an image from images and a prompt
	KindDepth = "depth" // a depth map of an image
	KindVideo = "video"
)

// GuessKind: what a flow most likely makes, from its roles and nodes
func GuessKind(p Prompt, roles []Role) string {
	images := 0
	for _, r := range roles {
		if r.Kind == RoleImage {
			images++
		}
	}
	for _, n := range p {
		c := strings.ToLower(n.Class)
		if strings.Contains(c, "videocombine") || c == "savevideo" || strings.Contains(c, "savewebm") {
			return KindVideo
		}
	}
	for _, n := range p {
		c := strings.ToLower(n.Class + " " + n.Title())
		if strings.Contains(c, "depth") && images > 0 {
			return KindDepth
		}
	}
	if images > 0 {
		return KindEdit
	}
	return KindImage
}

// Import: a workflow (the interface's or the API's JSON) as a flow: converted, its roles detected
func Import(b []byte, name, source string, info ObjectInfo) (*Flow, error) {
	f := &Flow{Name: name, Source: source}
	if IsUIFormat(b) {
		p, warn, err := Convert(b, info)
		if err != nil {
			return nil, err
		}
		f.UI, f.API, f.Warnings = json.RawMessage(b), p, warn
	} else {
		var p Prompt
		if err := json.Unmarshal(b, &p); err != nil || len(p) == 0 {
			return nil, errors.New("not a ComfyUI workflow")
		}
		for id, n := range p {
			if n == nil || n.Class == "" {
				return nil, fmt.Errorf("node %s has no class: not a ComfyUI workflow", id)
			}
		}
		f.API = p
	}
	f.Roles = Detect(f.API, info)
	f.Kind = GuessKind(f.API, f.Roles)
	return f, nil
}

// Values: what a run gives a flow: by role kind (prompt, negative, seed, width, height), its
// images (in the order of its image roles: names already in ComfyUI's input folder), and the
// exposed params ("node|input" → value)
type Values struct {
	Prompt   *string        `json:"prompt,omitempty"`
	Negative *string        `json:"negative,omitempty"`
	Seed     *int64         `json:"seed,omitempty"` // nil: a new one each run
	Width    int            `json:"width,omitempty"`
	Height   int            `json:"height,omitempty"`
	Images   []string       `json:"images,omitempty"`
	Params   map[string]any `json:"params,omitempty"`
}

// Build: the flow's prompt with v in its roles (a copy; its seed: a random one when v has none)
func (f *Flow) Build(v Values) (Prompt, int64, error) {
	b, _ := json.Marshal(f.API)
	var p Prompt
	_ = json.Unmarshal(b, &p)
	set := func(node, input string, val any) error {
		n := p[node]
		if n == nil {
			return fmt.Errorf("the flow has no node %s (its roles need fixing)", node)
		}
		n.Inputs[input] = val
		return nil
	}
	seed := rand.Int64N(1 << 50)
	if v.Seed != nil {
		seed = *v.Seed
	}
	img := 0
	for _, r := range f.Roles {
		var err error
		switch r.Kind {
		case RolePrompt:
			if v.Prompt != nil {
				err = set(r.Node, r.Input, *v.Prompt)
			}
		case RoleNegative:
			if v.Negative != nil {
				err = set(r.Node, r.Input, *v.Negative)
			}
		case RoleSeed:
			err = set(r.Node, r.Input, seed)
		case RoleWidth:
			if v.Width > 0 {
				err = set(r.Node, r.Input, v.Width)
			}
		case RoleHeight:
			if v.Height > 0 {
				err = set(r.Node, r.Input, v.Height)
			}
		case RoleImage:
			if img < len(v.Images) && v.Images[img] != "" {
				err = set(r.Node, r.Input, v.Images[img])
			}
			img++
		case RoleParam:
			if val, ok := v.Params[r.Node+"|"+r.Input]; ok {
				err = set(r.Node, r.Input, val)
			}
		}
		if err != nil {
			return nil, seed, err
		}
	}
	return p, seed, nil
}

// OutputNode: the node its result is taken from ("": all of them)
func (f *Flow) OutputNode() string {
	for _, r := range f.Roles {
		if r.Kind == RoleOutput {
			return r.Node
		}
	}
	return ""
}

// Progress: how far a run is (0…1) and what it is doing; or (msg "") a preview of the image forming
type Progress func(frac float64, msg string, preview []byte)

// Run: prompt p queued and followed until it ends: the files its output node made
func (c *Client) Run(ctx context.Context, p Prompt, outNode string, progress Progress) ([]File, error) {
	clientID := NewClientID()
	type end struct{ err error }
	done := make(chan end, 1)
	var once sync.Once
	finish := func(err error) { once.Do(func() { done <- end{err} }) }
	var mu sync.Mutex
	var promptID string
	// how far: before the steps up to 10 %, the steps (of a sampler, the slow part) to 90 %, then on
	nodes, seen := len(p), map[string]bool{}
	stepped := false
	lctx, stop := context.WithCancel(ctx)
	defer stop()
	ready := make(chan struct{})
	go func() {
		err := c.Listen(lctx, clientID, func(e Event) {
			mu.Lock()
			pid := promptID
			mu.Unlock()
			if id, _ := e.Data["prompt_id"].(string); id != "" && pid != "" && id != pid {
				return
			}
			switch e.Type {
			case "status":
				select {
				case <-ready:
				default:
					close(ready)
				}
			case "executing":
				node, _ := e.Data["node"].(string)
				if node == "" && pid != "" {
					finish(nil)
					return
				}
				seen[node] = true
				if n := p[node]; n != nil && progress != nil {
					f := 0.1 * float64(len(seen)-1) / float64(max(nodes, 1))
					if stepped {
						f = 0.9 + 0.09*float64(len(seen)-1)/float64(max(nodes, 1))
					}
					progress(f, n.Title(), nil)
				}
			case "progress":
				if progress != nil {
					v, _ := e.Data["value"].(float64)
					m, _ := e.Data["max"].(float64)
					node, _ := e.Data["node"].(string)
					title := ""
					if n := p[node]; n != nil {
						title = n.Title()
					}
					f := 0.1
					if m > 0 {
						f = 0.1 + 0.8*v/m
					}
					stepped = true
					progress(f, fmt.Sprintf("%s %d/%d", title, int(v), int(m)), nil)
				}
			case "preview":
				if img, _ := e.Data["image"].([]byte); img != nil && progress != nil {
					progress(-1, "", img)
				}
			case "execution_success":
				finish(nil)
			case "execution_error":
				msg, _ := e.Data["exception_message"].(string)
				nt, _ := e.Data["node_type"].(string)
				finish(fmt.Errorf("ComfyUI failed in %s: %s", nt, strings.TrimSpace(msg)))
			case "execution_interrupted":
				finish(errors.New("cancelled in ComfyUI"))
			}
		})
		if err != nil && lctx.Err() == nil {
			select {
			case <-ready:
			default:
				close(ready)
			}
		}
	}()
	select { // (its websocket listening before queueing, a moment at most)
	case <-ready:
	case <-time.After(3 * time.Second):
	}
	id, err := c.Queue(ctx, p, clientID)
	if err != nil {
		return nil, err
	}
	mu.Lock()
	promptID = id
	mu.Unlock()
	// the websocket's end, or the history's (when the websocket can't be had)
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			_ = c.Interrupt(context.Background())
			return nil, ctx.Err()
		case e := <-done:
			if e.err != nil {
				return nil, e.err
			}
			return c.Outputs(ctx, id, outNode)
		case <-tick.C:
			if fs, err := c.Outputs(ctx, id, outNode); err == nil && len(fs) > 0 {
				return fs, nil
			}
		}
	}
}
