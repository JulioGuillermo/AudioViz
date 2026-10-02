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
    VKR_NUM_BUFS = 7,
};

typedef struct {
    int width, height;   // visible resolution
    int pad_w, pad_h;    // padded resolution (multiple of 16)
    int slots;           // frames in flight
    int device_index;    // -1 = automatic
    size_t params_size;
    size_t text_size;    // text atlas size in bytes (SDF)
    const uint32_t *spirv;
    size_t spirv_size;
} vkr_config;

int vkr_create(vkr **out, const vkr_config *cfg, char *err, int errlen);
void vkr_destroy(vkr *r);
const char *vkr_device_name(vkr *r);
void *vkr_map(vkr *r, int slot, int which);
int vkr_submit(vkr *r, int slot);
int vkr_wait(vkr *r, int slot);
int vkr_list_devices(char *out, int len);

#endif
