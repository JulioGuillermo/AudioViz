#define VK_NO_PROTOTYPES
#include "vk.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <vulkan/vulkan.h>

#define MAX_SLOTS 8

// AUDIOVIZ_VK_DEBUG=1 prints each step of the Vulkan setup to stderr (to see where a driver fails)
static int vk_debug = -1;
static void vk_step(const char *what) {
    if (vk_debug < 0) {
        const char *e = getenv("AUDIOVIZ_VK_DEBUG");
        vk_debug = e && *e && *e != '0';
    }
    if (vk_debug) fprintf(stderr, "[vulkan] %s\n", what);
}

// Loading the Vulkan loader at runtime (the binary must start without it and fall back to CPU):
//   Linux    libvulkan.so.1 (part of the GPU driver)
//   Windows  vulkan-1.dll (installed with the NVIDIA / AMD / Intel driver)
//   macOS    MoltenVK (Vulkan on top of Metal): linked statically when built with
//            AUDIOVIZ_MOLTENVK_STATIC, otherwise a Vulkan SDK / MoltenVK dylib if present
#if defined(_WIN32)
#include <windows.h>
static void *dl_open(const char *n) { return (void *)LoadLibraryA(n); }
static void *dl_sym(void *h, const char *s) { return (void *)GetProcAddress((HMODULE)h, s); }
static const char *vk_lib_names[] = {"vulkan-1.dll", NULL};
#else
#include <dlfcn.h>
static void *dl_open(const char *n) { return dlopen(n, RTLD_NOW | RTLD_LOCAL); }
static void *dl_sym(void *h, const char *s) { return dlsym(h, s); }
#if defined(__APPLE__)
static const char *vk_lib_names[] = {"libvulkan.1.dylib", "libvulkan.dylib", "libMoltenVK.dylib", NULL};
#else
static const char *vk_lib_names[] = {"libvulkan.so.1", "libvulkan.so", NULL};
#endif
#endif
#if defined(AUDIOVIZ_MOLTENVK_STATIC)
VKAPI_ATTR PFN_vkVoidFunction VKAPI_CALL vkGetInstanceProcAddr(VkInstance instance, const char *pName);
#endif

#define VK_GLOBAL_FUNCS(X) \
    X(vkCreateInstance)    \
    X(vkEnumerateInstanceVersion)

#define VK_INSTANCE_FUNCS(X)                     \
    X(vkDestroyInstance)                         \
    X(vkEnumeratePhysicalDevices)                \
    X(vkGetPhysicalDeviceProperties)             \
    X(vkGetPhysicalDeviceQueueFamilyProperties)  \
    X(vkGetPhysicalDeviceMemoryProperties)       \
    X(vkCreateDevice)                            \
    X(vkEnumerateDeviceExtensionProperties)      \
    X(vkGetDeviceProcAddr)

#define VK_DEVICE_FUNCS(X)              \
    X(vkDestroyDevice)                  \
    X(vkGetDeviceQueue)                 \
    X(vkCreateBuffer)                   \
    X(vkDestroyBuffer)                  \
    X(vkGetBufferMemoryRequirements)    \
    X(vkAllocateMemory)                 \
    X(vkFreeMemory)                     \
    X(vkBindBufferMemory)               \
    X(vkMapMemory)                      \
    X(vkUnmapMemory)                    \
    X(vkInvalidateMappedMemoryRanges)   \
    X(vkCreateShaderModule)             \
    X(vkDestroyShaderModule)            \
    X(vkCreateDescriptorSetLayout)      \
    X(vkDestroyDescriptorSetLayout)     \
    X(vkCreatePipelineLayout)           \
    X(vkDestroyPipelineLayout)          \
    X(vkCreateComputePipelines)         \
    X(vkDestroyPipeline)                \
    X(vkCreateDescriptorPool)           \
    X(vkDestroyDescriptorPool)          \
    X(vkAllocateDescriptorSets)         \
    X(vkUpdateDescriptorSets)           \
    X(vkCreateCommandPool)              \
    X(vkDestroyCommandPool)             \
    X(vkAllocateCommandBuffers)         \
    X(vkFreeCommandBuffers)             \
    X(vkBeginCommandBuffer)             \
    X(vkEndCommandBuffer)               \
    X(vkCmdBindPipeline)                \
    X(vkCmdBindDescriptorSets)          \
    X(vkCmdDispatch)                    \
    X(vkCmdPushConstants)               \
    X(vkCmdPipelineBarrier)             \
    X(vkCreateFence)                    \
    X(vkDestroyFence)                   \
    X(vkResetFences)                    \
    X(vkWaitForFences)                  \
    X(vkQueueSubmit)                    \
    X(vkDeviceWaitIdle)

#define DECL(name) static PFN_##name p_##name;
static PFN_vkGetInstanceProcAddr p_vkGetInstanceProcAddr;
VK_GLOBAL_FUNCS(DECL)
VK_INSTANCE_FUNCS(DECL)
VK_DEVICE_FUNCS(DECL)

static void *g_lib;

typedef struct {
    VkBuffer buf;
    VkDeviceMemory mem;
    void *map;
    VkDeviceSize size;
    int coherent;
} vbuf;

typedef struct {
    vbuf b[VKR_NUM_BUFS];
    VkDescriptorSet ds;
    VkCommandBuffer cmd, cmdFluid; // main pass; fluid pass + main pass
    VkFence fence;
    int pending;
} slot_t;

struct vkr {
    VkInstance inst;
    VkPhysicalDevice phys;
    VkDevice dev;
    VkQueue queue;
    uint32_t qfam;
    VkPhysicalDeviceMemoryProperties memprops;
    VkDescriptorSetLayout dsl;
    VkPipelineLayout layout;
    VkPipeline pipe, pipeFluid;
    VkDescriptorPool dpool;
    VkCommandPool cpool;
    int nslots;
    slot_t slots[MAX_SLOTS];
    char name[256];
};

static int load_lib(char *err, int errlen) {
    if (p_vkGetInstanceProcAddr) return 0;
#if defined(__APPLE__)
    // MoltenVK logs every step at "info" level: keep only its errors (unless set by the user)
    setenv("MVK_CONFIG_LOG_LEVEL", "1", 0);
#endif
#if defined(AUDIOVIZ_MOLTENVK_STATIC)
    p_vkGetInstanceProcAddr = vkGetInstanceProcAddr; // MoltenVK linked into the binary
#else
    for (int i = 0; vk_lib_names[i] && !g_lib; i++) g_lib = dl_open(vk_lib_names[i]);
    if (!g_lib) {
        snprintf(err, errlen, "libvulkan not found (Vulkan driver not installed)");
        return -1;
    }
    p_vkGetInstanceProcAddr = (PFN_vkGetInstanceProcAddr)dl_sym(g_lib, "vkGetInstanceProcAddr");
#endif
    if (!p_vkGetInstanceProcAddr) {
        snprintf(err, errlen, "libvulkan has no vkGetInstanceProcAddr");
        return -1;
    }
#define LOADG(name) p_##name = (PFN_##name)p_vkGetInstanceProcAddr(NULL, #name);
    VK_GLOBAL_FUNCS(LOADG)
    if (!p_vkCreateInstance) {
        snprintf(err, errlen, "invalid libvulkan");
        return -1;
    }
    return 0;
}

static int create_instance(VkInstance *inst, char *err, int errlen) {
    VkApplicationInfo app = {VK_STRUCTURE_TYPE_APPLICATION_INFO};
    app.pApplicationName = "audioviz";
    app.apiVersion = VK_API_VERSION_1_1;
    VkInstanceCreateInfo ci = {VK_STRUCTURE_TYPE_INSTANCE_CREATE_INFO};
    ci.pApplicationInfo = &app;
    const char *exts[] = {"VK_KHR_portability_enumeration"};
    VkResult res = p_vkCreateInstance(&ci, NULL, inst);
    if (res != VK_SUCCESS) {
        // MoltenVK requires portability
        ci.flags = 0x00000001; // VK_INSTANCE_CREATE_ENUMERATE_PORTABILITY_BIT_KHR
        ci.enabledExtensionCount = 1;
        ci.ppEnabledExtensionNames = exts;
        res = p_vkCreateInstance(&ci, NULL, inst);
    }
    if (res != VK_SUCCESS) {
        snprintf(err, errlen, "vkCreateInstance failed (%d)", res);
        return -1;
    }
#define LOADI(name) p_##name = (PFN_##name)p_vkGetInstanceProcAddr(*inst, #name);
    VK_INSTANCE_FUNCS(LOADI)
    return 0;
}

int vkr_list_devices(char *out, int len) {
    char err[256];
    if (load_lib(err, sizeof err) != 0) {
        snprintf(out, len, "%s", err);
        return -1;
    }
    VkInstance inst;
    if (create_instance(&inst, err, sizeof err) != 0) {
        snprintf(out, len, "%s", err);
        return -1;
    }
    uint32_t n = 0;
    p_vkEnumeratePhysicalDevices(inst, &n, NULL);
    VkPhysicalDevice devs[16];
    if (n > 16) n = 16;
    p_vkEnumeratePhysicalDevices(inst, &n, devs);
    out[0] = 0;
    for (uint32_t i = 0; i < n; i++) {
        VkPhysicalDeviceProperties p;
        p_vkGetPhysicalDeviceProperties(devs[i], &p);
        const char *t = p.deviceType == VK_PHYSICAL_DEVICE_TYPE_DISCRETE_GPU     ? "discrete"
                        : p.deviceType == VK_PHYSICAL_DEVICE_TYPE_INTEGRATED_GPU ? "integrated"
                        : p.deviceType == VK_PHYSICAL_DEVICE_TYPE_CPU            ? "cpu"
                                                                                 : "other";
        size_t l = strlen(out);
        snprintf(out + l, len - l, "  [%u] %s (%s)\n", i, p.deviceName, t);
    }
    p_vkDestroyInstance(inst, NULL);
    return (int)n;
}

static int find_mem(vkr *r, uint32_t bits, VkMemoryPropertyFlags req, VkMemoryPropertyFlags pref) {
    for (int pass = 0; pass < 2; pass++) {
        for (uint32_t i = 0; i < r->memprops.memoryTypeCount; i++) {
            VkMemoryPropertyFlags f = r->memprops.memoryTypes[i].propertyFlags;
            if (!(bits & (1u << i))) continue;
            if ((f & req) != req) continue;
            if (pass == 0 && (f & pref) != pref) continue;
            return (int)i;
        }
    }
    return -1;
}

static int make_buf(vkr *r, vbuf *b, VkDeviceSize size, VkMemoryPropertyFlags pref) {
    VkBufferCreateInfo bi = {VK_STRUCTURE_TYPE_BUFFER_CREATE_INFO};
    bi.size = size;
    bi.usage = VK_BUFFER_USAGE_STORAGE_BUFFER_BIT;
    bi.sharingMode = VK_SHARING_MODE_EXCLUSIVE;
    if (p_vkCreateBuffer(r->dev, &bi, NULL, &b->buf) != VK_SUCCESS) return -1;
    VkMemoryRequirements mr;
    p_vkGetBufferMemoryRequirements(r->dev, b->buf, &mr);
    VkMemoryPropertyFlags req = VK_MEMORY_PROPERTY_HOST_VISIBLE_BIT;
    int t = find_mem(r, mr.memoryTypeBits, req, pref);
    if (t < 0) return -1;
    b->coherent = (r->memprops.memoryTypes[t].propertyFlags & VK_MEMORY_PROPERTY_HOST_COHERENT_BIT) != 0;
    VkMemoryAllocateInfo ai = {VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_INFO};
    ai.allocationSize = mr.size;
    ai.memoryTypeIndex = (uint32_t)t;
    if (p_vkAllocateMemory(r->dev, &ai, NULL, &b->mem) != VK_SUCCESS) return -1;
    if (p_vkBindBufferMemory(r->dev, b->buf, b->mem, 0) != VK_SUCCESS) return -1;
    if (p_vkMapMemory(r->dev, b->mem, 0, VK_WHOLE_SIZE, 0, &b->map) != VK_SUCCESS) return -1;
    b->size = size;
    memset(b->map, 0, size);
    return 0;
}

#define FAIL(...)                          \
    do {                                   \
        snprintf(err, errlen, __VA_ARGS__); \
        vkr_destroy(r);                    \
        return -1;                         \
    } while (0)

int vkr_create(vkr **out, const vkr_config *cfg, char *err, int errlen) {
    *out = NULL;
    vk_step("loading the Vulkan loader");
    if (load_lib(err, errlen) != 0) return -1;
    vk_step("creating the instance");
    vkr *r = calloc(1, sizeof(vkr));
    r->nslots = cfg->slots > MAX_SLOTS ? MAX_SLOTS : cfg->slots;
    if (create_instance(&r->inst, err, errlen) != 0) {
        free(r);
        return -1;
    }

    uint32_t n = 0;
    vk_step("enumerating devices");
    p_vkEnumeratePhysicalDevices(r->inst, &n, NULL);
    if (n == 0) FAIL("no Vulkan devices");
    VkPhysicalDevice devs[16];
    if (n > 16) n = 16;
    p_vkEnumeratePhysicalDevices(r->inst, &n, devs);

    // Pick device: the requested one, or the best (discrete > integrated > other, never CPU if a GPU exists)
    int best = -1, bestScore = -1;
    for (uint32_t i = 0; i < n; i++) {
        VkPhysicalDeviceProperties p;
        p_vkGetPhysicalDeviceProperties(devs[i], &p);
        int score = p.deviceType == VK_PHYSICAL_DEVICE_TYPE_DISCRETE_GPU     ? 4
                    : p.deviceType == VK_PHYSICAL_DEVICE_TYPE_INTEGRATED_GPU ? 3
                    : p.deviceType == VK_PHYSICAL_DEVICE_TYPE_VIRTUAL_GPU    ? 2
                    : p.deviceType == VK_PHYSICAL_DEVICE_TYPE_CPU            ? 0
                                                                             : 1;
        if (cfg->device_index >= 0) score = ((int)i == cfg->device_index) ? 100 : -1;
        if (score > bestScore) {
            bestScore = score;
            best = (int)i;
        }
    }
    if (best < 0) FAIL("Vulkan device %d does not exist", cfg->device_index);
    r->phys = devs[best];
    VkPhysicalDeviceProperties props;
    p_vkGetPhysicalDeviceProperties(r->phys, &props);
    snprintf(r->name, sizeof r->name, "%s", props.deviceName);
    p_vkGetPhysicalDeviceMemoryProperties(r->phys, &r->memprops);

    uint32_t nq = 0;
    p_vkGetPhysicalDeviceQueueFamilyProperties(r->phys, &nq, NULL);
    VkQueueFamilyProperties qf[32];
    if (nq > 32) nq = 32;
    p_vkGetPhysicalDeviceQueueFamilyProperties(r->phys, &nq, qf);
    int fam = -1;
    for (uint32_t i = 0; i < nq; i++) {
        if (qf[i].queueFlags & VK_QUEUE_COMPUTE_BIT) {
            fam = (int)i;
            break;
        }
    }
    if (fam < 0) FAIL("no compute queue");
    r->qfam = (uint32_t)fam;

    float prio = 1.0f;
    VkDeviceQueueCreateInfo qci = {VK_STRUCTURE_TYPE_DEVICE_QUEUE_CREATE_INFO};
    qci.queueFamilyIndex = r->qfam;
    qci.queueCount = 1;
    qci.pQueuePriorities = &prio;
    VkDeviceCreateInfo dci = {VK_STRUCTURE_TYPE_DEVICE_CREATE_INFO};
    dci.queueCreateInfoCount = 1;
    dci.pQueueCreateInfos = &qci;
    // Portability implementations (MoltenVK on macOS) require enabling this extension
    const char *dexts[1];
    uint32_t nde = 0;
    if (p_vkEnumerateDeviceExtensionProperties) {
        p_vkEnumerateDeviceExtensionProperties(r->phys, NULL, &nde, NULL);
        VkExtensionProperties *props = calloc(nde ? nde : 1, sizeof *props);
        p_vkEnumerateDeviceExtensionProperties(r->phys, NULL, &nde, props);
        for (uint32_t i = 0; i < nde; i++)
            if (!strcmp(props[i].extensionName, "VK_KHR_portability_subset")) {
                dexts[0] = "VK_KHR_portability_subset";
                dci.enabledExtensionCount = 1;
                dci.ppEnabledExtensionNames = dexts;
            }
        free(props);
    }
    vk_step("creating the device");
    if (p_vkCreateDevice(r->phys, &dci, NULL, &r->dev) != VK_SUCCESS) FAIL("vkCreateDevice failed");
#define LOADD(name)                                                    \
    p_##name = (PFN_##name)p_vkGetDeviceProcAddr(r->dev, #name);       \
    if (!p_##name) p_##name = (PFN_##name)p_vkGetInstanceProcAddr(r->inst, #name);
    VK_DEVICE_FUNCS(LOADD)
    p_vkGetDeviceQueue(r->dev, r->qfam, 0, &r->queue);

    if (!cfg->spirv) {
        // Device only, for compute jobs (vkj_*)
        VkCommandPoolCreateInfo cpci = {VK_STRUCTURE_TYPE_COMMAND_POOL_CREATE_INFO};
        cpci.flags = VK_COMMAND_POOL_CREATE_RESET_COMMAND_BUFFER_BIT;
        cpci.queueFamilyIndex = r->qfam;
        if (p_vkCreateCommandPool(r->dev, &cpci, NULL, &r->cpool) != VK_SUCCESS) FAIL("command pool");
        *out = r;
        return 0;
    }

    // Compute pipeline
    vk_step("creating the shaders and pipelines");

    VkDescriptorSetLayoutBinding binds[VKR_NUM_BUFS];
    for (int i = 0; i < VKR_NUM_BUFS; i++) {
        binds[i] = (VkDescriptorSetLayoutBinding){(uint32_t)i, VK_DESCRIPTOR_TYPE_STORAGE_BUFFER, 1,
                                                  VK_SHADER_STAGE_COMPUTE_BIT, NULL};
    }
    VkDescriptorSetLayoutCreateInfo dli = {VK_STRUCTURE_TYPE_DESCRIPTOR_SET_LAYOUT_CREATE_INFO};
    dli.bindingCount = VKR_NUM_BUFS;
    dli.pBindings = binds;
    if (p_vkCreateDescriptorSetLayout(r->dev, &dli, NULL, &r->dsl) != VK_SUCCESS) FAIL("descriptor layout");
    VkPipelineLayoutCreateInfo pli = {VK_STRUCTURE_TYPE_PIPELINE_LAYOUT_CREATE_INFO};
    pli.setLayoutCount = 1;
    pli.pSetLayouts = &r->dsl;
    if (p_vkCreatePipelineLayout(r->dev, &pli, NULL, &r->layout) != VK_SUCCESS) FAIL("pipeline layout");
    // Main pass and fluid pass: two pipelines on the same descriptor layout
    for (int k = 0; k < 2; k++) {
        VkShaderModuleCreateInfo smi = {VK_STRUCTURE_TYPE_SHADER_MODULE_CREATE_INFO};
        smi.codeSize = k == 0 ? cfg->spirv_size : cfg->spirv_fluid_size;
        smi.pCode = k == 0 ? cfg->spirv : cfg->spirv_fluid;
        VkShaderModule sm;
        if (p_vkCreateShaderModule(r->dev, &smi, NULL, &sm) != VK_SUCCESS) FAIL("invalid shader");
        VkComputePipelineCreateInfo cpi = {VK_STRUCTURE_TYPE_COMPUTE_PIPELINE_CREATE_INFO};
        cpi.stage.sType = VK_STRUCTURE_TYPE_PIPELINE_SHADER_STAGE_CREATE_INFO;
        cpi.stage.stage = VK_SHADER_STAGE_COMPUTE_BIT;
        cpi.stage.module = sm;
        cpi.stage.pName = "main";
        cpi.layout = r->layout;
        VkResult pres =
            p_vkCreateComputePipelines(r->dev, VK_NULL_HANDLE, 1, &cpi, NULL, k == 0 ? &r->pipe : &r->pipeFluid);
        p_vkDestroyShaderModule(r->dev, sm, NULL);
        if (pres != VK_SUCCESS) FAIL("vkCreateComputePipelines failed (%d)", pres);
    }

    VkDescriptorPoolSize ps = {VK_DESCRIPTOR_TYPE_STORAGE_BUFFER, (uint32_t)(VKR_NUM_BUFS * r->nslots)};
    VkDescriptorPoolCreateInfo dpi = {VK_STRUCTURE_TYPE_DESCRIPTOR_POOL_CREATE_INFO};
    dpi.maxSets = (uint32_t)r->nslots;
    dpi.poolSizeCount = 1;
    dpi.pPoolSizes = &ps;
    if (p_vkCreateDescriptorPool(r->dev, &dpi, NULL, &r->dpool) != VK_SUCCESS) FAIL("descriptor pool");
    VkCommandPoolCreateInfo cpci = {VK_STRUCTURE_TYPE_COMMAND_POOL_CREATE_INFO};
    cpci.queueFamilyIndex = r->qfam;
    if (p_vkCreateCommandPool(r->dev, &cpci, NULL, &r->cpool) != VK_SUCCESS) FAIL("command pool");
    vk_step("allocating buffers");

    VkDeviceSize sizes[VKR_NUM_BUFS];
    sizes[VKR_BUF_PARAMS] = cfg->params_size;
    sizes[VKR_BUF_BG] = (VkDeviceSize)cfg->width * cfg->height * 4;
    sizes[VKR_BUF_Y] = (VkDeviceSize)cfg->pad_w * cfg->pad_h;
    sizes[VKR_BUF_UV] = (VkDeviceSize)cfg->pad_w * cfg->pad_h / 2;
    sizes[VKR_BUF_RGBA] = (VkDeviceSize)cfg->width * cfg->height * 4;
    sizes[VKR_BUF_TEXT] = cfg->text_size < 16 ? 16 : (cfg->text_size + 3) / 4 * 4;
    sizes[VKR_BUF_BG2] = sizes[VKR_BUF_BG];
    sizes[VKR_BUF_FLUID] = cfg->fluid_size < 16 ? 16 : (cfg->fluid_size + 3) / 4 * 4;
    sizes[VKR_BUF_FIMG] = sizes[VKR_BUF_BG];
    VkMemoryPropertyFlags prefs[VKR_NUM_BUFS] = {
        VK_MEMORY_PROPERTY_DEVICE_LOCAL_BIT | VK_MEMORY_PROPERTY_HOST_COHERENT_BIT,
        VK_MEMORY_PROPERTY_DEVICE_LOCAL_BIT | VK_MEMORY_PROPERTY_HOST_COHERENT_BIT,
        VK_MEMORY_PROPERTY_HOST_CACHED_BIT | VK_MEMORY_PROPERTY_HOST_COHERENT_BIT,
        VK_MEMORY_PROPERTY_HOST_CACHED_BIT | VK_MEMORY_PROPERTY_HOST_COHERENT_BIT,
        VK_MEMORY_PROPERTY_HOST_CACHED_BIT | VK_MEMORY_PROPERTY_HOST_COHERENT_BIT,
        VK_MEMORY_PROPERTY_DEVICE_LOCAL_BIT | VK_MEMORY_PROPERTY_HOST_COHERENT_BIT,
        VK_MEMORY_PROPERTY_DEVICE_LOCAL_BIT | VK_MEMORY_PROPERTY_HOST_COHERENT_BIT,
        VK_MEMORY_PROPERTY_DEVICE_LOCAL_BIT | VK_MEMORY_PROPERTY_HOST_COHERENT_BIT,
        VK_MEMORY_PROPERTY_DEVICE_LOCAL_BIT,
    };

    uint32_t gx = (uint32_t)((cfg->pad_w / 4 + 7) / 8);
    uint32_t gy = (uint32_t)((cfg->pad_h / 2 + 7) / 8);

    for (int s = 0; s < r->nslots; s++) {
        slot_t *sl = &r->slots[s];
        for (int i = 0; i < VKR_NUM_BUFS; i++) {
            if (make_buf(r, &sl->b[i], sizes[i], prefs[i]) != 0) FAIL("out of GPU memory for buffers");
        }
        VkDescriptorSetAllocateInfo dai = {VK_STRUCTURE_TYPE_DESCRIPTOR_SET_ALLOCATE_INFO};
        dai.descriptorPool = r->dpool;
        dai.descriptorSetCount = 1;
        dai.pSetLayouts = &r->dsl;
        if (p_vkAllocateDescriptorSets(r->dev, &dai, &sl->ds) != VK_SUCCESS) FAIL("descriptor set");
        VkDescriptorBufferInfo dbi[VKR_NUM_BUFS];
        VkWriteDescriptorSet wr[VKR_NUM_BUFS];
        for (int i = 0; i < VKR_NUM_BUFS; i++) {
            dbi[i] = (VkDescriptorBufferInfo){sl->b[i].buf, 0, VK_WHOLE_SIZE};
            wr[i] = (VkWriteDescriptorSet){VK_STRUCTURE_TYPE_WRITE_DESCRIPTOR_SET};
            wr[i].dstSet = sl->ds;
            wr[i].dstBinding = (uint32_t)i;
            wr[i].descriptorCount = 1;
            wr[i].descriptorType = VK_DESCRIPTOR_TYPE_STORAGE_BUFFER;
            wr[i].pBufferInfo = &dbi[i];
        }
        p_vkUpdateDescriptorSets(r->dev, VKR_NUM_BUFS, wr, 0, NULL);

        // Pre-recorded command buffers (each slot's buffers are fixed): the main pass alone, and
        // the fluid pass (one invocation per pixel) followed by the main pass
        VkCommandBufferAllocateInfo cai = {VK_STRUCTURE_TYPE_COMMAND_BUFFER_ALLOCATE_INFO};
        cai.commandPool = r->cpool;
        cai.level = VK_COMMAND_BUFFER_LEVEL_PRIMARY;
        cai.commandBufferCount = 1;
        for (int k = 0; k < 2; k++) {
            VkCommandBuffer cmd;
            if (p_vkAllocateCommandBuffers(r->dev, &cai, &cmd) != VK_SUCCESS) FAIL("command buffer");
            VkCommandBufferBeginInfo cbi = {VK_STRUCTURE_TYPE_COMMAND_BUFFER_BEGIN_INFO};
            p_vkBeginCommandBuffer(cmd, &cbi);
            p_vkCmdBindDescriptorSets(cmd, VK_PIPELINE_BIND_POINT_COMPUTE, r->layout, 0, 1, &sl->ds, 0, NULL);
            if (k == 1) {
                p_vkCmdBindPipeline(cmd, VK_PIPELINE_BIND_POINT_COMPUTE, r->pipeFluid);
                p_vkCmdDispatch(cmd, (uint32_t)((cfg->width + 7) / 8), (uint32_t)((cfg->height + 7) / 8), 1);
                VkMemoryBarrier fb = {VK_STRUCTURE_TYPE_MEMORY_BARRIER};
                fb.srcAccessMask = VK_ACCESS_SHADER_WRITE_BIT;
                fb.dstAccessMask = VK_ACCESS_SHADER_READ_BIT;
                p_vkCmdPipelineBarrier(cmd, VK_PIPELINE_STAGE_COMPUTE_SHADER_BIT, VK_PIPELINE_STAGE_COMPUTE_SHADER_BIT,
                                       0, 1, &fb, 0, NULL, 0, NULL);
            }
            p_vkCmdBindPipeline(cmd, VK_PIPELINE_BIND_POINT_COMPUTE, r->pipe);
            p_vkCmdDispatch(cmd, gx, gy, 1);
            VkMemoryBarrier mb = {VK_STRUCTURE_TYPE_MEMORY_BARRIER};
            mb.srcAccessMask = VK_ACCESS_SHADER_WRITE_BIT;
            mb.dstAccessMask = VK_ACCESS_HOST_READ_BIT;
            p_vkCmdPipelineBarrier(cmd, VK_PIPELINE_STAGE_COMPUTE_SHADER_BIT, VK_PIPELINE_STAGE_HOST_BIT, 0, 1, &mb,
                                   0, NULL, 0, NULL);
            if (p_vkEndCommandBuffer(cmd) != VK_SUCCESS) FAIL("command recording");
            if (k == 0)
                sl->cmd = cmd;
            else
                sl->cmdFluid = cmd;
        }

        VkFenceCreateInfo fci = {VK_STRUCTURE_TYPE_FENCE_CREATE_INFO};
        if (p_vkCreateFence(r->dev, &fci, NULL, &sl->fence) != VK_SUCCESS) FAIL("fence");
    }

    *out = r;
    return 0;
}

const char *vkr_device_name(vkr *r) { return r->name; }

void *vkr_map(vkr *r, int slot, int which) { return r->slots[slot].b[which].map; }

int vkr_submit(vkr *r, int slot, int fluid) {
    slot_t *sl = &r->slots[slot];
    VkSubmitInfo si = {VK_STRUCTURE_TYPE_SUBMIT_INFO};
    si.commandBufferCount = 1;
    si.pCommandBuffers = fluid ? &sl->cmdFluid : &sl->cmd;
    if (p_vkQueueSubmit(r->queue, 1, &si, sl->fence) != VK_SUCCESS) return -1;
    sl->pending = 1;
    return 0;
}

int vkr_wait(vkr *r, int slot) {
    slot_t *sl = &r->slots[slot];
    if (!sl->pending) return 0;
    VkResult res = p_vkWaitForFences(r->dev, 1, &sl->fence, VK_TRUE, UINT64_MAX);
    if (res != VK_SUCCESS) return -1;
    p_vkResetFences(r->dev, 1, &sl->fence);
    sl->pending = 0;
    for (int i = VKR_BUF_Y; i <= VKR_BUF_RGBA; i++) {
        if (!sl->b[i].coherent) {
            VkMappedMemoryRange mr = {VK_STRUCTURE_TYPE_MAPPED_MEMORY_RANGE};
            mr.memory = sl->b[i].mem;
            mr.size = VK_WHOLE_SIZE;
            p_vkInvalidateMappedMemoryRanges(r->dev, 1, &mr);
        }
    }
    return 0;
}

void vkr_destroy(vkr *r) {
    if (!r) return;
    if (r->dev) {
        p_vkDeviceWaitIdle(r->dev);
        for (int s = 0; s < r->nslots; s++) {
            slot_t *sl = &r->slots[s];
            for (int i = 0; i < VKR_NUM_BUFS; i++) {
                if (sl->b[i].buf) p_vkDestroyBuffer(r->dev, sl->b[i].buf, NULL);
                if (sl->b[i].mem) p_vkFreeMemory(r->dev, sl->b[i].mem, NULL);
            }
            if (sl->fence) p_vkDestroyFence(r->dev, sl->fence, NULL);
        }
        if (r->cpool) p_vkDestroyCommandPool(r->dev, r->cpool, NULL);
        if (r->dpool) p_vkDestroyDescriptorPool(r->dev, r->dpool, NULL);
        if (r->pipe) p_vkDestroyPipeline(r->dev, r->pipe, NULL);
        if (r->pipeFluid) p_vkDestroyPipeline(r->dev, r->pipeFluid, NULL);
        if (r->layout) p_vkDestroyPipelineLayout(r->dev, r->layout, NULL);
        if (r->dsl) p_vkDestroyDescriptorSetLayout(r->dev, r->dsl, NULL);
        p_vkDestroyDevice(r->dev, NULL);
    }
    if (r->inst) p_vkDestroyInstance(r->inst, NULL);
    free(r);
}

// ---------------------------------------------------------------- compute jobs
// A job: one compute shader (its kernels picked by a push constant), its storage buffers
// (bindings 0..n-1, host-visible) and a list of dispatches, each followed by a barrier, run in
// submissions short enough for drivers that reset long jobs.

struct vkj {
    vkr *r;
    vbuf b[VKJ_MAX_BUFS];
    int nb;
    VkDescriptorSetLayout dsl;
    VkPipelineLayout layout;
    VkPipeline pipe;
    VkDescriptorPool dpool;
    VkDescriptorSet ds;
    VkCommandBuffer cmd;
    VkFence fence;
    int recording;
    uint32_t push_size;
};

#undef FAIL
#define FAIL(...)                           \
    do {                                    \
        snprintf(err, errlen, __VA_ARGS__); \
        vkj_destroy(j);                     \
        return -1;                          \
    } while (0)

int vkj_create(vkj **out, vkr *r, const uint32_t *spirv, size_t spirv_size, const size_t *sizes, int nbufs,
               int push_size, char *err, int errlen) {
    *out = NULL;
    if (nbufs > VKJ_MAX_BUFS) {
        snprintf(err, errlen, "too many buffers");
        return -1;
    }
    vkj *j = calloc(1, sizeof(vkj));
    j->r = r;
    j->push_size = (uint32_t)push_size;
    for (int i = 0; i < nbufs; i++) {
        VkDeviceSize sz = sizes[i] < 16 ? 16 : (sizes[i] + 3) / 4 * 4;
        if (make_buf(r, &j->b[i], sz, VK_MEMORY_PROPERTY_DEVICE_LOCAL_BIT | VK_MEMORY_PROPERTY_HOST_COHERENT_BIT) != 0)
            FAIL("out of GPU memory for buffers");
        j->nb = i + 1;
    }
    VkDescriptorSetLayoutBinding binds[VKJ_MAX_BUFS];
    for (int i = 0; i < nbufs; i++)
        binds[i] = (VkDescriptorSetLayoutBinding){(uint32_t)i, VK_DESCRIPTOR_TYPE_STORAGE_BUFFER, 1,
                                                  VK_SHADER_STAGE_COMPUTE_BIT, NULL};
    VkDescriptorSetLayoutCreateInfo dli = {VK_STRUCTURE_TYPE_DESCRIPTOR_SET_LAYOUT_CREATE_INFO};
    dli.bindingCount = (uint32_t)nbufs;
    dli.pBindings = binds;
    if (p_vkCreateDescriptorSetLayout(r->dev, &dli, NULL, &j->dsl) != VK_SUCCESS) FAIL("descriptor layout");
    VkPushConstantRange pcr = {VK_SHADER_STAGE_COMPUTE_BIT, 0, j->push_size};
    VkPipelineLayoutCreateInfo pli = {VK_STRUCTURE_TYPE_PIPELINE_LAYOUT_CREATE_INFO};
    pli.setLayoutCount = 1;
    pli.pSetLayouts = &j->dsl;
    pli.pushConstantRangeCount = push_size > 0 ? 1 : 0;
    pli.pPushConstantRanges = &pcr;
    if (p_vkCreatePipelineLayout(r->dev, &pli, NULL, &j->layout) != VK_SUCCESS) FAIL("pipeline layout");
    VkShaderModuleCreateInfo smi = {VK_STRUCTURE_TYPE_SHADER_MODULE_CREATE_INFO};
    smi.codeSize = spirv_size;
    smi.pCode = spirv;
    VkShaderModule sm;
    if (p_vkCreateShaderModule(r->dev, &smi, NULL, &sm) != VK_SUCCESS) FAIL("invalid shader");
    VkComputePipelineCreateInfo cpi = {VK_STRUCTURE_TYPE_COMPUTE_PIPELINE_CREATE_INFO};
    cpi.stage.sType = VK_STRUCTURE_TYPE_PIPELINE_SHADER_STAGE_CREATE_INFO;
    cpi.stage.stage = VK_SHADER_STAGE_COMPUTE_BIT;
    cpi.stage.module = sm;
    cpi.stage.pName = "main";
    cpi.layout = j->layout;
    VkResult pres = p_vkCreateComputePipelines(r->dev, VK_NULL_HANDLE, 1, &cpi, NULL, &j->pipe);
    p_vkDestroyShaderModule(r->dev, sm, NULL);
    if (pres != VK_SUCCESS) FAIL("vkCreateComputePipelines failed (%d)", pres);
    VkDescriptorPoolSize ps = {VK_DESCRIPTOR_TYPE_STORAGE_BUFFER, (uint32_t)nbufs};
    VkDescriptorPoolCreateInfo dpi = {VK_STRUCTURE_TYPE_DESCRIPTOR_POOL_CREATE_INFO};
    dpi.maxSets = 1;
    dpi.poolSizeCount = 1;
    dpi.pPoolSizes = &ps;
    if (p_vkCreateDescriptorPool(r->dev, &dpi, NULL, &j->dpool) != VK_SUCCESS) FAIL("descriptor pool");
    VkDescriptorSetAllocateInfo dai = {VK_STRUCTURE_TYPE_DESCRIPTOR_SET_ALLOCATE_INFO};
    dai.descriptorPool = j->dpool;
    dai.descriptorSetCount = 1;
    dai.pSetLayouts = &j->dsl;
    if (p_vkAllocateDescriptorSets(r->dev, &dai, &j->ds) != VK_SUCCESS) FAIL("descriptor set");
    VkDescriptorBufferInfo dbi[VKJ_MAX_BUFS];
    VkWriteDescriptorSet wr[VKJ_MAX_BUFS];
    for (int i = 0; i < nbufs; i++) {
        dbi[i] = (VkDescriptorBufferInfo){j->b[i].buf, 0, VK_WHOLE_SIZE};
        wr[i] = (VkWriteDescriptorSet){VK_STRUCTURE_TYPE_WRITE_DESCRIPTOR_SET};
        wr[i].dstSet = j->ds;
        wr[i].dstBinding = (uint32_t)i;
        wr[i].descriptorCount = 1;
        wr[i].descriptorType = VK_DESCRIPTOR_TYPE_STORAGE_BUFFER;
        wr[i].pBufferInfo = &dbi[i];
    }
    p_vkUpdateDescriptorSets(r->dev, (uint32_t)nbufs, wr, 0, NULL);
    VkCommandBufferAllocateInfo cai = {VK_STRUCTURE_TYPE_COMMAND_BUFFER_ALLOCATE_INFO};
    cai.commandPool = r->cpool;
    cai.level = VK_COMMAND_BUFFER_LEVEL_PRIMARY;
    cai.commandBufferCount = 1;
    if (p_vkAllocateCommandBuffers(r->dev, &cai, &j->cmd) != VK_SUCCESS) FAIL("command buffer");
    VkFenceCreateInfo fci = {VK_STRUCTURE_TYPE_FENCE_CREATE_INFO};
    if (p_vkCreateFence(r->dev, &fci, NULL, &j->fence) != VK_SUCCESS) FAIL("fence");
    *out = j;
    return 0;
}

void *vkj_map(vkj *j, int buf) { return j->b[buf].map; }

int vkj_dispatch(vkj *j, const void *push, uint32_t gx, uint32_t gy, uint32_t gz) {
    if (!j->recording) {
        VkCommandBufferBeginInfo cbi = {VK_STRUCTURE_TYPE_COMMAND_BUFFER_BEGIN_INFO};
        cbi.flags = VK_COMMAND_BUFFER_USAGE_ONE_TIME_SUBMIT_BIT;
        if (p_vkBeginCommandBuffer(j->cmd, &cbi) != VK_SUCCESS) return -1;
        p_vkCmdBindPipeline(j->cmd, VK_PIPELINE_BIND_POINT_COMPUTE, j->pipe);
        p_vkCmdBindDescriptorSets(j->cmd, VK_PIPELINE_BIND_POINT_COMPUTE, j->layout, 0, 1, &j->ds, 0, NULL);
        j->recording = 1;
    }
    if (j->push_size) p_vkCmdPushConstants(j->cmd, j->layout, VK_SHADER_STAGE_COMPUTE_BIT, 0, j->push_size, push);
    p_vkCmdDispatch(j->cmd, gx, gy, gz);
    VkMemoryBarrier mb = {VK_STRUCTURE_TYPE_MEMORY_BARRIER};
    mb.srcAccessMask = VK_ACCESS_SHADER_WRITE_BIT;
    mb.dstAccessMask = VK_ACCESS_SHADER_READ_BIT | VK_ACCESS_SHADER_WRITE_BIT | VK_ACCESS_HOST_READ_BIT;
    p_vkCmdPipelineBarrier(j->cmd, VK_PIPELINE_STAGE_COMPUTE_SHADER_BIT,
                           VK_PIPELINE_STAGE_COMPUTE_SHADER_BIT | VK_PIPELINE_STAGE_HOST_BIT, 0, 1, &mb, 0, NULL, 0,
                           NULL);
    return 0;
}

int vkj_run(vkj *j) {
    if (!j->recording) return 0;
    j->recording = 0;
    if (p_vkEndCommandBuffer(j->cmd) != VK_SUCCESS) return -1;
    VkSubmitInfo si = {VK_STRUCTURE_TYPE_SUBMIT_INFO};
    si.commandBufferCount = 1;
    si.pCommandBuffers = &j->cmd;
    if (p_vkQueueSubmit(j->r->queue, 1, &si, j->fence) != VK_SUCCESS) return -1;
    VkResult res = p_vkWaitForFences(j->r->dev, 1, &j->fence, VK_TRUE, UINT64_MAX);
    p_vkResetFences(j->r->dev, 1, &j->fence);
    if (res != VK_SUCCESS) return -1;
    for (int i = 0; i < j->nb; i++) {
        if (!j->b[i].coherent) {
            VkMappedMemoryRange mr = {VK_STRUCTURE_TYPE_MAPPED_MEMORY_RANGE};
            mr.memory = j->b[i].mem;
            mr.size = VK_WHOLE_SIZE;
            p_vkInvalidateMappedMemoryRanges(j->r->dev, 1, &mr);
        }
    }
    return 0;
}

void vkj_destroy(vkj *j) {
    if (!j) return;
    VkDevice d = j->r->dev;
    p_vkDeviceWaitIdle(d);
    for (int i = 0; i < j->nb; i++) {
        if (j->b[i].buf) p_vkDestroyBuffer(d, j->b[i].buf, NULL);
        if (j->b[i].mem) p_vkFreeMemory(d, j->b[i].mem, NULL);
    }
    if (j->fence) p_vkDestroyFence(d, j->fence, NULL);
    if (j->cmd) p_vkFreeCommandBuffers(d, j->r->cpool, 1, &j->cmd);
    if (j->dpool) p_vkDestroyDescriptorPool(d, j->dpool, NULL);
    if (j->pipe) p_vkDestroyPipeline(d, j->pipe, NULL);
    if (j->layout) p_vkDestroyPipelineLayout(d, j->layout, NULL);
    if (j->dsl) p_vkDestroyDescriptorSetLayout(d, j->dsl, NULL);
    free(j);
}
