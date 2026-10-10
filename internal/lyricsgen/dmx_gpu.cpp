// Demucs' cross-transformer on the GPU (ggml, the same backends as image generation): its ten
// layers (self-attention and cross-attention between the frequency and the time branches, the
// slowest half of the separation on the CPU) as one graph per segment. The weights go to the GPU
// once per model (dmx_gpu_attach); demucs.cpp calls in through its cross-transformer hook
// (scripts/ml/patches-demucs). The separation's threads take turns on the GPU.
#include "dmx.h"
#include "crosstransformer.hpp"
#include "model.hpp"
#include <Eigen/Dense>
#include <cmath>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <map>
#include <mutex>
#include <vector>

#include <ggml-alloc.h>
#include <ggml-backend.h>
#include <ggml.h>

namespace {

const int HEADS = 8;
const float EPS = 1e-5f;

// a layer's weights on the GPU
struct LayerW {
    ggml_tensor *n1w, *n1b, *n2w, *n2b, *inw, *inb, *ow, *ob, *g1, *n3w, *n3b, *l1w, *l1b, *l2w, *l2b, *g2, *now_, *nob;
};

struct Gpu {
    ggml_backend_t be = nullptr;
    ggml_context *wctx = nullptr;
    ggml_backend_buffer_t wbuf = nullptr;
    LayerW my[2][3], cross[2][2];
    std::vector<std::pair<ggml_tensor *, std::vector<uint16_t>>> f16;  // uploads, then dropped
    std::vector<std::pair<ggml_tensor *, std::vector<float>>> f32;
};

std::mutex mu;                                 // (one graph at a time on the GPU)
std::map<const demucscpp::demucs_model *, Gpu *> gpus;

ggml_tensor *vec(Gpu &g, const float *d, int n) {
    ggml_tensor *t = ggml_new_tensor_1d(g.wctx, GGML_TYPE_F32, n);
    g.f32.push_back({t, std::vector<float>(d, d + n)});
    return t;
}
ggml_tensor *vec(Gpu &g, const Eigen::VectorXf &v) { return vec(g, v.data(), (int)v.size()); }
ggml_tensor *vec(Gpu &g, const Eigen::Tensor1dXf &v) { return vec(g, v.data(), (int)v.size()); }

// a weight matrix (Eigen: out × in) as ggml's [in, out], in f16 (the model's own precision)
ggml_tensor *mat(Gpu &g, const Eigen::MatrixXf &m) {
    const int out = (int)m.rows(), in = (int)m.cols();
    ggml_tensor *t = ggml_new_tensor_2d(g.wctx, GGML_TYPE_F16, in, out);
    std::vector<uint16_t> d((size_t)in * out);
    for (int o = 0; o < out; o++)
        for (int i = 0; i < in; i++) d[(size_t)o * in + i] = ggml_fp32_to_fp16(m(o, i));
    g.f16.push_back({t, std::move(d)});
    return t;
}

ggml_tensor *norm_affine(ggml_context *c, ggml_tensor *x, ggml_tensor *w, ggml_tensor *b) {
    return ggml_add(c, ggml_mul(c, ggml_norm(c, x, EPS), w), b);
}

// one encoder layer (demucscpp::common_encoder_layer): q [C, T] attends to k [C, S] (itself when
// self); its feed-forward; the group norm over the whole of it
ggml_tensor *layer(ggml_context *c, ggml_tensor *q, ggml_tensor *k, const LayerW &w, bool self) {
    const int64_t C = q->ne[0], T = q->ne[1], S = k->ne[1], D = C / HEADS;
    ggml_tensor *qn = norm_affine(c, q, w.n1w, w.n1b);
    ggml_tensor *kn = self ? qn : norm_affine(c, k, w.n2w, w.n2b);
    auto proj = [&](ggml_tensor *x, int part) {  // in_proj's part: 0 Q, 1 K, 2 V
        ggml_tensor *wp = ggml_view_2d(c, w.inw, C, C, w.inw->nb[1], part * C * w.inw->nb[1]);
        ggml_tensor *bp = ggml_view_1d(c, w.inb, C, part * C * ggml_element_size(w.inb));
        return ggml_add(c, ggml_mul_mat(c, wp, x), bp);
    };
    ggml_tensor *Q = proj(qn, 0), *K = proj(kn, 1), *V = proj(kn, 2);
    // heads: [D, T, H]
    ggml_tensor *Qh = ggml_permute(c, ggml_reshape_3d(c, Q, D, HEADS, T), 0, 2, 1, 3);
    ggml_tensor *Kh = ggml_cast(c, ggml_permute(c, ggml_reshape_3d(c, K, D, HEADS, S), 0, 2, 1, 3), GGML_TYPE_F16);
    ggml_tensor *Vh = ggml_cast(c, ggml_permute(c, ggml_reshape_3d(c, V, D, HEADS, S), 0, 2, 1, 3), GGML_TYPE_F16);
    ggml_tensor *O = ggml_flash_attn_ext(c, Qh, Kh, Vh, nullptr, 1.0f / std::sqrt((float)D), 0.0f, 0.0f);
    ggml_flash_attn_ext_set_prec(O, GGML_PREC_F32);
    O = ggml_reshape_2d(c, O, C, T);  // ([D, H, T])
    ggml_tensor *a = ggml_mul(c, ggml_add(c, ggml_mul_mat(c, w.ow, O), w.ob), w.g1);
    q = ggml_add(c, q, a);
    ggml_tensor *h = norm_affine(c, q, w.n3w, w.n3b);
    ggml_tensor *f = ggml_gelu_erf(c, ggml_add(c, ggml_mul_mat(c, w.l1w, h), w.l1b));
    f = ggml_mul(c, ggml_add(c, ggml_mul_mat(c, w.l2w, f), w.l2b), w.g2);
    q = ggml_add(c, q, f);
    // group norm with one group: over all of it, then per channel
    ggml_tensor *g = ggml_reshape_2d(c, ggml_norm(c, ggml_reshape_2d(c, q, C * T, 1), EPS), C, T);
    return ggml_add(c, ggml_mul(c, g, w.now_), w.nob);
}

// the hook: x (1, T, C), xt (1, S, C)
bool run(const demucscpp::demucs_model &model, Eigen::Tensor3dXf &x, Eigen::Tensor3dXf &xt) {
    std::lock_guard<std::mutex> lk(mu);
    auto it = gpus.find(&model);
    if (it == gpus.end()) return false;
    Gpu &g = *it->second;
    const int T = (int)x.dimension(1), C = (int)x.dimension(2), S = (int)xt.dimension(1);
    ggml_init_params ip = {ggml_tensor_overhead() * 4096 + ggml_graph_overhead_custom(4096, false), nullptr, true};
    ggml_context *c = ggml_init(ip);
    ggml_tensor *X = ggml_new_tensor_2d(c, GGML_TYPE_F32, C, T);
    ggml_tensor *XT = ggml_new_tensor_2d(c, GGML_TYPE_F32, C, S);
    ggml_set_input(X);
    ggml_set_input(XT);
    ggml_tensor *a = X, *b = XT;
    for (int i = 0; i < 5; i++) {
        if (i % 2 == 0) {
            a = layer(c, a, a, g.my[0][i / 2], true);
            b = layer(c, b, b, g.my[1][i / 2], true);
        } else {
            ggml_tensor *old = a;
            a = layer(c, a, b, g.cross[0][i / 2], false);
            b = layer(c, b, old, g.cross[1][i / 2], false);
        }
    }
    ggml_set_output(a);
    ggml_set_output(b);
    ggml_cgraph *gf = ggml_new_graph_custom(c, 4096, false);
    ggml_build_forward_expand(gf, a);
    ggml_build_forward_expand(gf, b);
    ggml_gallocr_t al = ggml_gallocr_new(ggml_backend_get_default_buffer_type(g.be));
    bool ok = ggml_gallocr_alloc_graph(al, gf);
    if (ok) {
        // Eigen (1, T, C) column-major: (t, c) at t + T·c; ggml [C, T]: c + C·t
        std::vector<float> buf((size_t)std::max(T, S) * C);
        auto in = [&](ggml_tensor *t, const Eigen::Tensor3dXf &e, int n) {
            for (int j = 0; j < n; j++)
                for (int k = 0; k < C; k++) buf[(size_t)j * C + k] = e(0, j, k);
            ggml_backend_tensor_set(t, buf.data(), 0, (size_t)n * C * sizeof(float));
        };
        auto out = [&](ggml_tensor *t, Eigen::Tensor3dXf &e, int n) {
            ggml_backend_tensor_get(t, buf.data(), 0, (size_t)n * C * sizeof(float));
            for (int j = 0; j < n; j++)
                for (int k = 0; k < C; k++) e(0, j, k) = buf[(size_t)j * C + k];
        };
        in(X, x, T);
        in(XT, xt, S);
        ok = ggml_backend_graph_compute(g.be, gf) == GGML_STATUS_SUCCESS;
        if (ok) {
            out(a, x, T);
            out(b, xt, S);
        }
    }
    ggml_gallocr_free(al);
    ggml_free(c);
    return ok;
}

ggml_backend_dev_t gpu_device() {
    for (size_t i = 0; i < ggml_backend_dev_count(); i++) {
        ggml_backend_dev_t d = ggml_backend_dev_get(i);
        enum ggml_backend_dev_type t = ggml_backend_dev_type(d);
        if (t == GGML_BACKEND_DEVICE_TYPE_GPU || t == GGML_BACKEND_DEVICE_TYPE_IGPU) return d;
    }
    return nullptr;
}

} // namespace

// dmx_gpu_attach: the model's cross-transformer weights on the GPU (0: no GPU, it stays on the CPU)
extern "C" int dmx_gpu_attach(void *model) {
    const char *off = getenv("AUDIOVIZ_DEMUCS_GPU");
    if (off && strcmp(off, "0") == 0) return 0;
    auto *m = static_cast<const demucscpp::demucs_model *>(model);
    if (!m->is_4sources || !m->crosstransformer) return 0;
    auto *ct = m->crosstransformer.get();
    ggml_backend_dev_t dev = gpu_device();
    if (!dev) return 0;
    Gpu *g = new Gpu();
    g->be = ggml_backend_dev_init(dev, nullptr);
    if (!g->be) {
        delete g;
        return 0;
    }
    ggml_init_params ip = {ggml_tensor_overhead() * 512, nullptr, true};
    g->wctx = ggml_init(ip);
    for (int b = 0; b < 2; b++) {
        for (int i = 0; i < 3; i++) {
            LayerW &w = g->my[b][i];
            w.n1w = vec(*g, ct->crosstransformer_my_layers_norm1_weight[b][i]);
            w.n1b = vec(*g, ct->crosstransformer_my_layers_norm1_bias[b][i]);
            w.n2w = w.n1w; w.n2b = w.n1b;  // (self-attention: unused)
            w.inw = mat(*g, ct->crosstransformer_my_layers_self_attn_in_proj_weight[b][i]);
            w.inb = vec(*g, ct->crosstransformer_my_layers_self_attn_in_proj_bias[b][i]);
            w.ow = mat(*g, ct->crosstransformer_my_layers_self_attn_out_proj_weight[b][i]);
            w.ob = vec(*g, ct->crosstransformer_my_layers_self_attn_out_proj_bias[b][i]);
            w.g1 = vec(*g, ct->crosstransformer_my_layers_gamma_1_scale[b][i]);
            w.n3w = vec(*g, ct->crosstransformer_my_layers_norm2_weight[b][i]);  // (its norm2 before the feed-forward)
            w.n3b = vec(*g, ct->crosstransformer_my_layers_norm2_bias[b][i]);
            w.l1w = mat(*g, ct->crosstransformer_my_layers_linear1_weight[b][i]);
            w.l1b = vec(*g, ct->crosstransformer_my_layers_linear1_bias[b][i]);
            w.l2w = mat(*g, ct->crosstransformer_my_layers_linear2_weight[b][i]);
            w.l2b = vec(*g, ct->crosstransformer_my_layers_linear2_bias[b][i]);
            w.g2 = vec(*g, ct->crosstransformer_my_layers_gamma_2_scale[b][i]);
            w.now_ = vec(*g, ct->crosstransformer_my_layers_norm_out_weight[b][i]);
            w.nob = vec(*g, ct->crosstransformer_my_layers_norm_out_bias[b][i]);
        }
        for (int i = 0; i < 2; i++) {
            LayerW &w = g->cross[b][i];
            w.n1w = vec(*g, ct->crosstransformer_cross_layers_norm1_weight[b][i]);
            w.n1b = vec(*g, ct->crosstransformer_cross_layers_norm1_bias[b][i]);
            w.n2w = vec(*g, ct->crosstransformer_cross_layers_norm2_weight[b][i]);
            w.n2b = vec(*g, ct->crosstransformer_cross_layers_norm2_bias[b][i]);
            w.inw = mat(*g, ct->crosstransformer_cross_layers_cross_attn_in_proj_weight[b][i]);
            w.inb = vec(*g, ct->crosstransformer_cross_layers_cross_attn_in_proj_bias[b][i]);
            w.ow = mat(*g, ct->crosstransformer_cross_layers_cross_attn_out_proj_weight[b][i]);
            w.ob = vec(*g, ct->crosstransformer_cross_layers_cross_attn_out_proj_bias[b][i]);
            w.g1 = vec(*g, ct->crosstransformer_cross_layers_gamma_1_scale[b][i]);
            w.n3w = vec(*g, ct->crosstransformer_cross_layers_norm3_weight[b][i]);
            w.n3b = vec(*g, ct->crosstransformer_cross_layers_norm3_bias[b][i]);
            w.l1w = mat(*g, ct->crosstransformer_cross_layers_linear1_weight[b][i]);
            w.l1b = vec(*g, ct->crosstransformer_cross_layers_linear1_bias[b][i]);
            w.l2w = mat(*g, ct->crosstransformer_cross_layers_linear2_weight[b][i]);
            w.l2b = vec(*g, ct->crosstransformer_cross_layers_linear2_bias[b][i]);
            w.g2 = vec(*g, ct->crosstransformer_cross_layers_gamma_2_scale[b][i]);
            w.now_ = vec(*g, ct->crosstransformer_cross_layers_norm_out_weight[b][i]);
            w.nob = vec(*g, ct->crosstransformer_cross_layers_norm_out_bias[b][i]);
        }
    }
    g->wbuf = ggml_backend_alloc_ctx_tensors(g->wctx, g->be);
    if (!g->wbuf) {
        ggml_free(g->wctx);
        ggml_backend_free(g->be);
        delete g;
        return 0;
    }
    for (auto &p : g->f16) ggml_backend_tensor_set(p.first, p.second.data(), 0, ggml_nbytes(p.first));
    for (auto &p : g->f32) ggml_backend_tensor_set(p.first, p.second.data(), 0, ggml_nbytes(p.first));
    g->f16.clear();
    g->f16.shrink_to_fit();
    g->f32.clear();
    g->f32.shrink_to_fit();
    std::lock_guard<std::mutex> lk(mu);
    gpus[m] = g;
    demucscpp::crosstransformer_layers_hook = run;
    return 1;
}

extern "C" void dmx_gpu_detach(void *model) {
    std::lock_guard<std::mutex> lk(mu);
    auto it = gpus.find(static_cast<const demucscpp::demucs_model *>(model));
    if (it == gpus.end()) return;
    Gpu *g = it->second;
    ggml_backend_buffer_free(g->wbuf);
    ggml_free(g->wctx);
    ggml_backend_free(g->be);
    delete g;
    gpus.erase(it);
}
