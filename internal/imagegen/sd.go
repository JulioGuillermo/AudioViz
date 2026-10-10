package imagegen

/*
// Per target: static libraries from scripts/build-ml.sh (stable-diffusion.cpp on the same ggml as whisper.cpp)
#cgo CFLAGS: -DGGML_MAX_NAME=160
#cgo linux CFLAGS: -I${SRCDIR}/../../third_party/build/include
#cgo linux LDFLAGS: ${SRCDIR}/../../third_party/build/lib/libstable-diffusion.a ${SRCDIR}/../../third_party/build/lib/libggml.a ${SRCDIR}/../../third_party/build/lib/libggml-vulkan.a ${SRCDIR}/../../third_party/build/lib/libggml-cpu.a ${SRCDIR}/../../third_party/build/lib/libggml-base.a -lm -lpthread -ldl
#cgo windows CFLAGS: -I${SRCDIR}/../../third_party/build-windows-amd64/include
#cgo windows LDFLAGS: ${SRCDIR}/../../third_party/build-windows-amd64/lib/libstable-diffusion.a ${SRCDIR}/../../third_party/build-windows-amd64/lib/libggml.a ${SRCDIR}/../../third_party/build-windows-amd64/lib/libggml-vulkan.a ${SRCDIR}/../../third_party/build-windows-amd64/lib/libggml-cpu.a ${SRCDIR}/../../third_party/build-windows-amd64/lib/libggml-base.a
#cgo darwin,arm64 CFLAGS: -I${SRCDIR}/../../third_party/build-darwin-arm64/include
#cgo darwin,arm64 LDFLAGS: ${SRCDIR}/../../third_party/build-darwin-arm64/lib/libstable-diffusion.a ${SRCDIR}/../../third_party/build-darwin-arm64/lib/libggml.a ${SRCDIR}/../../third_party/build-darwin-arm64/lib/libggml-metal.a ${SRCDIR}/../../third_party/build-darwin-arm64/lib/libggml-blas.a ${SRCDIR}/../../third_party/build-darwin-arm64/lib/libggml-cpu.a ${SRCDIR}/../../third_party/build-darwin-arm64/lib/libggml-base.a -framework Accelerate -framework Metal -framework MetalKit -framework Foundation
#cgo darwin,amd64 CFLAGS: -I${SRCDIR}/../../third_party/build-darwin-amd64/include
#cgo darwin,amd64 LDFLAGS: ${SRCDIR}/../../third_party/build-darwin-amd64/lib/libstable-diffusion.a ${SRCDIR}/../../third_party/build-darwin-amd64/lib/libggml.a ${SRCDIR}/../../third_party/build-darwin-amd64/lib/libggml-metal.a ${SRCDIR}/../../third_party/build-darwin-amd64/lib/libggml-blas.a ${SRCDIR}/../../third_party/build-darwin-amd64/lib/libggml-cpu.a ${SRCDIR}/../../third_party/build-darwin-amd64/lib/libggml-base.a -framework Accelerate -framework Metal -framework MetalKit -framework Foundation
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <stable-diffusion.h>
#include <ggml.h>
#include <ggml-backend.h>

// stable-diffusion.cpp reports through global callbacks: they only store the state, which Go
// polls (no calls from C into Go). One generation runs at a time (see gen in sd.go).
//
// The phase comes from its log messages ([EDIT mode: encode the references] → "parse '…'" text
// encoding → "generating image" sampling → "decoding latents"). The weights are loaded
// lazily inside each phase ("loading N/M tensors from …"); the progress callback then counts
// tensors instead of sampling steps or VAE tiles, so it is stored apart.
enum { PH_NONE, PH_TEXT, PH_REFS, PH_SAMPLE, PH_DECODE, PH_DONE };
static volatile int av_phase, av_loading, av_step, av_steps, av_lstep, av_lsteps;
static void av_progress(int step, int steps, float t, void *d) {
    (void)t; (void)d;
    if (av_loading) { av_lstep = step; av_lsteps = steps; }
    else { av_step = step; av_steps = steps; }
}
// Current phase; steps (sampling steps or VAE tiles) and, while loading weights, tensors loaded
static int av_get_state(int *step, int *steps, int *loading, int *lstep, int *lsteps) {
    *step = av_step; *steps = av_steps; *loading = av_loading; *lstep = av_lstep; *lsteps = av_lsteps;
    return av_phase;
}
static void av_set_phase(int p) {
    if (p != av_phase) { av_phase = p; av_step = 0; av_steps = 0; }
}

// Latest preview (a cheap projection of the latents at each step), copied under a spin lock
static unsigned char *av_prev;
static int av_pw, av_ph, av_pc, av_pcap;
static volatile int av_pseq, av_plock;
static void lock(void) { while (__atomic_exchange_n(&av_plock, 1, __ATOMIC_ACQUIRE)) {} }
static void unlock(void) { __atomic_store_n(&av_plock, 0, __ATOMIC_RELEASE); }
static void av_preview(int step, int n, sd_image_t *f, bool noisy, void *d) {
    (void)step; (void)noisy; (void)d;
    if (n < 1 || !f[0].data) return;
    int sz = f[0].width * f[0].height * f[0].channel;
    lock();
    if (sz > av_pcap) { free(av_prev); av_prev = malloc(sz); av_pcap = av_prev ? sz : 0; }
    if (av_prev) {
        memcpy(av_prev, f[0].data, sz);
        av_pw = f[0].width; av_ph = f[0].height; av_pc = f[0].channel;
        av_pseq++;
    }
    unlock();
}
// Copies the latest preview if newer than seq; returns its sequence number (0 = none yet)
static int av_preview_get(int seq, unsigned char *dst, int cap, int *w, int *h, int *c) {
    lock();
    int s = av_pseq;
    if (s != seq && av_prev && av_pw * av_ph * av_pc <= cap) {
        memcpy(dst, av_prev, av_pw * av_ph * av_pc);
        *w = av_pw; *h = av_ph; *c = av_pc;
    } else if (s != seq) {
        s = seq;
    }
    unlock();
    return s;
}

// Logs: the last error is kept for the error message; AUDIOVIZ_DEBUG_SD=1 prints everything
static char av_err[512];
static int av_debug;
static void av_log(enum sd_log_level_t l, const char *t, void *d) {
    (void)d;
    if (l >= SD_LOG_ERROR) { strncpy(av_err, t, sizeof av_err - 1); }
    if (strstr(t, "tensors from ")) { av_loading = 1; av_lstep = 0; av_lsteps = 0; }
    else if (strstr(t, "loading tensors completed")) av_loading = 0;
    else if (strstr(t, "EDIT mode")) av_set_phase(PH_REFS);
    else if (strstr(t, "parse '") && av_phase < PH_SAMPLE) av_set_phase(PH_TEXT);
    else if (strstr(t, "generating image:")) av_set_phase(PH_SAMPLE);
    else if (strstr(t, "decoding") && strstr(t, "latents")) av_set_phase(PH_DECODE);
    else if (strstr(t, "decode_first_stage completed")) av_set_phase(PH_DONE);
    if (av_debug) fputs(t, stderr);
}

static const char *av_last_err(void) { return av_err; }

static void av_ggml_log(enum ggml_log_level l, const char *t, void *d) { (void)l; (void)d; if (av_debug) fputs(t, stderr); }

static void av_setup(int debug) {
    av_debug = debug;
    ggml_log_set(av_ggml_log, NULL); // until stable-diffusion.cpp routes it to av_log
    sd_set_log_callback(av_log, NULL);
    sd_set_progress_callback(av_progress, NULL);
    sd_set_preview_callback(av_preview, PREVIEW_PROJ, 1, true, false, NULL);
}

// What the backend plugins take from ggml-base besides what AudioViz itself uses (exported with the
// rest of ggml, scripts/ggml-exports.list): kept in the binary by referring to them
#define AV_KEEP(f) extern void f(void);
#define AV_KEEP_LIST(X) X(quantize_iq1_m) X(quantize_iq1_s) X(quantize_iq2_s) X(quantize_iq2_xs) X(quantize_iq2_xxs) \
    X(quantize_row_iq3_s_ref) X(quantize_row_iq3_xxs_ref) X(quantize_row_iq4_xs_ref) X(quantize_row_q2_K_ref) \
    X(quantize_row_q3_K_ref) X(quantize_row_q4_K_ref) X(quantize_row_q5_K_ref) X(quantize_row_q6_K_ref)
AV_KEEP_LIST(AV_KEEP)
#define AV_KEEP_PTR(f) (void *) f,
__attribute__((used)) void *av_keep[] = { AV_KEEP_LIST(AV_KEEP_PTR) };

// Backend plugins (plugins.go): loaded into ggml's registry; av_dev: the device the models use
// ("" = stable-diffusion.cpp's choice, the first GPU)
static char av_dev[64];
int av_load_backend(const char *path) { return ggml_backend_load(path) != NULL; }
static const char *av_dev_name(void) { return av_dev; }
// av_use: the first GPU whose device name starts with prefix ("SYCL", "Vulkan") is used; 0: none
int av_use(const char *prefix) {
    av_dev[0] = 0;
    if (!prefix || !*prefix) return 1;
    for (size_t i = 0; i < ggml_backend_dev_count(); i++) {
        ggml_backend_dev_t d = ggml_backend_dev_get(i);
        const char *n = ggml_backend_dev_name(d);
        enum ggml_backend_dev_type t = ggml_backend_dev_type(d);  // (an integrated GPU: IGPU)
        if ((t == GGML_BACKEND_DEVICE_TYPE_GPU || t == GGML_BACKEND_DEVICE_TYPE_IGPU) && strncmp(n, prefix, strlen(prefix)) == 0) {
            strncpy(av_dev, n, sizeof av_dev - 1);
            return 1;
        }
    }
    return 0;
}

static const char *av_device(void) {
    if (av_dev[0]) {
        ggml_backend_dev_t d = ggml_backend_dev_by_name(av_dev);
        if (d) return ggml_backend_dev_description(d);
    }
    for (size_t i = 0; i < ggml_backend_dev_count(); i++) {
        ggml_backend_dev_t d = ggml_backend_dev_get(i);
        enum ggml_backend_dev_type t = ggml_backend_dev_type(d);
        if (t != GGML_BACKEND_DEVICE_TYPE_CPU && t != GGML_BACKEND_DEVICE_TYPE_ACCEL) return ggml_backend_dev_description(d);
    }
    return "CPU";
}

static sd_ctx_t *av_open(const char *diff, const char *llm, const char *vision, const char *vae, int fa) {
    sd_ctx_params_t p;
    sd_ctx_params_init(&p);
    p.diffusion_model_path = diff;
    p.llm_path = llm;
    p.llm_vision_path = vision;
    p.vae_path = vae;
    p.n_threads = sd_get_num_physical_cores();
    p.diffusion_flash_attn = fa;
    if (av_dev[0]) p.backend = av_dev;
    av_err[0] = 0;
    return new_sd_ctx(&p);
}

// sampler / scheduler: names as in stable-diffusion.cpp ("euler", "dpm++2m", "karras"…), NULL =
// the model's default (or Euler if euler); init: optional starting image (img2img) with strength
static int av_generate(sd_ctx_t *ctx, const char *prompt, const char *negative, int w, int h, int steps,
                       float cfg, const char *sampler, const char *scheduler, int euler, long long seed,
                       sd_image_t *refs, int nrefs, sd_image_t *init, float strength, int tile, sd_image_t *out) {
    sd_img_gen_params_t g;
    sd_img_gen_params_init(&g);
    g.prompt = prompt;
    g.negative_prompt = negative ? negative : "";
    g.width = w;
    g.height = h;
    g.seed = seed;
    g.batch_count = 1;
    g.ref_images = refs;
    g.ref_images_count = nrefs;
    g.sample_params.sample_steps = steps;
    g.sample_params.guidance.txt_cfg = cfg;
    if (euler) g.sample_params.sample_method = EULER_SAMPLE_METHOD;
    if (sampler) g.sample_params.sample_method = str_to_sample_method(sampler);
    if (scheduler) g.sample_params.scheduler = str_to_scheduler(scheduler);
    if (init) { g.init_image = *init; g.strength = strength; }
    // Large images: the VAE decodes (and encodes) in tiles; its attention over every latent
    // pixel otherwise grows with the square of the size (217 s instead of 28 s at 1920x1088)
    g.vae_tiling_params.enabled = tile;
    av_phase = PH_NONE; av_loading = 0; av_step = 0; av_steps = 0; av_err[0] = 0;
    __atomic_store_n(&av_pseq, 0, __ATOMIC_RELEASE);
    sd_cancel_generation(ctx, SD_CANCEL_RESET);
    sd_image_t *imgs = NULL;
    int n = 0;
    if (!generate_image(ctx, &g, &imgs, &n) || n < 1 || !imgs || !imgs[0].data) {
        if (imgs) { for (int i = 0; i < n; i++) free(imgs[i].data); free(imgs); }
        return 0;
    }
    *out = imgs[0];
    for (int i = 1; i < n; i++) free(imgs[i].data);
    free(imgs);
    return 1;
}
*/
import "C"

import (
	"errors"
	"image"
	"os"
	"strings"
	"sync"
	"unsafe"

	_ "audioviz/internal/ggml" // Vulkan loader bridge
)

var setupOnce sync.Once

// engine keeps the loaded models between generations (loading takes seconds and the next image
// usually uses the same model); a different model replaces them.
type engine struct {
	ctx    *C.sd_ctx_t
	key    string
	device string
}

var (
	gen sync.Mutex // one generation at a time (the C callbacks are global)
	eng engine
)

func lastErr(def string) error {
	if s := strings.TrimSpace(C.GoString(C.av_last_err())); s != "" {
		return errors.New(s)
	}
	return errors.New(def)
}

func setup() {
	setupOnce.Do(func() {
		C.av_setup(C.int(btoi(os.Getenv("AUDIOVIZ_DEBUG_SD") != "")))
		loadPlugins()
	})
}

// Device is the GPU used (or "CPU")
func Device() string {
	setup()
	return C.GoString(C.av_device())
}

// DefaultFlashAttn: flash attention when the request doesn't say (on: its memory stays small
// at any size). On Intel GPUs ggml-vulkan only has its scalar kernel (the cooperative-matrix
// version needs 16×16×16 tiles, Intel XMX uses 8×16×16), so turning it off is ~1.6× faster for
// small images, but the plain attention needs heads × tokens² × 4 bytes (2.4 GB at 5000 tokens,
// 6 GB at 8000). It is the user's choice. AUDIOVIZ_SD_FA=0 changes the default.
func DefaultFlashAttn() bool { return os.Getenv("AUDIOVIZ_SD_FA") != "0" }

// IntelGPU: the device is an Intel GPU (where large images are slow: see DefaultFlashAttn)
func IntelGPU() bool { return strings.Contains(Device(), "Intel") }

// DeviceName: the ggml device the engine setting picks ("SYCL0"…; "": the default GPU), for the
// other models (depth) to run on the same one
func DeviceName() string { setup(); return C.GoString(C.av_dev_name()) }

// onSYCL: the models run on the Intel plugin's SYCL backend (plugins.go)
func onSYCL() bool { setup(); return strings.HasPrefix(C.GoString(C.av_dev_name()), "SYCL") }

// Tokens: how many the model processes for an image of w×h with n reference images (the
// references are resized to the image size; 16×16 pixels per token)
func Tokens(w, h, refs int) int { return (round32(w) / 16) * (round32(h) / 16) * (1 + refs) }

// load opens the models (if not already loaded); paths: diffusion, text encoder, vision (or ""), VAE
func (e *engine) load(key, diff, llm, vision, vae string, fa bool) error {
	setup()
	// Z-Image's feed-forward output in f16 on Intel (scripts/ml/patches-sd): its forced f32
	// accumulation is 3× slower there, and its 1/128 scale keeps f16 in range (checked on Xe2)
	if IntelGPU() && os.Getenv("SD_ZIMAGE_W2_F16ACC") == "" {
		os.Setenv("SD_ZIMAGE_W2_F16ACC", "1")
	}
	if e.ctx != nil && e.key == key {
		return nil
	}
	e.free()
	cs := func(s string) *C.char {
		if s == "" {
			return nil
		}
		return C.CString(s)
	}
	cd, cl, cv, ca := cs(diff), cs(llm), cs(vision), cs(vae)
	defer func() {
		for _, p := range []*C.char{cd, cl, cv, ca} {
			C.free(unsafe.Pointer(p))
		}
	}()
	dev := C.GoString(C.av_device())
	ctx := C.av_open(cd, cl, cv, ca, C.int(btoi(fa)))
	if ctx == nil {
		return lastErr("could not load the image model")
	}
	e.ctx, e.key, e.device = ctx, key, dev
	return nil
}

func (e *engine) free() {
	if e.ctx != nil {
		C.free_sd_ctx(e.ctx)
		e.ctx, e.key = nil, ""
	}
}

// Unload frees the loaded image model (its memory), e.g. before another heavy job
func Unload() {
	gen.Lock()
	defer gen.Unlock()
	eng.free()
}

func cancel() {
	if eng.ctx != nil {
		C.sd_cancel_generation(eng.ctx, C.SD_CANCEL_ALL)
	}
}

// Phases of a generation, as stable-diffusion.cpp goes through them
const (
	phNone = iota
	phText
	phRefs
	phSample
	phDecode
	phDone
)

// state: phase, its progress (sampling steps or VAE tiles) and, while loading weights, the tensors
type state struct {
	phase, step, steps    int
	loading               bool
	tensors, tensorsTotal int
}

func getState() state {
	var s, n, l, ls, ln C.int
	ph := C.av_get_state(&s, &n, &l, &ls, &ln)
	return state{int(ph), int(s), int(n), l != 0, int(ls), int(ln)}
}

// preview copies the latest preview if it is newer than seq
func preview(seq int, buf []byte) (int, *image.NRGBA) {
	var w, h, c C.int
	s := int(C.av_preview_get(C.int(seq), (*C.uchar)(unsafe.Pointer(&buf[0])), C.int(len(buf)), &w, &h, &c))
	if s == seq || w == 0 {
		return seq, nil
	}
	return s, toNRGBA(buf[:int(w*h*c)], int(w), int(h), int(c))
}

// rgb is an image as tightly packed 8-bit RGB, as stable-diffusion.cpp takes it
type rgb struct {
	w, h int
	pix  []byte
}

// sampling: what run needs besides the prompt and the images
type sampling struct {
	negative           string
	w, h, steps        int
	cfg                float32
	sampler, scheduler string // "" = default
	euler              bool   // the default is Euler (the family asks for it)
	seed               int64
	strength           float32 // with an initial image
	tile               bool    // VAE in tiles (large images)
}

func (sp sampling) cstr(s string) *C.char {
	if s == "" {
		return nil
	}
	return C.CString(s)
}

// run generates one image with the loaded model; init (optional) is the starting image, at the output size
func (e *engine) run(prompt string, sp sampling, refs []rgb, init *rgb) (*image.NRGBA, error) {
	cp, cn := C.CString(prompt), C.CString(sp.negative)
	csa, csc := sp.cstr(sp.sampler), sp.cstr(sp.scheduler)
	defer func() {
		for _, p := range []*C.char{cp, cn, csa, csc} {
			C.free(unsafe.Pointer(p))
		}
	}()
	var cr *C.sd_image_t
	if len(refs) > 0 {
		cr = (*C.sd_image_t)(C.calloc(C.size_t(len(refs)), C.size_t(unsafe.Sizeof(C.sd_image_t{}))))
		defer C.free(unsafe.Pointer(cr))
		arr := unsafe.Slice(cr, len(refs))
		for i, r := range refs {
			p := C.CBytes(r.pix)
			defer C.free(p)
			arr[i] = C.sd_image_t{width: C.uint32_t(r.w), height: C.uint32_t(r.h), channel: 3, data: (*C.uint8_t)(p)}
		}
	}
	var ci *C.sd_image_t
	if init != nil {
		p := C.CBytes(init.pix)
		defer C.free(p)
		ci = &C.sd_image_t{width: C.uint32_t(init.w), height: C.uint32_t(init.h), channel: 3, data: (*C.uint8_t)(p)}
	}
	var out C.sd_image_t
	if C.av_generate(e.ctx, cp, cn, C.int(sp.w), C.int(sp.h), C.int(sp.steps), C.float(sp.cfg), csa, csc,
		C.int(btoi(sp.euler)), C.longlong(sp.seed), cr, C.int(len(refs)), ci, C.float(sp.strength), C.int(btoi(sp.tile)), &out) == 0 {
		return nil, lastErr("the image could not be generated")
	}
	defer C.free(unsafe.Pointer(out.data))
	n := int(out.width * out.height * out.channel)
	return toNRGBA(unsafe.Slice((*byte)(unsafe.Pointer(out.data)), n), int(out.width), int(out.height), int(out.channel)), nil
}

func toNRGBA(pix []byte, w, h, c int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < w*h; i++ {
		d := img.Pix[i*4 : i*4+4]
		s := pix[i*c:]
		switch c {
		case 1:
			d[0], d[1], d[2] = s[0], s[0], s[0]
		default:
			d[0], d[1], d[2] = s[0], s[1], s[2]
		}
		d[3] = 255
		if c == 4 {
			d[3] = s[3]
		}
	}
	return img
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}
