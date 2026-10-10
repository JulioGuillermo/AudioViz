package studio

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Collaboration: everyone with the project open (people in the studio, agents through MCP)
// changes it with operations on its JSON (JSON Pointer: set, merge, insert, remove), never all of
// it at once. The server applies them in order (a version each), moving the list indexes they
// name past what others inserted or removed meanwhile, saves, and sends them to everyone (event
// "ops"); each studio applies them on its own copy without losing what it is doing (web:
// collab.js). Each studio has one batch on its way at a time: the server's transform only has
// to go past other people's batches. Presence: who is connected, where their cursor is and what
// they have selected (events "presence" and "cursor").

// jsonOp: one change of the project's JSON
type jsonOp struct {
	Op    string `json:"op"` // set, merge, insert, remove
	Path  string `json:"path"`
	Value any    `json:"value"`
}

// opBatch: a client's operations, as applied (version v)
type opBatch struct {
	V      int      `json:"v"`
	Ops    []jsonOp `json:"ops"`
	Client string   `json:"client"`
	ID     string   `json:"id,omitempty"`
	Name   string   `json:"name,omitempty"`
	// when; by an agent (MCP); what undoes it (on the project as it was right after it); whether
	// it was reviewed: "accepted" or "reverted" (agents' changes: review.go)
	Time   int64    `json:"t,omitempty"`
	Agent  bool     `json:"agent,omitempty"`
	Inv    []jsonOp `json:"inv,omitempty"`
	Status string   `json:"status,omitempty"`
	// a revert: the batch it undid
	Reverts int `json:"reverts,omitempty"`
}

// errStale: the client is too far behind (or its project is another): it must take the project
// again
var errStale = errors.New("stale: the project changed too much meanwhile, take it again")

type collab struct {
	mu    sync.Mutex
	path  string         // the project the versions are of
	epoch string         // (new each time it is opened: a studio from before is told to take it again)
	ver   int            // its version
	log   []opBatch      // the last batches applied (for the transform)
	proj  map[string]any // the project's JSON now (without the read-only defaults)
	// presence: the studios and agents connected
	pmu   sync.Mutex
	peers map[string]*peer
}

// peer: someone connected: a studio (its event stream) or an agent (MCP)
type peer struct {
	ID     string    `json:"id"`
	Name   string    `json:"name"`
	Color  string    `json:"color"`
	Agent  bool      `json:"agent,omitempty"`
	Sel    any       `json:"sel,omitempty"`    // what it has selected (the studio's sel)
	Cursor any       `json:"cursor,omitempty"` // where its pointer is
	Seen   time.Time `json:"-"`
	conns  int
}

const collabLog = 500

// readOnlyKeys: the project JSON's keys that are not the project (defaults for the UI)
var readOnlyKeys = []string{"clipDefaults", "textDefaults", "emitterPresets"}

// projectMap: the open project as plain JSON (without the read-only defaults)
func projectMap(pj *ProjectJSON) map[string]any {
	b, _ := json.Marshal(pj)
	m := map[string]any{}
	_ = json.Unmarshal(b, &m)
	for _, k := range readOnlyKeys {
		delete(m, k)
	}
	return m
}

// collabReset: a project was opened: its versions start again
func (s *Server) collabReset() {
	c := s.collab
	c.mu.Lock()
	defer c.mu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	c.path, c.epoch, c.ver, c.log, c.proj = s.projPath, newID(), 0, nil, nil
	if s.cfg != nil {
		c.proj = projectMap(ProjectToJSON(s.cfg))
	}
}

// applyOps applies a client's operations made on version base. strict: all of them or none (an
// agent's edit); else the ones whose place is gone (someone removed it meanwhile) are left out.
func (s *Server) applyOps(epoch string, base int, ops []jsonOp, client, id, name string, strict bool) (*opBatch, error) {
	return s.applyOpsR(epoch, base, ops, client, id, name, strict, 0)
}

// applyOpsR: applyOps; reverts: the batch these operations undo (0: none)
func (s *Server) applyOpsR(epoch string, base int, ops []jsonOp, client, id, name string, strict bool, reverts int) (*opBatch, error) {
	c := s.collab
	c.mu.Lock()
	defer c.mu.Unlock()
	s.mu.Lock()
	sess, path, open := s.sess, s.projPath, c.path == s.projPath && c.proj != nil
	s.mu.Unlock()
	if sess == nil {
		return nil, errors.New("no project is open")
	}
	if !open {
		return nil, errStale
	}
	if base < 0 {
		base = c.ver // (an agent: on the project as it is now)
	} else if epoch != c.epoch {
		return nil, errStale
	}
	if base > c.ver || c.ver-base > len(c.log) {
		return nil, errStale
	}
	// past the batches applied since base (other clients': its own are in base already)
	var since []opBatch
	if n := c.ver - base; n > 0 {
		since = c.log[len(c.log)-n:]
	}
	var against []jsonOp
	for _, b := range since {
		if b.Client != client {
			against = append(against, b.Ops...)
		}
	}
	todo := transformBatch(ops, against)
	if strict && len(todo) < len(ops) {
		return nil, errors.New("what it changes was changed meanwhile: read the project again")
	}
	for _, o := range todo {
		if slices.ContainsFunc(readOnlyKeys, func(k string) bool { return strings.HasPrefix(o.Path, "/"+k) }) {
			return nil, fmt.Errorf("%s is read-only", o.Path)
		}
	}
	// on a copy: kept only if the whole project is still valid
	var root any = cloneJSON(c.proj)
	var done, inv []jsonOp
	for i, o := range todo {
		o = resolveOp(root, o)
		undo := inverseOp(root, o)
		next, err := ptrApply(root, o.Op, o.Path, cloneJSON(o.Value))
		if err != nil {
			if strict {
				return nil, fmt.Errorf("operation %d (%s %s): %w", i+1, o.Op, o.Path, err)
			}
			continue
		}
		root = next
		done = append(done, o)
		inv = append([]jsonOp{undo}, inv...)
	}
	if len(done) == 0 {
		return &opBatch{V: c.ver, Client: client, ID: id}, nil
	}
	// the lyrics changed: written to their file (the session reads them from it)
	if slices.ContainsFunc(done, func(o jsonOp) bool { return o.Path == "/lyrics" || strings.HasPrefix(o.Path, "/lyrics/") }) {
		extra, err := writeLyricsOf(root, filepath.Dir(path))
		if err != nil {
			return nil, err
		}
		for _, o := range extra {
			if root, err = ptrApply(root, o.Op, o.Path, o.Value); err == nil {
				done = append(done, o)
			}
		}
	}
	b, _ := json.Marshal(root)
	var pj ProjectJSON
	if err := json.Unmarshal(b, &pj); err != nil {
		return nil, err
	}
	if len(pj.Tracks) == 0 || pj.Audio == nil || pj.Output == nil {
		return nil, errors.New("not a project: tracks, audio and output are required")
	}
	cfg, err := ProjectFromJSON(&pj, filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	s.renderMu.Lock()
	err = sess.Update(cfg)
	if err == nil && s.rend != nil && !s.rend.Fits() {
		s.rend.Close()
		s.rend = nil
	}
	s.renderMu.Unlock()
	if err != nil {
		return nil, err
	}
	if err := cfg.Save(path); err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.cfg = cfg
	s.mu.Unlock()
	// the project as it is now (its values filled in): the operations carry those values, so
	// that every copy ends the same
	c.proj = projectMap(ProjectToJSON(cfg))
	// the lyrics read again from another file, or with other lines than were sent (a line
	// without words is not kept): sent whole as they are now (else each operation carries its
	// value as the file keeps it, below)
	if lyricsShape(c.proj) != lyricsShape(rootMap(root)) {
		done = append(done, jsonOp{Op: "set", Path: "/lyrics"})
	}
	for i, o := range done {
		if o.Op == "remove" {
			continue
		}
		if v, err := ptrGet(c.proj, o.Path); err == nil {
			if o.Op == "merge" {
				o.Op = "set"
			}
			done[i].Op, done[i].Value = o.Op, v
		}
	}
	c.ver++
	bt := opBatch{V: c.ver, Ops: done, Client: client, ID: id, Name: name, Time: time.Now().UnixMilli(), Inv: inv,
		Agent: s.isAgent(client), Reverts: reverts}
	c.log = append(c.log, bt)
	if len(c.log) > collabLog {
		c.log = c.log[len(c.log)-collabLog:]
	}
	s.hub.send("ops", bt)
	return &bt, nil
}

// inverseOp: what undoes o on root (before o is applied)
func inverseOp(root any, o jsonOp) jsonOp {
	old, err := ptrGet(root, o.Path)
	switch o.Op {
	case "insert":
		return jsonOp{Op: "remove", Path: o.Path}
	case "remove":
		ts, _ := ptrTokens(o.Path)
		if len(ts) > 0 {
			if _, e := strconv.Atoi(ts[len(ts)-1]); e == nil {
				if parent, e := ptrGet(root, "/"+strings.Join(escTokens(ts[:len(ts)-1]), "/")); e == nil {
					if _, isList := parent.([]any); isList {
						return jsonOp{Op: "insert", Path: o.Path, Value: cloneJSON(old)}
					}
				}
			}
		}
		return jsonOp{Op: "set", Path: o.Path, Value: cloneJSON(old)}
	}
	if err != nil {
		return jsonOp{Op: "remove", Path: o.Path} // (it was not there)
	}
	return jsonOp{Op: "set", Path: o.Path, Value: cloneJSON(old)}
}

// isAgent: whether a client is an agent (MCP)
func (s *Server) isAgent(id string) bool {
	c := s.collab
	c.pmu.Lock()
	defer c.pmu.Unlock()
	p := c.peers[id]
	return p != nil && p.Agent
}

// reloadLyrics: a lyrics file written outside the project's operations (generated, aligned
// again) taken into it: the lyrics track reads it and /lyrics is its lines, for every studio
// (else they keep the lines they had, and the next edit would write them over the new file)
func (s *Server) reloadLyrics(path string) {
	lines, err := readLyricsJSON(path)
	if err != nil {
		return
	}
	var lv any
	b, _ := json.Marshal(LyricsJSON{Path: path, Lines: lines})
	_ = json.Unmarshal(b, &lv)
	ops := []jsonOp{{Op: "set", Path: "/lyrics", Value: lv}}
	s.mu.Lock()
	n := 0
	if s.cfg != nil {
		n = len(s.cfg.Timeline.Lyrics)
	}
	s.mu.Unlock()
	for i := 0; i < n; i++ {
		ops = append(ops, jsonOp{Op: "set", Path: fmt.Sprintf("/tracks/lyrics/%d/cfg/path", i), Value: path})
	}
	if n > 0 {
		ops = append(ops, jsonOp{Op: "set", Path: "/tracks/lyrics/0/cfg/enabled", Value: true})
	}
	if _, err := s.applyOps("", -1, ops, "", "", "AudioViz", false); err != nil {
		s.logf("⚠️  lyrics not taken into the project: %v", err)
	}
}

// lyricsShape: the lyrics' file and how many lines
func lyricsShape(m map[string]any) string {
	l, _ := m["lyrics"].(map[string]any)
	lines, _ := l["lines"].([]any)
	return fmt.Sprint(l["path"], "|", len(lines))
}

func rootMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

// writeLyricsOf: the project's /lyrics written to the lyrics track's file (dir/lyrics.lrc when it
// has none: the operation that sets the track's path to it is returned)
func writeLyricsOf(root any, dir string) ([]jsonOp, error) {
	lv, err := ptrGet(root, "/lyrics/lines")
	if err != nil {
		return nil, nil
	}
	b, _ := json.Marshal(lv)
	var lines []lineJSON
	if err := json.Unmarshal(b, &lines); err != nil {
		return nil, fmt.Errorf("lyrics: %w", err)
	}
	var extra []jsonOp
	p, _ := ptrGet(root, "/tracks/lyrics/0/cfg/path")
	path, _ := p.(string)
	if path == "" {
		path = filepath.Join(dir, "lyrics.lrc")
		extra = append(extra, jsonOp{Op: "set", Path: "/tracks/lyrics/0/cfg/path", Value: path})
	}
	if !strings.HasSuffix(strings.ToLower(path), ".lrc") {
		path = strings.TrimSuffix(path, filepath.Ext(path)) + ".lrc"
		extra = append(extra, jsonOp{Op: "set", Path: "/tracks/lyrics/0/cfg/path", Value: path})
	}
	return extra, writeLRC(path, lines)
}

func cloneJSON(v any) any {
	switch t := v.(type) {
	case map[string]any:
		m := make(map[string]any, len(t))
		for k, x := range t {
			m[k] = cloneJSON(x)
		}
		return m
	case []any:
		a := make([]any, len(t))
		for i, x := range t {
			a[i] = cloneJSON(x)
		}
		return a
	}
	return v
}

// resolveOp: its list place made explicit: "-" (the end) as an index, a set one past the end as
// an insert (so that others can move past it)
func resolveOp(root any, o jsonOp) jsonOp {
	ts, err := ptrTokens(o.Path)
	if err != nil || len(ts) == 0 {
		return o
	}
	parent, err := ptrGet(root, "/"+strings.Join(escTokens(ts[:len(ts)-1]), "/"))
	if len(ts) == 1 {
		parent, err = root, nil
	}
	a, ok := parent.([]any)
	if err != nil || !ok {
		return o
	}
	last := ts[len(ts)-1]
	if last == "-" {
		ts[len(ts)-1] = strconv.Itoa(len(a))
		last = ts[len(ts)-1]
		o.Path = "/" + strings.Join(escTokens(ts), "/")
	}
	if k, err := strconv.Atoi(last); err == nil && k == len(a) && (o.Op == "set" || o.Op == "merge") {
		o.Op = "insert"
	}
	return o
}

func escTokens(ts []string) []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = strings.ReplaceAll(strings.ReplaceAll(t, "~", "~0"), "/", "~1")
	}
	return out
}

// transformBatch: a client's operations (made one after another on a state) moved past against
// (others' operations, applied first on that same state): each one past each of those, which in
// turn move past it (pairwise); the ones whose place is gone are left out. As collab.js.
func transformBatch(ops, against []jsonOp) []jsonOp {
	ag := slices.Clone(against)
	var out []jsonOp
	for _, o := range ops {
		for j := range ag {
			if o.Op == "" {
				break
			}
			if ag[j].Op == "" {
				continue
			}
			o, ag[j] = transformOp(o, ag[j], false), transformOp(ag[j], o, true)
		}
		if o.Op != "" {
			out = append(out, o)
		}
	}
	return out
}

// transformOp: o moved past a (applied before it on the same state): its list indexes shifted
// by what a inserted or removed; Op "" when its place is gone (a removed it). first: o goes
// first where both insert at the same place.
func transformOp(o, a jsonOp, first bool) jsonOp {
	if a.Op != "insert" && a.Op != "remove" {
		return o
	}
	at, err1 := ptrTokens(a.Path)
	ot, err2 := ptrTokens(o.Path)
	if err1 != nil || err2 != nil || len(at) == 0 || len(ot) < len(at) {
		return o
	}
	n := len(at) - 1
	for i := 0; i < n; i++ {
		if at[i] != ot[i] {
			return o // not in that list
		}
	}
	ai, e1 := strconv.Atoi(at[n])
	oi, e2 := strconv.Atoi(ot[n])
	if e1 != nil || e2 != nil {
		return o
	}
	exact := len(ot) == len(at) // o is at that place of the list (not inside an item)
	switch a.Op {
	case "insert":
		if oi > ai || (oi == ai && !(first && exact && o.Op == "insert")) {
			oi++
		}
	case "remove":
		if oi > ai {
			oi--
		} else if oi == ai && !(exact && o.Op == "insert") {
			return jsonOp{}
		}
	}
	ot[n] = strconv.Itoa(oi)
	o.Path = "/" + strings.Join(escTokens(ot), "/")
	return o
}

// ---------------------------------------------------------------- HTTP

// hOps: a studio's operations: {base, ops, client, id}; the batch applied (its version)
func (s *Server) hOps(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Epoch  string   `json:"epoch"`
		Base   int      `json:"base"`
		Ops    []jsonOp `json:"ops"`
		Client string   `json:"client"`
		ID     string   `json:"id"`
	}
	if err := readJSON(r, &req); err != nil {
		fail(w, 400, err)
		return
	}
	bt, err := s.applyOps(req.Epoch, req.Base, req.Ops, req.Client, req.ID, s.peerName(req.Client), false)
	if err != nil {
		code := 400
		if errors.Is(err, errStale) {
			code = 409
		}
		fail(w, code, err)
		return
	}
	s.mu.Lock()
	m := s.meta()
	s.mu.Unlock()
	writeJSON(w, map[string]any{"batch": bt, "meta": m, "saved": time.Now().Format("15:04:05")})
}

// hOpsSince: the batches after version v (a studio that missed some), or 409: take it all again
func (s *Server) hOpsSince(w http.ResponseWriter, r *http.Request) {
	v, _ := strconv.Atoi(r.URL.Query().Get("v"))
	c := s.collab
	c.mu.Lock()
	defer c.mu.Unlock()
	if r.URL.Query().Get("epoch") != c.epoch || v > c.ver || c.ver-v > len(c.log) {
		fail(w, 409, errStale)
		return
	}
	writeJSON(w, map[string]any{"v": c.ver, "batches": c.log[len(c.log)-(c.ver-v):]})
}

// projectVersion: the version of the open project's JSON
func (s *Server) projectVersion() int {
	s.collab.mu.Lock()
	defer s.collab.mu.Unlock()
	return s.collab.ver
}

// ---------------------------------------------------------------- presence

func (s *Server) peerName(id string) string {
	c := s.collab
	c.pmu.Lock()
	defer c.pmu.Unlock()
	if p := c.peers[id]; p != nil {
		return p.Name
	}
	return ""
}

// peerJoin / peerLeave: a studio's event stream opened / closed
func (s *Server) peerJoin(id, name, color string, agent bool) {
	if id == "" {
		return
	}
	c := s.collab
	c.pmu.Lock()
	p := c.peers[id]
	if p == nil {
		p = &peer{ID: id}
		c.peers[id] = p
	}
	if name != "" {
		p.Name = name
	}
	if color != "" {
		p.Color = color
	}
	p.Agent = p.Agent || agent
	if p.Agent && p.Name == "" {
		p.Name, p.Color = "🤖 Agent", "#a78bfa"
	}
	p.Seen = time.Now()
	if !agent {
		p.conns++
	}
	c.pmu.Unlock()
	s.sendPresence()
}

func (s *Server) peerLeave(id string) {
	c := s.collab
	c.pmu.Lock()
	if p := c.peers[id]; p != nil {
		if p.conns--; p.conns <= 0 {
			delete(c.peers, id)
		}
	}
	c.pmu.Unlock()
	s.sendPresence()
}

// sendPresence: who is connected (agents quiet for 10 minutes are gone)
func (s *Server) sendPresence() {
	c := s.collab
	c.pmu.Lock()
	var list []*peer
	for id, p := range c.peers {
		if p.Agent && time.Since(p.Seen) > 10*time.Minute {
			delete(c.peers, id)
			continue
		}
		q := *p
		list = append(list, &q)
	}
	c.pmu.Unlock()
	slices.SortFunc(list, func(a, b *peer) int { return strings.Compare(a.ID, b.ID) })
	s.hub.send("presence", list)
}

// hPresence: a studio's cursor and selection {client, cursor, sel}: sent to the others
func (s *Server) hPresence(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Client string `json:"client"`
		Name   string `json:"name"`
		Color  string `json:"color"`
		Cursor any    `json:"cursor"`
		Sel    any    `json:"sel"`
	}
	if err := readJSON(r, &req); err != nil {
		fail(w, 400, err)
		return
	}
	c := s.collab
	c.pmu.Lock()
	p := c.peers[req.Client]
	renamed := false
	if p != nil {
		p.Cursor, p.Sel, p.Seen = req.Cursor, req.Sel, time.Now()
		if (req.Name != "" && req.Name != p.Name) || (req.Color != "" && req.Color != p.Color) {
			p.Name, p.Color, renamed = req.Name, req.Color, true
		}
	}
	c.pmu.Unlock()
	if renamed {
		s.sendPresence()
	}
	s.hub.send("cursor", map[string]any{"id": req.Client, "cursor": req.Cursor, "sel": req.Sel})
	w.WriteHeader(http.StatusNoContent)
}
