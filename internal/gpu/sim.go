package gpu

/*
#include <stdlib.h>
#include "vk.h"
*/
import "C"

import (
	_ "embed"
	"fmt"
	"math"
	"sync"
	"unsafe"
)

//go:embed shaders/fluidsim.spv
var fluidSimSPV []byte

// FluidSim is the input of the fluid / liquid simulation (see effects.Fluid.simulate, the same
// steps on the CPU)
type FluidSim struct {
	W, H, N int
	Liquid  bool
	Ends    []float32 // N × (start x, y, slot x, y)
	Times   []float32 // N × (departure, arrival)
	// Liquid: the divergence-free stream on a coarse grid (u, v per cell)
	Flow                 []float32
	FlowW, FlowH         int
	FlowHX, FlowHY, Gain float32
	Diam, CellSize       float32 // collisions: particle diameter, grid cell size
	GridW, GridH         int
	VMax                 float32
	LCell, Rest, Visc    float32 // liquid grid: cell size, particles per cell at rest, viscosity
	Pres                 float32
	LGridW, LGridH       int
	Snaps, Sub           int // stored instants, steps between them
}

// Compute device for simulations (opened once, without a renderer)
var (
	simMu  sync.Mutex
	simDev *C.vkr
	simIdx int
	simErr error
)

func simDevice(deviceIndex int) (*C.vkr, error) {
	if simDev != nil && simIdx == deviceIndex {
		return simDev, nil
	}
	if simDev != nil {
		C.vkr_destroy(simDev)
		simDev = nil
	}
	type res struct {
		r   *C.vkr
		err error
	}
	v, ok := withTimeout(func() res {
		cfg := C.vkr_config{device_index: C.int(deviceIndex), width: 16, height: 16, pad_w: 16, pad_h: 16}
		errBuf := make([]byte, 512)
		var r *C.vkr
		if C.vkr_create(&r, &cfg, (*C.char)(unsafe.Pointer(&errBuf[0])), C.int(len(errBuf))) != 0 {
			return res{nil, fmt.Errorf("%s", C.GoString((*C.char)(unsafe.Pointer(&errBuf[0]))))}
		}
		return res{r, nil}
	})
	if !ok {
		return nil, ErrStuck
	}
	if v.err != nil {
		return nil, v.err
	}
	simDev, simIdx = v.r, deviceIndex
	return simDev, nil
}

// RunFluidSim runs the simulation on the GPU. It returns the stored positions (Snaps × N × (x,
// y) in 1/8 pixel, the layout of effects.Fluid) and the final positions (N × (x, y)).
func RunFluidSim(deviceIndex int, s *FluidSim) ([]int16, []float32, error) {
	simMu.Lock()
	defer simMu.Unlock()
	dev, err := simDevice(deviceIndex)
	if err != nil {
		return nil, nil, err
	}
	n := s.N
	const capPerCell = 16
	cells := s.GridW * s.GridH
	lcells := s.LGridW * s.LGridH
	flow := s.Flow
	if len(flow) == 0 {
		flow = []float32{0, 0}
	}
	sizes := []C.size_t{
		16 * 6,                           // 0 config
		C.size_t(n * 8),                  // 1 positions
		C.size_t(n * 8),                  // 2 velocities
		C.size_t(n * 8),                  // 3 predicted
		C.size_t(n * 8),                  // 4 corrections
		C.size_t(n * 16),                 // 5 start, slot
		C.size_t(n * 8),                  // 6 times
		C.size_t(len(flow) * 4),          // 7 stream
		C.size_t(cells * 4),              // 8 particles per cell
		C.size_t(cells * capPerCell * 4), // 9 cell slots
		C.size_t(max(1, lcells) * 12),    // 10 liquid grid
		C.size_t(s.Snaps * n * 4),        // 11 snapshots
	}
	errBuf := make([]byte, 512)
	spv := C.CBytes(fluidSimSPV)
	defer C.free(spv)
	var j *C.vkj
	if C.vkj_create(&j, dev, (*C.uint32_t)(spv), C.size_t(len(fluidSimSPV)), &sizes[0], C.int(len(sizes)), 16, 0,
		(*C.char)(unsafe.Pointer(&errBuf[0])), C.int(len(errBuf))) != 0 {
		return nil, nil, fmt.Errorf("%s", C.GoString((*C.char)(unsafe.Pointer(&errBuf[0]))))
	}
	defer C.vkj_destroy(j)
	f32 := func(buf, cnt int) []float32 { return unsafe.Slice((*float32)(C.vkj_map(j, C.int(buf))), cnt) }
	cfgI := unsafe.Slice((*int32)(C.vkj_map(j, 0)), 24)
	cfgF := unsafe.Slice((*float32)(C.vkj_map(j, 0)), 24)
	liquid := int32(0)
	if s.Liquid {
		liquid = 1
	}
	copy(cfgI[0:12], []int32{int32(n), int32(s.W), int32(s.H), liquid, int32(s.GridW), int32(s.GridH), capPerCell, 0,
		int32(s.LGridW), int32(s.LGridH), int32(s.FlowW), int32(s.FlowH)})
	dt := float32(1.0 / float64((s.Snaps-1)*s.Sub))
	copy(cfgF[12:24], []float32{s.Diam, s.CellSize, s.VMax, dt, s.LCell, s.Rest, s.Visc, s.Pres, s.FlowHX, s.Gain, s.FlowHY, 0})
	pos := f32(1, n*2)
	for i := range n {
		pos[2*i], pos[2*i+1] = s.Ends[4*i], s.Ends[4*i+1]
	}
	clear(f32(2, n*2))
	copy(f32(5, n*4), s.Ends)
	copy(f32(6, n*2), s.Times)
	copy(f32(7, len(flow)), flow)

	type push struct {
		Kernel, Arg int32
		T, _        float32
	}
	groups := func(m int) C.uint32_t { return C.uint32_t((m + 63) / 64) }
	run := func(k int32, arg int32, t float32, m int) error {
		p := push{Kernel: k, Arg: arg, T: t}
		if C.vkj_dispatch(j, unsafe.Pointer(&p), groups(m), 1, 1) != 0 {
			return fmt.Errorf("vkCmdDispatch failed")
		}
		return nil
	}
	if err := run(9, 0, 0, n); err != nil {
		return nil, nil, err
	}
	// One step: the kernels of fluidsim.comp in order, each over its particles or cells
	type kernel struct {
		k int32
		m int
	}
	step := []kernel{{0, n}}
	if s.Liquid {
		step = append(step, kernel{1, lcells * 3}, kernel{2, n}, kernel{3, n})
	}
	for range 2 { // two collision passes
		step = append(step, kernel{4, cells}, kernel{5, n}, kernel{6, n}, kernel{7, n})
	}
	step = append(step, kernel{8, n})
	steps := (s.Snaps - 1) * s.Sub
	// Short submissions: some drivers (Intel xe) reset jobs that run for seconds
	const stepsPerSubmit = 8
	for st := 1; st <= steps; st++ {
		t := float32(st) * dt
		for _, k := range step {
			if err := run(k.k, 0, t, k.m); err != nil {
				return nil, nil, err
			}
		}
		if st%s.Sub == 0 {
			if err := run(9, int32(st/s.Sub), t, n); err != nil {
				return nil, nil, err
			}
		}
		if st%stepsPerSubmit == 0 || st == steps {
			if C.vkj_run(j) != 0 {
				return nil, nil, fmt.Errorf("error running the simulation on the GPU (device lost?)")
			}
		}
	}
	out := make([]int16, s.Snaps*n*2)
	copy(out, unsafe.Slice((*int16)(C.vkj_map(j, 11)), len(out)))
	final := make([]float32, n*2)
	copy(final, pos)
	for _, v := range final {
		if math.IsNaN(float64(v)) {
			return nil, nil, fmt.Errorf("the GPU simulation diverged")
		}
	}
	return out, final, nil
}
