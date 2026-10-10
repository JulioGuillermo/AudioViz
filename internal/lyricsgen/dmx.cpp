// C wrapper for demucs.cpp: separates the vocals of a 44.1 kHz stereo song using N threads.
// Each thread processes an overlapping chunk and the chunks are blended with ramps. Each thread's
// progress is written to progress[i] (0..1), and *cancel != 0 aborts as soon as the model reports progress.
#include "dmx.h"
#include "model.hpp"
#include <Eigen/Dense>
#include <algorithm>
#include <atomic>
#include <cmath>
#include <cstring>
#include <exception>
#include <string>
#include <thread>
#include <vector>

namespace {
struct Cancelled : std::exception {};
const int SR = 44100;
const int OVERLAP = SR * 3 / 4; // 0.75 s of overlap between chunks
}

extern "C" void *dmx_load(const char *path, char *err, int errlen) {
    auto *m = new demucscpp::demucs_model();
    if (!demucscpp::load_demucs_model(path, m)) {
        snprintf(err, errlen, "could not load the separation model %s", path);
        delete m;
        return nullptr;
    }
    dmx_gpu_attach(m);
    return m;
}

extern "C" void dmx_free(void *model) {
    dmx_gpu_detach(model);
    delete static_cast<demucscpp::demucs_model *>(model);
}

// separate: the stems' audio blended across the threads' chunks. all: the four stems (model
// order), else the vocals only; mono: each as (L+R)/2 (n samples), else stereo interleaved (2n).
static int separate(void *model, const float *stereo, long n, int threads, float *out, bool all, bool mono,
                    float *progress, volatile int *cancel, char *err, int errlen) {
    auto *m = static_cast<demucscpp::demucs_model *>(model);
    threads = std::max(1, std::min(threads, (int)(n / (SR * 10)) + 1));
    long seg = (n + threads - 1) / threads;
    const int vocals = 3; // drums, bass, other, vocals
    const int nst = all ? 4 : 1, nch = mono ? 1 : 2;
    std::vector<Eigen::MatrixXf> outs(threads); // rows: stem × channel
    std::vector<long> starts(threads), lens(threads);
    std::atomic<int> failed{0};
    std::string firstErr;
    std::vector<std::thread> pool;

    for (int t = 0; t < threads; t++) {
        long s0 = t * seg, s1 = std::min(n, s0 + seg);
        long a = std::max(0L, s0 - OVERLAP), b = std::min(n, s1 + OVERLAP);
        starts[t] = a;
        lens[t] = b - a;
        progress[t] = 0;
        pool.emplace_back([&, t, a, b]() {
            try {
                Eigen::MatrixXf audio(2, b - a);
                for (long i = a; i < b; i++) {
                    audio(0, i - a) = stereo[2 * i];
                    audio(1, i - a) = stereo[2 * i + 1];
                }
                demucscpp::ProgressCallback cb = [&, t](float p, const std::string &) {
                    progress[t] = p;
                    if (*cancel) throw Cancelled();
                };
                Eigen::Tensor3dXf r = demucscpp::demucs_inference(*m, audio, cb);
                Eigen::MatrixXf v(nst * nch, b - a);
                for (long i = 0; i < b - a; i++) {
                    for (int k = 0; k < nst; k++) {
                        int st = all ? k : vocals;
                        if (mono) {
                            v(k, i) = 0.5f * (r(st, 0, i) + r(st, 1, i));
                        } else {
                            v(2 * k, i) = r(st, 0, i);
                            v(2 * k + 1, i) = r(st, 1, i);
                        }
                    }
                }
                outs[t] = std::move(v);
                progress[t] = 1;
            } catch (const Cancelled &) {
                failed = 2;
            } catch (const std::exception &e) {
                if (!failed.exchange(1)) firstErr = e.what();
            }
        });
    }
    for (auto &th : pool) th.join();
    if (failed == 2) {
        snprintf(err, errlen, "cancelled");
        return 2;
    }
    if (failed) {
        snprintf(err, errlen, "separation error: %s", firstErr.c_str());
        return 1;
    }

    // Blend with linear ramps in the overlap regions
    const long rows = nst * nch;
    std::vector<float> wsum(n, 0.0f);
    std::memset(out, 0, sizeof(float) * rows * n);
    for (int t = 0; t < threads; t++) {
        long a = starts[t], len = lens[t];
        for (long i = 0; i < len; i++) {
            float w = 1.0f;
            if (a > 0 && i < 2 * OVERLAP) w = std::min(w, (i + 1) / float(2 * OVERLAP));
            if (a + len < n && len - i <= 2 * OVERLAP) w = std::min(w, (len - i) / float(2 * OVERLAP));
            long g = a + i;
            for (int k = 0; k < nst; k++) {
                if (mono) {
                    out[k * n + g] += outs[t](k, i) * w;
                } else {
                    out[k * 2 * n + 2 * g] += outs[t](2 * k, i) * w;
                    out[k * 2 * n + 2 * g + 1] += outs[t](2 * k + 1, i) * w;
                }
            }
            wsum[g] += w;
        }
    }
    for (long g = 0; g < n; g++) {
        if (wsum[g] <= 0) continue;
        for (int k = 0; k < nst; k++) {
            if (mono) {
                out[k * n + g] /= wsum[g];
            } else {
                out[k * 2 * n + 2 * g] /= wsum[g];
                out[k * 2 * n + 2 * g + 1] /= wsum[g];
            }
        }
    }
    return 0;
}

extern "C" int dmx_vocals(void *model, const float *stereo, long n, int threads, float *out,
                          float *progress, volatile int *cancel, char *err, int errlen) {
    return separate(model, stereo, n, threads, out, false, false, progress, cancel, err, errlen);
}

extern "C" int dmx_stems_mono(void *model, const float *stereo, long n, int threads, float *out,
                              float *progress, volatile int *cancel, char *err, int errlen) {
    return separate(model, stereo, n, threads, out, true, true, progress, cancel, err, errlen);
}

extern "C" int dmx_stems_stereo(void *model, const float *stereo, long n, int threads, float *out,
                                float *progress, volatile int *cancel, char *err, int errlen) {
    return separate(model, stereo, n, threads, out, true, false, progress, cancel, err, errlen);
}
