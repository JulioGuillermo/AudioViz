package comfy

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

// Client talks to a ComfyUI server (its HTTP API and its websocket for progress)
type Client struct {
	Base string // e.g. http://127.0.0.1:8188
	HTTP *http.Client
}

func New(base string) *Client {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base != "" && !strings.Contains(base, "://") {
		base = "http://" + base
	}
	return &Client{Base: base, HTTP: &http.Client{Timeout: 60 * time.Second}}
}

func (c *Client) get(ctx context.Context, p string, v any) error {
	req, err := http.NewRequestWithContext(ctx, "GET", c.Base+p, nil)
	if err != nil {
		return err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("ComfyUI at %s: %w", c.Base, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2000))
		return fmt.Errorf("ComfyUI %s: %s %s", p, resp.Status, strings.TrimSpace(string(b)))
	}
	if raw, ok := v.(*[]byte); ok {
		*raw, err = io.ReadAll(resp.Body)
		return err
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

// Stats: its version and devices (GET /system_stats)
func (c *Client) Stats(ctx context.Context) (map[string]any, error) {
	var v map[string]any
	return v, c.get(ctx, "/system_stats", &v)
}

// ObjectInfo: the node definitions
func (c *Client) ObjectInfo(ctx context.Context) (ObjectInfo, error) {
	var v ObjectInfo
	return v, c.get(ctx, "/object_info", &v)
}

// UserWorkflows: the workflows saved in ComfyUI (paths in user/…/workflows)
func (c *Client) UserWorkflows(ctx context.Context) ([]string, error) {
	var v []string
	err := c.get(ctx, "/api/userdata?dir=workflows&recurse=true&split=false", &v)
	var out []string
	for _, p := range v {
		if strings.HasSuffix(strings.ToLower(p), ".json") {
			out = append(out, p)
		}
	}
	return out, err
}

// UserWorkflow: one saved workflow (its JSON)
func (c *Client) UserWorkflow(ctx context.Context, p string) ([]byte, error) {
	var b []byte
	return b, c.get(ctx, "/api/userdata/"+url.PathEscape("workflows/"+p), &b)
}

// Template: a template's listing (as /templates/index.json has it)
type Template struct {
	Name        string   `json:"name"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Category    string   `json:"category"`
	MediaType   string   `json:"mediaType"`
	MediaSub    string   `json:"mediaSubtype"`
	Tags        []string `json:"tags"`
	Models      []string `json:"models"`
}

// Templates: ComfyUI's workflow templates, by category
func (c *Client) Templates(ctx context.Context) ([]Template, error) {
	var idx []struct {
		Title     string     `json:"title"`
		Category  string     `json:"category"`
		Templates []Template `json:"templates"`
	}
	if err := c.get(ctx, "/templates/index.json", &idx); err != nil {
		return nil, err
	}
	var out []Template
	for _, cat := range idx {
		for _, t := range cat.Templates {
			t.Category = firstNonEmpty(cat.Title, cat.Category)
			out = append(out, t)
		}
	}
	return out, nil
}

// TemplateJSON: a template's workflow
func (c *Client) TemplateJSON(ctx context.Context, name string) ([]byte, error) {
	var b []byte
	return b, c.get(ctx, "/templates/"+url.PathEscape(name)+".json", &b)
}

// TemplateThumb: a template's picture (its bytes, its type)
func (c *Client) TemplateThumb(ctx context.Context, name, sub string) ([]byte, string, error) {
	if sub == "" {
		sub = "webp"
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", c.Base+"/templates/"+url.PathEscape(name)+"-1."+sub, nil)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, "", errors.New(resp.Status)
	}
	b, err := io.ReadAll(resp.Body)
	return b, resp.Header.Get("Content-Type"), err
}

// Upload: a file into ComfyUI's input folder (subfolder "audioviz"); its name there for a loader
func (c *Client) Upload(ctx context.Context, name string, data []byte) (string, error) {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("image", path.Base(name))
	fw.Write(data)
	mw.WriteField("subfolder", "audioviz")
	mw.WriteField("overwrite", "true")
	mw.Close()
	req, _ := http.NewRequestWithContext(ctx, "POST", c.Base+"/upload/image", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var r struct {
		Name      string `json:"name"`
		Subfolder string `json:"subfolder"`
	}
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("upload: %s %s", resp.Status, b)
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return "", err
	}
	if r.Subfolder != "" {
		return r.Subfolder + "/" + r.Name, nil
	}
	return r.Name, nil
}

// Queue: a prompt queued (for client clientID); its id. A prompt ComfyUI refuses (a missing
// model, a wrong value) returns its errors in words.
func (c *Client) Queue(ctx context.Context, p Prompt, clientID string) (string, error) {
	// (with previews of the image forming, though ComfyUI was started without --preview-method)
	b, _ := json.Marshal(map[string]any{"prompt": p, "client_id": clientID, "extra_data": map[string]any{"preview_method": "auto"}})
	req, _ := http.NewRequestWithContext(ctx, "POST", c.Base+"/prompt", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var r struct {
		PromptID   string         `json:"prompt_id"`
		Error      any            `json:"error"`
		NodeErrors map[string]any `json:"node_errors"`
	}
	raw, _ := io.ReadAll(resp.Body)
	_ = json.Unmarshal(raw, &r)
	if resp.StatusCode >= 300 || r.PromptID == "" {
		return "", fmt.Errorf("ComfyUI refused the flow: %s", describeErrors(r.Error, r.NodeErrors, p, raw))
	}
	return r.PromptID, nil
}

// describeErrors: ComfyUI's validation errors in a line each (the node by its title)
func describeErrors(e any, nodes map[string]any, p Prompt, raw []byte) string {
	var parts []string
	if m, ok := e.(map[string]any); ok {
		if s, _ := m["message"].(string); s != "" {
			parts = append(parts, s)
		}
	}
	for id, ne := range nodes {
		m, _ := ne.(map[string]any)
		errs, _ := m["errors"].([]any)
		title := id
		if n := p[id]; n != nil {
			title = n.Title() + " (" + id + ")"
		}
		for _, x := range errs {
			em, _ := x.(map[string]any)
			parts = append(parts, fmt.Sprintf("%s: %v %v", title, em["message"], em["details"]))
		}
	}
	if len(parts) == 0 {
		return strings.TrimSpace(string(raw))
	}
	return strings.Join(parts, "; ")
}

// Interrupt: stop what it is running
func (c *Client) Interrupt(ctx context.Context) error {
	req, _ := http.NewRequestWithContext(ctx, "POST", c.Base+"/interrupt", nil)
	resp, err := c.HTTP.Do(req)
	if err == nil {
		resp.Body.Close()
	}
	return err
}

// File: a result (as /history lists it)
type File struct {
	Filename  string `json:"filename"`
	Subfolder string `json:"subfolder"`
	Type      string `json:"type"`
	Format    string `json:"format,omitempty"`
}

// Outputs: the files node nodeID made in prompt id (images, videos, audio)
// ErrNoHistory: ComfyUI keeps nothing (yet) for a prompt: it writes its history a moment after
// saying it ended
var ErrNoHistory = errors.New("ComfyUI has no result for it")

func (c *Client) Outputs(ctx context.Context, id, nodeID string) ([]File, error) {
	var h map[string]struct {
		Outputs map[string]map[string]json.RawMessage `json:"outputs"`
		Status  struct {
			StatusStr string `json:"status_str"`
		} `json:"status"`
	}
	if err := c.get(ctx, "/history/"+url.PathEscape(id), &h); err != nil {
		return nil, err
	}
	e, ok := h[id]
	if !ok {
		return nil, ErrNoHistory
	}
	var out []File
	for nid, kinds := range e.Outputs {
		if nodeID != "" && nid != nodeID {
			continue
		}
		for _, k := range []string{"images", "gifs", "videos", "audio", "files"} {
			var fs []File
			if json.Unmarshal(kinds[k], &fs) == nil {
				out = append(out, fs...)
			}
		}
	}
	return out, nil
}

// Download: a result's bytes
func (c *Client) Download(ctx context.Context, f File) ([]byte, error) {
	q := url.Values{"filename": {f.Filename}, "subfolder": {f.Subfolder}, "type": {f.Type}}
	var b []byte
	return b, c.get(ctx, "/view?"+q.Encode(), &b)
}

// ---------------------------------------------------------------- progress (websocket)

// Event: a message of ComfyUI's websocket ({type, data})
type Event struct {
	Type string         `json:"type"`
	Data map[string]any `json:"data"`
}

// Listen: ComfyUI's events for clientID (until ctx ends or the connection closes)
func (c *Client) Listen(ctx context.Context, clientID string, on func(Event)) error {
	u, err := url.Parse(c.Base)
	if err != nil {
		return err
	}
	host := u.Host
	if u.Port() == "" {
		if u.Scheme == "https" {
			host += ":443"
		} else {
			host += ":80"
		}
	}
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", host)
	if err != nil {
		return err
	}
	if u.Scheme == "https" {
		conn = tls.Client(conn, &tls.Config{ServerName: u.Hostname()})
	}
	go func() { <-ctx.Done(); conn.Close() }()
	defer conn.Close()
	key := make([]byte, 16)
	_, _ = rand.Read(key)
	p := strings.TrimRight(u.Path, "/") + "/ws?clientId=" + url.QueryEscape(clientID)
	fmt.Fprintf(conn, "GET %s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n\r\n",
		p, u.Host, base64.StdEncoding.EncodeToString(key))
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		return fmt.Errorf("ComfyUI websocket: %s", resp.Status)
	}
	for {
		op, payload, err := readFrame(br)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		switch op {
		case 1: // text
			var e Event
			if json.Unmarshal(payload, &e) == nil {
				on(e)
			}
		case 2: // binary: a preview of the image forming (when ComfyUI runs with --preview-method)
			if img := previewImage(payload); img != nil {
				on(Event{Type: "preview", Data: map[string]any{"image": img}})
			}
		case 8: // close
			return nil
		case 9: // ping → pong
			writeFrame(conn, 10, payload)
		}
	}
}

// previewImage: the image in a binary event (1: a preview, 4: a preview with its metadata)
func previewImage(b []byte) []byte {
	if len(b) < 8 {
		return nil
	}
	switch binary.BigEndian.Uint32(b) {
	case 1:
		return b[8:] // (then its format: 1 JPEG, 2 PNG)
	case 4:
		n := int(binary.BigEndian.Uint32(b[4:]))
		if 8+n <= len(b) {
			return b[8+n:]
		}
	}
	return nil
}

func readFrame(r *bufio.Reader) (byte, []byte, error) {
	var h [2]byte
	if _, err := io.ReadFull(r, h[:]); err != nil {
		return 0, nil, err
	}
	op := h[0] & 0x0f
	n := uint64(h[1] & 0x7f)
	switch n {
	case 126:
		var b [2]byte
		if _, err := io.ReadFull(r, b[:]); err != nil {
			return 0, nil, err
		}
		n = uint64(binary.BigEndian.Uint16(b[:]))
	case 127:
		var b [8]byte
		if _, err := io.ReadFull(r, b[:]); err != nil {
			return 0, nil, err
		}
		n = binary.BigEndian.Uint64(b[:])
	}
	if n > 64<<20 {
		return 0, nil, errors.New("websocket frame too big")
	}
	var mask [4]byte
	masked := h[1]&0x80 != 0
	if masked {
		if _, err := io.ReadFull(r, mask[:]); err != nil {
			return 0, nil, err
		}
	}
	p := make([]byte, n)
	if _, err := io.ReadFull(r, p); err != nil {
		return 0, nil, err
	}
	if masked {
		for i := range p {
			p[i] ^= mask[i%4]
		}
	}
	return op, p, nil
}

// writeFrame: a masked frame (from the client)
func writeFrame(w io.Writer, op byte, p []byte) {
	var b bytes.Buffer
	b.WriteByte(0x80 | op)
	switch {
	case len(p) < 126:
		b.WriteByte(0x80 | byte(len(p)))
	case len(p) < 65536:
		b.WriteByte(0x80 | 126)
		binary.Write(&b, binary.BigEndian, uint16(len(p)))
	default:
		b.WriteByte(0x80 | 127)
		binary.Write(&b, binary.BigEndian, uint64(len(p)))
	}
	var mask [4]byte
	_, _ = rand.Read(mask[:])
	b.Write(mask[:])
	for i, c := range p {
		b.WriteByte(c ^ mask[i%4])
	}
	w.Write(b.Bytes())
}

// NewClientID: an id for a run's progress
func NewClientID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "audioviz-" + hex.EncodeToString(b)
}
