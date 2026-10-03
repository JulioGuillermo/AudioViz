package studio

import (
	"bytes"
	"fmt"
	"image/jpeg"
	"math"
	"net/http"
	"os"
	"strconv"
	"sync"

	"audioviz/internal/video"
)

// Thumbnails for the file browser: images, GIFs and videos scaled to fit a square box (aspect
// kept), as JPEG. Kept in memory (key includes size and mtime, so an edited file gets a new
// one); at most 3 are made at once so a folder full of videos doesn't swamp the machine.

const thumbCacheMax = 400

var (
	thumbMu    sync.Mutex
	thumbCache = map[string][]byte{}
	thumbOrder []string
	thumbSem   = make(chan struct{}, 3)
)

func (s *Server) hThumb(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	size, _ := strconv.Atoi(r.URL.Query().Get("s"))
	if size <= 0 {
		size = 240
	}
	size = min(max(size, 64), 480)
	st, err := os.Stat(path)
	if err != nil || st.IsDir() {
		http.NotFound(w, r)
		return
	}
	key := fmt.Sprintf("%s|%d|%d|%d", path, st.Size(), st.ModTime().UnixNano(), size)
	thumbMu.Lock()
	b, ok := thumbCache[key]
	thumbMu.Unlock()
	if !ok {
		thumbSem <- struct{}{}
		b, err = makeThumb(path, size)
		<-thumbSem
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnsupportedMediaType)
			return
		}
		thumbMu.Lock()
		if len(thumbOrder) >= thumbCacheMax {
			delete(thumbCache, thumbOrder[0])
			thumbOrder = thumbOrder[1:]
		}
		thumbCache[key] = b
		thumbOrder = append(thumbOrder, key)
		thumbMu.Unlock()
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Write(b)
}

// makeThumb decodes the file scaled to fit size×size (a video: a frame near its start)
func makeThumb(path string, size int) ([]byte, error) {
	pr, err := video.ProbeMedia(path)
	if err != nil {
		return nil, err
	}
	if pr.Width <= 0 || pr.Height <= 0 {
		return nil, fmt.Errorf("no size")
	}
	tw, th := size, size
	if pr.Width >= pr.Height {
		th = size * pr.Height / pr.Width
	} else {
		tw = size * pr.Width / pr.Height
	}
	tw, th = max(tw&^1, 2), max(th&^1, 2)
	rd, err := video.NewBackgroundReader(path, tw, th, "stretch", false)
	if err != nil {
		return nil, err
	}
	defer rd.Close()
	t := 0.0
	if pr.IsVideo {
		t = math.Min(1.0, pr.Duration*0.1) // skip a possible black first frame
	}
	img, _ := rd.FrameAt(t)
	if img == nil {
		return nil, fmt.Errorf("no frame")
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
