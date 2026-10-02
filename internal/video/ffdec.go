package video

/*
#cgo CFLAGS: -I${SRCDIR}/../../third_party/build/include
#cgo LDFLAGS: ${SRCDIR}/../../third_party/build/lib/libavformat.a ${SRCDIR}/../../third_party/build/lib/libavcodec.a ${SRCDIR}/../../third_party/build/lib/libswscale.a ${SRCDIR}/../../third_party/build/lib/libavutil.a -lm -lpthread
#include <stdlib.h>
#include <string.h>
#include <libavformat/avformat.h>
#include <libavcodec/avcodec.h>
#include <libswscale/swscale.h>
#include <libavutil/imgutils.h>

typedef struct {
    AVFormatContext *fmt;
    AVCodecContext *dec;
    int vs;
    AVFrame *frm;
    AVPacket *pkt;
    struct SwsContext *sws;
    int sw, sh, sfmt;     // source of the current sws
    int dw, dh;           // scaled size
    double tb;
    double duration;
    double fps;
    double pts;           // time (s) of the last decoded frame
    int eof;
} vdec;

static int vd_err(char *err, int len, const char *what, int code) {
    char b[128];
    av_strerror(code, b, sizeof b);
    snprintf(err, len, "%s: %s", what, b);
    return -1;
}

static int vd_open(vdec *v, const char *path, char *err, int errlen) {
    memset(v, 0, sizeof *v);
    int r = avformat_open_input(&v->fmt, path, NULL, NULL);
    if (r < 0) return vd_err(err, errlen, "could not open video", r);
    if ((r = avformat_find_stream_info(v->fmt, NULL)) < 0) return vd_err(err, errlen, "video info", r);
    const AVCodec *codec = NULL;
    v->vs = av_find_best_stream(v->fmt, AVMEDIA_TYPE_VIDEO, -1, -1, &codec, 0);
    if (v->vs < 0 || !codec) { snprintf(err, errlen, "the file has no supported video stream"); return -1; }
    AVStream *st = v->fmt->streams[v->vs];
    v->dec = avcodec_alloc_context3(codec);
    avcodec_parameters_to_context(v->dec, st->codecpar);
    v->dec->thread_count = 0; // automatic
    if ((r = avcodec_open2(v->dec, codec, NULL)) < 0) return vd_err(err, errlen, "decoder", r);
    v->tb = av_q2d(st->time_base);
    v->duration = v->fmt->duration > 0 ? v->fmt->duration / (double)AV_TIME_BASE
                : (st->duration > 0 ? st->duration * v->tb : 0);
    AVRational fr = av_guess_frame_rate(v->fmt, st, NULL);
    v->fps = fr.num > 0 && fr.den > 0 ? av_q2d(fr) : 30;
    v->frm = av_frame_alloc();
    v->pkt = av_packet_alloc();
    v->pts = -1;
    return 0;
}

static void vd_size(vdec *v, int *w, int *h) { *w = v->dec->width; *h = v->dec->height; }

// Decodes the next frame. 1 = ok, 0 = end, <0 = error
static int vd_next(vdec *v) {
    for (;;) {
        int r = avcodec_receive_frame(v->dec, v->frm);
        if (r == 0) {
            int64_t p = v->frm->best_effort_timestamp;
            if (p == AV_NOPTS_VALUE) p = v->frm->pts;
            v->pts = p == AV_NOPTS_VALUE ? v->pts + 1.0 / v->fps : p * v->tb;
            return 1;
        }
        if (r == AVERROR_EOF) { v->eof = 1; return 0; }
        if (r != AVERROR(EAGAIN)) return r;
        if (v->eof) { avcodec_send_packet(v->dec, NULL); continue; }
        r = av_read_frame(v->fmt, v->pkt);
        if (r < 0) { v->eof = 1; avcodec_send_packet(v->dec, NULL); continue; }
        if (v->pkt->stream_index == v->vs) avcodec_send_packet(v->dec, v->pkt);
        av_packet_unref(v->pkt);
    }
}

static int vd_seek(vdec *v, double t) {
    int64_t ts = (int64_t)(t / v->tb);
    int r = av_seek_frame(v->fmt, v->vs, ts, AVSEEK_FLAG_BACKWARD);
    if (r < 0) r = av_seek_frame(v->fmt, -1, (int64_t)(t * AV_TIME_BASE), AVSEEK_FLAG_BACKWARD);
    avcodec_flush_buffers(v->dec);
    v->eof = 0;
    v->pts = -1;
    return r;
}

// Scales the last frame to dw x dh RGBA into out (stride dw*4)
static int vd_scale(vdec *v, int dw, int dh, uint8_t *out) {
    AVFrame *f = v->frm;
    if (!v->sws || v->sw != f->width || v->sh != f->height || v->sfmt != f->format || v->dw != dw || v->dh != dh) {
        sws_freeContext(v->sws);
        v->sws = sws_getContext(f->width, f->height, f->format, dw, dh, AV_PIX_FMT_RGBA, SWS_BICUBIC, NULL, NULL, NULL);
        v->sw = f->width; v->sh = f->height; v->sfmt = f->format; v->dw = dw; v->dh = dh;
        if (!v->sws) return -1;
    }
    uint8_t *dst[4] = {out, NULL, NULL, NULL};
    int ls[4] = {dw * 4, 0, 0, 0};
    sws_scale(v->sws, (const uint8_t *const *)f->data, f->linesize, 0, f->height, dst, ls);
    return 0;
}

static void vd_close(vdec *v) {
    if (v->sws) sws_freeContext(v->sws);
    if (v->frm) av_frame_free(&v->frm);
    if (v->pkt) av_packet_free(&v->pkt);
    if (v->dec) avcodec_free_context(&v->dec);
    if (v->fmt) avformat_close_input(&v->fmt);
}
*/
import "C"

import (
	"fmt"
	"image"
	"math"
	"unsafe"
)

// videoReader decodes a background video with the minimal statically linked FFmpeg
type videoReader struct {
	v        *C.vdec
	w, h     int // output
	srcW     int
	srcH     int
	dw, dh   int // scaled size
	ox, oy   int // position of the scaled image within the output (may be negative: cropping)
	canvas   *image.RGBA
	scaled   []byte
	sbuf     unsafe.Pointer
	loop     bool
	duration float64
	fps      float64
	ver      int
	have     bool
}

func probeVideo(path string) (w, h int, dur float64, err error) {
	r, err := openVideo(path)
	if err != nil {
		return 0, 0, 0, err
	}
	defer r.close()
	return r.srcW, r.srcH, r.duration, nil
}

func openVideo(path string) (*videoReader, error) {
	v := (*C.vdec)(C.calloc(1, C.size_t(unsafe.Sizeof(C.vdec{}))))
	cp := C.CString(path)
	defer C.free(unsafe.Pointer(cp))
	errBuf := make([]byte, 256)
	if C.vd_open(v, cp, (*C.char)(unsafe.Pointer(&errBuf[0])), C.int(len(errBuf))) != 0 {
		C.vd_close(v)
		C.free(unsafe.Pointer(v))
		return nil, fmt.Errorf("%s: %s", path, C.GoString((*C.char)(unsafe.Pointer(&errBuf[0]))))
	}
	var cw, ch C.int
	C.vd_size(v, &cw, &ch)
	return &videoReader{v: v, srcW: int(cw), srcH: int(ch), duration: float64(v.duration), fps: float64(v.fps)}, nil
}

func newVideoReader(path string, w, h int, fitMode string, loop bool) (*videoReader, error) {
	r, err := openVideo(path)
	if err != nil {
		return nil, err
	}
	r.w, r.h, r.loop = w, h, loop
	r.canvas = image.NewRGBA(image.Rect(0, 0, w, h))
	sx, sy := float64(w)/float64(r.srcW), float64(h)/float64(r.srcH)
	switch fitMode {
	case "stretch":
		r.dw, r.dh = w, h
	case "contain":
		s := math.Min(sx, sy)
		r.dw, r.dh = int(float64(r.srcW)*s+0.5), int(float64(r.srcH)*s+0.5)
	default: // cover
		s := math.Max(sx, sy)
		r.dw, r.dh = int(float64(r.srcW)*s+0.5), int(float64(r.srcH)*s+0.5)
	}
	r.dw, r.dh = max(r.dw, 2), max(r.dh, 2)
	r.ox, r.oy = (w-r.dw)/2, (h-r.dh)/2
	r.sbuf = C.malloc(C.size_t(r.dw * r.dh * 4))
	r.scaled = unsafe.Slice((*byte)(r.sbuf), r.dw*r.dh*4)
	for i := 3; i < len(r.canvas.Pix); i += 4 {
		r.canvas.Pix[i] = 255
	}
	return r, nil
}

func (r *videoReader) Width() int  { return r.w }
func (r *videoReader) Height() int { return r.h }

func (r *videoReader) decodeNext() bool {
	if C.vd_next(r.v) == 1 {
		return true
	}
	if r.loop && r.duration > 0 {
		C.vd_seek(r.v, 0)
		return C.vd_next(r.v) == 1
	}
	return false
}

// FrameAt returns the video frame at time t (s since its segment starts)
func (r *videoReader) FrameAt(t float64) (*image.RGBA, int) {
	if t < 0 {
		t = 0
	}
	if r.loop && r.duration > 0 {
		t = math.Mod(t, r.duration)
	}
	cur := float64(r.v.pts)
	half := 0.5 / math.Max(r.fps, 1)
	if !r.have || t < cur-half || t > cur+2.0 {
		// Seek: find the previous keyframe and decode up to t
		C.vd_seek(r.v, C.double(t))
		if !r.decodeNext() {
			return r.canvas, r.ver
		}
		cur = float64(r.v.pts)
	}
	changed := !r.have
	for cur+half < t-1e-6 {
		before := cur
		if !r.decodeNext() {
			break
		}
		cur = float64(r.v.pts)
		changed = true
		if cur < before { // wrapped around (loop)
			break
		}
	}
	if changed {
		r.blit()
		r.have = true
		r.ver++
	}
	return r.canvas, r.ver
}

func (r *videoReader) blit() {
	if C.vd_scale(r.v, C.int(r.dw), C.int(r.dh), (*C.uint8_t)(r.sbuf)) != 0 {
		return
	}
	// Copy the visible area (crop in cover, black bars in contain)
	x0, y0 := max(r.ox, 0), max(r.oy, 0)
	x1, y1 := min(r.ox+r.dw, r.w), min(r.oy+r.dh, r.h)
	for y := y0; y < y1; y++ {
		src := r.scaled[((y-r.oy)*r.dw+(x0-r.ox))*4 : ((y-r.oy)*r.dw+(x1-r.ox))*4]
		copy(r.canvas.Pix[(y*r.w+x0)*4:], src)
	}
}

func (r *videoReader) close() {
	if r.v != nil {
		C.vd_close(r.v)
		C.free(unsafe.Pointer(r.v))
		r.v = nil
	}
	if r.sbuf != nil {
		C.free(r.sbuf)
		r.sbuf = nil
	}
}

func (r *videoReader) Close() error { r.close(); return nil }
