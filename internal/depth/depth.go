// Package depth works out how near each point of an image is (a depth map) with Depth Anything 3
// (depth-anything.cpp, on the GPU through the same ggml as the image and lyrics models), or from
// its brightness, and keeps the result in the project's cache.
package depth

/*
#cgo CFLAGS: -DGGML_MAX_NAME=160
#cgo linux CFLAGS: -I${SRCDIR}/../../third_party/build/include
#cgo linux LDFLAGS: ${SRCDIR}/../../third_party/build/lib/libdepthanything.a ${SRCDIR}/../../third_party/build/lib/libggml.a ${SRCDIR}/../../third_party/build/lib/libggml-vulkan.a ${SRCDIR}/../../third_party/build/lib/libggml-cpu.a ${SRCDIR}/../../third_party/build/lib/libggml-base.a -l:libstdc++.a -lm -lpthread -ldl
#cgo windows CFLAGS: -I${SRCDIR}/../../third_party/build-windows-amd64/include
#cgo windows LDFLAGS: ${SRCDIR}/../../third_party/build-windows-amd64/lib/libdepthanything.a ${SRCDIR}/../../third_party/build-windows-amd64/lib/libggml.a ${SRCDIR}/../../third_party/build-windows-amd64/lib/libggml-vulkan.a ${SRCDIR}/../../third_party/build-windows-amd64/lib/libggml-cpu.a ${SRCDIR}/../../third_party/build-windows-amd64/lib/libggml-base.a -lc++
#cgo darwin,arm64 CFLAGS: -I${SRCDIR}/../../third_party/build-darwin-arm64/include
#cgo darwin,arm64 LDFLAGS: ${SRCDIR}/../../third_party/build-darwin-arm64/lib/libdepthanything.a ${SRCDIR}/../../third_party/build-darwin-arm64/lib/libggml.a ${SRCDIR}/../../third_party/build-darwin-arm64/lib/libggml-metal.a ${SRCDIR}/../../third_party/build-darwin-arm64/lib/libggml-blas.a ${SRCDIR}/../../third_party/build-darwin-arm64/lib/libggml-cpu.a ${SRCDIR}/../../third_party/build-darwin-arm64/lib/libggml-base.a -framework Accelerate -framework Metal -framework MetalKit -framework Foundation -lc++
#cgo darwin,amd64 CFLAGS: -I${SRCDIR}/../../third_party/build-darwin-amd64/include
#cgo darwin,amd64 LDFLAGS: ${SRCDIR}/../../third_party/build-darwin-amd64/lib/libdepthanything.a ${SRCDIR}/../../third_party/build-darwin-amd64/lib/libggml.a ${SRCDIR}/../../third_party/build-darwin-amd64/lib/libggml-metal.a ${SRCDIR}/../../third_party/build-darwin-amd64/lib/libggml-blas.a ${SRCDIR}/../../third_party/build-darwin-amd64/lib/libggml-cpu.a ${SRCDIR}/../../third_party/build-darwin-amd64/lib/libggml-base.a -framework Accelerate -framework Metal -framework MetalKit -framework Foundation -lc++
#include <stdlib.h>
#include <da_capi.h>
*/
import "C"

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"
	"unsafe"

	"audioviz/internal/aimodel"
	"audioviz/internal/imagegen"
	_ "audioviz/internal/ggml" // the shared Vulkan loader bridge
)

// Kinds of model output (Spec.Kind)
const (
	KindDA3    = "da3"    // Depth Anything 3 (depth + confidence + pose): relative depth, far = high
	KindMono   = "mono"   // DA3 Mono / Metric: depth (far = high) and a sky mask
	KindNested = "nested" // DA3 Nested Giant + Large: metric depth (two files)
	KindDA2    = "da2"    // Depth Anything 2 relative: inverse depth, near = high
	KindDA2M   = "da2m"   // Depth Anything 2 metric: metres, far = high
)

// Spec is a depth model: its weights (and a second file for the nested one), what it gives back
// and the size of image it was trained at
type Spec struct {
	aimodel.Model
	Branch *aimodel.Model // the nested model's metric branch
	Kind   string
	Tile   int // the size it works at (px, a multiple of its 14 px patches)
}

const hf = "https://huggingface.co/mudler/depth-anything.cpp-gguf/resolve/main/"

func spec(name, file string, size int64, label, kind string) Spec {
	tile := 504
	if kind == KindDA2 || kind == KindDA2M {
		tile = 518
	}
	return Spec{Model: aimodel.Model{Name: "depth-" + name, File: file, Size: size, Label: label, URL: hf + file}, Kind: kind, Tile: tile}
}

// Models: the Depth Anything 3 family and Depth Anything 2, as converted for depth-anything.cpp.
// Their licenses differ: DA3 Small, Base, Mono and Metric and DA2 Small are Apache-2.0; DA3 Large,
// Giant and Nested and DA2 Base and Large (metric ones too) are CC BY-NC 4.0 (non-commercial).
var Models = map[string]Spec{
	"small":  spec("small", "depth-anything-small-f32.gguf", 104737600, "Depth Anything 3 Small", KindDA3),
	"base":   spec("base", "depth-anything-base-q8_0.gguf", 148713024, "Depth Anything 3 Base", KindDA3),
	"large":  spec("large", "depth-anything-large-f32.gguf", 1381787616, "Depth Anything 3 Large", KindDA3),
	"giant":  spec("giant", "depth-anything-giant-f32.gguf", 4906314144, "Depth Anything 3 Giant", KindDA3),
	"mono":   spec("mono", "depth-anything-mono-large-f32.gguf", 1336711232, "Depth Anything 3 Mono Large", KindMono),
	"metric": spec("metric", "depth-anything-metric-large-f32.gguf", 1336711232, "Depth Anything 3 Metric Large", KindMono),
	"nested": func() Spec {
		s := spec("nested", "depth-anything-nested-anyview.gguf", 4906314144, "Depth Anything 3 Nested Giant + Large", KindNested)
		b := spec("nested-metric", "depth-anything-nested-metric.gguf", 1336711840, "Depth Anything 3 Nested (metric branch)", KindNested)
		s.Branch = &b.Model
		return s
	}(),
	"da2-small":   spec("da2-small", "depth-anything2-small-f32.gguf", 99154784, "Depth Anything 2 Small", KindDA2),
	"da2-base":    spec("da2-base", "depth-anything2-base-q8_0.gguf", 140400448, "Depth Anything 2 Base", KindDA2),
	"da2-large":   spec("da2-large", "depth-anything2-large-q8_0.gguf", 454188672, "Depth Anything 2 Large", KindDA2),
	"da2-indoor":  spec("da2-indoor", "depth-anything2-metric-hypersim-large-q8_0.gguf", 454188736, "Depth Anything 2 Metric Indoor Large", KindDA2M),
	"da2-outdoor": spec("da2-outdoor", "depth-anything2-metric-vkitti-large-q8_0.gguf", 454188704, "Depth Anything 2 Metric Outdoor Large", KindDA2M),
}

// ModelFor: the model of a name (small if unknown)
func ModelFor(name string) Spec {
	if m, ok := Models[name]; ok {
		return m
	}
	return Models["small"]
}

// Ensure downloads what is missing (both files of the nested model): the local paths.
// progress(done, total) over all of it
func (sp Spec) Ensure(ctx context.Context, progress func(done, total int64)) ([]string, error) {
	ms := []aimodel.Model{sp.Model}
	if sp.Branch != nil {
		ms = append(ms, *sp.Branch)
	}
	var total, before int64
	for _, m := range ms {
		total += m.Size
	}
	var paths []string
	for _, m := range ms {
		p, err := m.Ensure(ctx, func(done, _ int64, _ float64) { progress(before+done, total) })
		if err != nil {
			return nil, err
		}
		before += m.Size
		paths = append(paths, p)
	}
	return paths, nil
}

// Plain: it can work at a larger size than it was trained at in one go (the others, by tiles)
func (sp Spec) Plain() bool { return sp.Kind == KindDA3 }

// Map is a depth map: how near each point is, 0 (far) … 1 (near), W×H row by row
type Map struct {
	W, H int
	V    []float32
}

// At: the nearness at (x, y) of the map (clamped)
func (m *Map) At(x, y int) float32 {
	x, y = max(0, min(m.W-1, x)), max(0, min(m.H-1, y))
	return m.V[y*m.W+x]
}

var (
	mu      sync.Mutex
	ctx     *C.da_ctx
	ctxFile string
)

// DetailSize: the long side (px) the model works at for a detail level: normal is the size it
// was trained at; higher sizes keep finer shapes (and take longer: about 0.3 s, 0.9 s, 2.7 s for
// Small on an Intel iGPU); 0 (native): the image's own size, by tiles
func DetailSize(detail string) int {
	switch detail {
	case "normal":
		return 504
	case "max":
		return 1512
	case "native":
		return 0
	}
	return 1008
}

// Raw is what a model gives back, made comparable: larger = nearer (inverse depth, at any scale)
// and, from the models that see it, how much each point is sky (0..1)
type Raw struct {
	W, H int
	V    []float32
	Sky  []float32
}

// lastDev: the device set in DA_DEVICE from the engine setting (one set by hand is kept)
var lastDev string

// load: the model's context (kept between calls)
func load(sp Spec, paths []string) error {
	// on the image engine's device (Settings › Plugins: the Intel plugin's SYCL), unless DA_DEVICE says
	dev := os.Getenv("DA_DEVICE")
	if dev == "" || dev == lastDev {
		dev = imagegen.DeviceName()
		if dev != "" {
			os.Setenv("DA_DEVICE", dev)
		} else {
			os.Unsetenv("DA_DEVICE")
		}
		lastDev = dev
	}
	key := dev + "|" + strings.Join(paths, "|")
	if ctx != nil && ctxFile == key {
		return nil
	}
	if ctx != nil {
		C.da_capi_free(ctx)
		ctx = nil
	}
	cs := make([]*C.char, len(paths))
	for i, p := range paths {
		cs[i] = C.CString(p)
		defer C.free(unsafe.Pointer(cs[i]))
	}
	t0 := time.Now()
	if sp.Kind == KindNested && len(cs) == 2 {
		ctx = C.da_capi_load_nested(cs[0], cs[1], C.int(runtime.NumCPU()))
	} else {
		ctx = C.da_capi_load(cs[0], C.int(runtime.NumCPU()))
	}
	debugf("load %s %v", sp.Name, time.Since(t0))
	if ctx == nil {
		return errors.New("the depth model could not be loaded")
	}
	ctxFile = key
	return nil
}

// Infer runs the model (its files already downloaded) on an image file. plain: at the image's
// own size (both sides multiples of 14; Spec.Plain models only); otherwise the model's own resize
// policy, the size it was trained at.
func Infer(sp Spec, paths []string, imagePath string, plain bool) (*Raw, error) {
	mu.Lock()
	defer mu.Unlock()
	if err := load(sp, paths); err != nil {
		return nil, err
	}
	ci := C.CString(imagePath)
	defer C.free(unsafe.Pointer(ci))
	var h, w, metric C.int
	var p, conf, sky *C.float
	var ext [12]C.float
	var intr [9]C.float
	t1 := time.Now()
	var rc C.int
	if plain && sp.Plain() {
		if p = C.da_capi_depth_path(ctx, ci, &h, &w); p == nil {
			rc = -1
		}
	} else {
		rc = C.da_capi_depth_dense(ctx, ci, &h, &w, &p, &conf, &sky, &ext[0], &intr[0], &metric)
	}
	debugf("depth %v %dx%d", time.Since(t1), int(w), int(h))
	if conf != nil {
		C.da_capi_free_floats(conf)
	}
	if rc != 0 || p == nil {
		if sky != nil {
			C.da_capi_free_floats(sky)
		}
		msg := C.GoString(C.da_capi_last_error(ctx))
		if msg == "" {
			msg = "the depth could not be worked out"
		}
		return nil, errors.New(msg)
	}
	defer C.da_capi_free_floats(p)
	n := int(w) * int(h)
	raw := unsafe.Slice((*float32)(unsafe.Pointer(p)), n)
	r := &Raw{W: int(w), H: int(h), V: make([]float32, n)}
	// on a log scale: the near and the far both keep their steps (inverse depth gives all the
	// range to what is nearest, plain depth to what is farthest)
	var top float32
	for _, v := range raw {
		top = max(top, v)
	}
	for i, v := range raw {
		d := max(v, 1e-4)
		if sp.Kind == KindDA2 {
			d = 1 / max(v, top*1e-3) // it gives inverse depth
		}
		r.V[i] = -float32(math.Log(float64(d)))
	}
	if sp.Kind == KindDA2 && top > 0 {
		// Depth Anything 2 gives the sky (endless) as no nearness at all: it is the sky
		r.Sky = make([]float32, n)
		for i, v := range raw {
			r.Sky[i] = 1 - smooth(0.002, 0.01, v/top)
		}
	}
	if sky != nil {
		if r.Sky == nil {
			s := unsafe.Slice((*float32)(unsafe.Pointer(sky)), n)
			r.Sky = make([]float32, n)
			for i, v := range s {
				r.Sky[i] = max(0, min(1, v))
			}
		}
		C.da_capi_free_floats(sky)
	}
	return r, nil
}

// Nearness: a Raw as a depth map 0 (far) … 1 (near), stretched between its 0.2 % and 99.8 %
// (a few odd points don't squash the rest) and the sky at the very back
func (r *Raw) Nearness() *Map {
	lo, hi := percentiles(r.V, r.Sky, 0.002, 0.998)
	m := &Map{W: r.W, H: r.H, V: make([]float32, len(r.V))}
	span := max(hi-lo, 1e-9)
	for i, v := range r.V {
		m.V[i] = max(0, min(1, (v-lo)/span))
		if r.Sky != nil {
			m.V[i] *= 1 - smooth(0.3, 0.7, r.Sky[i])
		}
	}
	return m
}

// percentiles a, b of v (leaving out the sky)
func percentiles(v, sky []float32, a, b float64) (float32, float32) {
	s := make([]float32, 0, len(v))
	for i, x := range v {
		if sky == nil || sky[i] < 0.5 {
			s = append(s, x)
		}
	}
	if len(s) == 0 {
		s = append(s, v...)
	}
	slices.Sort(s)
	at := func(q float64) float32 { return s[min(len(s)-1, int(q*float64(len(s)-1)+0.5))] }
	return at(a), at(b)
}

func smooth(a, b, x float32) float32 {
	t := max(0, min(1, (x-a)/(b-a)))
	return t * t * (3 - 2*t)
}

// Unload frees the model (it is loaded again when needed)
func Unload() {
	mu.Lock()
	defer mu.Unlock()
	if ctx != nil {
		C.da_capi_free(ctx)
		ctx, ctxFile = nil, ""
	}
}

// debugf prints timings with AUDIOVIZ_DEBUG_DEPTH=1
func debugf(f string, a ...any) {
	if os.Getenv("AUDIOVIZ_DEBUG_DEPTH") != "" {
		fmt.Fprintf(os.Stderr, "[depth] "+f+"\n", a...)
	}
}
