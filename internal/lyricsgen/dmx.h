#ifndef AUDIOVIZ_DMX_H
#define AUDIOVIZ_DMX_H
#ifdef __cplusplus
extern "C" {
#endif
void *dmx_load(const char *path, char *err, int errlen);
void dmx_free(void *model);
// stereo/out: muestras entrelazadas L,R a 44100 Hz. progress: un float por hilo.
int dmx_vocals(void *model, const float *stereo, long n, int threads, float *out,
               float *progress, volatile int *cancel, char *err, int errlen);
#ifdef __cplusplus
}
#endif
#endif
