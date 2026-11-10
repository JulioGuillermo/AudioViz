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

//go:embed shaders/gas.spv
var gasSPV []byte

// The simulated gas on the GPU (gas.comp; effects.GasSim is the same on the CPU): a job that
// keeps the gas's fields between frames. Its buffers are mapped: the state is read and written
// in place (checkpoints), the sources and the tile lists are written before each frame.

const (
	GasFieldCount = 51 // fields in the field buffer (gas.comp; then the solid cells, the gas given off)
	GasStateCount = 14 // the first ones: the state (velocity, pressure, smoke…, coordinates)
	GasSrcFloats  = 10 // per source: x, y, radius, alpha, vx, vy, r, g, b, -
	GasTile       = 16 // cells per side of a tile of the sources' lists
	GasMaxSrc     = 1 << 15
	GasMaxList    = 1 << 21
	gasCfgFloats  = 4 * 9
	gasPushBytes  = 32
)

// GasJob: one gas simulation on the GPU
type GasJob struct {
	j      *C.vkj
	GW, GH int
	n      int
	tw, th int
	// recorded since the last Run: where the next substep's sources and tiles go
	srcOff, listOff int
}

// NewGasJob: an empty gas of gw×gh cells on GPU deviceIndex
func NewGasJob(deviceIndex, gw, gh int) (*GasJob, error) {
	simMu.Lock()
	defer simMu.Unlock()
	dev, err := simDevice(deviceIndex)
	if err != nil {
		return nil, err
	}
	n := gw * gh
	tw, th := (gw+GasTile-1)/GasTile, (gh+GasTile-1)/GasTile
	sizes := []C.size_t{
		C.size_t(gasCfgFloats * 4),
		C.size_t(GasFieldCount * n * 4),
		C.size_t(GasMaxSrc * GasSrcFloats * 4),
		C.size_t(GasMaxList * 4),
		C.size_t(n * 6 * 4),
		C.size_t(GasStateCount * n * 4),
	}
	errBuf := make([]byte, 512)
	spv := C.CBytes(gasSPV)
	defer C.free(spv)
	g := &GasJob{GW: gw, GH: gh, n: n, tw: tw, th: th}
	if C.vkj_create(&g.j, dev, (*C.uint32_t)(spv), C.size_t(len(gasSPV)), &sizes[0], C.int(len(sizes)), gasPushBytes, 1<<4|1<<5, // (read back: the frame's cells, the state)
		(*C.char)(unsafe.Pointer(&errBuf[0])), C.int(len(errBuf))) != 0 {
		return nil, fmt.Errorf("%s", C.GoString((*C.char)(unsafe.Pointer(&errBuf[0]))))
	}
	clear(g.Fields())
	return g, nil
}

func (g *GasJob) Close() {
	if g.j != nil {
		simMu.Lock()
		C.vkj_destroy(g.j)
		simMu.Unlock()
		g.j = nil
	}
}

// Cfg: the settings words (gas.comp's Cfg: 9 vec4, ints as their bits where it says)
func (g *GasJob) Cfg() []float32 { return unsafe.Slice((*float32)(C.vkj_map(g.j, 0)), gasCfgFloats) }

// Fields: every field (field f of cell i at f·n + i); the state is the first GasStateCount
func (g *GasJob) Fields() []float32 {
	return unsafe.Slice((*float32)(C.vkj_map(g.j, 1)), GasFieldCount*g.n)
}

// Out: the frame's cells (6 words each) after Light
func (g *GasJob) Out() []uint32 { return unsafe.Slice((*uint32)(C.vkj_map(g.j, 4)), g.n*6) }

// gasK: a kernel of gas.comp with its arguments
type gasK struct {
	k          int32
	a0, a1, a2 int32
	p0, p1     float32
}

type gasPush struct {
	Kernel, A0, A1, A2 int32
	T, Dt, P0, P1      float32
}

func (g *GasJob) run(k, a0, a1, a2 int32, t, dt, p0, p1 float32) error {
	p := gasPush{k, a0, a1, a2, t, dt, p0, p1}
	if C.vkj_dispatch(g.j, unsafe.Pointer(&p), C.uint32_t((g.n+255)/256), 1, 1) != 0 {
		return fmt.Errorf("vkCmdDispatch failed")
	}
	return nil
}

func (g *GasJob) runGroups(k, a0, a1, a2 int32, t, dt float32, groups int) error {
	p := gasPush{k, a0, a1, a2, t, dt, 0, 0}
	if C.vkj_dispatch(g.j, unsafe.Pointer(&p), C.uint32_t(groups), 1, 1) != 0 {
		return fmt.Errorf("vkCmdDispatch failed")
	}
	return nil
}

// Begin: a new frame's recording (its sources and lists from the start of their buffers)
func (g *GasJob) Begin() { g.srcOff, g.listOff = 0, 0 }

// GasStep: what one substep needs besides the sources: time, dt, the forces' wind factor, the
// diffusion's factor (0: none), what is kept of smoke, flames, warmth, the fuel burnt, which
// wisp sets restart (bits)
type GasStep struct {
	T, Dt, Wk, Diff, Fade, Cool, Warm, Burn float32
	Restart                                 int32
}

// Step records one substep: the sources (GasSrcFloats each; those of each tile listed in their
// order) then the steps of effects.GasSim.Step. False if the sources do not fit (the frame must
// be run before more).
func (g *GasJob) Step(src []float32, st GasStep) (bool, error) {
	ns := len(src) / GasSrcFloats
	cs := g.Cfg()[4]
	// the tiles each source's disc touches, listed per tile in order
	lists := make([][]uint32, g.tw*g.th)
	for k := 0; k < ns; k++ {
		o := k * GasSrcFloats
		x, y, r := src[o], src[o+1], max(src[o+2], cs*0.75)
		x0, x1 := max(int((x-r)/cs), 0), min(int((x+r)/cs)+1, g.GW-1)
		y0, y1 := max(int((y-r)/cs), 0), min(int((y+r)/cs)+1, g.GH-1)
		if x1 < x0 || y1 < y0 {
			continue
		}
		for ty := y0 / GasTile; ty <= y1/GasTile; ty++ {
			for tx := x0 / GasTile; tx <= x1/GasTile; tx++ {
				lists[ty*g.tw+tx] = append(lists[ty*g.tw+tx], uint32(k))
			}
		}
	}
	total := 2 * len(lists)
	for _, l := range lists {
		total += len(l)
	}
	if g.srcOff+len(src) > GasMaxSrc*GasSrcFloats || g.listOff+total > GasMaxList {
		return false, nil
	}
	sb := unsafe.Slice((*float32)(C.vkj_map(g.j, 2)), GasMaxSrc*GasSrcFloats)
	copy(sb[g.srcOff:], src)
	lb := unsafe.Slice((*uint32)(C.vkj_map(g.j, 3)), GasMaxList)
	head, at := g.listOff, g.listOff+2*len(lists)
	for t, l := range lists {
		lb[head+2*t], lb[head+2*t+1] = uint32(at), uint32(len(l))
		at += copy(lb[at:], l)
	}
	steps := []gasK{
		{0, int32(head), int32(g.srcOff), 0, 0, 0}, // feed
		{1, 0, 0, 0, 0, 0},                         // curl, ψ
		{2, 0, 0, 0, st.Wk, 0},                     // forces (into T1, T3)
		{25, 0, 0, 0, 0, 0},                        // the velocity carried (from T1, T3)
		{5, 0, 0, 0, 0, 0},                         // divergence
	}
	for _, s := range steps {
		if err := g.run(s.k, s.a0, s.a1, s.a2, st.T, st.Dt, s.p0, s.p1); err != nil {
			return false, err
		}
	}
	// the pressure: by blocks of 16×16 cells in shared memory (gas.comp's sorTiles), 8 sweeps
	// each, the blocks shifted between the 4 dispatches so their seams move (warm-started: the
	// pressure carries on from the last step)
	tiles := ((g.GW+15)/16 + 1) * ((g.GH+15)/16 + 1)
	// (from the pressure into T0 and back: an even number of passes)
	for k, off := range []int32{0, 8, 4, 12} {
		from := int32(2) // P
		if k%2 == 1 {
			from = 14 // T0
		}
		if err := g.runGroups(24, off, 8, from, st.T, st.Dt, tiles); err != nil {
			return false, err
		}
	}
	f32 := func(v float32) int32 { return int32(*(*uint32)(unsafe.Pointer(&v))) }
	rest := []gasK{
		{7, 0, 0, 0, 0, 0},  // gradient
		{9, 0, 0, 0, 0, 0},  // carried: back (and where it came from)
		{10, 0, 0, 0, 0, 0}, // forth, corrected, in place
	}
	diff := int32(0)
	if st.Diff > 0 {
		rest, diff = append(rest, gasK{12, 0, 0, 0, st.Diff, 0}), 1
	}
	// the wisp coordinates (and the diffusion's results in place), then the reactions (and the
	// coordinates in place, the sets restarted)
	rest = append(rest, gasK{14, 0, diff, 0, 0, 0}, gasK{16, st.Restart, f32(st.Warm), f32(st.Burn), st.Fade, st.Cool})
	for _, s := range rest {
		if err := g.run(s.k, s.a0, s.a1, s.a2, st.T, st.Dt, s.p0, s.p1); err != nil {
			return false, err
		}
	}
	g.srcOff += len(src)
	g.listOff += total
	return true, nil
}

// Run: what was recorded, on the GPU (waits for it)
func (g *GasJob) Run() error {
	simMu.Lock() // (one queue, shared with the fluid simulation)
	defer simMu.Unlock()
	if C.vkj_run(g.j) != 0 {
		return fmt.Errorf("error running the gas simulation on the GPU (device lost?)")
	}
	g.Begin()
	return nil
}

// Quantize: the state as halves, the pressure cleared (a checkpoint's state; run at once)
func (g *GasJob) Quantize() error {
	if err := g.run(17, 0, 0, 0, 0, 0, 0, 0); err != nil {
		return err
	}
	if err := g.run(26, 0, 0, 0, 0, 0, 0, 0); err != nil {
		return err
	}
	return g.Run()
}

// State: the state as Quantize left it (read-back memory: fast to read)
func (g *GasJob) State() []float32 {
	return unsafe.Slice((*float32)(C.vkj_map(g.j, 5)), GasStateCount*g.n)
}

// Light: the frame's light and its cells into Out (run at once); radii of the blurs of the light
// around and of the fire's light (cells)
func (g *GasJob) Light(ambR, fireR int) error {
	const (
		fD, fAmb, fFireE, fBL1, fBL2 = 3, 45, 46, 47, 48
	)
	blur := func(from, to, r int32) error {
		for _, p := range [][3]int32{{20, from, fBL1}, {21, fBL1, fBL2}, {20, fBL2, fBL1}, {21, fBL1, to}} {
			if err := g.run(p[0], p[1], p[2], r, 0, 0, 0, 0); err != nil {
				return err
			}
		}
		return nil
	}
	if err := g.run(18, 0, 0, 0, 0, 0, 0, 0); err != nil {
		return err
	}
	if err := blur(fD, fAmb, int32(ambR)); err != nil {
		return err
	}
	if err := g.run(19, 0, 0, 0, 0, 0, 0, 0); err != nil {
		return err
	}
	if err := blur(fFireE, fFireE, int32(fireR)); err != nil {
		return err
	}
	for _, k := range []int32{22, 23} {
		if err := g.run(k, 0, 0, 0, 0, 0, 0, 0); err != nil {
			return err
		}
	}
	return g.Run()
}
