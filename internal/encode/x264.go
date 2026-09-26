// Package encode envuelve libx264 enlazada estáticamente (third_party/build/lib/libx264.a).
package encode

/*
#cgo CFLAGS: -I${SRCDIR}/../../third_party/build/include
#cgo LDFLAGS: ${SRCDIR}/../../third_party/build/lib/libx264.a -lm -lpthread -ldl
#include <stdlib.h>
#include <string.h>
#include <stdint.h>
#include <x264.h>

typedef struct {
    x264_t *h;
    x264_param_t param;
    x264_picture_t pic, out;
    uint8_t *buf; int buf_len;
} enc_t;

static const char *enc_open(enc_t *e, int w, int h, int fps, const char *preset, float crf, int threads) {
    if (x264_param_default_preset(&e->param, preset, NULL) < 0) return "preset x264 inválido";
    x264_param_t *p = &e->param;
    p->i_width = w; p->i_height = h;
    p->i_csp = X264_CSP_NV12;
    p->i_fps_num = fps; p->i_fps_den = 1;
    p->i_timebase_num = 1; p->i_timebase_den = fps;
    p->b_vfr_input = 0;
    p->b_repeat_headers = 0;
    p->b_annexb = 1;
    p->i_threads = threads;
    p->i_keyint_max = fps * 4;
    p->rc.i_rc_method = X264_RC_CRF;
    p->rc.f_rf_constant = crf;
    p->vui.i_colorprim = 1; p->vui.i_transfer = 1; p->vui.i_colmatrix = 1; // BT.709
    p->vui.b_fullrange = 0;
    p->i_log_level = X264_LOG_ERROR;
    if (x264_param_apply_profile(p, "high") < 0) return "perfil x264 inválido";
    e->h = x264_encoder_open(p);
    if (!e->h) return "x264_encoder_open falló";
    x264_picture_init(&e->pic);
    e->pic.img.i_csp = X264_CSP_NV12;
    e->pic.img.i_plane = 2;
    return NULL;
}

static int enc_headers(enc_t *e, uint8_t **out) {
    x264_nal_t *nals; int n;
    int sz = x264_encoder_headers(e->h, &nals, &n);
    if (sz < 0) return -1;
    *out = nals[0].p_payload;
    return sz;
}

// Devuelve bytes de salida (>0), 0 si el cuadro quedó retenido, <0 error
static int enc_frame(enc_t *e, uint8_t *y, uint8_t *uv, int stride, int64_t pts, uint8_t **out) {
    x264_nal_t *nals; int n;
    x264_picture_t *in = NULL;
    if (y) {
        e->pic.img.plane[0] = y; e->pic.img.i_stride[0] = stride;
        e->pic.img.plane[1] = uv; e->pic.img.i_stride[1] = stride;
        e->pic.i_pts = pts;
        e->pic.i_type = X264_TYPE_AUTO;
        in = &e->pic;
    }
    int sz = x264_encoder_encode(e->h, &nals, &n, in, &e->out);
    if (sz > 0) *out = nals[0].p_payload;
    return sz;
}

static int enc_delayed(enc_t *e) { return x264_encoder_delayed_frames(e->h); }
static void enc_close(enc_t *e) { if (e->h) x264_encoder_close(e->h); e->h = NULL; }
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// Packet es una unidad de acceso H.264 codificada (Annex B)
type Packet struct {
	Data     []byte
	PTS, DTS int64
	Key      bool
}

type X264 struct {
	e *C.enc_t
}

func NewX264(width, height, fps int, preset string, crf float64) (*X264, error) {
	e := (*C.enc_t)(C.calloc(1, C.size_t(unsafe.Sizeof(C.enc_t{}))))
	cp := C.CString(preset)
	defer C.free(unsafe.Pointer(cp))
	if msg := C.enc_open(e, C.int(width), C.int(height), C.int(fps), cp, C.float(crf), 0); msg != nil {
		C.free(unsafe.Pointer(e))
		return nil, fmt.Errorf("%s", C.GoString(msg))
	}
	return &X264{e: e}, nil
}

// Headers devuelve SPS/PPS/SEI en Annex B
func (x *X264) Headers() []byte {
	var out *C.uint8_t
	n := C.enc_headers(x.e, &out)
	if n <= 0 {
		return nil
	}
	return C.GoBytes(unsafe.Pointer(out), n)
}

func (x *X264) packet(n C.int, out *C.uint8_t) *Packet {
	if n <= 0 {
		return nil
	}
	return &Packet{
		Data: C.GoBytes(unsafe.Pointer(out), n),
		PTS:  int64(x.e.out.i_pts),
		DTS:  int64(x.e.out.i_dts),
		Key:  x.e.out.b_keyframe != 0,
	}
}

// Encode codifica un cuadro NV12. y/uv deben apuntar a memoria no gestionada por Go
// (buffers mapeados de Vulkan o C.malloc). x264 copia la imagen antes de volver.
func (x *X264) Encode(y, uv unsafe.Pointer, stride int, pts int64) (*Packet, error) {
	var out *C.uint8_t
	n := C.enc_frame(x.e, (*C.uint8_t)(y), (*C.uint8_t)(uv), C.int(stride), C.int64_t(pts), &out)
	if n < 0 {
		return nil, fmt.Errorf("x264_encoder_encode falló")
	}
	return x.packet(n, out), nil
}

// Flush vacía los cuadros retenidos (B-frames / lookahead)
func (x *X264) Flush(emit func(*Packet) error) error {
	for C.enc_delayed(x.e) > 0 {
		var out *C.uint8_t
		n := C.enc_frame(x.e, nil, nil, 0, 0, &out)
		if n < 0 {
			return fmt.Errorf("x264 flush falló")
		}
		if p := x.packet(n, out); p != nil {
			if err := emit(p); err != nil {
				return err
			}
		}
	}
	return nil
}

func (x *X264) Close() {
	if x.e != nil {
		C.enc_close(x.e)
		C.free(unsafe.Pointer(x.e))
		x.e = nil
	}
}

// Alloc reserva memoria C para planos NV12 en el camino de render por CPU
func Alloc(n int) unsafe.Pointer { return C.malloc(C.size_t(n)) }
func Free(p unsafe.Pointer)      { C.free(p) }
