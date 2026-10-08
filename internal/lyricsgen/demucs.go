package lyricsgen

/*
#cgo CXXFLAGS: -std=c++17 -O3 -DNDEBUG -w -I${SRCDIR}/../../third_party/demucs.cpp/src -I${SRCDIR}/../../third_party/demucs.cpp/vendor/eigen
#cgo linux,amd64 darwin,amd64 CXXFLAGS: -march=x86-64-v3
#cgo windows,amd64 CXXFLAGS: -mcpu=x86_64_v3
// C++ runtime for demucs and whisper: static libstdc++ (Linux/gcc), zig's libc++ (Windows), libc++ (macOS)
#cgo linux LDFLAGS: ${SRCDIR}/../../third_party/build/lib/libdemucs.a -l:libstdc++.a
#cgo windows LDFLAGS: ${SRCDIR}/../../third_party/build-windows-amd64/lib/libdemucs.a -lc++
#cgo darwin,arm64 LDFLAGS: ${SRCDIR}/../../third_party/build-darwin-arm64/lib/libdemucs.a -lc++
#cgo darwin,amd64 LDFLAGS: ${SRCDIR}/../../third_party/build-darwin-amd64/lib/libdemucs.a -lc++
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

// SeparateVocals extracts the vocals from an interleaved stereo song at 44.1 kHz with Demucs (htdemucs).
// progress receives 0..1.
func SeparateVocals(ctx context.Context, modelPath string, stereo44 []float32, progress func(float64)) ([]float32, error) {
	var res []float32
	err := separate(ctx, modelPath, stereo44, sepVocals, progress, func(x []float32) error {
		res = append([]float32(nil), x...)
		return nil
	})
	return res, err
}

// StemNames: the four parts Demucs separates, in its order
var StemNames = [4]string{"drums", "bass", "other", "vocals"}

// SeparateStemsTo separates the four stems (StemNames) of an interleaved stereo song at 44.1 kHz
// and gives each to save while it is in memory (only during the call): mono (n samples) or
// stereo (2n, interleaved), at 44.1 kHz. progress receives 0..1.
func SeparateStemsTo(ctx context.Context, modelPath string, stereo44 []float32, stereo bool, progress func(float64),
	save func(i int, x []float32) error) error {
	mode, ch := sepMono, 1
	if stereo {
		mode, ch = sepStereo, 2
	}
	n := len(stereo44) / 2
	return separate(ctx, modelPath, stereo44, mode, progress, func(all []float32) error {
		for i := range StemNames {
			if err := save(i, all[i*ch*n:(i+1)*ch*n]); err != nil {
				return err
			}
		}
		return nil
	})
}

// what separate gives: the vocals in stereo, or the four stems, mono or stereo
const (
	sepVocals = iota
	sepMono
	sepStereo
)

// separate runs Demucs (sepVocals: the vocals in stereo, 2n samples; sepMono: the four stems mono,
// 4n; sepStereo: the four in stereo, 8n) and gives the result to use while it is in C memory
func separate(ctx context.Context, modelPath string, stereo44 []float32, mode int, progress func(float64), use func([]float32) error) error {
	cp := C.CString(modelPath)
	defer C.free(unsafe.Pointer(cp))
	errBuf := (*C.char)(C.calloc(512, 1))
	defer C.free(unsafe.Pointer(errBuf))
	model := C.dmx_load(cp, errBuf, 512)
	if model == nil {
		return errors.New(C.GoString(errBuf))
	}
	defer C.dmx_free(model)

	n := len(stereo44) / 2
	outLen := 2 * n
	switch mode {
	case sepMono:
		outLen = 4 * n
	case sepStereo:
		outLen = 8 * n
	}
	in := (*C.float)(C.malloc(C.size_t(len(stereo44) * 4)))
	out := (*C.float)(C.malloc(C.size_t(outLen * 4)))
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
		switch mode {
		case sepMono:
			done <- C.dmx_stems_mono(model, in, C.long(n), C.int(threads), out, prog, cancel, errBuf, 512)
		case sepStereo:
			done <- C.dmx_stems_stereo(model, in, C.long(n), C.int(threads), out, prog, cancel, errBuf, 512)
		default:
			done <- C.dmx_vocals(model, in, C.long(n), C.int(threads), out, prog, cancel, errBuf, 512)
		}
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
				// Only the threads actually used write progress; the rest stay at 0 if unused
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
		return ctx.Err()
	}
	if ret != 0 {
		return fmt.Errorf("%s", C.GoString(errBuf))
	}
	return use(unsafe.Slice((*float32)(unsafe.Pointer(out)), outLen))
}
