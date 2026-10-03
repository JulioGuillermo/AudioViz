// Vulkan bridge for ggml (whisper.cpp): the Vulkan backend only imports these 4 functions
// (it gets the rest via vkGetInstanceProcAddr). Here they are resolved by loading the Vulkan
// loader at runtime; if it is missing, vkCreateInstance fails with an error and ggml falls back
// to the CPU. This way the binary does not depend on libvulkan to start.
// On macOS whisper.cpp uses Metal instead, so this file is empty there.
#if !defined(__APPLE__)
#define VK_NO_PROTOTYPES
#include <stddef.h>
#include <stdlib.h>
#include <string.h>
#include <vulkan/vulkan.h>

#if defined(_WIN32)
#include <windows.h>
static void *dl_open(const char *n) { return (void *)LoadLibraryA(n); }
static void *dl_sym(void *h, const char *s) { return (void *)GetProcAddress((HMODULE)h, s); }
static const char *names[] = {"vulkan-1.dll", NULL};
#else
#include <dlfcn.h>
static void *dl_open(const char *n) { return dlopen(n, RTLD_NOW | RTLD_LOCAL); }
static void *dl_sym(void *h, const char *s) { return dlsym(h, s); }
static const char *names[] = {"libvulkan.so.1", "libvulkan.so", NULL};
#endif

static void *lib;
static PFN_vkGetInstanceProcAddr real_gipa;

static void load(void) {
    // To test the no-GPU mode: AUDIOVIZ_NO_VULKAN=1 simulates a missing Vulkan loader
    const char *nv = getenv("AUDIOVIZ_NO_VULKAN");
    if (nv && *nv && *nv != '0') return;
    for (int i = 0; names[i] && !lib; i++) lib = dl_open(names[i]);
    if (lib) real_gipa = (PFN_vkGetInstanceProcAddr)dl_sym(lib, "vkGetInstanceProcAddr");
}

// Runs load() once, thread-safe (portable: no pthread_once on Windows)
static int load_state; // 0 = not loaded, 1 = loading, 2 = done
static void ensure_loaded(void) {
    if (__atomic_load_n(&load_state, __ATOMIC_ACQUIRE) == 2) return;
    int expected = 0;
    if (__atomic_compare_exchange_n(&load_state, &expected, 1, 0, __ATOMIC_ACQ_REL, __ATOMIC_ACQUIRE)) {
        load();
        __atomic_store_n(&load_state, 2, __ATOMIC_RELEASE);
    } else {
        while (__atomic_load_n(&load_state, __ATOMIC_ACQUIRE) != 2) {
        }
    }
}

static VKAPI_ATTR VkResult VKAPI_CALL no_create_instance(const VkInstanceCreateInfo *a, const VkAllocationCallbacks *b, VkInstance *c) {
    (void)a; (void)b; (void)c;
    return VK_ERROR_INITIALIZATION_FAILED;
}
static VKAPI_ATTR VkResult VKAPI_CALL no_enum_version(uint32_t *v) {
    *v = VK_API_VERSION_1_0;
    return VK_SUCCESS;
}
static VKAPI_ATTR VkResult VKAPI_CALL no_enum_ext(const char *l, uint32_t *n, VkExtensionProperties *p) {
    (void)l; (void)p;
    *n = 0;
    return VK_SUCCESS;
}
static VKAPI_ATTR VkResult VKAPI_CALL no_enum_layers(uint32_t *n, VkLayerProperties *p) {
    (void)p;
    *n = 0;
    return VK_SUCCESS;
}

VKAPI_ATTR PFN_vkVoidFunction VKAPI_CALL vkGetInstanceProcAddr(VkInstance inst, const char *name) {
    ensure_loaded();
    if (real_gipa) return real_gipa(inst, name);
    // No Vulkan: global functions that fail cleanly
    if (!strcmp(name, "vkCreateInstance")) return (PFN_vkVoidFunction)no_create_instance;
    if (!strcmp(name, "vkEnumerateInstanceVersion")) return (PFN_vkVoidFunction)no_enum_version;
    if (!strcmp(name, "vkEnumerateInstanceExtensionProperties")) return (PFN_vkVoidFunction)no_enum_ext;
    if (!strcmp(name, "vkEnumerateInstanceLayerProperties")) return (PFN_vkVoidFunction)no_enum_layers;
    return NULL;
}

// The others are only used with an already created instance (i.e. with libvulkan loaded)
static void *sym(const char *name) {
    ensure_loaded();
    return lib ? dl_sym(lib, name) : NULL;
}

VKAPI_ATTR PFN_vkVoidFunction VKAPI_CALL vkGetDeviceProcAddr(VkDevice dev, const char *name) {
    static PFN_vkGetDeviceProcAddr f;
    if (!f) f = (PFN_vkGetDeviceProcAddr)sym("vkGetDeviceProcAddr");
    return f ? f(dev, name) : NULL;
}

VKAPI_ATTR void VKAPI_CALL vkCmdCopyBuffer(VkCommandBuffer cb, VkBuffer src, VkBuffer dst, uint32_t n, const VkBufferCopy *r) {
    static PFN_vkCmdCopyBuffer f;
    if (!f) f = (PFN_vkCmdCopyBuffer)sym("vkCmdCopyBuffer");
    if (f) f(cb, src, dst, n, r);
}

VKAPI_ATTR void VKAPI_CALL vkGetPhysicalDeviceFeatures2(VkPhysicalDevice pd, VkPhysicalDeviceFeatures2 *feat) {
    static PFN_vkGetPhysicalDeviceFeatures2 f;
    if (!f) f = (PFN_vkGetPhysicalDeviceFeatures2)sym("vkGetPhysicalDeviceFeatures2");
    if (f) f(pd, feat);
}
#endif // !__APPLE__
