// Package depth works out how near each point of an image is (a depth map) with Depth Anything 3
// (depth-anything.cpp, on the GPU through the same ggml as the image and lyrics models), or from
// its brightness, and keeps the result in the project's cache.
package depth

/*
#cgo CFLAGS: -DGGML_MAX_NAME=160
#cgo linux CFLAGS: -I${SRCDIR}/../../third_party/build/include
#cgo linux LDFLAGS: ${SRCDIR}/../../third_party/build/lib/libdepthanything.a ${SRCDIR}/../../third_party/build/lib/libggml.a ${SRCDIR}/../../third_party/build/lib/libggml-vulkan.a ${SRCDIR}/../../third_party/build/lib/libggml-cpu.a ${SRCDIR}/../../third_party/build/lib/libggml-base.a -lm -lpthread -ldl
#cgo windows CFLAGS: -I${SRCDIR}/../../third_party/build-windows-amd64/include
#cgo windows LDFLAGS: ${SRCDIR}/../../third_party/build-windows-amd64/lib/libdepthanything.a ${SRCDIR}/../../third_party/build-windows-amd64/lib/libggml.a ${SRCDIR}/../../third_party/build-windows-amd64/lib/libggml-vulkan.a ${SRCDIR}/../../third_party/build-windows-amd64/lib/libggml-cpu.a ${SRCDIR}/../../third_party/build-windows-amd64/lib/libggml-base.a
#cgo darwin,arm64 CFLAGS: -I${SRCDIR}/../../third_party/build-darwin-arm64/include
#cgo darwin,arm64 LDFLAGS: ${SRCDIR}/../../third_party/build-darwin-arm64/lib/libdepthanything.a ${SRCDIR}/../../third_party/build-darwin-arm64/lib/libggml.a ${SRCDIR}/../../third_party/build-darwin-arm64/lib/libggml-metal.a ${SRCDIR}/../../third_party/build-darwin-arm64/lib/libggml-blas.a ${SRCDIR}/../../third_party/build-darwin-arm64/lib/libggml-cpu.a ${SRCDIR}/../../third_party/build-darwin-arm64/lib/libggml-base.a -framework Accelerate -framework Metal -framework MetalKit -framework Foundation
#cgo darwin,amd64 CFLAGS: -I${SRCDIR}/../../third_party/build-darwin-amd64/include
#cgo darwin,amd64 LDFLAGS: ${SRCDIR}/../../third_party/build-darwin-amd64/lib/libdepthanything.a ${SRCDIR}/../../third_party/build-darwin-amd64/lib/libggml.a ${SRCDIR}/../../third_party/build-darwin-amd64/lib/libggml-metal.a ${SRCDIR}/../../third_party/build-darwin-amd64/lib/libggml-blas.a ${SRCDIR}/../../third_party/build-darwin-amd64/lib/libggml-cpu.a ${SRCDIR}/../../third_party/build-darwin-amd64/lib/libggml-base.a -framework Accelerate -framework Metal -framework MetalKit -framework Foundation
#include <stdlib.h>
#include <da_capi.h>
*/
import "C"

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"sync"
	"time"
	"unsafe"

	"audioviz/internal/aimodel"
	_ "audioviz/internal/ggml" // the shared Vulkan loader bridge
)

// Models: Depth Anything 3 Small (fast, the default) and Base (more detail), both Apache-2.0
var Models = map[string]aimodel.Model{
	"small": {Name: "depth-small", File: "depth-anything-small-f32.gguf", Size: 104737600, Label: "Depth Anything 3 Small",
		URL: "https://huggingface.co/mudler/depth-anything.cpp-gguf/resolve/main/depth-anything-small-f32.gguf"},
	"base": {Name: "depth-base", File: "depth-anything-base-q8_0.gguf", Size: 148713024, Label: "Depth Anything 3 Base",
		URL: "https://huggingface.co/mudler/depth-anything.cpp-gguf/resolve/main/depth-anything-base-q8_0.gguf"},
}

// ModelFor: the model of a name (small if unknown)
func ModelFor(name string) aimodel.Model {
	if m, ok := Models[name]; ok {
		return m
	}
	return Models["small"]
}

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
// Small on an Intel iGPU)
func DetailSize(detail string) int {
	switch detail {
	case "normal":
		return 504
	case "max":
		return 1512
	}
	return 1008
}

// Estimate runs the model (a weights file already downloaded) on an image file: its depth map,
// as nearness 0..1. size 504: the model's own resize policy (its working size); larger: the
// image (already that size, both sides multiples of 14) at its own size.
func Estimate(modelPath, imagePath string, size int) (*Map, error) {
	mu.Lock()
	defer mu.Unlock()
	if ctx == nil || ctxFile != modelPath {
		if ctx != nil {
			C.da_capi_free(ctx)
			ctx = nil
		}
		cm := C.CString(modelPath)
		defer C.free(unsafe.Pointer(cm))
		t0 := time.Now()
		ctx = C.da_capi_load(cm, C.int(runtime.NumCPU()))
		debugf("load %v", time.Since(t0))
		if ctx == nil {
			return nil, errors.New("the depth model could not be loaded")
		}
		ctxFile = modelPath
	}
	ci := C.CString(imagePath)
	defer C.free(unsafe.Pointer(ci))
	// the dense path: Depth Anything 3's own resize policy (the long side to its working size,
	// about 504 px), as it was trained; the simpler call would work at the image's full size
	var h, w, metric C.int
	var p, conf, sky *C.float
	var ext [12]C.float
	var intr [9]C.float
	t1 := time.Now()
	var rc C.int
	if size > 504 {
		// the plain path works at the size of the image it is given
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
	if sky != nil {
		C.da_capi_free_floats(sky)
	}
	if rc != 0 || p == nil {
		msg := C.GoString(C.da_capi_last_error(ctx))
		if msg == "" {
			msg = "the depth could not be worked out"
		}
		return nil, errors.New(msg)
	}
	defer C.da_capi_free_floats(p)
	n := int(w) * int(h)
	raw := unsafe.Slice((*float32)(unsafe.Pointer(p)), n)
	// distance → nearness 0..1 (the nearest point 1, the farthest 0)
	lo, hi := raw[0], raw[0]
	for _, v := range raw {
		lo, hi = min(lo, v), max(hi, v)
	}
	m := &Map{W: int(w), H: int(h), V: make([]float32, n)}
	span := max(hi-lo, 1e-6)
	for i, v := range raw {
		m.V[i] = (hi - v) / span
	}
	return m, nil
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
