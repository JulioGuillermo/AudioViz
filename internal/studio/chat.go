package studio

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The project's chat: messages (in threads) with files and images, by the people and agents who
// have it open. All of it lives in the project: chat/messages.jsonl (what happened, one event a
// line: a message posted, edited, removed) and chat/files/ (what was shared). Each change goes to
// everyone at once (event "chat"); agents read and post through MCP, and can wait for a message
// that mentions them (@name).

// chatFile: a file shared in a message (its name in chat/files/)
type chatFile struct {
	Name  string  `json:"name"`            // its file name in chat/files
	Title string  `json:"title,omitempty"` // as it was called
	Type  string  `json:"type"`            // its media type
	Size  int64   `json:"size"`
	Time  float64 `json:"time,omitempty"` // a frame of the song: its time (s)
	Frame bool    `json:"frame,omitempty"`
}

type chatMsg struct {
	ID     string     `json:"id"`
	Thread string     `json:"thread,omitempty"` // the message it answers in a thread ("" top level)
	Author peer       `json:"author"`
	At     time.Time  `json:"at"`
	Text   string     `json:"text"`
	Files  []chatFile `json:"files,omitempty"`
	Edited *time.Time `json:"edited,omitempty"`
	Gone   bool       `json:"gone,omitempty"` // removed (its thread stays)
}

// chatEvent: a line of messages.jsonl
type chatEvent struct {
	Kind string  `json:"kind"` // post, edit, remove
	Msg  chatMsg `json:"msg"`
}

type chat struct {
	mu   sync.Mutex
	dir  string // the project's folder
	msgs []*chatMsg
	byID map[string]*chatMsg
	// waiting for a mention: agents' waits (wait_for_mention)
	waits map[chan *chatMsg]string
}

func (s *Server) chatDir() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.projPath == "" {
		return ""
	}
	return filepath.Dir(s.projPath)
}

// chatOf: the open project's chat (read from its file the first time)
func (s *Server) chatOf() (*chat, error) {
	dir := s.chatDir()
	if dir == "" {
		return nil, errors.New("no project is open")
	}
	s.chatMu.Lock()
	defer s.chatMu.Unlock()
	if s.chat != nil && s.chat.dir == dir {
		return s.chat, nil
	}
	c := &chat{dir: dir, byID: map[string]*chatMsg{}, waits: map[chan *chatMsg]string{}}
	if f, err := os.Open(filepath.Join(dir, "chat", "messages.jsonl")); err == nil {
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 1<<20), 16<<20)
		for sc.Scan() {
			var e chatEvent
			if json.Unmarshal(sc.Bytes(), &e) == nil {
				c.apply(e)
			}
		}
		f.Close()
	}
	s.chat = c
	return c, nil
}

func (c *chat) apply(e chatEvent) {
	m := e.Msg
	switch e.Kind {
	case "post":
		if c.byID[m.ID] == nil {
			p := m
			c.msgs = append(c.msgs, &p)
			c.byID[m.ID] = &p
		}
	case "edit":
		if p := c.byID[m.ID]; p != nil {
			p.Text, p.Edited = m.Text, m.Edited
		}
	case "remove":
		if p := c.byID[m.ID]; p != nil {
			p.Gone, p.Text, p.Files = true, "", nil
		}
	}
}

// record: an event written to messages.jsonl and applied
func (c *chat) record(e chatEvent) error {
	if err := os.MkdirAll(filepath.Join(c.dir, "chat"), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(c.dir, "chat", "messages.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	b, _ := json.Marshal(e)
	_, err = f.Write(append(b, '\n'))
	f.Close()
	if err != nil {
		return err
	}
	c.apply(e)
	return nil
}

// post: a new message by author (its files already in chat/files)
func (s *Server) chatPost(author peer, thread, text string, files []chatFile) (*chatMsg, error) {
	c, err := s.chatOf()
	if err != nil {
		return nil, err
	}
	text = strings.TrimSpace(text)
	if text == "" && len(files) == 0 {
		return nil, errors.New("an empty message")
	}
	c.mu.Lock()
	if thread != "" {
		t := c.byID[thread]
		if t == nil {
			c.mu.Unlock()
			return nil, fmt.Errorf("no message %s to answer", thread)
		}
		if t.Thread != "" {
			thread = t.Thread // (a thread has one level: answers go to its first message)
		}
	}
	m := chatMsg{ID: newID()[:12], Thread: thread, Author: author, At: time.Now(), Text: text, Files: files}
	author.Cursor, author.Sel = nil, nil
	m.Author = author
	if err := c.record(chatEvent{Kind: "post", Msg: m}); err != nil {
		c.mu.Unlock()
		return nil, err
	}
	// those waiting for a mention of them
	for ch, name := range c.waits {
		if mentions(text, name) && name != author.Name {
			select {
			case ch <- &m:
			default:
			}
		}
	}
	c.mu.Unlock()
	s.hub.send("chat", map[string]any{"kind": "post", "msg": m})
	return &m, nil
}

// mentions: whether text mentions @name (its words, without the agent's 🤖; case aside)
func mentions(text, name string) bool {
	n := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(name, "🤖")))
	if n == "" {
		return false
	}
	t := strings.ToLower(text)
	for _, alias := range []string{n, strings.ReplaceAll(n, " ", ""), strings.ReplaceAll(n, " ", "_"), strings.ReplaceAll(n, " ", "-")} {
		re := regexp.MustCompile(`(^|[^\pL\pN_])@` + regexp.QuoteMeta(alias) + `($|[^\pL\pN_])`)
		if re.MatchString(t) {
			return true
		}
	}
	return false
}

// chatAuthor: who a client is (its presence; an unknown one by the name it gives)
func (s *Server) chatAuthor(client, name, color string) peer {
	c := s.collab
	c.pmu.Lock()
	defer c.pmu.Unlock()
	if p := c.peers[client]; p != nil {
		q := *p
		q.Cursor, q.Sel = nil, nil
		return q
	}
	if name == "" {
		name = "?"
	}
	return peer{ID: client, Name: name, Color: color}
}

// ---------------------------------------------------------------- HTTP

// hChat: the messages (all of them; or after id since)
func (s *Server) hChat(w http.ResponseWriter, r *http.Request) {
	c, err := s.chatOf()
	if err != nil {
		fail(w, 404, err)
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]chatMsg, 0, len(c.msgs))
	for _, m := range c.msgs {
		out = append(out, *m)
	}
	writeJSON(w, map[string]any{"messages": out})
}

// hChatPost: {client, name, color, thread, text, files}
func (s *Server) hChatPost(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Client, Name, Color, Thread, Text string
		Files                             []chatFile
	}
	if err := readJSON(r, &req); err != nil {
		fail(w, 400, err)
		return
	}
	m, err := s.chatPost(s.chatAuthor(req.Client, req.Name, req.Color), req.Thread, req.Text, req.Files)
	if err != nil {
		fail(w, 400, err)
		return
	}
	writeJSON(w, m)
}

// hChatEdit: {client, id, text, remove}: its author changes or removes it
func (s *Server) hChatEdit(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Client, ID, Text string
		Remove           bool
	}
	if err := readJSON(r, &req); err != nil {
		fail(w, 400, err)
		return
	}
	c, err := s.chatOf()
	if err != nil {
		fail(w, 404, err)
		return
	}
	c.mu.Lock()
	m := c.byID[req.ID]
	if m == nil || m.Gone {
		c.mu.Unlock()
		fail(w, 404, errors.New("no such message"))
		return
	}
	if m.Author.ID != req.Client {
		c.mu.Unlock()
		fail(w, 403, errors.New("only its author can change it"))
		return
	}
	now := time.Now()
	e := chatEvent{Kind: "edit", Msg: chatMsg{ID: m.ID, Text: strings.TrimSpace(req.Text), Edited: &now}}
	if req.Remove {
		e.Kind = "remove"
	}
	err = c.record(e)
	cp := *m
	c.mu.Unlock()
	if err != nil {
		fail(w, 500, err)
		return
	}
	s.hub.send("chat", map[string]any{"kind": e.Kind, "msg": cp})
	writeJSON(w, cp)
}

// hChatUpload: a file shared in the chat (multipart "file"): saved in chat/files
func (s *Server) hChatUpload(w http.ResponseWriter, r *http.Request) {
	dir := s.chatDir()
	if dir == "" {
		fail(w, 400, errors.New("no project is open"))
		return
	}
	file, hdr, err := r.FormFile("file")
	if err != nil {
		fail(w, 400, err)
		return
	}
	defer file.Close()
	f, err := saveChatFile(dir, hdr.Filename, file)
	if err != nil {
		fail(w, 500, err)
		return
	}
	f.Type = hdr.Header.Get("Content-Type")
	writeJSON(w, f)
}

// saveChatFile: r saved as chat/files/<id>-<name>
func saveChatFile(dir, name string, r io.Reader) (chatFile, error) {
	fd := filepath.Join(dir, "chat", "files")
	if err := os.MkdirAll(fd, 0o755); err != nil {
		return chatFile{}, err
	}
	base := filepath.Base(name)
	if base == "." || base == "/" || base == "" {
		base = "file"
	}
	fn := newID()[:8] + "-" + base
	out, err := os.Create(filepath.Join(fd, fn))
	if err != nil {
		return chatFile{}, err
	}
	n, err := io.Copy(out, io.LimitReader(r, 512<<20))
	out.Close()
	return chatFile{Name: fn, Title: base, Size: n, Type: mimeOf(base)}, err
}

func mimeOf(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	case ".mp4":
		return "video/mp4"
	case ".webm":
		return "video/webm"
	case ".mp3":
		return "audio/mpeg"
	case ".wav":
		return "audio/wav"
	case ".flac":
		return "audio/flac"
	case ".pdf":
		return "application/pdf"
	case ".txt", ".lrc", ".md":
		return "text/plain"
	}
	return "application/octet-stream"
}

// hChatFile: a shared file (?name=)
func (s *Server) hChatFile(w http.ResponseWriter, r *http.Request) {
	dir := s.chatDir()
	name := filepath.Base(r.URL.Query().Get("name"))
	if dir == "" || name == "." || name == "/" {
		http.NotFound(w, r)
		return
	}
	p := filepath.Join(dir, "chat", "files", name)
	if r.URL.Query().Get("download") == "1" {
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename=%q`, strings.SplitN(name, "-", 2)[len(strings.SplitN(name, "-", 2))-1]))
	}
	http.ServeFile(w, r, p)
}

// hChatFrame: {t, scale}: the preview's frame at t saved as a shared image (its time with it)
func (s *Server) hChatFrame(w http.ResponseWriter, r *http.Request) {
	var req struct {
		T     float64 `json:"t"`
		Scale int     `json:"scale"`
	}
	if err := readJSON(r, &req); err != nil {
		fail(w, 400, err)
		return
	}
	f, err := s.frameFile(req.T, req.Scale)
	if err != nil {
		fail(w, 400, err)
		return
	}
	writeJSON(w, f)
}

// frameFile: the frame at t (preview size 1/scale) saved in chat/files
func (s *Server) frameFile(t float64, scale int) (chatFile, error) {
	dir := s.chatDir()
	s.mu.Lock()
	fps := 0
	if s.sess != nil {
		fps = s.sess.FPS
	}
	s.mu.Unlock()
	if dir == "" || fps == 0 {
		return chatFile{}, errors.New("no project is open")
	}
	if scale <= 0 {
		scale = 2
	}
	f := int(t*float64(fps) + 0.5)
	rec := httptest.NewRecorder()
	s.hFrame(rec, httptest.NewRequest("GET", "/api/frame?f="+strconv.Itoa(f)+"&scale="+strconv.Itoa(scale), nil))
	if rec.Code >= 300 {
		return chatFile{}, fmt.Errorf("frame: %s", strings.TrimSpace(rec.Body.String()))
	}
	cf, err := saveChatFile(dir, fmt.Sprintf("frame-%s.jpg", strings.ReplaceAll(fmtClock(t), ":", "m")), rec.Body)
	if err != nil {
		return cf, err
	}
	cf.Type, cf.Time, cf.Frame = "image/jpeg", t, true
	return cf, nil
}

func fmtClock(t float64) string {
	m := int(t) / 60
	return fmt.Sprintf("%d:%05.2f", m, t-float64(m*60))
}

// ---------------------------------------------------------------- agents

// chatWait: the next message that mentions name (or none in timeout)
func (s *Server) chatWait(name string, timeout time.Duration, done <-chan struct{}) (*chatMsg, error) {
	c, err := s.chatOf()
	if err != nil {
		return nil, err
	}
	ch := make(chan *chatMsg, 1)
	c.mu.Lock()
	c.waits[ch] = name
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.waits, ch)
		c.mu.Unlock()
	}()
	select {
	case m := <-ch:
		return m, nil
	case <-time.After(timeout):
		return nil, nil
	case <-done:
		return nil, errors.New("cancelled")
	}
}

// chatThread: a thread (its first message and the answers), or the last n top-level messages
// (with how many answers each has)
func (s *Server) chatThread(thread string, n int) ([]map[string]any, error) {
	c, err := s.chatOf()
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	answers := map[string]int{}
	for _, m := range c.msgs {
		if m.Thread != "" && !m.Gone {
			answers[m.Thread]++
		}
	}
	var out []map[string]any
	for _, m := range c.msgs {
		if m.Gone || !((thread == "" && m.Thread == "") || (thread != "" && (m.ID == thread || m.Thread == thread))) {
			continue
		}
		o := map[string]any{"id": m.ID, "author": m.Author.Name, "at": m.At.Format("2006-01-02 15:04:05"), "text": m.Text}
		if m.Thread != "" {
			o["thread"] = m.Thread
		}
		if k := answers[m.ID]; k > 0 && thread == "" {
			o["answers"] = k
		}
		for _, f := range m.Files {
			fo := map[string]any{"name": f.Title, "file": f.Name, "type": f.Type}
			if f.Frame {
				fo["frame_time"] = f.Time
			}
			o["files"] = append(anySlice(o["files"]), fo)
		}
		out = append(out, o)
	}
	if n > 0 && len(out) > n {
		out = out[len(out)-n:]
	}
	return slices.Clip(out), nil
}

func anySlice(v any) []any {
	s, _ := v.([]any)
	return s
}

// hChatTyping: {client, thread}: someone is writing (the others show it a moment)
func (s *Server) hChatTyping(w http.ResponseWriter, r *http.Request) {
	var req struct{ Client, Thread string }
	if err := readJSON(r, &req); err != nil {
		fail(w, 400, err)
		return
	}
	s.hub.send("typing", map[string]any{"id": req.Client, "name": s.peerName(req.Client), "thread": req.Thread})
	w.WriteHeader(http.StatusNoContent)
}
