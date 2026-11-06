#ifndef AUDIOVIZ_DMX_H
#define AUDIOVIZ_DMX_H
#ifdef __cplusplus
extern "C" {
#endif
void *dmx_load(const char *path, char *err, int errlen);
void dmx_free(void *model);
// The model's cross-transformer on the GPU (dmx_gpu.cpp): 1 when there is one (AUDIOVIZ_DEMUCS_GPU=0: never)
int dmx_gpu_attach(void *model);
void dmx_gpu_detach(void *model);
// The GPU the cross-transformer would use (its name in buf): 0 when it stays on the CPU
int dmx_gpu_name(char *buf, int len);
// stereo/out: interleaved L,R samples at 44100 Hz. progress: one float per thread.
int dmx_vocals(void *model, const float *stereo, long n, int threads, float *out,
               float *progress, volatile int *cancel, char *err, int errlen);
// All four stems (drums, bass, other, vocals), one after the other: each as a mono mix (L+R)/2
// (out holds 4*n samples), or in stereo, interleaved (dmx_stems_stereo: 8*n).
int dmx_stems_mono(void *model, const float *stereo, long n, int threads, float *out,
                   float *progress, volatile int *cancel, char *err, int errlen);
int dmx_stems_stereo(void *model, const float *stereo, long n, int threads, float *out,
                     float *progress, volatile int *cancel, char *err, int errlen);
#ifdef __cplusplus
}
#endif
#endif
