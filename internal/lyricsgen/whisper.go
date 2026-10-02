package lyricsgen

/*
#cgo CFLAGS: -I${SRCDIR}/../../third_party/build/include
#cgo LDFLAGS: ${SRCDIR}/../../third_party/build/lib/libwhisper.a ${SRCDIR}/../../third_party/build/lib/libggml.a ${SRCDIR}/../../third_party/build/lib/libggml-vulkan.a ${SRCDIR}/../../third_party/build/lib/libggml-cpu.a ${SRCDIR}/../../third_party/build/lib/libggml-base.a -lm -lpthread -ldl
#include <stdlib.h>
#include <stdbool.h>
#include <whisper.h>
#include <ggml.h>
#include <ggml-backend.h>

static void wh_quiet(enum ggml_log_level level, const char *text, void *ud) { (void)level; (void)text; (void)ud; }
static void wh_silence(void) { whisper_log_set(wh_quiet, NULL); ggml_log_set(wh_quiet, NULL); }

static void wh_progress(struct whisper_context *ctx, struct whisper_state *st, int p, void *ud) {
    (void)ctx; (void)st;
    *(volatile int *)ud = p;
}
static bool wh_abort(void *ud) { return *(volatile int *)ud != 0; }
static bool wh_abort_enc(struct whisper_context *ctx, struct whisper_state *st, void *ud) {
    (void)ctx; (void)st;
    return *(volatile int *)ud == 0; // true = continuar
}

static struct whisper_context *wh_open(const char *path, int gpu, int aheads) {
    struct whisper_context_params cp = whisper_context_default_params();
    cp.use_gpu = gpu;
    cp.flash_attn = false;              // DTW necesita las atenciones
    cp.dtw_token_timestamps = true;      // tiempos por palabra precisos
    cp.dtw_aheads_preset = (enum whisper_alignment_heads_preset)aheads;
    return whisper_init_from_file_with_params(path, cp);
}

static int wh_run(struct whisper_context *ctx, const float *pcm, int n, int threads, const char *lang,
                  const char *prompt, int *progress, int *cancel) {
    struct whisper_full_params p = whisper_full_default_params(WHISPER_SAMPLING_GREEDY);
    p.n_threads = threads;
    p.language = lang;
    p.translate = false;
    p.print_progress = false;
    p.print_realtime = false;
    p.print_timestamps = false;
    p.print_special = false;
    p.token_timestamps = true;
    p.suppress_blank = true;
    p.initial_prompt = prompt;
    p.progress_callback = wh_progress;
    p.progress_callback_user_data = progress;
    p.abort_callback = wh_abort;
    p.abort_callback_user_data = cancel;
    p.encoder_begin_callback = wh_abort_enc;
    p.encoder_begin_callback_user_data = cancel;
    return whisper_full(ctx, p, pcm, n);
}

// Dispositivo de cálculo en uso (para mostrarlo en la interfaz)
static const char *wh_device(void) {
    for (size_t i = 0; i < ggml_backend_dev_count(); i++) {
        ggml_backend_dev_t d = ggml_backend_dev_get(i);
        enum ggml_backend_dev_type t = ggml_backend_dev_type(d);
        if (t != GGML_BACKEND_DEVICE_TYPE_CPU && t != GGML_BACKEND_DEVICE_TYPE_ACCEL) return ggml_backend_dev_description(d);
    }
    return "CPU";
}
*/
import "C"

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"time"
	"unsafe"
)

// Word es una palabra reconocida con su instante (s)
type Word struct {
	Text       string
	Start, End float64
	Prob       float64
	T0, DTW    float64 // tiempos crudos de whisper (depuración)
}

type Whisper struct {
	ctx *C.struct_whisper_context
	mu  sync.Mutex
	// Device es la GPU usada (o "CPU")
	Device string
}

var silenceOnce sync.Once

// DTWBias: adelanto aplicado a los tiempos DTW (s)
var DTWBias = 0.40

// OpenWhisper carga un modelo ggml de whisper. aheads: preset de cabezas de alineamiento para DTW.
func OpenWhisper(path string, aheads int, gpu bool) (*Whisper, error) {
	silenceOnce.Do(func() { C.wh_silence() })
	cp := C.CString(path)
	defer C.free(unsafe.Pointer(cp))
	g := C.int(0)
	if gpu {
		g = 1
	}
	ctx := C.wh_open(cp, g, C.int(aheads))
	if ctx == nil {
		return nil, fmt.Errorf("no se pudo cargar el modelo de reconocimiento %s", path)
	}
	dev := "CPU"
	if gpu {
		dev = C.GoString(C.wh_device())
	}
	return &Whisper{ctx: ctx, Device: dev}, nil
}

func (w *Whisper) Close() {
	if w.ctx != nil {
		C.whisper_free(w.ctx)
		w.ctx = nil
	}
}

// Transcribe reconoce pcm (16 kHz mono) y devuelve las palabras con tiempos (+offset s).
// progress recibe 0..1. prompt orienta el reconocimiento (p. ej. la letra esperada).
func (w *Whisper) Transcribe(ctx context.Context, pcm []float32, offset float64, lang, prompt string, progress func(float64)) ([]Word, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(pcm) < 1600 {
		return nil, nil
	}
	n := len(pcm)
	cpcm := (*C.float)(C.malloc(C.size_t(n * 4)))
	defer C.free(unsafe.Pointer(cpcm))
	copy(unsafe.Slice((*float32)(unsafe.Pointer(cpcm)), n), pcm)
	flags := (*C.int)(C.calloc(2, 4)) // [0] progreso %, [1] cancelar
	defer C.free(unsafe.Pointer(flags))
	fl := unsafe.Slice((*int32)(unsafe.Pointer(flags)), 2)
	clang := C.CString(lang)
	defer C.free(unsafe.Pointer(clang))
	var cprompt *C.char
	if prompt != "" {
		cprompt = C.CString(prompt)
		defer C.free(unsafe.Pointer(cprompt))
	}

	done := make(chan C.int, 1)
	go func() {
		runtime.LockOSThread()
		done <- C.wh_run(w.ctx, cpcm, C.int(n), C.int(runtime.NumCPU()), clang, cprompt, flags, (*C.int)(unsafe.Add(unsafe.Pointer(flags), 4)))
	}()
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	var ret C.int
loop:
	for {
		select {
		case ret = <-done:
			break loop
		case <-tick.C:
			if ctx.Err() != nil {
				fl[1] = 1
			}
			if progress != nil {
				progress(float64(fl[0]) / 100)
			}
		}
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if ret != 0 {
		return nil, fmt.Errorf("el reconocimiento de voz falló (%d)", int(ret))
	}
	if progress != nil {
		progress(1)
	}

	// Agrupar tokens en palabras (un token que empieza por espacio abre palabra nueva)
	var words []Word
	eot := C.whisper_token_eot(w.ctx)
	nseg := int(C.whisper_full_n_segments(w.ctx))
	for s := 0; s < nseg; s++ {
		segT1 := float64(C.whisper_full_get_segment_t1(w.ctx, C.int(s))) / 100
		ntok := int(C.whisper_full_n_tokens(w.ctx, C.int(s)))
		for t := 0; t < ntok; t++ {
			td := C.whisper_full_get_token_data(w.ctx, C.int(s), C.int(t))
			if td.id >= eot {
				continue
			}
			text := C.GoString(C.whisper_full_get_token_text(w.ctx, C.int(s), C.int(t)))
			if text == "" {
				continue
			}
			t0 := float64(td.t0) / 100
			dtw := float64(td.t_dtw) / 100
			// El instante DTW llega sistemáticamente ~0,4 s tarde respecto al inicio real de la
			// palabra (medido contra un alineado forzado); se compensa.
			at := dtw - DTWBias
			// DTW no fiable: sin dato, o fuera del segmento (pasa al final del audio)
			if td.t_dtw < 0 || dtw > segT1+0.5 {
				at = t0
			}
			if strings.HasPrefix(text, " ") || len(words) == 0 || strings.TrimSpace(text) == "" {
				if strings.TrimSpace(text) == "" {
					continue
				}
				words = append(words, Word{Text: strings.TrimSpace(text), Start: at + offset, End: segT1 + offset, Prob: float64(td.p), T0: t0 + offset, DTW: dtw + offset})
			} else {
				last := &words[len(words)-1]
				last.Text += text
				last.Prob = (last.Prob + float64(td.p)) / 2
			}
		}
	}
	// Fin de cada palabra = inicio de la siguiente (se recorta luego con la energía de la voz)
	for i := 0; i+1 < len(words); i++ {
		if words[i+1].Start > words[i].Start {
			words[i].End = words[i+1].Start
		}
	}
	return words, nil
}
