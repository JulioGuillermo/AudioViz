package depth

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"audioviz/internal/video"
)

// Sources of a depth map (config.DepthConfig.Source)
const (
	SourceAI         = "ai"
	SourceBrightness = "brightness"
	SourceFile       = "file"
	SourceComfy      = "comfy" // a ComfyUI flow of the project (Request.Model: its id, File: its file)
)

// Maker: a depth map made elsewhere (a ComfyUI flow: the studio registers it), any size
type Maker func(ctx context.Context, r Request, img *image.RGBA, stage func(string, float64)) (image.Image, error)

var (
	makersMu sync.Mutex
	makers   = map[string]Maker{}
)

// Register: who makes the maps of a source
func Register(source string, m Maker) {
	makersMu.Lock()
	makers[source] = m
	makersMu.Unlock()
}

// FlowFile: where a project keeps its ComfyUI flow id
func FlowFile(projectDir, id string) string {
	return filepath.Join(projectDir, "comfy", filepath.Base(id)+".json")
}

// rows runs f over [0, n) in parallel blocks
func rows(n int, f func(y0, y1 int)) {
	k := min(runtime.NumCPU(), max(1, n/16))
	var wg sync.WaitGroup
	for i := 0; i < k; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			f(n*i/k, n*(i+1)/k)
		}(i)
	}
	wg.Wait()
}

// firstFrame: the image of a background file at its own size (the first frame of a GIF or video)
func firstFrame(path string) (*image.RGBA, error) {
	r, err := video.NewLayerReader(path, 4096, 4096, false)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	img, _ := r.FrameAt(0)
	if img == nil {
		return nil, fmt.Errorf("could not read %s", filepath.Base(path))
	}
	cp := image.NewRGBA(img.Bounds())
	copy(cp.Pix, img.Pix)
	return cp, nil
}

// Guided enlarges a coarse depth map to the size of its image, following the image's edges (a
// joint bilateral filter: each pixel takes the coarse samples around it, weighted by how near
// they are and by how alike the image's color is there), so depth edges stay on the image's edges
func Guided(m *Map, img *image.RGBA) *Map {
	W, H := img.Bounds().Dx(), img.Bounds().Dy()
	// the image's mean color at each coarse sample
	low := make([][3]float32, m.W*m.H)
	sx, sy := float64(W)/float64(m.W), float64(H)/float64(m.H)
	rows(m.H, func(y0, y1 int) {
		for ly := y0; ly < y1; ly++ {
			for lx := 0; lx < m.W; lx++ {
				x0, x1 := int(float64(lx)*sx), max(int(float64(lx)*sx)+1, int(float64(lx+1)*sx))
				yy0, yy1 := int(float64(ly)*sy), max(int(float64(ly)*sy)+1, int(float64(ly+1)*sy))
				var c [3]float32
				n := 0
				for y := yy0; y < min(yy1, H); y++ {
					for x := x0; x < min(x1, W); x++ {
						o := y*img.Stride + x*4
						c[0] += float32(img.Pix[o])
						c[1] += float32(img.Pix[o+1])
						c[2] += float32(img.Pix[o+2])
						n++
					}
				}
				if n > 0 {
					for j := range c {
						c[j] /= float32(n) * 255
					}
				}
				low[ly*m.W+lx] = c
			}
		}
	})
	out := &Map{W: W, H: H, V: make([]float32, W*H)}
	const sigmaR = 0.12
	rows(H, func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			for x := 0; x < W; x++ {
				o := y*img.Stride + x*4
				cr, cg, cb := float32(img.Pix[o])/255, float32(img.Pix[o+1])/255, float32(img.Pix[o+2])/255
				qx, qy := (float64(x)+0.5)/sx-0.5, (float64(y)+0.5)/sy-0.5
				ix, iy := int(math.Floor(qx)), int(math.Floor(qy))
				var sum, wsum float32
				for dy := -1; dy <= 2; dy++ {
					for dx := -1; dx <= 2; dx++ {
						lx, ly := max(0, min(m.W-1, ix+dx)), max(0, min(m.H-1, iy+dy))
						ex, ey := float64(ix+dx)-qx, float64(iy+dy)-qy
						ws := float32(math.Exp(-(ex*ex + ey*ey) / 1.4))
						c := low[ly*m.W+lx]
						d := (c[0]-cr)*(c[0]-cr) + (c[1]-cg)*(c[1]-cg) + (c[2]-cb)*(c[2]-cb)
						w := ws * float32(math.Exp(float64(-d/(2*sigmaR*sigmaR))))
						sum += w * m.V[ly*m.W+lx]
						wsum += w
					}
				}
				if wsum > 1e-8 {
					out.V[y*W+x] = sum / wsum
				} else {
					out.V[y*W+x] = m.At(ix, iy)
				}
			}
		}
	})
	return out
}

// FromBrightness: a quick depth guess without AI: bright = near, softened
func FromBrightness(img *image.RGBA) *Map {
	W, H := img.Bounds().Dx(), img.Bounds().Dy()
	m := &Map{W: W, H: H, V: make([]float32, W*H)}
	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			o := y*img.Stride + x*4
			m.V[y*W+x] = (0.2126*float32(img.Pix[o]) + 0.7152*float32(img.Pix[o+1]) + 0.0722*float32(img.Pix[o+2])) / 255
		}
	}
	blur(m, max(1, W/150))
	normalize(m)
	return m
}

// blur: three box blurs of radius r (≈ gaussian), rows then columns
func blur(m *Map, r int) {
	tmp := make([]float32, len(m.V))
	pass := func(src, dst []float32, n, stride, count, step int) {
		rows(count, func(a, b int) {
			for k := a; k < b; k++ {
				base := k * step
				var acc float32
				w := float32(2*r + 1)
				get := func(i int) float32 { return src[base+max(0, min(n-1, i))*stride] }
				for i := -r; i <= r; i++ {
					acc += get(i)
				}
				for i := 0; i < n; i++ {
					dst[base+i*stride] = acc / w
					acc += get(i+r+1) - get(i-r)
				}
			}
		})
	}
	for it := 0; it < 3; it++ {
		pass(m.V, tmp, m.W, 1, m.H, m.W)
		pass(tmp, m.V, m.H, m.W, m.W, 1)
	}
}

func normalize(m *Map) {
	lo, hi := float32(math.Inf(1)), float32(math.Inf(-1))
	for _, v := range m.V {
		lo, hi = min(lo, v), max(hi, v)
	}
	span := max(hi-lo, 1e-6)
	for i, v := range m.V {
		m.V[i] = (v - lo) / span
	}
}

// FromFile: a depth map of your own (white = near), at the size of the image
func FromFile(path string, W, H int) (*Map, error) {
	img, err := firstFrame(path)
	if err != nil {
		return nil, err
	}
	return FromImage(img, W, H), nil
}

// FromImage: a depth map in an image (white = near), at size W×H
func FromImage(src image.Image, W, H int) *Map {
	img, ok := src.(*image.RGBA)
	if !ok || img.Bounds().Min != (image.Point{}) {
		img = image.NewRGBA(image.Rect(0, 0, src.Bounds().Dx(), src.Bounds().Dy()))
		draw.Draw(img, img.Bounds(), src, src.Bounds().Min, draw.Src)
	}
	sw, sh := img.Bounds().Dx(), img.Bounds().Dy()
	m := &Map{W: W, H: H, V: make([]float32, W*H)}
	rows(H, func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			for x := 0; x < W; x++ {
				o := (y*sh/H)*img.Stride + (x*sw/W)*4
				m.V[y*W+x] = (0.2126*float32(img.Pix[o]) + 0.7152*float32(img.Pix[o+1]) + 0.0722*float32(img.Pix[o+2])) / 255
			}
		}
	})
	return m
}

// Save writes the map as a 16-bit grey PNG (white = near)
func (m *Map) Save(path string) error {
	img := image.NewGray16(image.Rect(0, 0, m.W, m.H))
	for i, v := range m.V {
		y := uint16(max(0, min(1, v))*65535 + 0.5)
		img.Pix[2*i], img.Pix[2*i+1] = byte(y>>8), byte(y)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path + ".tmp")
	if err != nil {
		return err
	}
	if err := (&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(f, img); err != nil {
		f.Close()
		return err
	}
	f.Close()
	return os.Rename(path+".tmp", path)
}

// Load reads a map saved by Save (or any grey image: white = near)
func Load(path string) (*Map, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		return nil, err
	}
	b := img.Bounds()
	m := &Map{W: b.Dx(), H: b.Dy(), V: make([]float32, b.Dx()*b.Dy())}
	if g, ok := img.(*image.Gray16); ok {
		for i := range m.V {
			x, y := i%m.W, i/m.W
			m.V[i] = float32(g.Gray16At(b.Min.X+x, b.Min.Y+y).Y) / 65535
		}
		return m, nil
	}
	for i := range m.V {
		r, g, bb, _ := img.At(b.Min.X+i%m.W, b.Min.Y+i/m.W).RGBA()
		m.V[i] = (0.2126*float32(r) + 0.7152*float32(g) + 0.0722*float32(bb)) / 65535
	}
	return m, nil
}

// Request: the depth map wanted for a background file
type Request struct {
	Image  string // the background (image, GIF or video: its first frame)
	Source string // ai, brightness, file
	Model  string // ai: small or base
	Detail string // ai: normal, high, max (DetailSize)
	File   string // file: the depth map
	All    bool   // a video: every frame (a sequence, seq.go), not only the first
	Force  bool   // worked out again though it is in the cache
}

// CachePath: where the map of a request is kept in the project's cache folder (it changes with
// the background file, its date, the source and the model)
func CachePath(cacheDir string, r Request) string {
	h := sha1.New()
	for _, p := range []string{r.Image, r.File} {
		fmt.Fprintf(h, "%s|", p)
		if st, err := os.Stat(p); err == nil {
			fmt.Fprintf(h, "%d|%d|", st.Size(), st.ModTime().UnixNano())
		}
	}
	model := r.Model
	switch r.Source {
	case SourceAI:
		model += "|" + fmt.Sprint(DetailSize(r.Detail))
	case SourceComfy: // (its flow, File, counts with its date)
	default:
		model = ""
	}
	fmt.Fprintf(h, "%s|%s|v2", r.Source, model)
	if r.All {
		fmt.Fprint(h, "|all")
	}
	return filepath.Join(cacheDir, "depth", hex.EncodeToString(h.Sum(nil))[:20]+".png")
}

// Cached: the map of a request if it is already in the cache (every frame's: its first frame's,
// once the whole sequence is there)
func Cached(cacheDir string, r Request) (*Map, bool) {
	if r.All {
		if _, ok := OpenSeq(cacheDir, r); !ok {
			return nil, false
		}
	}
	m, err := Load(CachePath(cacheDir, r))
	return m, err == nil
}

// Make works out the map of a request (the image's size), keeps it in the cache and returns it.
// stage(label, progress 0..1) reports what it is doing (downloading the model, working out...).
func Make(ctx context.Context, cacheDir string, r Request, stage func(string, float64)) (*Map, error) {
	if m, ok := Cached(cacheDir, r); ok && !r.Force {
		return m, nil
	}
	if r.All {
		return makeSeq(ctx, cacheDir, r, stage)
	}
	img, err := firstFrame(r.Image)
	if err != nil {
		return nil, err
	}
	W, H := img.Bounds().Dx(), img.Bounds().Dy()
	var m *Map
	switch r.Source {
	case SourceBrightness:
		stage("brightness", 0)
		m = FromBrightness(img)
	case SourceFile:
		stage("file", 0)
		if m, err = FromFile(r.File, W, H); err != nil {
			return nil, err
		}
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
		m = FromImage(out, W, H)
	default:
		if m, err = aiMap(ctx, img, r, stage); err != nil {
			return nil, err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := m.Save(CachePath(cacheDir, r)); err != nil {
		return nil, err
	}
	stage("done", 1)
	return m, nil
}
