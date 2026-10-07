// Minimal Vulkan (compute) layer for AudioViz.
// libvulkan is loaded at runtime with dlopen: the binary does not link against it
// and can fall back to CPU rendering if there is no Vulkan GPU/driver.
#ifndef AUDIOVIZ_VK_H
#define AUDIOVIZ_VK_H

#include <stddef.h>
#include <stdint.h>

typedef struct vkr vkr;

enum {
    VKR_BUF_PARAMS = 0,
    VKR_BUF_BG = 1,
    VKR_BUF_Y = 2,
    VKR_BUF_UV = 3,
    VKR_BUF_RGBA = 4,
    VKR_BUF_TEXT = 5,
    VKR_BUF_BG2 = 6,
    VKR_BUF_FLUID = 7, // fluid transition data (particles, bins, coarse level)
    VKR_BUF_FIMG = 8,  // fluid pass output, read by the main pass
    VKR_BUF_LAYERS = 9, // images of the overlay clips on screen (RGBA8, premultiplied)
    VKR_BUF_EACC = 10,  // emitter pass: fixed-point accumulation (4 words per pixel per emitter)
    VKR_BUF_EDATA = 11, // emitter pass: emitters and their births
    VKR_NUM_BUFS = 12,
};

typedef struct {
    int width, height;   // visible resolution
    int pad_w, pad_h;    // padded resolution (multiple of 16)
    int slots;           // frames in flight
    int device_index;    // -1 = automatic
    size_t params_size;
    size_t text_size;    // text atlas size in bytes (SDF)
    size_t fluid_size;   // fluid data in bytes
    size_t layers_size;  // overlay images in bytes
    const uint32_t *spirv;
    size_t spirv_size;
    const uint32_t *spirv_fluid; // fluid pass (same descriptor layout)
    size_t spirv_fluid_size;
    const uint32_t *spirv_emit; // emitter pass (same descriptor layout, push constant: phase)
    size_t spirv_emit_size;
    int emitters;       // frame-sized accumulations of the emitter pass (0 = none)
    size_t edata_size;  // emitter data in bytes
} vkr_config;

int vkr_create(vkr **out, const vkr_config *cfg, char *err, int errlen);
void vkr_destroy(vkr *r);
const char *vkr_device_name(vkr *r);
void *vkr_map(vkr *r, int slot, int which);
// passes: bit 0 = run the fluid pass first (a fluid / liquid transition is on screen), bit 1 =
// the emitter pass (particle emitters on screen)
int vkr_submit(vkr *r, int slot, int passes);
// Invocations of each emitter pass phase (4096 groups of 64): the most particles at once
#define VKR_EMIT_THREADS (4096 * 64)
int vkr_wait(vkr *r, int slot);
int vkr_list_devices(char *out, int len);

// Compute jobs on a device opened with cfg->spirv == NULL (no renderer): one shader whose
// storage buffers are bindings 0..nbufs-1 (mapped, host-visible), kernels chosen by push constants.
// Each dispatch is followed by a barrier; vkj_run submits what was recorded and waits.
#define VKJ_MAX_BUFS 16
typedef struct vkj vkj;
int vkj_create(vkj **out, vkr *r, const uint32_t *spirv, size_t spirv_size, const size_t *sizes, int nbufs,
               int push_size, char *err, int errlen);
void *vkj_map(vkj *j, int buf);
int vkj_dispatch(vkj *j, const void *push, uint32_t gx, uint32_t gy, uint32_t gz);
int vkj_run(vkj *j);
void vkj_destroy(vkj *j);

#endif
