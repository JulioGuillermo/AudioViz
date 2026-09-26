// Package gpu renderiza cuadros completos con un compute shader Vulkan.
// Vulkan es agnóstico de fabricante (Intel, AMD, NVIDIA, Apple vía MoltenVK...).
// libvulkan se carga en tiempo de ejecución: si no existe, el motor cae a CPU.
package gpu

/*
#cgo linux LDFLAGS: -ldl
#include <stdlib.h>
#include "vk.h"
*/
import "C"

import (
	_ "embed"
	"fmt"
	"unsafe"
)

//go:embed shaders/render.spv
var renderSPV []byte

const (
	MaxGrad   = 8
	MaxGlitch = 64
	MaxBars   = 256
	MaxParts  = 1024

	MaxTextItems = 96
)

// Params refleja exactamente el bloque std430 'Params' de render.comp (todo vec4/ivec4)
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

	// Letra
	TextInfo                                         [4]int32 // nItems, atlasW, chroma(0/1), gradCount
	TextStyle                                        [4]float32 // outlineW, glowRadius, glowIntensity, chromaPx
	TextColor, TextUpcoming, TextOutline, TextGlowC [4]float32
	TextShadow                                       [4]float32 // dx, dy, opacidad, blur
	TextShadowC                                      [4]float32
	TextGrad                                         [MaxGrad][4]float32
	TItemA                                           [MaxTextItems][4]float32 // atlas x, y, w, h
	TItemB                                           [MaxTextItems][4]float32 // centro x, y, escala, alfa
	TItemC                                           [MaxTextItems][4]float32 // relleno karaoke, pad
}

type Renderer struct {
	r            *C.vkr
	Width        int
	Height       int
	PadW, PadH   int
	Slots        int
	DeviceName   string
	spirv        unsafe.Pointer
}

// ListDevices devuelve la lista de GPUs Vulkan visibles
func ListDevices() string {
	buf := make([]byte, 4096)
	C.vkr_list_devices((*C.char)(unsafe.Pointer(&buf[0])), C.int(len(buf)))
	return C.GoString((*C.char)(unsafe.Pointer(&buf[0])))
}

func align(v, a int) int { return (v + a - 1) / a * a }

// New crea el renderer. deviceIndex -1 = automático.
func New(width, height, slots, deviceIndex int, textSize int) (*Renderer, error) {
	g := &Renderer{Width: width, Height: height, PadW: align(width, 16), PadH: align(height, 16), Slots: slots}
	g.spirv = C.CBytes(renderSPV)
	cfg := C.vkr_config{
		width: C.int(width), height: C.int(height),
		pad_w: C.int(g.PadW), pad_h: C.int(g.PadH),
		slots: C.int(slots), device_index: C.int(deviceIndex),
		params_size: C.size_t(unsafe.Sizeof(Params{})),
		text_size:   C.size_t(textSize),
		spirv:       (*C.uint32_t)(g.spirv), spirv_size: C.size_t(len(renderSPV)),
	}
	errBuf := make([]byte, 512)
	var r *C.vkr
	if C.vkr_create(&r, &cfg, (*C.char)(unsafe.Pointer(&errBuf[0])), C.int(len(errBuf))) != 0 {
		C.free(g.spirv)
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

// Background devuelve el buffer RGBA (W*H*4) del fondo del slot
func (g *Renderer) Background(slot int) []byte { return g.bytes(slot, C.VKR_BUF_BG, g.Width*g.Height*4) }

// PlaneY / PlaneUV: salida NV12 (stride = PadW)
func (g *Renderer) PlaneY(slot int) unsafe.Pointer  { return C.vkr_map(g.r, C.int(slot), C.VKR_BUF_Y) }
func (g *Renderer) PlaneUV(slot int) unsafe.Pointer { return C.vkr_map(g.r, C.int(slot), C.VKR_BUF_UV) }
// Text devuelve el buffer del atlas SDF de la letra del slot
func (g *Renderer) Text(slot, n int) []byte { return g.bytes(slot, C.VKR_BUF_TEXT, n) }

func (g *Renderer) RGBA(slot int) []byte            { return g.bytes(slot, C.VKR_BUF_RGBA, g.Width*g.Height*4) }

func (g *Renderer) Submit(slot int) error {
	if C.vkr_submit(g.r, C.int(slot)) != 0 {
		return fmt.Errorf("vkQueueSubmit falló")
	}
	return nil
}

func (g *Renderer) Wait(slot int) error {
	if C.vkr_wait(g.r, C.int(slot)) != 0 {
		return fmt.Errorf("error esperando a la GPU (¿dispositivo perdido?)")
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
}
