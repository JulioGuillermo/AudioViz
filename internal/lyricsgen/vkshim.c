// Puente de Vulkan para ggml (whisper.cpp): el backend Vulkan solo importa estas 4 funciones
// (el resto las obtiene con vkGetInstanceProcAddr). Aquí se resuelven cargando libvulkan en
// tiempo de ejecución; si no existe, vkCreateInstance falla con un error y ggml usa la CPU.
// Así el binario no depende de libvulkan para arrancar.
#define VK_NO_PROTOTYPES
#include <dlfcn.h>
#include <pthread.h>
#include <stddef.h>
#include <stdlib.h>
#include <string.h>
#include <vulkan/vulkan.h>

static void *lib;
static PFN_vkGetInstanceProcAddr real_gipa;
static pthread_once_t once = PTHREAD_ONCE_INIT;

static void load(void) {
    // Para probar el modo sin GPU: AUDIOVIZ_NO_VULKAN=1 simula que no hay libvulkan
    const char *nv = getenv("AUDIOVIZ_NO_VULKAN");
    if (nv && *nv && *nv != '0') return;
    const char *names[] = {"libvulkan.so.1", "libvulkan.so", NULL};
    for (int i = 0; names[i] && !lib; i++) lib = dlopen(names[i], RTLD_NOW | RTLD_LOCAL);
    if (lib) real_gipa = (PFN_vkGetInstanceProcAddr)dlsym(lib, "vkGetInstanceProcAddr");
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
    pthread_once(&once, load);
    if (real_gipa) return real_gipa(inst, name);
    // Sin Vulkan: funciones globales que fallan limpiamente
    if (!strcmp(name, "vkCreateInstance")) return (PFN_vkVoidFunction)no_create_instance;
    if (!strcmp(name, "vkEnumerateInstanceVersion")) return (PFN_vkVoidFunction)no_enum_version;
    if (!strcmp(name, "vkEnumerateInstanceExtensionProperties")) return (PFN_vkVoidFunction)no_enum_ext;
    if (!strcmp(name, "vkEnumerateInstanceLayerProperties")) return (PFN_vkVoidFunction)no_enum_layers;
    return NULL;
}

// Las demás solo se usan con una instancia ya creada (es decir, con libvulkan cargada)
static void *sym(const char *name) {
    pthread_once(&once, load);
    return lib ? dlsym(lib, name) : NULL;
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
