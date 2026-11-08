package studio

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"sync"

	"audioviz/internal/depth"
	"audioviz/internal/video"
)

// The depth editor (web/depthedit.js): a background's depth map (a still's, or each frame of a
// video's sequence) painted by hand. The maps travel as raw 16-bit nearness (little endian, row
// by row: what the editor works on), the edits are kept by internal/depth (depth-edits folder).

func depthQuery(q url.Values) depthReq {
	return depthReq{Image: q.Get("image"), Source: q.Get("source"), Model: q.Get("model"), Detail: q.Get("detail"),
		File: q.Get("file"), Flow: q.Get("flow"), All: q.Get("all") == "1"}
}

// GET /api/depth/edit/info?(as /api/depth/view): {ready, frames, fps, w, h, edited: [frame…]}
func (s *Server) hDepthEditInfo(w http.ResponseWriter, r *http.Request) {
	dir := s.cacheDir()
	req := s.depthRequest(depthQuery(r.URL.Query()))
	out := map[string]any{"ready": false}
	if dir == "" {
		writeJSON(w, out)
		return
	}
	mw, mh, ok := depth.Size(dir, req)
	if !ok {
		writeJSON(w, out)
		return
	}
	n, fps := 1, 0.0
	if req.All {
		sq, _ := depth.OpenSeq(dir, req)
		n, fps = sq.N, sq.FPS
	}
	ed := depth.Edited(dir, req)
	if ed == nil {
		ed = []int{}
	}
	writeJSON(w, map[string]any{"ready": true, "frames": n, "fps": fps, "w": mw, "h": mh, "edited": ed})
}

// GET /api/depth/edit/map?…&frame=i[&orig=1]: frame i's map as raw 16-bit (X-Width, X-Height,
// X-Edited headers); orig: the worked out one, without the hand edit
// PUT (same query, &w=&h=): the edited map of frame i (raw 16-bit, w×h)
// DELETE (same query; frame=all: every frame): back to the worked out map
func (s *Server) hDepthEditMap(w http.ResponseWriter, r *http.Request) {
	dir := s.cacheDir()
	if dir == "" {
		fail(w, 400, errors.New("no project is open"))
		return
	}
	q := r.URL.Query()
	req := s.depthRequest(depthQuery(q))
	frame, _ := strconv.Atoi(q.Get("frame"))
	switch r.Method {
	case http.MethodGet:
		edited := depth.EditStamp(dir, req, frame) != 0
		var m *depth.Map
		var ok bool
		if edited && q.Get("orig") != "1" {
			var err error
			m, err = depth.Load(depth.EditPath(dir, req, frame))
			ok = err == nil
		}
		if !ok {
			m, ok = depth.Original(dir, req, frame)
		}
		if !ok {
			http.Error(w, "not worked out yet", 404)
			return
		}
		b := make([]byte, 2*len(m.V))
		for i, v := range m.V {
			binary.LittleEndian.PutUint16(b[2*i:], uint16(max(0, min(1, v))*65535+0.5))
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Width", strconv.Itoa(m.W))
		w.Header().Set("X-Height", strconv.Itoa(m.H))
		w.Header().Set("X-Edited", strconv.FormatBool(edited))
		w.Write(b)
	case http.MethodPut:
		mw, _ := strconv.Atoi(q.Get("w"))
		mh, _ := strconv.Atoi(q.Get("h"))
		if mw <= 0 || mh <= 0 || mw*mh > 8192*8192 {
			fail(w, 400, errors.New("bad size"))
			return
		}
		b, err := io.ReadAll(io.LimitReader(r.Body, int64(2*mw*mh)+1))
		if err != nil {
			fail(w, 400, err)
			return
		}
		if len(b) != 2*mw*mh {
			fail(w, 400, fmt.Errorf("%d bytes for a %d×%d map", len(b), mw, mh))
			return
		}
		m := &depth.Map{W: mw, H: mh, V: make([]float32, mw*mh)}
		for i := range m.V {
			m.V[i] = float32(binary.LittleEndian.Uint16(b[2*i:])) / 65535
		}
		if err := depth.SaveEdit(dir, req, frame, m); err != nil {
			fail(w, 500, err)
			return
		}
		s.hub.send("depthedit", map[string]any{"image": req.Image, "frame": frame})
		writeJSON(w, map[string]bool{"ok": true})
	case http.MethodDelete:
		i := frame
		if q.Get("frame") == "all" {
			i = -1
		}
		if err := depth.DropEdit(dir, req, i); err != nil {
			fail(w, 500, err)
			return
		}
		s.hub.send("depthedit", map[string]any{"image": req.Image, "frame": i})
		writeJSON(w, map[string]bool{"ok": true})
	default:
		http.Error(w, "method not allowed", 405)
	}
}

// the video being edited, decoded once (the editor goes frame by frame: mostly forward)
var (
	editVidMu   sync.Mutex
	editVidPath string
	editVid     video.BackgroundReader
)

// GET /api/depth/edit/image?image=&frame=&fps=&w=&h=: the background's picture on frame i (a
// still: itself) as JPEG, at most w×h
func (s *Server) hDepthEditImage(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	path := q.Get("image")
	frame, _ := strconv.Atoi(q.Get("frame"))
	fps, _ := strconv.ParseFloat(q.Get("fps"), 64)
	mw, _ := strconv.Atoi(q.Get("w"))
	mh, _ := strconv.Atoi(q.Get("h"))
	mw, mh = max(16, min(mw, 4096)), max(16, min(mh, 4096))
	editVidMu.Lock()
	defer editVidMu.Unlock()
	key := fmt.Sprintf("%s|%d|%d", path, mw, mh)
	if editVid == nil || editVidPath != key {
		if editVid != nil {
			editVid.Close()
			editVid = nil
		}
		rd, err := video.NewLayerReader(path, mw, mh, false)
		if err != nil {
			fail(w, 404, err)
			return
		}
		editVid, editVidPath = rd, key
	}
	t := 0.0
	if fps > 0 {
		t = float64(frame) / fps
	}
	img, _ := editVid.FrameAt(t)
	if img == nil {
		http.Error(w, "no frame", 404)
		return
	}
	// (opaque: what is see-through in a still shows dark; its colors are premultiplied already)
	op := image.NewRGBA(img.Bounds())
	copy(op.Pix, img.Pix)
	for i := 3; i < len(op.Pix); i += 4 {
		op.Pix[i] = 255
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, op, &jpeg.Options{Quality: 88}); err != nil {
		fail(w, 500, err)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(b.Bytes())
}
