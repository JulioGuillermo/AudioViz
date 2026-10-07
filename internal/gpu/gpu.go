// Package gpu renders complete frames with a Vulkan compute shader.
// Vulkan is vendor-agnostic (Intel, AMD, NVIDIA, Apple via MoltenVK...).
// libvulkan is loaded at runtime: if it is missing, the engine falls back to CPU.
package gpu

/*
#cgo linux LDFLAGS: -ldl
#cgo windows CFLAGS: -I${SRCDIR}/../../third_party/vulkan-headers
#cgo darwin,arm64 CFLAGS: -I${SRCDIR}/../../third_party/build-darwin-arm64/include
#cgo darwin,amd64 CFLAGS: -I${SRCDIR}/../../third_party/build-darwin-amd64/include
#include <stdlib.h>
#include "vk.h"
*/
import "C"

import (
	_ "embed"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
)

// A broken Vulkan driver can hang inside its setup calls instead of failing. Those calls run
// with a time limit: past it the GPU is given up for the rest of the process (the CPU renders),
// rather than freezing the program. The stuck call stays parked in its own thread.
const setupTimeout = 20 * time.Second

var stuck atomic.Bool

// ErrStuck: a previous Vulkan setup did not return in time
var ErrStuck = errors.New("the Vulkan driver did not respond (setup timed out); using the CPU")

func withTimeout[T any](f func() T) (T, bool) {
	var zero T
	if stuck.Load() {
		return zero, false
	}
	ch := make(chan T, 1)
	go func() { ch <- f() }()
	select {
	case v := <-ch:
		return v, true
	case <-time.After(setupTimeout):
		stuck.Store(true)
		return zero, false
	}
}

var (
	listOnce sync.Once
	listed   string
)

//go:embed shaders/render.spv
var renderSPV []byte

//go:embed shaders/fluid.spv
var fluidSPV []byte

const (
	MaxGrad   = 8
	MaxGlitch = 64
	MaxBars   = 256
	MaxParts  = 1024

	MaxTextItems  = 96
	MaxTextStyles = 8
)

// Params mirrors exactly the std430 'Params' block in render.comp (all vec4/ivec4)
type Params struct {
	Dims, Flags, Flags2, Flags3 [4]int32
	ZoomRipple                  [4]float32
	ColorAdj                    [4]float32
	Chroma                      [4]float32
	Viz0, Viz1, Viz2, Viz3      [4]float32
	SolidColor                  [4]float32
	PartColor                   [4]float32
	WaveColL, WaveColR          [4]float32
	GradInfo                    [4]float32
	Shake                       [4]float32
	Grad                        [MaxGrad][4]float32
	Glitch                      [MaxGlitch][4]float32
	Bars                        [MaxBars][4]float32
	Parts                       [MaxParts][4]float32
	PartCol                     [MaxParts][4]float32

	// Texts (lyrics + fixed texts): items index a style table
	TextInfo [4]int32 // nItems, atlasW, any chroma (0/1), -
	TStyles  [MaxTextStyles]TextStyle
	TItemA   [MaxTextItems][4]float32 // atlas x, y, w, h
	TItemB   [MaxTextItems][4]float32 // center x, y, scale, alpha
	TItemC   [MaxTextItems][4]float32 // karaoke fill, pad, style index

	BgMix [4]float32 // -, visualizer opacity, -, -
	Prev  [4]int32   // preview: scale, width, height (scale 0/1 = normal render)

	// Background layers and transition (effects.Layer / effects.Trans)
	LayerA, LayerB [4]float32 // scale, offX, offY, rot of image A (BG) and B (BG2)
	Trans          [4]int32   // mode, direction, has (bit0 A, bit1 B), seed
	TransP         [4]float32 // progress k, -, -, -
	// Burn transition: edge, roughness, max distance, point count; ember color; points (x, y)
	BurnP   [4]float32
	BurnCol [4]float32
	BurnPts [8][4]float32
	// Fluid / liquid (see fluid.comp): word offsets of the bin starts, - and coarse level
	// in the FluidData buffer, and the bin side (0 = no fluid); bins across and down, coarse
	// width and height; piece half width and height, progress, debug
	FluidI, FluidN [4]int32
	FluidF         [4]float32

	// Overlay layers (see drawLayer in render.comp): count; stages of the particles, the
	// visualizer, and a mask of the stages with texts (each text style says its own stage in
	// GradN[1]); the background's flash (contrast factor, brightness) for clips that follow it
	LayerN  [4]int32
	ZIdx    [4]int32
	FxFlash [4]float32
	Layers  [MaxLayers]LayerP
}

// MaxLayers: overlay clips drawn at once
const MaxLayers = 16

// LayerP mirrors 'LayerP' in render.comp: one overlay clip
type LayerP struct {
	Info   [4]int32   // image offset (pixels), width, height, flags (blend 0-3, key 4, glitch 5-6: 1 background's / 2 own, own count 8-11)
	Xf     [4]float32 // center x, y (px), screen px per image px x, y (negative = flipped)
	Xf2    [4]float32 // rotation (rad), opacity, blur (px), hue (rad)
	Adj    [4]float32 // brightness, contrast, saturation, -
	Key    [4]float32 // key color r, g, b, tolerance
	Key2   [4]float32 // softness, -, -, -
	Warp0  [4]float32 // zoom, ripple amplitude, frequency, phase
	Warp1  [4]float32 // shake x, y, chromatic shift, mode
	Warp2  [4]float32 // flash contrast factor, brightness
	Glitch [8][4]float32
	Mask   [4]int32 // mask: offset (words; one byte per mask px), width, height, on
}

// TextStyle mirrors 'TStyle' in render.comp: one entry of the per-frame text style table
type TextStyle struct {
	Info                            [4]float32 // outlineW, glowRadius, glowIntensity, chromaPx (0 = none)
	Color, Upcoming, Outline, GlowC [4]float32
	Shadow                          [4]float32 // dx, dy, opacity, blur
	ShadowC                         [4]float32
	GradN                           [4]int32 // gradient stop count
	Grad                            [MaxGrad][4]float32
}

type Renderer struct {
	r          *C.vkr
	Width      int
	Height     int
	PadW, PadH int
	Slots      int
	DeviceName string
	FluidWords int // capacity of the fluid data buffer (32-bit words)
	LayerWords int // capacity of the overlay images buffer (pixels)
	spirv      unsafe.Pointer
	spirvFluid unsafe.Pointer
}

// ListDevices returns the list of visible Vulkan GPUs
func ListDevices() string {
	// Asked often by the studio: computed once, with the setup time limit
	listOnce.Do(func() {
		s, ok := withTimeout(func() string {
			buf := make([]byte, 4096)
			C.vkr_list_devices((*C.char)(unsafe.Pointer(&buf[0])), C.int(len(buf)))
			return C.GoString((*C.char)(unsafe.Pointer(&buf[0])))
		})
		if !ok {
			s = ErrStuck.Error()
		}
		listed = s
	})
	return listed
}

func align(v, a int) int { return (v + a - 1) / a * a }

// New creates the renderer. deviceIndex -1 = automatic. fluidWords: size of the fluid
// transition data (effects.FluidBufWords).
func New(width, height, slots, deviceIndex int, textSize, fluidWords, layerWords int) (*Renderer, error) {
	type res struct {
		g   *Renderer
		err error
	}
	r, ok := withTimeout(func() res {
		g, err := newRenderer(width, height, slots, deviceIndex, textSize, fluidWords, layerWords)
		return res{g, err}
	})
	if !ok {
		return nil, ErrStuck
	}
	return r.g, r.err
}

func newRenderer(width, height, slots, deviceIndex int, textSize, fluidWords, layerWords int) (*Renderer, error) {
	g := &Renderer{Width: width, Height: height, PadW: align(width, 16), PadH: align(height, 16), Slots: slots,
		FluidWords: fluidWords, LayerWords: max(layerWords, 4)}
	g.spirv = C.CBytes(renderSPV)
	g.spirvFluid = C.CBytes(fluidSPV)
	cfg := C.vkr_config{
		width: C.int(width), height: C.int(height),
		pad_w: C.int(g.PadW), pad_h: C.int(g.PadH),
		slots: C.int(slots), device_index: C.int(deviceIndex),
		params_size: C.size_t(unsafe.Sizeof(Params{})),
		text_size:   C.size_t(textSize),
		fluid_size:  C.size_t(fluidWords * 4),
		layers_size: C.size_t(g.LayerWords * 4),
		spirv:       (*C.uint32_t)(g.spirv), spirv_size: C.size_t(len(renderSPV)),
		spirv_fluid: (*C.uint32_t)(g.spirvFluid), spirv_fluid_size: C.size_t(len(fluidSPV)),
	}
	errBuf := make([]byte, 512)
	var r *C.vkr
	if C.vkr_create(&r, &cfg, (*C.char)(unsafe.Pointer(&errBuf[0])), C.int(len(errBuf))) != 0 {
		C.free(g.spirv)
		C.free(g.spirvFluid)
		return nil, fmt.Errorf("%s", C.GoString((*C.char)(unsafe.Pointer(&errBuf[0]))))
	}
	g.r = r
	g.DeviceName = C.GoString(C.vkr_device_name(r))
	return g, nil
}

func (g *Renderer) Params(slot int) *Params {
	return (*Params)(C.vkr_map(g.r, C.int(slot), C.VKR_BUF_PARAMS))
}

func (g *Renderer) bytes(slot int, which C.int, n int) []byte {
	return unsafe.Slice((*byte)(C.vkr_map(g.r, C.int(slot), which)), n)
}

// Background returns the slot's background RGBA buffer (W*H*4)
func (g *Renderer) Background(slot int) []byte {
	return g.bytes(slot, C.VKR_BUF_BG, g.Width*g.Height*4)
}

// PlaneY / PlaneUV: NV12 output (stride = PadW)
func (g *Renderer) PlaneY(slot int) unsafe.Pointer  { return C.vkr_map(g.r, C.int(slot), C.VKR_BUF_Y) }
func (g *Renderer) PlaneUV(slot int) unsafe.Pointer { return C.vkr_map(g.r, C.int(slot), C.VKR_BUF_UV) }

// Background2 is the target background during a crossfade
func (g *Renderer) Background2(slot int) []byte {
	return g.bytes(slot, C.VKR_BUF_BG2, g.Width*g.Height*4)
}

// Text returns the slot's lyrics SDF atlas buffer
func (g *Renderer) Text(slot, n int) []byte { return g.bytes(slot, C.VKR_BUF_TEXT, n) }

func (g *Renderer) RGBA(slot int) []byte { return g.bytes(slot, C.VKR_BUF_RGBA, g.Width*g.Height*4) }

// FluidData returns the slot's fluid data buffer (FluidWords words) as integers and as floats
func (g *Renderer) FluidData(slot int) ([]uint32, []float32) {
	p := C.vkr_map(g.r, C.int(slot), C.VKR_BUF_FLUID)
	return unsafe.Slice((*uint32)(p), g.FluidWords), unsafe.Slice((*float32)(p), g.FluidWords)
}

// LayerData returns the slot's overlay images buffer (LayerWords pixels, RGBA8)
func (g *Renderer) LayerData(slot int) []byte {
	return unsafe.Slice((*byte)(C.vkr_map(g.r, C.int(slot), C.VKR_BUF_LAYERS)), g.LayerWords*4)
}

// Submit launches the slot's frame; fluid runs the fluid pass first (FluidData and Params.Fluid* filled)
func (g *Renderer) Submit(slot int, fluid bool) error {
	f := C.int(0)
	if fluid {
		f = 1
	}
	if C.vkr_submit(g.r, C.int(slot), f) != 0 {
		return fmt.Errorf("vkQueueSubmit failed")
	}
	return nil
}

func (g *Renderer) Wait(slot int) error {
	if C.vkr_wait(g.r, C.int(slot)) != 0 {
		return fmt.Errorf("error waiting for the GPU (device lost?)")
	}
	return nil
}

func (g *Renderer) Close() {
	if g.r != nil {
		C.vkr_destroy(g.r)
		g.r = nil
	}
	if g.spirv != nil {
		C.free(g.spirv)
		g.spirv = nil
	}
	if g.spirvFluid != nil {
		C.free(g.spirvFluid)
		g.spirvFluid = nil
	}
}
