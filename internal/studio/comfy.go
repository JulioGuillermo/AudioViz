package studio

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"audioviz/internal/comfy"
)

// The project's ComfyUI flows (internal/comfy): the server it runs on (the project's comfy.url;
// the user starts it), the flows kept in <project>/comfy/<id>.json, browsing what ComfyUI has
// (the workflows saved in it, its templates) to import them, editing their roles, running them.

const comfyDefaultURL = "http://127.0.0.1:8188"

var (
	comfyInfoMu sync.Mutex
	comfyInfo   = map[string]comfy.ObjectInfo{} // its node definitions, per server (once)
)

func (s *Server) comfyClient() (*comfy.Client, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u := comfyDefaultURL
	if s.cfg != nil && s.cfg.Comfy.URL != "" {
		u = s.cfg.Comfy.URL
	}
	return comfy.New(u), u
}

func comfyObjectInfo(ctx context.Context, c *comfy.Client, fresh bool) (comfy.ObjectInfo, error) {
	comfyInfoMu.Lock()
	info := comfyInfo[c.Base]
	comfyInfoMu.Unlock()
	if info != nil && !fresh {
		return info, nil
	}
	info, err := c.ObjectInfo(ctx)
	if err != nil {
		return nil, err
	}
	comfyInfoMu.Lock()
	comfyInfo[c.Base] = info
	comfyInfoMu.Unlock()
	return info, nil
}

func (s *Server) comfyDir() string {
	d := s.chatDir() // (the project's folder)
	if d == "" {
		return ""
	}
	return filepath.Join(d, "comfy")
}

var flowIDRe = regexp.MustCompile(`[^a-z0-9]+`)

func flowID(name string) string {
	id := strings.Trim(flowIDRe.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if len(id) > 40 {
		id = id[:40]
	}
	if id == "" {
		id = "flow"
	}
	return id
}

func (s *Server) loadFlow(id string) (*comfy.Flow, error) {
	d := s.comfyDir()
	if d == "" {
		return nil, errors.New("no project is open")
	}
	b, err := os.ReadFile(filepath.Join(d, filepath.Base(id)+".json"))
	if err != nil {
		return nil, fmt.Errorf("no flow %q", id)
	}
	var f comfy.Flow
	return &f, json.Unmarshal(b, &f)
}

func (s *Server) saveFlow(f *comfy.Flow) error {
	d := s.comfyDir()
	if d == "" {
		return errors.New("no project is open")
	}
	if err := os.MkdirAll(d, 0o755); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(f, "", " ")
	return os.WriteFile(filepath.Join(d, f.ID+".json"), b, 0o644)
}

// hComfyStatus: whether ComfyUI answers at the project's address (its version, devices)
func (s *Server) hComfyStatus(w http.ResponseWriter, r *http.Request) {
	c, u := s.comfyClient()
	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
	defer cancel()
	st, err := c.Stats(ctx)
	out := map[string]any{"url": u, "ok": err == nil}
	if err != nil {
		out["error"] = err.Error()
	} else {
		out["system"] = st["system"]
		out["devices"] = st["devices"]
	}
	writeJSON(w, out)
}

// hComfyBrowse: what ComfyUI has to import: ?src=workflows (saved in it) or templates
func (s *Server) hComfyBrowse(w http.ResponseWriter, r *http.Request) {
	c, _ := s.comfyClient()
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	if r.URL.Query().Get("src") == "templates" {
		ts, err := c.Templates(ctx)
		if err != nil {
			fail(w, 502, err)
			return
		}
		writeJSON(w, map[string]any{"templates": ts})
		return
	}
	ws, err := c.UserWorkflows(ctx)
	if err != nil {
		fail(w, 502, err)
		return
	}
	sort.Strings(ws)
	writeJSON(w, map[string]any{"workflows": ws})
}

// hComfyThumb: a template's picture (from ComfyUI)
func (s *Server) hComfyThumb(w http.ResponseWriter, r *http.Request) {
	c, _ := s.comfyClient()
	b, ct, err := c.TemplateThumb(r.Context(), r.URL.Query().Get("name"), r.URL.Query().Get("sub"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "max-age=86400")
	w.Write(b)
}

// hComfyImport: {src: workflow | template | json, path | name | json, title}: a flow added to the
// project (converted, its roles proposed)
func (s *Server) hComfyImport(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Src   string          `json:"src"`
		Path  string          `json:"path"`
		Name  string          `json:"name"`
		JSON  json.RawMessage `json:"json"`
		Title string          `json:"title"`
	}
	if err := readJSON(r, &req); err != nil {
		fail(w, 400, err)
		return
	}
	c, _ := s.comfyClient()
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	var b []byte
	var err error
	name, source := req.Title, ""
	switch req.Src {
	case "workflow":
		b, err = c.UserWorkflow(ctx, req.Path)
		name, source = firstNonEmptyS(name, comfy.Clean(req.Path)), "workflows/"+req.Path
	case "template":
		b, err = c.TemplateJSON(ctx, req.Name)
		name, source = firstNonEmptyS(name, comfy.Clean(req.Name)), "template:"+req.Name
	default:
		b, source = req.JSON, "file"
		name = firstNonEmptyS(name, "Flow")
	}
	if err != nil {
		fail(w, 502, err)
		return
	}
	info, err := comfyObjectInfo(ctx, c, true)
	if err != nil {
		fail(w, 502, fmt.Errorf("ComfyUI's nodes: %w", err))
		return
	}
	f, err := comfy.Import(b, name, source, info)
	if err != nil {
		fail(w, 400, err)
		return
	}
	// a free id in the project
	base := flowID(name)
	f.ID = base
	for k := 2; ; k++ {
		if _, err := os.Stat(filepath.Join(s.comfyDir(), f.ID+".json")); err != nil {
			break
		}
		f.ID = fmt.Sprintf("%s-%d", base, k)
	}
	if err := s.saveFlow(f); err != nil {
		fail(w, 500, err)
		return
	}
	s.hub.send("comfy", map[string]any{"flow": f.ID})
	writeJSON(w, flowView(f, true, info))
}

func firstNonEmptyS(xs ...string) string {
	for _, x := range xs {
		if x != "" {
			return x
		}
	}
	return ""
}

// flowView: a flow for the studio (its nodes in brief for editing its roles, with full); with
// ComfyUI's nodes (info), what keeps it from running there ("issues"; none: it can)
func flowView(f *comfy.Flow, full bool, info comfy.ObjectInfo) map[string]any {
	v := map[string]any{"id": f.ID, "name": f.Name, "kind": f.Kind, "source": f.Source, "roles": f.Roles, "warnings": f.Warnings}
	if info != nil {
		v["issues"] = append([]comfy.Issue{}, f.Check(info)...)
	}
	if full {
		type nodeView struct {
			ID     string         `json:"id"`
			Class  string         `json:"class"`
			Title  string         `json:"title"`
			Inputs map[string]any `json:"inputs"` // its values (links left out)
			Output bool           `json:"output,omitempty"` // it saves or shows a result
		}
		var ns []nodeView
		for id, n := range f.API {
			in := map[string]any{}
			for k, x := range n.Inputs {
				if _, _, isLink := comfy.Link(x); !isLink {
					in[k] = x
				}
			}
			out := strings.HasPrefix(n.Class, "Save") || strings.HasPrefix(n.Class, "Preview") || strings.Contains(n.Class, "VideoCombine")
			if d := info[n.Class]; d != nil && d.OutputNode {
				out = true
			}
			ns = append(ns, nodeView{id, n.Class, n.Title(), in, out})
		}
		sort.Slice(ns, func(i, j int) bool { return ns[i].Title < ns[j].Title })
		v["nodes"] = ns
	}
	return v
}

// comfyInfoNow: ComfyUI's nodes to check flows against (nil: it isn't answering); fresh: asked
// again (a model installed since shows up)
func (s *Server) comfyInfoNow(r *http.Request, fresh bool) comfy.ObjectInfo {
	c, _ := s.comfyClient()
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	info, _ := comfyObjectInfo(ctx, c, fresh)
	return info
}

// hComfyFlows: the project's flows (?fresh=1: checked against ComfyUI asked again)
func (s *Server) hComfyFlows(w http.ResponseWriter, r *http.Request) {
	info := s.comfyInfoNow(r, r.URL.Query().Get("fresh") != "")
	d := s.comfyDir()
	out := []map[string]any{}
	if d != "" {
		es, _ := os.ReadDir(d)
		for _, e := range es {
			if !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			if f, err := s.loadFlow(strings.TrimSuffix(e.Name(), ".json")); err == nil {
				out = append(out, flowView(f, false, info))
			}
		}
	}
	writeJSON(w, map[string]any{"flows": out})
}

// hComfyFlow: GET one flow (with its nodes); PUT {id, name, kind, roles}; DELETE ?id=
func (s *Server) hComfyFlow(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		f, err := s.loadFlow(r.URL.Query().Get("id"))
		if err != nil {
			fail(w, 404, err)
			return
		}
		writeJSON(w, flowView(f, true, s.comfyInfoNow(r, false)))
	case "PUT":
		var req struct {
			ID    string       `json:"id"`
			Name  string       `json:"name"`
			Kind  string       `json:"kind"`
			Roles []comfy.Role `json:"roles"`
		}
		if err := readJSON(r, &req); err != nil {
			fail(w, 400, err)
			return
		}
		f, err := s.loadFlow(req.ID)
		if err != nil {
			fail(w, 404, err)
			return
		}
		if req.Name != "" {
			f.Name = req.Name
		}
		if req.Kind != "" {
			f.Kind = req.Kind
		}
		if req.Roles != nil {
			for _, ro := range req.Roles {
				if f.API[ro.Node] == nil {
					fail(w, 400, fmt.Errorf("the flow has no node %s", ro.Node))
					return
				}
			}
			f.Roles = req.Roles
		}
		if err := s.saveFlow(f); err != nil {
			fail(w, 500, err)
			return
		}
		s.hub.send("comfy", map[string]any{"flow": f.ID})
		writeJSON(w, flowView(f, true, s.comfyInfoNow(r, false)))
	case "DELETE":
		d := s.comfyDir()
		id := filepath.Base(r.URL.Query().Get("id"))
		if d == "" || id == "" || os.Remove(filepath.Join(d, id+".json")) != nil {
			fail(w, 404, errors.New("no such flow"))
			return
		}
		s.hub.send("comfy", map[string]any{"flow": id})
		writeJSON(w, map[string]bool{"ok": true})
	}
}

// hComfyRun: {id, values, images: [project files]}: the flow run in ComfyUI (a job); its result
// (an image) saved in the project's images
func (s *Server) hComfyRun(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID     string       `json:"id"`
		Values comfy.Values `json:"values"`
		Images []string     `json:"images"`
	}
	if err := readJSON(r, &req); err != nil {
		fail(w, 400, err)
		return
	}
	f, err := s.loadFlow(req.ID)
	if err != nil {
		fail(w, 404, err)
		return
	}
	s.mu.Lock()
	cfg := s.cfg
	s.mu.Unlock()
	if cfg == nil {
		fail(w, 400, errors.New("no project is open"))
		return
	}
	c, _ := s.comfyClient()
	dir := filepath.Join(cfg.Dir, imagesDir)
	j, err := s.startJob("comfy", func(ctx context.Context, j *job) error {
		t0 := time.Now()
		say := func(p float64, msg string) {
			j.mu.Lock()
			j.Progress, j.Message = p, msg
			j.mu.Unlock()
			s.hub.send("job", j.snapshot())
		}
		say(0, "ComfyUI · "+f.Name)
		// the input images uploaded to ComfyUI
		v := req.Values
		for _, p := range req.Images {
			if p == "" {
				v.Images = append(v.Images, "")
				continue
			}
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			name, err := c.Upload(ctx, filepath.Base(p), b)
			if err != nil {
				return err
			}
			v.Images = append(v.Images, name)
		}
		p, seed, err := f.Build(v)
		if err != nil {
			return err
		}
		previewMu.Lock()
		previewJPG = nil
		previewMu.Unlock()
		files, err := c.Run(ctx, p, f.OutputNode(), func(frac float64, msg string, img []byte) {
			if img != nil {
				previewMu.Lock()
				previewJPG = img
				previewMu.Unlock()
				j.mu.Lock()
				j.Previews++
				j.mu.Unlock()
				s.hub.send("job", j.snapshot())
				return
			}
			say(0.02+0.9*frac, "ComfyUI · "+msg)
		})
		if err != nil {
			return err
		}
		if len(files) == 0 {
			return errors.New("ComfyUI made nothing in the flow's output node (check its roles)")
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		prompt := ""
		if v.Prompt != nil {
			prompt = *v.Prompt
		}
		var outs []string
		for k, fl := range files {
			b, err := c.Download(ctx, fl)
			if err != nil {
				return err
			}
			ext := strings.ToLower(filepath.Ext(fl.Filename))
			if ext == "" {
				ext = ".png"
			}
			out := filepath.Join(dir, strings.TrimSuffix(imageName(firstNonEmptyS(prompt, f.Name), time.Now()), ".png")+fmt.Sprintf("-%d", k)+ext)
			if err := os.WriteFile(out, b, 0o644); err != nil {
				return err
			}
			meta := genImage{Prompt: prompt, Model: "ComfyUI · " + f.Name, Seed: seed, Elapsed: time.Since(t0).Seconds(), Created: time.Now(),
				Refs: req.Images, Width: v.Width, Height: v.Height}
			if v.Negative != nil {
				meta.Negative = *v.Negative
			}
			if mb, err := json.MarshalIndent(meta, "", "  "); err == nil {
				_ = os.WriteFile(strings.TrimSuffix(out, ext)+".json", mb, 0o644)
			}
			outs = append(outs, out)
		}
		j.mu.Lock()
		j.Output = outs[0]
		j.Message = fmt.Sprintf("ComfyUI · %s: done in %s", f.Name, fmtSecs(time.Since(t0).Seconds()))
		j.mu.Unlock()
		return nil
	})
	if err != nil {
		fail(w, 409, err)
		return
	}
	writeJSON(w, j.snapshot())
}
