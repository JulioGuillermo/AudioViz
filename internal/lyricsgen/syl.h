#ifndef AUDIOVIZ_SYL_H
#define AUDIOVIZ_SYL_H
#ifdef __cplusplus
extern "C" {
#endif
// Sylla (w2v-BERT 2.0 CTC, lyrics alignment) on ggml (syl.cpp): the GPU when there is one
// (AUDIOVIZ_SYLLA_GPU=0: the CPU), else the CPU.
void *syl_new(int threads, char *dev, int devlen, char *err, int errlen);
// One weight of model.safetensors by its name (f32 data, n values): 0 set, 1 not used, -1 wrong size
int syl_set(void *m, const char *name, const float *data, long n);
// 0 when every weight was set (else err names the first missing one)
int syl_check(void *m, char *err, int errlen);
// The CTC logits of one window: feat [T][160] (the stacked log-mel frames) -> out [T][921]
int syl_logits(void *m, const float *feat, int T, float *out, char *err, int errlen);
void syl_free(void *m);
#ifdef __cplusplus
}
#endif
#endif
