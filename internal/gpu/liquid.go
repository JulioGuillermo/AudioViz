package gpu

/*
#include <stdlib.h>
#include "vk.h"
*/
import "C"

import (
	_ "embed"
	"fmt"
	"unsafe"
)

//go:embed shaders/liquid.spv
var liquidSPV []byte

// The simulated liquid on the GPU (liquid.comp; effects.LiquidSim is the same on the CPU): the
// drops are written before a frame's steps and read after them (host-cached), the grid stays on
// the GPU (the pressure carries on from step to step).

const (
	LiquidCap     = 40000 // drops (liquid.comp's CAP, effects.LiquidMax)
	liquidCfgF    = 12
	liquidPushLen = 32
)

// LiquidJob: one liquid simulation on the GPU
type LiquidJob struct {
	j      *C.vkj
	GW, GH int
	n      int
}

// NewLiquidJob: a liquid of gw×gh cells on GPU deviceIndex
func NewLiquidJob(deviceIndex, gw, gh int) (*LiquidJob, error) {
	simMu.Lock()
	defer simMu.Unlock()
	dev, err := simDevice(deviceIndex)
	if err != nil {
		return nil, err
	}
	n := gw * gh
	nu, nv := (gw+1)*gh, gw*(gh+1)
	sizes := []C.size_t{
		C.size_t(liquidCfgF * 4),
		C.size_t(5 * LiquidCap * 4),
		C.size_t((3*nu + 3*nv + 3*n) * 4),
		C.size_t((3*nu + 3*nv + 2*n) * 4),
		C.size_t(n * 4),
		C.size_t(LiquidCap * 4),
	}
	errBuf := make([]byte, 512)
	spv := C.CBytes(liquidSPV)
	defer C.free(spv)
	l := &LiquidJob{GW: gw, GH: gh, n: n}
	if C.vkj_create(&l.j, dev, (*C.uint32_t)(spv), C.size_t(len(liquidSPV)), &sizes[0], C.int(len(sizes)), liquidPushLen, 1<<1, // (the drops: read back every frame)
		(*C.char)(unsafe.Pointer(&errBuf[0])), C.int(len(errBuf))) != 0 {
		return nil, fmt.Errorf("%s", C.GoString((*C.char)(unsafe.Pointer(&errBuf[0]))))
	}
	clear(unsafe.Slice((*float32)(C.vkj_map(l.j, 2)), 3*nu+3*nv+3*n))
	return l, nil
}

func (l *LiquidJob) Close() {
	if l.j != nil {
		simMu.Lock()
		C.vkj_destroy(l.j)
		simMu.Unlock()
		l.j = nil
	}
}

// Cfg: the settings (liquid.comp's Cfg: ints as their bits)
func (l *LiquidJob) Cfg() []float32 { return unsafe.Slice((*float32)(C.vkj_map(l.j, 0)), liquidCfgF) }

// Drops: x, y, vx, vy, born, LiquidCap of each
func (l *LiquidJob) Drops() []float32 {
	return unsafe.Slice((*float32)(C.vkj_map(l.j, 1)), 5*LiquidCap)
}

// Solid: the solid cells (1)
func (l *LiquidJob) Solid() []uint32 { return unsafe.Slice((*uint32)(C.vkj_map(l.j, 4)), l.n) }

type liquidPush struct {
	Kernel, A0, A1, A2 int32
	T, Dt, P0, P1      float32
}

func (l *LiquidJob) run(k, a0, a1, a2 int32, t, dt float32, groups int) error {
	p := liquidPush{k, a0, a1, a2, t, dt, 0, 0}
	if C.vkj_dispatch(l.j, unsafe.Pointer(&p), C.uint32_t(max(groups, 1)), 1, 1) != 0 {
		return fmt.Errorf("vkCmdDispatch failed")
	}
	return nil
}

// Step records one step of dt at time t over nd drops
func (l *LiquidJob) Step(nd int, t, dt float32) error {
	nu, nv := (l.GW+1)*l.GH, l.GW*(l.GH+1)
	gr := func(m int) int { return (m + 255) / 256 }
	cells := max(nu, nv, l.n)
	faces := nu + nv
	tiles := ((l.GW+15)/16 + 1) * ((l.GH+15)/16 + 1)
	for _, k := range []struct {
		k, a0 int32
		m     int
	}{{0, 0, cells}, {1, 0, nd}, {2, 0, nd}, {3, 0, nd}, {4, 0, faces}, {5, 0, l.n}} {
		if err := l.run(k.k, k.a0, 0, 0, t, dt, gr(k.m)); err != nil {
			return err
		}
	}
	// the pressure: by blocks, P → P2 → P… (an even number of passes), the blocks shifted
	for k, off := range []int32{0, 8, 4, 12, 0, 8} {
		if err := l.run(6, off, 10, int32(k%2), t, dt, tiles); err != nil {
			return err
		}
	}
	for _, k := range []struct {
		k, a0 int32
		m     int
	}{{7, 0, faces}, {8, 0, faces}, {8, 1, faces}, {9, 0, nd}} {
		if err := l.run(k.k, k.a0, 0, 0, t, dt, gr(k.m)); err != nil {
			return err
		}
	}
	return nil
}

// ClearPressure records clearing the pressure (a checkpoint's state)
func (l *LiquidJob) ClearPressure() error { return l.run(10, 0, 0, 0, 0, 0, (l.n+255)/256) }

// Run: what was recorded, on the GPU (waits for it)
func (l *LiquidJob) Run() error {
	simMu.Lock()
	defer simMu.Unlock()
	if C.vkj_run(l.j) != 0 {
		return fmt.Errorf("error running the liquid simulation on the GPU (device lost?)")
	}
	return nil
}
