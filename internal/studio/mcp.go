package studio

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"audioviz/docs"
)

// The MCP server (Model Context Protocol): agents (Claude Code, Claude Desktop, any MCP client)
// drive the studio remotely over HTTP — Streamable HTTP at /mcp, and the older SSE transport at
// /sse (+ /messages). Its tools go through the studio's own API (the same handlers the web app
// calls; its edits are operations, as everyone's: collab.go): what they change is saved and
// shows at once in every studio with the project open. With a token (studio --mcp-token, or AUDIOVIZ_MCP_TOKEN) every request must carry it
// (Authorization: Bearer …, or ?token=…); without one, only requests from no web page or from
// the studio's own origin are taken (a page elsewhere can't drive it).

// the protocol versions it speaks, newest first
var mcpProtocols = []string{"2025-06-18", "2025-03-26", "2024-11-05"}

type mcpServer struct {
	s     *Server
	mux   http.Handler // the studio's API, called in-process
	token string
	mu    sync.Mutex
	sse   map[string]chan []byte // the SSE transport's sessions
}

type rpcMsg struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResp struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcErr         `json:"error,omitempty"`
}

// mountMCP adds the MCP endpoints to the studio's server
func (s *Server) mountMCP(mux *http.ServeMux, token string) {
	m := &mcpServer{s: s, mux: mux, token: token, sse: map[string]chan []byte{}}
	mux.HandleFunc("POST /mcp", m.guard(m.hPost))
	mux.HandleFunc("GET /mcp", m.guard(func(w http.ResponseWriter, r *http.Request) {
		// (no stream of its own: every answer comes with its request)
		w.Header().Set("Allow", "POST, DELETE")
		http.Error(w, "POST JSON-RPC messages to /mcp", http.StatusMethodNotAllowed)
	}))
	mux.HandleFunc("DELETE /mcp", m.guard(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	mux.HandleFunc("GET /sse", m.guard(m.hSSE))
	mux.HandleFunc("POST /messages", m.guard(m.hMessages))
}

// guard: the token, or (without one) no foreign web page
func (m *mcpServer) guard(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if m.token != "" {
			tok := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
			if tok == "" {
				tok = r.URL.Query().Get("token")
			}
			if subtle.ConstantTimeCompare([]byte(tok), []byte(m.token)) != 1 {
				w.Header().Set("WWW-Authenticate", `Bearer realm="audioviz"`)
				http.Error(w, "unauthorized: the studio's MCP token is needed", http.StatusUnauthorized)
				return
			}
		} else if o := r.Header.Get("Origin"); o != "" {
			if u, err := url.Parse(o); err != nil || u.Host != r.Host {
				http.Error(w, "forbidden origin", http.StatusForbidden)
				return
			}
		}
		h(w, r)
	}
}

func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// parseRPC: one message or a batch
func parseRPC(body []byte) ([]rpcMsg, bool, error) {
	body = bytes.TrimSpace(body)
	if len(body) > 0 && body[0] == '[' {
		var ms []rpcMsg
		err := json.Unmarshal(body, &ms)
		return ms, true, err
	}
	var q rpcMsg
	err := json.Unmarshal(body, &q)
	return []rpcMsg{q}, false, err
}

// hPost: Streamable HTTP; the answers in the response (202 when there are none)
func (m *mcpServer) hPost(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 64<<20))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	msgs, batch, err := parseRPC(body)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(rpcResp{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcErr{-32700, "parse error: " + err.Error()}})
		return
	}
	var out []rpcResp
	sid := r.Header.Get("Mcp-Session-Id")
	for _, q := range msgs {
		if q.Method == "initialize" && sid == "" {
			sid = newID()
			w.Header().Set("Mcp-Session-Id", sid)
		}
	}
	if sid == "" {
		sid = "agent" // (a client without sessions)
	}
	ctx := context.WithValue(r.Context(), mcpSession{}, sid)
	for _, q := range msgs {
		if q.Method == "" {
			continue // a response of the client's
		}
		if res, ok := m.handle(ctx, q); ok {
			out = append(out, res)
		}
	}
	if len(out) == 0 {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if batch {
		_ = json.NewEncoder(w).Encode(out)
	} else {
		_ = json.NewEncoder(w).Encode(out[0])
	}
}

// hSSE: the older transport: a stream that first says where to post (endpoint), then carries
// the answers (message)
func (m *mcpServer) hSSE(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}
	id := newID()
	c := make(chan []byte, 32)
	m.mu.Lock()
	m.sse[id] = c
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.sse, id)
		m.mu.Unlock()
	}()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	ep := "/messages?sessionId=" + id
	if t := r.URL.Query().Get("token"); t != "" {
		ep += "&token=" + url.QueryEscape(t)
	}
	fmt.Fprintf(w, "event: endpoint\ndata: %s\n\n", ep)
	fl.Flush()
	tick := time.NewTicker(20 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case b := <-c:
			fmt.Fprintf(w, "event: message\ndata: %s\n\n", b)
			fl.Flush()
		case <-tick.C:
			fmt.Fprint(w, ": ping\n\n")
			fl.Flush()
		}
	}
}

// hMessages: a message for an SSE session: taken (202), its answer goes down its stream
func (m *mcpServer) hMessages(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("sessionId")
	m.mu.Lock()
	c := m.sse[id]
	m.mu.Unlock()
	if c == nil {
		http.Error(w, "no such session (open /sse first)", http.StatusNotFound)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 64<<20))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	msgs, _, err := parseRPC(body)
	if err != nil {
		http.Error(w, "parse error: "+err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusAccepted)
	go func() {
		for _, q := range msgs {
			if q.Method == "" {
				continue
			}
			if res, ok := m.handle(context.WithValue(context.Background(), mcpSession{}, id), q); ok {
				b, _ := json.Marshal(res)
				select {
				case c <- b:
				case <-time.After(30 * time.Second):
				}
			}
		}
	}()
}

// handle: one JSON-RPC message; false for a notification (no answer)
func (m *mcpServer) handle(ctx context.Context, q rpcMsg) (rpcResp, bool) {
	r := rpcResp{JSONRPC: "2.0", ID: q.ID}
	notif := len(q.ID) == 0 || string(q.ID) == "null"
	switch q.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
			ClientInfo      struct {
				Name string `json:"name"`
			} `json:"clientInfo"`
		}
		_ = json.Unmarshal(q.Params, &p)
		// it shows among those connected to the studio
		name := p.ClientInfo.Name
		if name == "" {
			name = "Agent"
		}
		m.s.peerJoin(sessionOf(ctx), "🤖 "+name, "#a78bfa", true)
		v := mcpProtocols[0]
		if slices.Contains(mcpProtocols, p.ProtocolVersion) {
			v = p.ProtocolVersion
		}
		r.Result = map[string]any{
			"protocolVersion": v,
			"capabilities": map[string]any{
				"tools":     map[string]any{"listChanged": false},
				"resources": map[string]any{"listChanged": false},
			},
			"serverInfo":   map[string]any{"name": "audioviz", "title": "AudioViz Studio", "version": m.s.version},
			"instructions": mcpInstructions,
		}
	case "ping":
		r.Result = map[string]any{}
	case "tools/list":
		var ts []map[string]any
		for _, t := range mcpTools {
			ts = append(ts, map[string]any{"name": t.Name, "title": t.Title, "description": t.Desc, "inputSchema": t.Schema})
		}
		r.Result = map[string]any{"tools": ts}
	case "tools/call":
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(q.Params, &p); err != nil {
			r.Error = &rpcErr{-32602, "invalid params: " + err.Error()}
			break
		}
		m.s.peerJoin(sessionOf(ctx), "", "", true) // (still here)
		r.Result = m.callTool(ctx, p.Name, p.Arguments)
	case "resources/list":
		r.Result = map[string]any{"resources": docResources()}
	case "resources/templates/list":
		r.Result = map[string]any{"resourceTemplates": []any{}}
	case "resources/read":
		var p struct {
			URI string `json:"uri"`
		}
		_ = json.Unmarshal(q.Params, &p)
		name := strings.TrimPrefix(p.URI, "audioviz://docs/")
		b, err := fs.ReadFile(docs.FS, name)
		if err != nil || name == p.URI {
			r.Error = &rpcErr{-32002, "resource not found: " + p.URI}
			break
		}
		r.Result = map[string]any{"contents": []any{map[string]any{"uri": p.URI, "mimeType": "text/markdown", "text": string(b)}}}
	case "prompts/list":
		r.Result = map[string]any{"prompts": []any{}}
	default:
		if strings.HasPrefix(q.Method, "notifications/") {
			return r, false
		}
		r.Error = &rpcErr{-32601, "method not found: " + q.Method}
	}
	return r, !notif
}

// mcpSession: the context key of the MCP session (its id: the agent's peer id)
type mcpSession struct{}

func sessionOf(ctx context.Context) string {
	if s, ok := ctx.Value(mcpSession{}).(string); ok {
		return s
	}
	return "agent"
}

// docResources: the documentation, as resources
func docResources() []any {
	var out []any
	_ = fs.WalkDir(docs.FS, ".", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".md") {
			out = append(out, map[string]any{"uri": "audioviz://docs/" + p, "name": p, "mimeType": "text/markdown"})
		}
		return nil
	})
	return out
}

// ---------------------------------------------------------------- tools

type mcpTool struct {
	Name, Title, Desc string
	Schema            map[string]any
	Run               func(m *mcpServer, ctx context.Context, a args) ([]any, error)
}

// args: a tool's arguments
type args map[string]any

func (a args) str(k string) string {
	s, _ := a[k].(string)
	return s
}

func (a args) num(k string, def float64) float64 {
	switch v := a[k].(type) {
	case float64:
		return v
	case string:
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return def
}

func (a args) has(k string) bool { _, ok := a[k]; return ok }

func textOut(s string) []any { return []any{map[string]any{"type": "text", "text": s}} }

func jsonOut(v any) []any {
	b, _ := json.MarshalIndent(v, "", " ")
	return textOut(string(b))
}

func (m *mcpServer) callTool(ctx context.Context, name string, a map[string]any) map[string]any {
	for _, t := range mcpTools {
		if t.Name != name {
			continue
		}
		out, err := t.Run(m, ctx, args(a))
		if err != nil {
			return map[string]any{"content": textOut("Error: " + err.Error()), "isError": true}
		}
		return map[string]any{"content": out}
	}
	return map[string]any{"content": textOut("unknown tool: " + name), "isError": true}
}

// api calls the studio's own API in-process
func (m *mcpServer) api(method, path string, body any) (int, []byte, http.Header) {
	return m.apiAs("", method, path, body)
}

// apiAs: as api, on behalf of an MCP session (the jobs it starts show it)
func (m *mcpServer) apiAs(sid, method, path string, body any) (int, []byte, http.Header) {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, rd)
	req.Header.Set("Content-Type", "application/json")
	if sid != "" {
		req.Header.Set(clientHeader, sid)
	}
	rec := httptest.NewRecorder()
	m.mux.ServeHTTP(rec, req)
	return rec.Code, rec.Body.Bytes(), rec.Header()
}

// apiJSON: the API's JSON answer, or its error
func (m *mcpServer) apiJSON(method, path string, body any) (any, error) {
	return m.apiJSONAs("", method, path, body)
}

// apiJSONAs: as apiJSON, on behalf of an MCP session
func (m *mcpServer) apiJSONAs(sid, method, path string, body any) (any, error) {
	code, b, _ := m.apiAs(sid, method, path, body)
	var v any
	_ = json.Unmarshal(b, &v)
	if code >= 300 {
		if e, ok := v.(map[string]any); ok && e["error"] != nil {
			return nil, fmt.Errorf("%v", e["error"])
		}
		return nil, fmt.Errorf("HTTP %d: %s", code, strings.TrimSpace(string(b)))
	}
	return v, nil
}

// changed: the open studio takes the project again (an undoable step)
func (m *mcpServer) changed(kind string) {
	m.s.hub.send("project", map[string]any{"kind": kind, "by": "mcp"})
}

// schema helpers
func obj(props map[string]any, required ...string) map[string]any {
	o := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		o["required"] = required
	}
	return o
}

func prop(typ, desc string) map[string]any { return map[string]any{"type": typ, "description": desc} }

var mcpTools []mcpTool

func init() {
	mcpTools = []mcpTool{
		{Name: "studio_state", Title: "Studio state",
			Desc:   "What the studio has open: the project (path, width, height, fps, frames, duration in seconds), the GPU, the job running (export, image, lyrics, stems, depth: its progress) and the last log lines.",
			Schema: obj(map[string]any{}),
			Run: func(m *mcpServer, _ context.Context, a args) ([]any, error) {
				v, err := m.apiJSON("GET", "/api/state", nil)
				if st, ok := v.(map[string]any); ok {
					if l, ok := st["logs"].([]any); ok && len(l) > 15 {
						st["logs"] = l[len(l)-15:]
					}
				}
				if err != nil {
					return nil, err
				}
				return jsonOut(v), nil
			}},
		{Name: "list_projects", Title: "List projects",
			Desc:   "The projects in the workspace folder (name, path, last change).",
			Schema: obj(map[string]any{}),
			Run: func(m *mcpServer, _ context.Context, a args) ([]any, error) {
				v, err := m.apiJSON("GET", "/api/workspace", nil)
				if err != nil {
					return nil, err
				}
				return jsonOut(v), nil
			}},
		{Name: "open_project", Title: "Open a project",
			Desc:   "Opens a project (its project.yaml or its folder) in the studio.",
			Schema: obj(map[string]any{"path": prop("string", "project.yaml or the project's folder (on the studio's machine)")}, "path"),
			Run: func(m *mcpServer, _ context.Context, a args) ([]any, error) {
				p := a.str("path")
				if st, err := os.Stat(p); err == nil && st.IsDir() {
					p = filepath.Join(p, "project.yaml")
				}
				if _, err := m.apiJSON("POST", "/api/project/open", map[string]string{"Path": p}); err != nil {
					return nil, err
				}
				m.changed("opened")
				return textOut("Opened " + p), nil
			}},
		{Name: "new_project", Title: "New project",
			Desc:   "Creates a new project (a folder with its project.yaml) in the workspace (or dir) and opens it. Set its song afterwards with edit_project (/audio/path) after upload_file or with a path on the studio's machine.",
			Schema: obj(map[string]any{"name": prop("string", "the project's name (its folder)"), "dir": prop("string", "where to create it (default: the workspace)")}, "name"),
			Run: func(m *mcpServer, _ context.Context, a args) ([]any, error) {
				v, err := m.apiJSON("POST", "/api/project/new", map[string]string{"Dir": a.str("dir"), "Name": a.str("name")})
				if err != nil {
					return nil, err
				}
				m.changed("opened")
				return jsonOut(v), nil
			}},
		{Name: "get_project", Title: "Read the project",
			Desc: "The open project as JSON (the same shape edit_project changes): audio, output, tracks (background, lyrics, visualizer, particles, effects…: lists of segments {at, label, transition, cfg, keys, exprs}), texts (fixed texts), layers (overlay layers with clips; kind \"emitter\" for particles), markers, expressions (the project's constants and functions). path: a JSON Pointer to read only a part (e.g. /texts/0, /layers/1/clips/0, /tracks/visualizer/0/cfg). defaults: also the values a new clip / text / emitter preset gets.",
			Schema: obj(map[string]any{"path": prop("string", "JSON Pointer to a part (default: all of it)"),
				"defaults": prop("boolean", "include clipDefaults, textDefaults, emitterPresets")}),
			Run: func(m *mcpServer, _ context.Context, a args) ([]any, error) {
				p, err := m.project()
				if err != nil {
					return nil, err
				}
				if d, _ := a["defaults"].(bool); !d && a.str("path") == "" {
					delete(p, "clipDefaults")
					delete(p, "textDefaults")
					delete(p, "emitterPresets")
				}
				v, err := ptrGet(p, a.str("path"))
				if err != nil {
					return nil, err
				}
				return jsonOut(v), nil
			}},
		{Name: "edit_project", Title: "Change the project",
			Desc: "Changes the open project: a list of operations on its JSON (see get_project), applied in order and saved together (all or nothing). Everyone with the project open sees the change at once. op: \"set\" (the value at path, creating missing objects), \"merge\" (an object's keys into the object at path), \"insert\" (into an array: at index, or \"-\" at the end), \"remove\". Paths are JSON Pointers, e.g. {op:\"set\", path:\"/texts/0/color\", value:\"#ff0000\"}; {op:\"insert\", path:\"/texts/-\", value:{text:\"Hello\", start:2, end:6}}; keyframes: {op:\"set\", path:\"/texts/0/keys/size\", value:[{t:0,v:80},{t:2,v:140,ease:\"ease_out\"}]} (t: seconds from the element's start); expressions: {op:\"set\", path:\"/texts/0/exprs/glow.intensity\", value:\"0.5 + bass\"} (see the docs: expressions.md). Times are seconds; colors \"#rrggbb\" or \"#rrggbbaa\".",
			Schema: obj(map[string]any{"ops": map[string]any{"type": "array", "description": "the operations, in order",
				"items": obj(map[string]any{
					"op":    map[string]any{"type": "string", "enum": []string{"set", "merge", "insert", "remove"}},
					"path":  prop("string", "JSON Pointer"),
					"value": map[string]any{"description": "the value (set, merge, insert)"},
				}, "op", "path")}}, "ops"),
			Run: func(m *mcpServer, ctx context.Context, a args) ([]any, error) {
				raw, _ := a["ops"].([]any)
				if len(raw) == 0 {
					return nil, errors.New("no operations")
				}
				var ops []jsonOp
				for _, o := range raw {
					op, _ := o.(map[string]any)
					ops = append(ops, jsonOp{Op: args(op).str("op"), Path: args(op).str("path"), Value: op["value"]})
				}
				sid := sessionOf(ctx)
				bt, err := m.s.applyOps("", -1, ops, sid, "", m.s.peerName(sid), true)
				if err != nil {
					return nil, fmt.Errorf("the project was not changed: %w", err)
				}
				// where it changed it: the studios show it (as its selection)
				m.s.hub.send("cursor", map[string]any{"id": sid, "sel": map[string]any{"path": ops[0].Path}})
				return textOut(fmt.Sprintf("Done: %d change(s) (version %d).", len(bt.Ops), bt.V)), nil
			}},
		{Name: "render_frame", Title: "See a frame",
			Desc: "Renders the frame at a time of the song (as the studio's preview) and returns it as an image, to see what the project looks like. scale divides its size (1 full, 2 half: default, 4 a quarter). view: \"depth\" for the scene's depth (white near) instead.",
			Schema: obj(map[string]any{"time": prop("number", "seconds"), "scale": prop("integer", "1–4 (default 2)"),
				"view": map[string]any{"type": "string", "enum": []string{"", "depth"}}}, "time"),
			Run: func(m *mcpServer, _ context.Context, a args) ([]any, error) {
				fps := m.fps()
				if fps == 0 {
					return nil, errors.New("no project is open")
				}
				f := int(math.Round(a.num("time", 0) * float64(fps)))
				q := url.Values{"f": {strconv.Itoa(f)}, "scale": {strconv.Itoa(int(a.num("scale", 2)))}}
				if v := a.str("view"); v != "" {
					q.Set("view", v)
				}
				code, b, h := m.api("GET", "/api/frame?"+q.Encode(), nil)
				if code >= 300 {
					return nil, fmt.Errorf("%s", strings.TrimSpace(string(b)))
				}
				return []any{
					map[string]any{"type": "image", "data": base64.StdEncoding.EncodeToString(b), "mimeType": h.Get("Content-Type")},
					map[string]any{"type": "text", "text": fmt.Sprintf("Frame %d (%.2f s), rendered in %s ms", f, float64(f)/float64(fps), h.Get("X-Render-Ms"))},
				}, nil
			}},
		{Name: "audio_analysis", Title: "The song's analysis",
			Desc: "The song's audio over a stretch of time: per step, the energy of bass, mid, treble and volume (0–1) and, when the song has been separated (separate_stems), of drums, bassline, vocals and the rest; and the times of the beats and, separated, of the kick, snare and hats hits. Use it to place things on the music.",
			Schema: obj(map[string]any{"from": prop("number", "seconds (default 0)"), "to": prop("number", "seconds (default: from + 30)"),
				"step": prop("number", "seconds per row (default 0.5)")}),
			Run: func(m *mcpServer, _ context.Context, a args) ([]any, error) { return m.analysis(a) }},
		{Name: "try_expression", Title: "Try an expression",
			Desc: "Checks an expression (the language of the properties' advanced settings: see the docs, expressions.md) on a property of an element and gives its values at some times (or its mistake). target: {kind:\"text\", text:i} | {kind:\"clip\", layer:\"<layer id>\", clip:i} | {kind:\"seg\", track:\"visualizer\", seg:i}; path: the property (e.g. \"size\", \"glow.intensity\", \"color\").",
			Schema: obj(map[string]any{"src": prop("string", "the expression"), "target": prop("object", "the element"),
				"path": prop("string", "the property"), "times": map[string]any{"type": "array", "items": map[string]any{"type": "number"}, "description": "seconds of the song (default: 12 across the song)"}},
				"src", "target", "path"),
			Run: func(m *mcpServer, _ context.Context, a args) ([]any, error) {
				times, _ := a["times"].([]any)
				if len(times) == 0 {
					d := m.duration()
					for i := 0; i < 12; i++ {
						times = append(times, d*float64(i)/11)
					}
				}
				v, err := m.apiJSON("POST", "/api/expr", map[string]any{"src": a.str("src"), "target": a["target"], "path": a.str("path"), "times": times})
				if err != nil {
					return nil, err
				}
				return jsonOut(v), nil
			}},
		{Name: "get_docs", Title: "Read the documentation",
			Desc:   "AudioViz's documentation (Markdown): without name, the list of pages; with name (e.g. \"index.md\", \"expressions.md\", \"texts.md\", \"layers.md\", \"emitters.md\", \"timeline.md\"), that page.",
			Schema: obj(map[string]any{"name": prop("string", "the page")}),
			Run: func(m *mcpServer, _ context.Context, a args) ([]any, error) {
				if n := a.str("name"); n != "" {
					b, err := fs.ReadFile(docs.FS, strings.TrimPrefix(n, "audioviz://docs/"))
					if err != nil {
						return nil, fmt.Errorf("no page %q", n)
					}
					return textOut(string(b)), nil
				}
				var names []string
				for _, r := range docResources() {
					names = append(names, r.(map[string]any)["name"].(string))
				}
				return textOut(strings.Join(names, "\n")), nil
			}},
		{Name: "list_files", Title: "List files",
			Desc:   "The files of a folder on the studio's machine (default: the project's). kind keeps only: audio, image, bg (images and videos), lyrics, txt, font, project, zip.",
			Schema: obj(map[string]any{"path": prop("string", "the folder"), "kind": prop("string", "a kind of file")}),
			Run: func(m *mcpServer, _ context.Context, a args) ([]any, error) {
				p := a.str("path")
				if p == "" {
					p = m.projectDir()
				}
				v, err := m.apiJSON("GET", "/api/fs?"+url.Values{"path": {p}, "kind": {a.str("kind")}}.Encode(), nil)
				if err != nil {
					return nil, err
				}
				return jsonOut(v), nil
			}},
		{Name: "upload_file", Title: "Upload a file",
			Desc:   "Puts a file (base64) in the project's folder (assets/ by default): a song, an image, a video, a font, lyrics… Returns its path, to use in the project (e.g. /audio/path, a clip's path, a text's font).",
			Schema: obj(map[string]any{"name": prop("string", "its file name"), "data": prop("string", "its content, base64"), "folder": prop("string", "a folder inside the project's (default assets)")}, "name", "data"),
			Run: func(m *mcpServer, _ context.Context, a args) ([]any, error) {
				dir := m.projectDir()
				if dir == "" {
					return nil, errors.New("open or create a project first")
				}
				sub := a.str("folder")
				if sub == "" {
					sub = "assets"
				}
				sub = filepath.Clean("/" + sub)[1:] // (inside the project's folder)
				b, err := base64.StdEncoding.DecodeString(a.str("data"))
				if err != nil {
					return nil, fmt.Errorf("data: %w", err)
				}
				d := filepath.Join(dir, sub)
				if err := os.MkdirAll(d, 0o755); err != nil {
					return nil, err
				}
				p := filepath.Join(d, filepath.Base(a.str("name")))
				if err := os.WriteFile(p, b, 0o644); err != nil {
					return nil, err
				}
				return textOut(p), nil
			}},
		{Name: "get_lyrics", Title: "Read the lyrics",
			Desc:   "The song's synced lyrics (the lyrics track's file): lines with their start, end and words (each with its start and end, seconds).",
			Schema: obj(map[string]any{}),
			Run: func(m *mcpServer, _ context.Context, a args) ([]any, error) {
				v, err := m.apiJSON("GET", "/api/lyrics", nil)
				if err != nil {
					return nil, err
				}
				return jsonOut(v), nil
			}},
		{Name: "set_lyrics", Title: "Write the lyrics",
			Desc:   "Replaces the synced lyrics (lines: [{start, end, words:[{text, start, end}]}], seconds) and saves them (to path, or the lyrics track's file).",
			Schema: obj(map[string]any{"lines": map[string]any{"type": "array", "items": map[string]any{"type": "object"}}, "path": prop("string", "the .lrc file (default: the track's)")}, "lines"),
			Run: func(m *mcpServer, _ context.Context, a args) ([]any, error) {
				v, err := m.apiJSON("PUT", "/api/lyrics", map[string]any{"lines": a["lines"], "path": a.str("path")})
				if err != nil {
					return nil, err
				}
				return jsonOut(v), nil
			}},
		{Name: "generate_lyrics", Title: "Sync the lyrics",
			Desc:   "Synchronizes lyrics with the song (a job: see job_status): from their text (text), or transcribed from the voice (transcribe: true). lang: the language (e.g. es, en; default: detected).",
			Schema: obj(map[string]any{"text": prop("string", "the lyrics' text, a line per row"), "transcribe": prop("boolean", "transcribe them from the voice"), "lang": prop("string", "language code"), "fast": prop("boolean", "a faster, rougher model")}),
			Run: func(m *mcpServer, ctx context.Context, a args) ([]any, error) {
				tr, _ := a["transcribe"].(bool)
				fast, _ := a["fast"].(bool)
				v, err := m.apiJSONAs(sessionOf(ctx), "POST", "/api/lyrics/generate", map[string]any{"text": a.str("text"), "transcribe": tr, "lang": a.str("lang"), "fast": fast})
				if err != nil {
					return nil, err
				}
				return jsonOut(v), nil
			}},
		{Name: "export_video", Title: "Export the video",
			Desc:   "Renders the video (a job: see job_status). start / end: only that stretch (seconds; default all of it); path: the output file (default the project's).",
			Schema: obj(map[string]any{"start": prop("number", "seconds"), "end": prop("number", "seconds"), "path": prop("string", "the .mp4 file")}),
			Run: func(m *mcpServer, ctx context.Context, a args) ([]any, error) {
				v, err := m.apiJSONAs(sessionOf(ctx), "POST", "/api/export", map[string]any{"path": a.str("path"), "start": a.num("start", 0), "end": a.num("end", 0)})
				if err != nil {
					return nil, err
				}
				return jsonOut(v), nil
			}},
		{Name: "job_status", Title: "The job's progress",
			Desc:   "The job running or last run (export, image, lyrics, stems, depth): its kind, status (running, done, error, cancelled), progress and output.",
			Schema: obj(map[string]any{}),
			Run: func(m *mcpServer, _ context.Context, a args) ([]any, error) {
				v, err := m.apiJSON("GET", "/api/state", nil)
				if err != nil {
					return nil, err
				}
				if st, ok := v.(map[string]any); ok && st["job"] != nil {
					return jsonOut(st["job"]), nil
				}
				return textOut("No job."), nil
			}},
		{Name: "cancel_job", Title: "Cancel the job",
			Desc:   "Stops the job running.",
			Schema: obj(map[string]any{}),
			Run: func(m *mcpServer, _ context.Context, a args) ([]any, error) {
				if _, err := m.apiJSON("POST", "/api/job/cancel", nil); err != nil {
					return nil, err
				}
				return textOut("Cancelled."), nil
			}},
		{Name: "generate_image", Title: "Generate an image",
			Desc: "Generates an image with the studio's local AI (a job: see job_status; the image lands in the project's images, list_images): a background, a texture… prompt describes it. Optional: model, width, height, steps, seed, negative, cfg; init + strength: from an image (img2img); refs: reference images (models that take them).",
			Schema: obj(map[string]any{"prompt": prop("string", "what it shows"), "model": prop("string", "the model (default: the studio's)"),
				"width": prop("integer", "px"), "height": prop("integer", "px"), "steps": prop("integer", ""), "seed": prop("integer", ""),
				"negative": prop("string", ""), "cfg": prop("number", ""), "init": prop("string", "a starting image's path"),
				"strength": prop("number", "0–1, how much it changes init"), "refs": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}}, "prompt"),
			Run: func(m *mcpServer, ctx context.Context, a args) ([]any, error) {
				v, err := m.apiJSONAs(sessionOf(ctx), "POST", "/api/images/generate", map[string]any(a))
				if err != nil {
					return nil, err
				}
				return jsonOut(v), nil
			}},
		{Name: "list_images", Title: "List images",
			Desc:   "The project's images (generated and others), with their paths.",
			Schema: obj(map[string]any{}),
			Run: func(m *mcpServer, _ context.Context, a args) ([]any, error) {
				v, err := m.apiJSON("GET", "/api/images", nil)
				if err != nil {
					return nil, err
				}
				return jsonOut(v), nil
			}},
		{Name: "separate_stems", Title: "Separate the song",
			Desc:   "Separates the song into drums, bass, vocals and the rest (a job: see job_status), so that things can react to each (react_to: drums, kick, snare, hats, bassline, vocals, instruments) and audio_analysis gives their hits.",
			Schema: obj(map[string]any{}),
			Run: func(m *mcpServer, ctx context.Context, a args) ([]any, error) {
				v, err := m.apiJSONAs(sessionOf(ctx), "POST", "/api/stems", map[string]any{})
				if err != nil {
					return nil, err
				}
				return jsonOut(v), nil
			}},
		{Name: "read_chat", Title: "Read the chat",
			Desc:   "The project's chat (people and agents working on it): the last top-level messages (each with how many answers its thread has), or a thread (thread: its first message's id) with all its answers. Files: their name; frame_time when it is a frame of the song.",
			Schema: obj(map[string]any{"thread": prop("string", "a thread: its first message's id"), "last": prop("integer", "how many messages (default 30)")}),
			Run: func(m *mcpServer, _ context.Context, a args) ([]any, error) {
				v, err := m.s.chatThread(a.str("thread"), int(a.num("last", 30)))
				if err != nil {
					return nil, err
				}
				if len(v) == 0 {
					return textOut("No messages."), nil
				}
				return jsonOut(v), nil
			}},
		{Name: "post_chat", Title: "Write in the chat",
			Desc: "Posts a message in the project's chat, as this agent (thread: answer in that thread — the id of its first message or of any of its answers). Markdown: **bold**, `code`, links; @name mentions someone. Attach: frame_time (a frame of the song at that second: everyone can click it to go there), images (paths of files on the studio's machine, e.g. from list_images), or file_name + file_data (base64).",
			Schema: obj(map[string]any{"text": prop("string", "the message"), "thread": prop("string", "the thread to answer in"),
				"frame_time": prop("number", "attach the frame at this second"),
				"images":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "files on the studio's machine to attach"},
				"file_name":  prop("string", "a file to attach: its name"), "file_data": prop("string", "its content, base64")}),
			Run: func(m *mcpServer, ctx context.Context, a args) ([]any, error) {
				dir := m.projectDir()
				if dir == "" {
					return nil, errors.New("no project is open")
				}
				var files []chatFile
				if a.has("frame_time") {
					f, err := m.s.frameFile(a.num("frame_time", 0), 2)
					if err != nil {
						return nil, err
					}
					files = append(files, f)
				}
				imgs, _ := a["images"].([]any)
				for _, x := range imgs {
					p, _ := x.(string)
					fh, err := os.Open(p)
					if err != nil {
						return nil, err
					}
					f, err := saveChatFile(dir, filepath.Base(p), fh)
					fh.Close()
					if err != nil {
						return nil, err
					}
					files = append(files, f)
				}
				if n := a.str("file_name"); n != "" {
					b, err := base64.StdEncoding.DecodeString(a.str("file_data"))
					if err != nil {
						return nil, fmt.Errorf("file_data: %w", err)
					}
					f, err := saveChatFile(dir, n, bytes.NewReader(b))
					if err != nil {
						return nil, err
					}
					files = append(files, f)
				}
				sid := sessionOf(ctx)
				msg, err := m.s.chatPost(m.s.chatAuthor(sid, "🤖 Agent", "#a78bfa"), a.str("thread"), a.str("text"), files)
				if err != nil {
					return nil, err
				}
				return textOut("Posted (id " + msg.ID + ")."), nil
			}},
		{Name: "wait_for_mention", Title: "Wait to be called in the chat",
			Desc:   "Waits until someone mentions this agent in the chat (@its name, or @name if given) and returns that message with its thread, so the agent can do what it asks and answer there (post_chat with its thread); then wait again to keep listening. Returns \"no mention\" after timeout seconds (default 300, at most 1800).",
			Schema: obj(map[string]any{"name": prop("string", "the name to listen for (default: this agent's)"), "timeout": prop("number", "seconds")}),
			Run: func(m *mcpServer, ctx context.Context, a args) ([]any, error) {
				sid := sessionOf(ctx)
				name := a.str("name")
				if name == "" {
					name = m.s.peerName(sid)
				}
				if name == "" || name == "🤖 Agent" {
					name = "agent"
				}
				t := math.Max(1, math.Min(1800, a.num("timeout", 300)))
				msg, err := m.s.chatWait(name, time.Duration(t*float64(time.Second)), ctx.Done())
				if err != nil {
					return nil, err
				}
				if msg == nil {
					return textOut(fmt.Sprintf("No mention of @%s in %.0f s. Wait again to keep listening.", strings.TrimSpace(strings.TrimPrefix(name, "🤖")), t)), nil
				}
				thread := msg.Thread
				if thread == "" {
					thread = msg.ID
				}
				th, _ := m.s.chatThread(thread, 0)
				return jsonOut(map[string]any{"message": map[string]any{"id": msg.ID, "author": msg.Author.Name, "text": msg.Text},
					"thread": thread, "conversation": th,
					"answer_with": map[string]any{"tool": "post_chat", "thread": thread}}), nil
			}},
		{Name: "list_comfy_flows", Title: "List the ComfyUI flows",
			Desc:   "The project's ComfyUI flows (ComfyUI runs them; the user starts it): id, name, kind (image, edit, depth, video, audio), what each takes (prompt, negative, seed, size, input files in order with their media, params by key \"node|input\" with their label and current value) and issues (what it lacks in ComfyUI: missing models or nodes; it can't run until fixed by the user). Run one with run_comfy_flow.",
			Schema: obj(map[string]any{}),
			Run: func(m *mcpServer, _ context.Context, a args) ([]any, error) {
				v, err := m.apiJSON("GET", "/api/comfy/flows?fresh=1", nil)
				if err != nil {
					return nil, err
				}
				r, _ := v.(map[string]any)
				fs, _ := r["flows"].([]any)
				var out []any
				for _, x := range fs {
					f, _ := x.(map[string]any)
					takes := map[string]any{}
					var files, params []any
					roles, _ := f["roles"].([]any)
					for _, y := range roles {
						ro, _ := y.(map[string]any)
						switch k, _ := ro["kind"].(string); k {
						case "prompt", "negative", "seed", "width", "height":
							takes[k] = ro["value"]
						case "image":
							media := ro["media"]
							if media == nil {
								media = "image"
							}
							files = append(files, map[string]any{"label": ro["label"], "media": media})
						case "param":
							params = append(params, map[string]any{"key": fmt.Sprint(ro["node"], "|", ro["input"]), "label": ro["label"], "value": ro["value"]})
						}
					}
					if files != nil {
						takes["files"] = files
					}
					if params != nil {
						takes["params"] = params
					}
					e := map[string]any{"id": f["id"], "name": f["name"], "kind": f["kind"], "takes": takes}
					if is, ok := f["issues"]; ok {
						e["issues"] = is
					} else {
						e["issues"] = "unknown: ComfyUI is not answering"
					}
					out = append(out, e)
				}
				if len(out) == 0 {
					return textOut("The project has no ComfyUI flows (the user imports them: Project › ComfyUI)."), nil
				}
				return jsonOut(out), nil
			}},
		{Name: "run_comfy_flow", Title: "Run a ComfyUI flow",
			Desc: "Runs a flow of the project in ComfyUI (a job: see job_status; its output, an image, video or audio, lands in the project's images/, videos/ or audio/). Give what it takes (list_comfy_flows): prompt, negative (empty: the flow's own), seed (none: random), width/height, files (project paths, in the order of its input files), params ({\"node|input\": value}). A depth flow is used through compute_depth instead.",
			Schema: obj(map[string]any{"id": prop("string", "the flow's id"), "prompt": prop("string", ""), "negative": prop("string", ""),
				"seed": prop("integer", ""), "width": prop("integer", "px"), "height": prop("integer", "px"),
				"files":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "input files' paths, in order"},
				"params": map[string]any{"type": "object", "description": "\"node|input\" → value"}}, "id"),
			Run: func(m *mcpServer, ctx context.Context, a args) ([]any, error) {
				v := map[string]any{}
				for _, k := range []string{"prompt", "negative"} {
					if s := a.str(k); strings.TrimSpace(s) != "" {
						v[k] = s
					}
				}
				for _, k := range []string{"seed", "width", "height"} {
					if a.has(k) {
						v[k] = int64(a.num(k, 0))
					}
				}
				if p, ok := a["params"].(map[string]any); ok {
					v["params"] = p
				}
				r, err := m.apiJSONAs(sessionOf(ctx), "POST", "/api/comfy/run", map[string]any{"id": a.str("id"), "values": v, "images": a["files"]})
				if err != nil {
					return nil, err
				}
				return jsonOut(r), nil
			}},
		{Name: "compute_depth", Title: "Depth of an image",
			Desc: "Works out the depth map of a background image (a job: see job_status), for the 2.5D effects (things behind others, the camera's parallax, depth of field, light): with the local AI (detail: low, medium, high; model), or with a depth flow of the project's ComfyUI (flow: its id, list_comfy_flows; the segment's depth then needs source \"comfy\" and that flow). force: work it out again though it is cached.",
			Schema: obj(map[string]any{"image": prop("string", "the image's path"), "detail": prop("string", "low | medium | high"), "model": prop("string", "the model (default: mono)"),
				"flow": prop("string", "a ComfyUI depth flow's id (instead of the local AI)"), "force": prop("boolean", "work it out again")}, "image"),
			Run: func(m *mcpServer, ctx context.Context, a args) ([]any, error) {
				req := map[string]any{"image": a.str("image"), "source": "ai", "model": a.str("model"), "detail": a.str("detail"), "force": a["force"] == true}
				if f := a.str("flow"); f != "" {
					req["source"], req["flow"] = "comfy", f
				}
				v, err := m.apiJSONAs(sessionOf(ctx), "POST", "/api/depth", req)
				if err != nil {
					return nil, err
				}
				return jsonOut(v), nil
			}},
	}
}

const mcpInstructions = `AudioViz makes audio-reactive music videos. This server drives its studio (everyone with the project open sees the agent's changes live; they show the agent among those connected).
Start with studio_state (is a project open?) and get_project. The project: audio (the song), output (size, fps, file), tracks (background, lyrics, visualizer, particles and the effects: lists of segments; the first, at 0, is the base), texts (fixed texts: titles), layers (overlay clips: images, videos; or particle emitters), markers.
Change it with edit_project (JSON Pointer operations); any number, color or switch can have keyframes (keys) or an expression (exprs: the language in expressions.md: audio bands like bass, kick; time; per letter / pixel …). Check visually with render_frame, and place things on the music with audio_analysis. Read the documentation with get_docs (index.md first).
Long work (export_video, generate_image, generate_lyrics, separate_stems, compute_depth, run_comfy_flow) runs as one job at a time: follow it with job_status.
The project may have ComfyUI flows (list_comfy_flows): images, videos, audio made by the user's ComfyUI (run_comfy_flow), depth maps (compute_depth with flow).
The project has a chat (read_chat, post_chat): the people working on it may ask you things there; wait_for_mention waits until someone writes @you, then answer in that thread (show results with frame_time).`

// ---------------------------------------------------------------- helpers

// project: the open project's JSON (as the web app gets it)
func (m *mcpServer) project() (map[string]any, error) {
	v, err := m.apiJSON("GET", "/api/project", nil)
	if err != nil {
		return nil, err
	}
	r, _ := v.(map[string]any)
	p, _ := r["project"].(map[string]any)
	if p == nil {
		return nil, errors.New("no project is open")
	}
	return p, nil
}

func (m *mcpServer) projectDir() string {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()
	if m.s.projPath == "" {
		return ""
	}
	return filepath.Dir(m.s.projPath)
}

func (m *mcpServer) fps() int {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()
	if m.s.sess == nil {
		return 0
	}
	return m.s.sess.FPS
}

func (m *mcpServer) duration() float64 {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()
	if m.s.sess == nil {
		return 0
	}
	return m.s.sess.Duration()
}

// analysis: the song's bands per step over [from, to], its beats and hits there
func (m *mcpServer) analysis(a args) ([]any, error) {
	m.s.mu.Lock()
	sess := m.s.sess
	m.s.mu.Unlock()
	if sess == nil {
		return nil, errors.New("no project is open")
	}
	feats := sess.Features()
	fps := float64(sess.FPS)
	n := len(feats)
	from := math.Max(0, a.num("from", 0))
	to := a.num("to", from+30)
	if d := float64(n) / fps; to > d {
		to = d
	}
	step := math.Max(1/fps, a.num("step", 0.5))
	if (to-from)/step > 2000 {
		step = (to - from) / 2000
	}
	r2 := func(v float64) float64 { return math.Round(v*100) / 100 }
	stems := n > 0 && feats[0].Stem != nil
	var rows []map[string]any
	for t := from; t < to; t += step {
		f0, f1 := int(t*fps), min(n, max(int(t*fps)+1, int((t+step)*fps)))
		var b, md, tr, vol, dr, bl, vo, ot float64
		for i := f0; i < f1; i++ {
			f := feats[i]
			b, md, tr, vol = b+f.BassEnergy, md+f.MidEnergy, tr+f.TrebleEnergy, vol+math.Min(f.OverallRMS*3, 1)
			if stems {
				dr, bl, vo, ot = dr+f.Stem.Drums, bl+f.Stem.Bass, vo+f.Stem.Vocals, ot+f.Stem.Other
			}
		}
		k := float64(max(f1-f0, 1))
		row := map[string]any{"t": r2(t), "bass": r2(b / k), "mid": r2(md / k), "treble": r2(tr / k), "volume": r2(vol / k)}
		if stems {
			row["drums"], row["bassline"], row["vocals"], row["other"] = r2(dr/k), r2(bl/k), r2(vo/k), r2(ot/k)
		}
		rows = append(rows, row)
	}
	beats, kicks, snares, hats := []float64{}, []float64{}, []float64{}, []float64{}
	for i := max(1, int(from*fps)); i < min(n, int(to*fps)); i++ {
		f := feats[i]
		t := math.Round(float64(i)/fps*1000) / 1000
		if f.IsBeat {
			beats = append(beats, t)
		}
		if stems {
			p := feats[i-1].Stem
			if f.Stem.Kick-p.Kick > 0.3 {
				kicks = append(kicks, t)
			}
			if f.Stem.Snare-p.Snare > 0.3 {
				snares = append(snares, t)
			}
			if f.Stem.Hats-p.Hats > 0.3 {
				hats = append(hats, t)
			}
		}
	}
	out := map[string]any{"from": from, "to": to, "step": step, "rows": rows, "beats": beats, "separated": stems}
	if stems {
		out["kicks"], out["snares"], out["hats"] = kicks, snares, hats
	}
	return jsonOut(out), nil
}

// ---------------------------------------------------------------- JSON Pointer

func ptrTokens(p string) ([]string, error) {
	if p == "" || p == "/" {
		return nil, nil
	}
	if !strings.HasPrefix(p, "/") {
		return nil, fmt.Errorf("a path starts with / (a JSON Pointer): %q", p)
	}
	ts := strings.Split(p[1:], "/")
	for i, t := range ts {
		ts[i] = strings.ReplaceAll(strings.ReplaceAll(t, "~1", "/"), "~0", "~")
	}
	return ts, nil
}

func ptrGet(root any, p string) (any, error) {
	ts, err := ptrTokens(p)
	if err != nil {
		return nil, err
	}
	cur := root
	for i, t := range ts {
		switch c := cur.(type) {
		case map[string]any:
			v, ok := c[t]
			if !ok {
				return nil, fmt.Errorf("%s: no %q", "/"+strings.Join(ts[:i], "/"), t)
			}
			cur = v
		case []any:
			k, err := strconv.Atoi(t)
			if err != nil || k < 0 || k >= len(c) {
				return nil, fmt.Errorf("%s: no item %q (there are %d)", "/"+strings.Join(ts[:i], "/"), t, len(c))
			}
			cur = c[k]
		default:
			return nil, fmt.Errorf("%s is not an object or a list", "/"+strings.Join(ts[:i], "/"))
		}
	}
	return cur, nil
}

// ptrApply: op at path p of root (which may be replaced: a returned new root)
func ptrApply(root any, op, p string, val any) (any, error) {
	ts, err := ptrTokens(p)
	if err != nil {
		return root, err
	}
	if len(ts) == 0 {
		return root, errors.New("the path can't be the whole project")
	}
	var set func(cur any, ts []string) (any, error)
	set = func(cur any, ts []string) (any, error) {
		t := ts[0]
		last := len(ts) == 1
		switch c := cur.(type) {
		case nil:
			if last && op != "set" && op != "merge" {
				return nil, fmt.Errorf("no %q", t)
			}
			cur = map[string]any{}
			return set(cur, ts)
		case map[string]any:
			if last {
				switch op {
				case "set":
					c[t] = val
				case "merge":
					c[t] = mergeJSON(c[t], val)
				case "remove":
					if _, ok := c[t]; !ok {
						return nil, fmt.Errorf("no %q", t)
					}
					delete(c, t)
				case "insert":
					return nil, fmt.Errorf("insert goes into a list, and %q is an object's key", t)
				default:
					return nil, fmt.Errorf("unknown op %q (set, merge, insert, remove)", op)
				}
				return c, nil
			}
			v, err := set(c[t], ts[1:])
			if err != nil {
				return nil, err
			}
			c[t] = v
			return c, nil
		case []any:
			k := len(c)
			if t != "-" {
				if k, err = strconv.Atoi(t); err != nil || k < 0 || k > len(c) {
					return nil, fmt.Errorf("no item %q in a list of %d", t, len(c))
				}
			}
			if last {
				switch op {
				case "insert":
					return slices.Insert(c, k, val), nil
				case "set", "merge":
					if k == len(c) {
						return append(c, val), nil
					}
					if op == "merge" {
						c[k] = mergeJSON(c[k], val)
					} else {
						c[k] = val
					}
				case "remove":
					if k == len(c) {
						return nil, fmt.Errorf("no item %q in a list of %d", t, len(c))
					}
					return slices.Delete(c, k, k+1), nil
				default:
					return nil, fmt.Errorf("unknown op %q (set, merge, insert, remove)", op)
				}
				return c, nil
			}
			if k == len(c) {
				return nil, fmt.Errorf("no item %q in a list of %d", t, len(c))
			}
			v, err := set(c[k], ts[1:])
			if err != nil {
				return nil, err
			}
			c[k] = v
			return c, nil
		}
		return nil, fmt.Errorf("%q is inside something that is not an object or a list", t)
	}
	return set(root, ts)
}

// mergeJSON: b's keys into a (objects, deeply); else b
func mergeJSON(a, b any) any {
	am, ok1 := a.(map[string]any)
	bm, ok2 := b.(map[string]any)
	if !ok1 || !ok2 {
		return b
	}
	for k, v := range bm {
		am[k] = mergeJSON(am[k], v)
	}
	return am
}
