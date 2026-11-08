package depth

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"audioviz/internal/video"
)

// A video background's depth on every frame (Request.All): the same sources and models as for an
// image, run on each frame of the video in order, then steadied (each frame's map is fitted to
// the previous one, then blended with it where little changed), so the depth follows the video
// without flickering. Kept in the project's cache as a folder of 16-bit PNGs (one per frame of
// the video, at most seqMaxSide px) and a meta.json written last (the sequence is complete).

const (
	seqMaxSide = 1280 // px: the long side the frames' maps are kept at
	seqDecode  = 1920 // px: the long side the video is decoded at for the models
)

// Seq: a complete depth sequence in the cache
type Seq struct {
	Dir string
	FPS float64
	N   int
	Dur float64
}

type seqMeta struct {
	FPS float64 `json:"fps"`
	N   int     `json:"n"`
	Dur float64 `json:"dur"`
}

// SeqDir: the folder of a request's sequence
func SeqDir(cacheDir string, r Request) string {
	return strings.TrimSuffix(CachePath(cacheDir, r), ".png") + ".seq"
}

func (s *Seq) framePath(i int) string { return filepath.Join(s.Dir, fmt.Sprintf("%05d.png", i)) }

// OpenSeq: the request's sequence if it is complete
func OpenSeq(cacheDir string, r Request) (*Seq, bool) {
	dir := SeqDir(cacheDir, r)
	b, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	if err != nil {
		return nil, false
	}
	var m seqMeta
	if json.Unmarshal(b, &m) != nil || m.N < 1 || m.FPS <= 0 {
		return nil, false
	}
	return &Seq{Dir: dir, FPS: m.FPS, N: m.N, Dur: m.Dur}, true
}

// Index: the frame shown at t s into the video (as the video reader picks it: the nearest frame;
// looped or held at the end)
func (s *Seq) Index(t float64, loop bool) int {
	if t < 0 {
		t = 0
	}
	if loop && s.Dur > 0 {
		t = math.Mod(t, s.Dur)
	}
	return max(0, min(s.N-1, int(math.Round(t*s.FPS))))
}

// Frame: the map of frame i
func (s *Seq) Frame(i int) (*Map, error) { return Load(s.framePath(max(0, min(s.N-1, i)))) }

// makeSeq works out the sequence of a video (resuming one left half done); its first frame's map
// is also kept where a single map goes (CachePath), so a look at the depth shows it
func makeSeq(ctx context.Context, cacheDir string, r Request, stage func(string, float64)) (*Map, error) {
	pr, err := video.ProbeMedia(r.Image)
	if err != nil {
		return nil, err
	}
	fps := pr.FPS
	if fps <= 0 || fps > 240 {
		fps = 30
	}
	n := max(1, int(math.Round(pr.Duration*fps)))
	rd, err := video.NewLayerReader(r.Image, seqDecode, seqDecode, false)
	if err != nil {
		return nil, err
	}
	defer rd.Close()
	s := &Seq{Dir: SeqDir(cacheDir, r), FPS: fps, N: n, Dur: pr.Duration}
	if r.Force {
		os.RemoveAll(s.Dir)
	}
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return nil, err
	}
	if r.Source == SourceAI || r.Source == "" { // the model, once (not by two frames at a time)
		if _, err := ModelFor(r.Model).Ensure(ctx, func(done, total int64) {
			if total > 0 {
				stage("download", float64(done)/float64(total))
			}
		}); err != nil {
			return nil, err
		}
	}
	var prev, first *Map
	var wg sync.WaitGroup
	var saveErr atomic.Pointer[error]
	saves := make(chan struct{}, 2)
	defer wg.Wait()
	// stages: "count" (the number of frames), "frames" (done / all), "frame" (this one's progress)
	stage("count", float64(n))

	// A pipeline, two frames at a time: while the GPU works one out (Infer holds it), the CPU
	// prepares or finishes the other; the frames are steadied and written in order. The video is
	// read in order by one goroutine (frames left from a stopped run are only loaded back)
	type job struct {
		i   int
		img *image.RGBA
	}
	type res struct {
		i     int
		m     *Map
		saved bool // already in the cache
		err   error
	}
	cctx, cancel := context.WithCancel(ctx)
	jobs := make(chan job, 2)
	out := make(chan res, 4)
	defer func() {
		cancel()
		go func() { // (what is still on its way)
			for range out {
			}
		}()
	}()
	var awaited atomic.Int64 // the frame whose progress is shown
	go func() {
		defer close(jobs)
		for i := 0; i < n; i++ {
			if cctx.Err() != nil {
				return
			}
			if !r.Force {
				if _, err := os.Stat(s.framePath(i)); err == nil {
					select {
					case out <- res{i: i, saved: true}:
					case <-cctx.Done():
						return
					}
					continue
				}
			}
			fr, _ := rd.FrameAt(float64(i) / fps)
			img := image.NewRGBA(fr.Bounds())
			copy(img.Pix, fr.Pix)
			select {
			case jobs <- job{i, img}:
			case <-cctx.Done():
				return
			}
		}
	}()
	var workers sync.WaitGroup
	nw := 2
	if r.Source == SourceComfy { // (one flow at a time in ComfyUI)
		nw = 1
	}
	for w := 0; w < nw; w++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for j := range jobs {
				inner := func(st string, p float64) {
					if int64(j.i) != awaited.Load() {
						return
					}
					switch st {
					case "estimate", "comfy":
						stage("frame", 0.9*p)
					case "refine":
						stage("frame", 0.95)
					}
				}
				t0 := time.Now()
				m, err := frameMap(cctx, r, j.img, inner)
				if err == nil {
					m = shrinkMap(m, seqMaxSide)
				}
				debugf("frame %d: map %v", j.i, time.Since(t0))
				select {
				case out <- res{i: j.i, m: m, err: err}:
				case <-cctx.Done():
					return
				}
			}
		}()
	}
	go func() {
		workers.Wait()
		close(out)
	}()
	pending := map[int]res{}
	for i := 0; i < n; i++ {
		awaited.Store(int64(i))
		stage("frames", float64(i)/float64(n))
		stage("frame", 0)
		rr, ok := pending[i]
		for !ok {
			x, open := <-out
			if !open {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				return nil, fmt.Errorf("frame %d of %d was not worked out", i+1, n)
			}
			pending[x.i] = x
			rr, ok = pending[i]
		}
		delete(pending, i)
		if rr.err != nil {
			return nil, fmt.Errorf("frame %d of %d: %w", i+1, n, rr.err)
		}
		m := rr.m
		if rr.saved {
			var err error
			if m, err = Load(s.framePath(i)); err != nil {
				return nil, err
			}
		} else {
			if prev != nil && prev.W == m.W && prev.H == m.H {
				steady(m, prev)
			}
			// written while the next frames are worked out (at most two at a time)
			saves <- struct{}{}
			wg.Add(1)
			go func(m *Map, path string) {
				defer func() { <-saves; wg.Done() }()
				if err := m.Save(path); err != nil {
					saveErr.CompareAndSwap(nil, &err)
				}
			}(m, s.framePath(i))
		}
		prev = m
		if i == 0 {
			first = m
		}
	}
	wg.Wait()
	if e := saveErr.Load(); e != nil {
		return nil, *e
	}
	if err := first.Save(CachePath(cacheDir, r)); err != nil {
		return nil, err
	}
	b, _ := json.Marshal(seqMeta{FPS: fps, N: n, Dur: pr.Duration})
	if err := os.WriteFile(filepath.Join(s.Dir, "meta.json"), b, 0o644); err != nil {
		return nil, err
	}
	stage("done", 1)
	return first, nil
}

// frameMap: one frame's map with the request's source (a map of my own has no frames)
func frameMap(ctx context.Context, r Request, img *image.RGBA, stage func(string, float64)) (*Map, error) {
	W, H := img.Bounds().Dx(), img.Bounds().Dy()
	switch r.Source {
	case SourceBrightness:
		return FromBrightness(img), nil
	case SourceComfy:
		makersMu.Lock()
		mk := makers[r.Source]
		makersMu.Unlock()
		if mk == nil {
			return nil, fmt.Errorf("a ComfyUI depth map is made from the studio (Depth section: Work out the depth)")
		}
		out, err := mk(ctx, r, img, stage)
		if err != nil {
			return nil, err
		}
		return FromImage(out, W, H), nil
	case SourceFile:
		return nil, fmt.Errorf("a depth map of my own has no frames: choose the AI, ComfyUI or the brightness")
	}
	return aiMap(ctx, img, r, stage)
}

// shrinkMap: m with its long side at most side px (box average)
func shrinkMap(m *Map, side int) *Map {
	k := max(m.W, m.H)
	if k <= side {
		return m
	}
	w, h := max(1, m.W*side/k), max(1, m.H*side/k)
	out := &Map{W: w, H: h, V: make([]float32, w*h)}
	rows(h, func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			sy0, sy1 := y*m.H/h, max(y*m.H/h+1, (y+1)*m.H/h)
			for x := 0; x < w; x++ {
				sx0, sx1 := x*m.W/w, max(x*m.W/w+1, (x+1)*m.W/w)
				var sum float32
				for sy := sy0; sy < sy1; sy++ {
					for sx := sx0; sx < sx1; sx++ {
						sum += m.V[sy*m.W+sx]
					}
				}
				out.V[y*w+x] = sum / float32((sy1-sy0)*(sx1-sx0))
			}
		}
	})
	return out
}

// steady: frame m fitted to the previous one (the models give each image its own scale, so a
// frame's nearness is a·v + b of the last one's, worked out on the points that did not move much),
// then blended with it where it changed little (still parts stop shimmering; what moves follows
// at once)
func steady(m, prev *Map) {
	a, b := 1.0, 0.0
	for pass := 0; pass < 3; pass++ {
		var sw, sx, sy, sxx, sxy float64
		for i := 0; i < len(m.V); i += 7 {
			x, y := float64(m.V[i]), float64(prev.V[i])
			if pass > 0 && math.Abs(a*x+b-y) > 0.08 { // (what moved: left out)
				continue
			}
			sw++
			sx += x
			sy += y
			sxx += x * x
			sxy += x * y
		}
		den := sw*sxx - sx*sx
		if sw < 100 || den <= 1e-9 {
			return
		}
		a = (sw*sxy - sx*sy) / den
		b = (sy - a*sx) / sw
	}
	if a < 0.5 || a > 2 { // (a cut or a wholly new view: left as it is)
		return
	}
	for i, v := range m.V {
		z := float32(a)*v + float32(b)
		d := z - prev.V[i]
		if d < 0 {
			d = -d
		}
		k := 0.45 + 0.55*smooth(0.02, 0.12, d) // how much of the new frame
		m.V[i] = max(0, min(1, prev.V[i]+(z-prev.V[i])*k))
	}
}
