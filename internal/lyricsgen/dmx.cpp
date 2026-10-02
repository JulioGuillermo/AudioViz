// Envoltorio C de demucs.cpp: separa la voz de una canción estéreo a 44,1 kHz usando N hilos.
// Cada hilo procesa un trozo con solape y se mezclan con rampas. El progreso de cada hilo se
// escribe en progress[i] (0..1) y *cancel != 0 aborta en cuanto el modelo informa progreso.
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
const int OVERLAP = SR * 3 / 4; // 0,75 s de solape entre trozos
}

extern "C" void *dmx_load(const char *path, char *err, int errlen) {
    auto *m = new demucscpp::demucs_model();
    if (!demucscpp::load_demucs_model(path, m)) {
        snprintf(err, errlen, "no se pudo cargar el modelo de separación %s", path);
        delete m;
        return nullptr;
    }
    return m;
}

extern "C" void dmx_free(void *model) { delete static_cast<demucscpp::demucs_model *>(model); }

extern "C" int dmx_vocals(void *model, const float *stereo, long n, int threads, float *out,
                          float *progress, volatile int *cancel, char *err, int errlen) {
    auto *m = static_cast<demucscpp::demucs_model *>(model);
    threads = std::max(1, std::min(threads, (int)(n / (SR * 10)) + 1));
    long seg = (n + threads - 1) / threads;
    const int vocals = 3; // drums, bass, other, vocals
    std::vector<Eigen::MatrixXf> outs(threads);
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
                Eigen::MatrixXf v(2, b - a);
                for (long i = 0; i < b - a; i++) {
                    v(0, i) = r(vocals, 0, i);
                    v(1, i) = r(vocals, 1, i);
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
        snprintf(err, errlen, "cancelado");
        return 2;
    }
    if (failed) {
        snprintf(err, errlen, "error en la separación: %s", firstErr.c_str());
        return 1;
    }

    // Mezcla con rampas lineales en las zonas de solape
    std::vector<float> wsum(n, 0.0f);
    std::memset(out, 0, sizeof(float) * 2 * n);
    for (int t = 0; t < threads; t++) {
        long a = starts[t], len = lens[t];
        for (long i = 0; i < len; i++) {
            float w = 1.0f;
            if (a > 0 && i < 2 * OVERLAP) w = std::min(w, (i + 1) / float(2 * OVERLAP));
            if (a + len < n && len - i <= 2 * OVERLAP) w = std::min(w, (len - i) / float(2 * OVERLAP));
            long g = a + i;
            out[2 * g] += outs[t](0, i) * w;
            out[2 * g + 1] += outs[t](1, i) * w;
            wsum[g] += w;
        }
    }
    for (long g = 0; g < n; g++) {
        if (wsum[g] > 0) {
            out[2 * g] /= wsum[g];
            out[2 * g + 1] /= wsum[g];
        }
    }
    return 0;
}
