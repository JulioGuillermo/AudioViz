// Sylla 600m (radiant-lyrics/sylla-600m-v1: transformers' Wav2Vec2BertForCTC) on ggml, the same
// backends as Demucs' GPU part: the feature projection, 24 conformer layers (feed-forward ½,
// self-attention with relative-key positions, causal depthwise convolution, feed-forward ½)
// and the CTC head, as one graph per window of audio. Matrices in f16 on the device.
#include "syl.h"

#include <cmath>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <map>
#include <string>
#include <vector>

#include <ggml-alloc.h>
#include <ggml-backend.h>
#include <ggml-cpu.h>
#include <ggml.h>

namespace {

const int C = 1024, H = 16, D = 64, FF = 4096, NL = 24, NV = 921, KW = 31, FIN = 160;
const int LEFT = 64, RIGHT = 8, NPOS = LEFT + RIGHT + 1;
const float EPS = 1e-5f;

struct Layer {
    ggml_tensor *f1nw, *f1nb, *f1w1, *f1b1, *f1w2, *f1b2;
    ggml_tensor *anw, *anb, *qw, *qb, *kw, *kb, *vw, *vb, *ow, *ob, *dist;
    ggml_tensor *cnw, *cnb, *pa, *pb, *dw, *dnw, *dnb, *p2;
    ggml_tensor *f2nw, *f2nb, *f2w1, *f2b1, *f2w2, *f2b2, *fnw, *fnb;
};

// how a safetensors weight lands: as it is, the pointwise_conv1 split in its two GLU halves,
// the depthwise kernel transposed to [C, KW] (one column per tap)
enum Kind { PLAIN, SPLIT, DEPTHWISE };
struct Slot {
    ggml_tensor *t, *t2;
    Kind kind;
    bool set;
};

struct Syl {
    ggml_backend_t be = nullptr;
    ggml_context *wctx = nullptr;
    ggml_backend_buffer_t wbuf = nullptr;
    ggml_tensor *fpnw, *fpnb, *fpw, *fpb, *lmw, *lmb;
    Layer ly[NL];
    std::map<std::string, Slot> slots;
};

ggml_tensor *decl(Syl &s, const std::string &name, ggml_type type, int64_t ne0, int64_t ne1 = 1, Kind kind = PLAIN) {
    ggml_tensor *t = ne1 > 1 || kind != PLAIN ? ggml_new_tensor_2d(s.wctx, type, ne0, ne1) : ggml_new_tensor_1d(s.wctx, type, ne0);
    s.slots[name] = Slot{t, nullptr, kind, false};
    return t;
}

void upload(ggml_tensor *t, const float *d, size_t n) {
    if (t->type == GGML_TYPE_F16) {
        std::vector<ggml_fp16_t> h(n);
        ggml_fp32_to_fp16_row(d, h.data(), (int64_t)n);
        ggml_backend_tensor_set(t, h.data(), 0, n * sizeof(ggml_fp16_t));
    } else {
        ggml_backend_tensor_set(t, d, 0, n * sizeof(float));
    }
}

ggml_tensor *ln(ggml_context *c, ggml_tensor *x, ggml_tensor *w, ggml_tensor *b) {
    return ggml_add(c, ggml_mul(c, ggml_norm(c, x, EPS), w), b);
}
ggml_tensor *lin(ggml_context *c, ggml_tensor *w, ggml_tensor *b, ggml_tensor *x) {
    ggml_tensor *y = ggml_mul_mat(c, w, x);
    return b ? ggml_add(c, y, b) : y;
}
ggml_tensor *ffn(ggml_context *c, ggml_tensor *x, ggml_tensor *w1, ggml_tensor *b1, ggml_tensor *w2, ggml_tensor *b2) {
    return lin(c, w2, b2, ggml_silu(c, lin(c, w1, b1, x)));
}

// self-attention, x [C, T]; idx [T(key), T(query)]: the relative position index of each pair
ggml_tensor *attn(ggml_context *c, ggml_tensor *x, const Layer &l, ggml_tensor *idx, int T) {
    ggml_tensor *Q = ggml_cont(c, ggml_permute(c, ggml_reshape_3d(c, lin(c, l.qw, l.qb, x), D, H, T), 0, 2, 1, 3));  // [D, T, H]
    ggml_tensor *K = ggml_cont(c, ggml_permute(c, ggml_reshape_3d(c, lin(c, l.kw, l.kb, x), D, H, T), 0, 2, 1, 3));  // [D, T, H]
    ggml_tensor *Vt = ggml_cont(c, ggml_permute(c, ggml_reshape_3d(c, lin(c, l.vw, l.vb, x), D, H, T), 1, 2, 0, 3)); // [T, D, H]
    ggml_tensor *S = ggml_mul_mat(c, K, Q);  // [Tk, Tq, H]
    // relative key: q·E[clamp(k - q, -64, 8)], each query against the 73 embeddings, then
    // gathered per (key, query) pair
    ggml_tensor *P = ggml_mul_mat(c, l.dist, Q);                    // [73, Tq, H]
    P = ggml_cont(c, ggml_permute(c, P, 1, 2, 0, 3));                // [H, 73, Tq]
    ggml_tensor *B = ggml_get_rows(c, P, idx);                       // [H, Tk, Tq]
    B = ggml_cont(c, ggml_permute(c, B, 2, 0, 1, 3));                // [Tk, Tq, H]
    S = ggml_soft_max_ext(c, ggml_add(c, S, B), nullptr, 1.0f / std::sqrt((float)D), 0.0f);
    ggml_tensor *O = ggml_mul_mat(c, Vt, S);                         // [D, Tq, H]
    O = ggml_reshape_2d(c, ggml_cont(c, ggml_permute(c, O, 0, 2, 1, 3)), C, T);
    return lin(c, l.ow, l.ob, O);
}

// the convolution module: GLU, causal depthwise convolution (31 taps over the past), norm, swish
ggml_tensor *conv(ggml_context *c, ggml_tensor *x, const Layer &l, ggml_tensor *zeros, int T) {
    ggml_tensor *h = ln(c, x, l.cnw, l.cnb);
    h = ggml_mul(c, ggml_mul_mat(c, l.pa, h), ggml_sigmoid(c, ggml_mul_mat(c, l.pb, h)));
    ggml_tensor *p = ggml_concat(c, zeros, h, 1);  // [C, KW-1 + T]
    ggml_tensor *acc = nullptr;
    for (int k = 0; k < KW; k++) {
        ggml_tensor *v = ggml_view_2d(c, p, C, T, p->nb[1], (size_t)k * p->nb[1]);
        ggml_tensor *w = ggml_view_1d(c, l.dw, C, (size_t)k * C * sizeof(float));
        ggml_tensor *term = ggml_mul(c, v, w);
        acc = acc ? ggml_add(c, acc, term) : term;
    }
    h = ggml_silu(c, ln(c, acc, l.dnw, l.dnb));
    return ggml_mul_mat(c, l.p2, h);
}

ggml_backend_dev_t pick_device() {
    const char *off = getenv("AUDIOVIZ_SYLLA_GPU");
    if (!(off && strcmp(off, "0") == 0)) {
        for (size_t i = 0; i < ggml_backend_dev_count(); i++) {
            ggml_backend_dev_t d = ggml_backend_dev_get(i);
            enum ggml_backend_dev_type t = ggml_backend_dev_type(d);
            if (t == GGML_BACKEND_DEVICE_TYPE_GPU || t == GGML_BACKEND_DEVICE_TYPE_IGPU) return d;
        }
    }
    return ggml_backend_dev_by_type(GGML_BACKEND_DEVICE_TYPE_CPU);
}

void seterr(char *err, int errlen, const char *msg) { snprintf(err, errlen, "%s", msg); }

} // namespace

extern "C" void *syl_new(int threads, char *dev, int devlen, char *err, int errlen) {
    ggml_backend_dev_t d = pick_device();
    if (!d) {
        seterr(err, errlen, "no ggml device");
        return nullptr;
    }
    Syl *s = new Syl();
    s->be = ggml_backend_dev_init(d, nullptr);
    if (!s->be) {
        delete s;
        seterr(err, errlen, "could not start the device");
        return nullptr;
    }
    const bool cpu = ggml_backend_dev_type(d) == GGML_BACKEND_DEVICE_TYPE_CPU;
    if (cpu) ggml_backend_cpu_set_n_threads(s->be, threads);
    snprintf(dev, devlen, "%s", cpu ? "CPU" : ggml_backend_dev_description(d));

    ggml_init_params ip = {ggml_tensor_overhead() * (NL * 40 + 16), nullptr, true};
    s->wctx = ggml_init(ip);
    const std::string pre = "wav2vec2_bert.";
    s->fpnw = decl(*s, pre + "feature_projection.layer_norm.weight", GGML_TYPE_F32, FIN);
    s->fpnb = decl(*s, pre + "feature_projection.layer_norm.bias", GGML_TYPE_F32, FIN);
    s->fpw = decl(*s, pre + "feature_projection.projection.weight", GGML_TYPE_F16, FIN, C);
    s->fpb = decl(*s, pre + "feature_projection.projection.bias", GGML_TYPE_F32, C);
    s->lmw = decl(*s, "lm_head.weight", GGML_TYPE_F16, C, NV);
    s->lmb = decl(*s, "lm_head.bias", GGML_TYPE_F32, NV);
    for (int i = 0; i < NL; i++) {
        Layer &l = s->ly[i];
        const std::string p = pre + "encoder.layers." + std::to_string(i) + ".";
        auto vec = [&](const char *n) { return decl(*s, p + n, GGML_TYPE_F32, C); };
        auto mat = [&](const char *n, int in, int out) { return decl(*s, p + n, GGML_TYPE_F16, in, out); };
        l.f1nw = vec("ffn1_layer_norm.weight");
        l.f1nb = vec("ffn1_layer_norm.bias");
        l.f1w1 = mat("ffn1.intermediate_dense.weight", C, FF);
        l.f1b1 = decl(*s, p + "ffn1.intermediate_dense.bias", GGML_TYPE_F32, FF);
        l.f1w2 = mat("ffn1.output_dense.weight", FF, C);
        l.f1b2 = vec("ffn1.output_dense.bias");
        l.anw = vec("self_attn_layer_norm.weight");
        l.anb = vec("self_attn_layer_norm.bias");
        l.qw = mat("self_attn.linear_q.weight", C, C);
        l.qb = vec("self_attn.linear_q.bias");
        l.kw = mat("self_attn.linear_k.weight", C, C);
        l.kb = vec("self_attn.linear_k.bias");
        l.vw = mat("self_attn.linear_v.weight", C, C);
        l.vb = vec("self_attn.linear_v.bias");
        l.ow = mat("self_attn.linear_out.weight", C, C);
        l.ob = vec("self_attn.linear_out.bias");
        l.dist = decl(*s, p + "self_attn.distance_embedding.weight", GGML_TYPE_F32, D, NPOS);
        l.cnw = vec("conv_module.layer_norm.weight");
        l.cnb = vec("conv_module.layer_norm.bias");
        l.pa = decl(*s, p + "conv_module.pointwise_conv1.weight", GGML_TYPE_F16, C, C, SPLIT);
        l.pb = ggml_new_tensor_2d(s->wctx, GGML_TYPE_F16, C, C);
        s->slots[p + "conv_module.pointwise_conv1.weight"].t2 = l.pb;
        l.dw = decl(*s, p + "conv_module.depthwise_conv.weight", GGML_TYPE_F32, C, KW, DEPTHWISE);
        l.dnw = vec("conv_module.depthwise_layer_norm.weight");
        l.dnb = vec("conv_module.depthwise_layer_norm.bias");
        l.p2 = mat("conv_module.pointwise_conv2.weight", C, C);
        l.f2nw = vec("ffn2_layer_norm.weight");
        l.f2nb = vec("ffn2_layer_norm.bias");
        l.f2w1 = mat("ffn2.intermediate_dense.weight", C, FF);
        l.f2b1 = decl(*s, p + "ffn2.intermediate_dense.bias", GGML_TYPE_F32, FF);
        l.f2w2 = mat("ffn2.output_dense.weight", FF, C);
        l.f2b2 = vec("ffn2.output_dense.bias");
        l.fnw = vec("final_layer_norm.weight");
        l.fnb = vec("final_layer_norm.bias");
    }
    s->wbuf = ggml_backend_alloc_ctx_tensors(s->wctx, s->be);
    if (!s->wbuf) {
        seterr(err, errlen, "not enough device memory for the model");
        syl_free(s);
        return nullptr;
    }
    return s;
}

extern "C" int syl_set(void *m, const char *name, const float *data, long n) {
    Syl *s = static_cast<Syl *>(m);
    auto it = s->slots.find(name);
    if (it == s->slots.end()) return 1;
    Slot &sl = it->second;
    switch (sl.kind) {
    case PLAIN:
        if (n != ggml_nelements(sl.t)) return -1;
        upload(sl.t, data, (size_t)n);
        break;
    case SPLIT:  // [2C, C, 1]: the rows of the value half, then those of the gate half
        if (n != 2L * C * C) return -1;
        upload(sl.t, data, (size_t)C * C);
        upload(sl.t2, data + (size_t)C * C, (size_t)C * C);
        break;
    case DEPTHWISE: {  // [C, 1, KW] -> [C, KW]
        if (n != (long)C * KW) return -1;
        std::vector<float> t((size_t)C * KW);
        for (int ch = 0; ch < C; ch++)
            for (int k = 0; k < KW; k++) t[(size_t)k * C + ch] = data[(size_t)ch * KW + k];
        upload(sl.t, t.data(), t.size());
        break;
    }
    }
    sl.set = true;
    return 0;
}

extern "C" int syl_check(void *m, char *err, int errlen) {
    Syl *s = static_cast<Syl *>(m);
    for (auto &kv : s->slots)
        if (!kv.second.set) {
            snprintf(err, errlen, "the model has no %s", kv.first.c_str());
            return -1;
        }
    return 0;
}

extern "C" int syl_logits(void *m, const float *feat, int T, float *out, char *err, int errlen) {
    Syl *s = static_cast<Syl *>(m);
    const int NODES = 16384;
    ggml_init_params ip = {ggml_tensor_overhead() * NODES + ggml_graph_overhead_custom(NODES, false), nullptr, true};
    ggml_context *c = ggml_init(ip);
    ggml_tensor *F = ggml_new_tensor_2d(c, GGML_TYPE_F32, FIN, T);
    ggml_tensor *idx = ggml_new_tensor_2d(c, GGML_TYPE_I32, T, T);
    ggml_tensor *zeros = ggml_new_tensor_2d(c, GGML_TYPE_F32, C, KW - 1);
    ggml_set_input(F);
    ggml_set_input(idx);
    ggml_set_input(zeros);

    ggml_tensor *x = lin(c, s->fpw, s->fpb, ln(c, F, s->fpnw, s->fpnb));
    for (int i = 0; i < NL; i++) {
        const Layer &l = s->ly[i];
        x = ggml_add(c, x, ggml_scale(c, ffn(c, ln(c, x, l.f1nw, l.f1nb), l.f1w1, l.f1b1, l.f1w2, l.f1b2), 0.5f));
        x = ggml_add(c, x, attn(c, ln(c, x, l.anw, l.anb), l, idx, T));
        x = ggml_add(c, x, conv(c, x, l, zeros, T));
        x = ggml_add(c, x, ggml_scale(c, ffn(c, ln(c, x, l.f2nw, l.f2nb), l.f2w1, l.f2b1, l.f2w2, l.f2b2), 0.5f));
        x = ln(c, x, l.fnw, l.fnb);
    }
    ggml_tensor *logits = lin(c, s->lmw, s->lmb, x);  // [NV, T]
    ggml_set_output(logits);
    ggml_cgraph *gf = ggml_new_graph_custom(c, NODES, false);
    ggml_build_forward_expand(gf, logits);

    ggml_gallocr_t al = ggml_gallocr_new(ggml_backend_get_default_buffer_type(s->be));
    bool ok = ggml_gallocr_alloc_graph(al, gf);
    if (!ok) {
        seterr(err, errlen, "not enough device memory for this window");
    } else {
        ggml_backend_tensor_set(F, feat, 0, (size_t)T * FIN * sizeof(float));
        std::vector<int32_t> ix((size_t)T * T);
        for (int q = 0; q < T; q++)
            for (int k = 0; k < T; k++) {
                int d = k - q;
                d = d < -LEFT ? -LEFT : d > RIGHT ? RIGHT : d;
                ix[(size_t)q * T + k] = d + LEFT;
            }
        ggml_backend_tensor_set(idx, ix.data(), 0, ix.size() * sizeof(int32_t));
        std::vector<float> z((size_t)C * (KW - 1), 0.0f);
        ggml_backend_tensor_set(zeros, z.data(), 0, z.size() * sizeof(float));
        ok = ggml_backend_graph_compute(s->be, gf) == GGML_STATUS_SUCCESS;
        if (ok)
            ggml_backend_tensor_get(logits, out, 0, (size_t)T * NV * sizeof(float));
        else
            seterr(err, errlen, "the graph failed on the device");
    }
    ggml_gallocr_free(al);
    ggml_free(c);
    return ok ? 0 : -1;
}

extern "C" void syl_free(void *m) {
    Syl *s = static_cast<Syl *>(m);
    if (!s) return;
    if (s->wbuf) ggml_backend_buffer_free(s->wbuf);
    if (s->wctx) ggml_free(s->wctx);
    if (s->be) ggml_backend_free(s->be);
    delete s;
}
