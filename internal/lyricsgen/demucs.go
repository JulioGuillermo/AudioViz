package lyricsgen

/*
#cgo CXXFLAGS: -std=c++17 -O3 -march=x86-64-v3 -DNDEBUG -w -I${SRCDIR}/../../third_party/demucs.cpp/src -I${SRCDIR}/../../third_party/demucs.cpp/vendor/eigen
#cgo LDFLAGS: ${SRCDIR}/../../third_party/build/lib/libdemucs.a -l:libstdc++.a
#include <stdlib.h>
#include "dmx.h"
*/
import "C"

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"time"
	"unsafe"
)

// SeparateVocals extrae la voz de una canción estéreo entrelazada a 44,1 kHz con Demucs (htdemucs).
// progress recibe 0..1.
func SeparateVocals(ctx context.Context, modelPath string, stereo44 []float32, progress func(float64)) ([]float32, error) {
	cp := C.CString(modelPath)
	defer C.free(unsafe.Pointer(cp))
	errBuf := (*C.char)(C.calloc(512, 1))
	defer C.free(unsafe.Pointer(errBuf))
	model := C.dmx_load(cp, errBuf, 512)
	if model == nil {
		return nil, errors.New(C.GoString(errBuf))
	}
	defer C.dmx_free(model)

	n := len(stereo44) / 2
	in := (*C.float)(C.malloc(C.size_t(len(stereo44) * 4)))
	out := (*C.float)(C.malloc(C.size_t(len(stereo44) * 4)))
	defer C.free(unsafe.Pointer(in))
	defer C.free(unsafe.Pointer(out))
	copy(unsafe.Slice((*float32)(unsafe.Pointer(in)), len(stereo44)), stereo44)

	threads := runtime.NumCPU()
	prog := (*C.float)(C.calloc(C.size_t(threads), 4))
	cancel := (*C.int)(C.calloc(1, 4))
	defer C.free(unsafe.Pointer(prog))
	defer C.free(unsafe.Pointer(cancel))
	pv := unsafe.Slice((*float32)(unsafe.Pointer(prog)), threads)

	done := make(chan C.int, 1)
	go func() {
		runtime.LockOSThread()
		done <- C.dmx_vocals(model, in, C.long(n), C.int(threads), out, prog, cancel, errBuf, 512)
	}()
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	var ret C.int
loop:
	for {
		select {
		case ret = <-done:
			break loop
		case <-tick.C:
			if ctx.Err() != nil {
				*cancel = 1
			}
			if progress != nil {
				// Los hilos realmente usados escriben progreso; el resto queda a 0 si no se usan
				used := min(threads, n/(44100*10)+1)
				s := 0.0
				for i := 0; i < used; i++ {
					s += float64(pv[i])
				}
				progress(s / float64(used))
			}
		}
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if ret != 0 {
		return nil, fmt.Errorf("%s", C.GoString(errBuf))
	}
	res := make([]float32, len(stereo44))
	copy(res, unsafe.Slice((*float32)(unsafe.Pointer(out)), len(stereo44)))
	return res, nil
}
