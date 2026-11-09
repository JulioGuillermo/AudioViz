// The backgrounds' effect sets, their depth and the 2.5D camera: shared by render.comp and
// fluid.comp (after common.glsl; the including shader declares the lay buffer, binding 9)

// The background effects read now: 0 the frame's (Params' own fields; in a transition drawn once
// per image, image A's), 1 image B's (fxB): shade sets it per image, parallaxPixel per image
int gFX = 0;
vec4 fZoomRipple() { return gFX == 1 ? P.fxB.zoomRipple : P.zoomRipple; }
vec4 fShake() { return gFX == 1 ? P.fxB.shake : P.shake; }
vec4 fChroma() { return gFX == 1 ? P.fxB.chroma : P.chroma; }
vec4 fColorAdj() { return gFX == 1 ? P.fxB.colorAdj : P.colorAdj; }
vec4 fFlash() { return gFX == 1 ? P.fxB.fxFlash : P.fxFlash; }
vec4 fParallax() { return gFX == 1 ? P.fxB.parallax : P.parallax; }
vec4 fDof() { return gFX == 1 ? P.fxB.dof : P.dof; }
vec4 fRange(int i) { return gFX == 1 ? P.fxB.ranges[i] : P.ranges[i]; }
int fGlitchN() { return gFX == 1 ? P.fxB.n.x : P.flags.w; }
vec4 fGlitch(int i) { return gFX == 1 ? P.fxB.glitch[i] : P.glitch[i]; }
int fChromaMode() { return gFX == 1 ? P.fxB.n.y : P.flags2.x; }
int fParN() { return gFX == 1 ? P.fxB.n.z : P.parallaxN.x; }
int fDofTaps() { return gFX == 1 ? P.fxB.n.w : P.parallaxN.y; }

// The depth of the frame at q (0 far … 1 near): the backgrounds' depth maps (a byte per pixel in
// lay, 16 bits per pixel) through the background's ripple, zoom, shake, motion and transition (as effects.Scene)
float depthTexel(int off, ivec2 c) {
    c = clamp(c, ivec2(0), ivec2(W - 1, H - 1));
    int idx = c.y * W + c.x;
    return float((lay[off + (idx >> 1)] >> ((idx & 1) * 16)) & 65535u) / 65535.0;
}
float layerDepth(int which, vec2 q) {
    vec4 L = which == 0 ? P.layerA : P.layerB;
    int off = which == 0 ? P.depthI.x : P.depthI.y;
    vec2 c = vec2(float(W), float(H)) * 0.5;
    vec2 d = q - c;
    if (L.w != 0.0) {
        float cs = cos(-L.w), sn = sin(-L.w);
        d = vec2(d.x * cs - d.y * sn, d.x * sn + d.y * cs);
    }
    float sc = L.x > 0.0 ? L.x : 1.0;
    vec2 u = clamp(c + d / sc + L.yz, vec2(0.0), vec2(float(W - 1), float(H - 1)));
    ivec2 i0 = ivec2(floor(u));
    vec2 f = u - vec2(i0);
    float a = depthTexel(off, i0) + (depthTexel(off, i0 + ivec2(1, 0)) - depthTexel(off, i0)) * f.x;
    float b = depthTexel(off, i0 + ivec2(0, 1)) + (depthTexel(off, i0 + ivec2(1, 1)) - depthTexel(off, i0 + ivec2(0, 1))) * f.x;
    return a + (b - a) * f.y;
}

// The 2.5D camera (as effects.Parallax): a point at nearness z seen at u is moved by
// (z - focus)·(shift + dolly·(u - c)); the point seen at u is found from the nearest depth to the
// farthest (the first where the scene is at least that near), refined between the last two tries
vec2 pDisp(float z, vec2 u) {
    vec2 c = vec2(float(W), float(H)) * 0.5;
    vec4 pp = fParallax();
    return (z - pp.w) * (pp.xy + pp.z * (u - c));
}
vec3 parallaxFind(int which, vec2 u) {
    int n = fParN();
    float thick = 1.5 / float(n) + 0.02; // a hit deeper than this past its depth: a side, go on
    float pz = 0.0, pe = 0.0;
    for (int k = 0; k <= n; k++) {
        float z = 1.0 - float(k) / float(n);
        vec2 p = u - pDisp(z, u);
        float d = layerDepth(which, p);
        float e = d - z;
        if (e < 0.0) {
            pz = z;
            pe = e;
            continue;
        }
        if (k == 0) return vec3(p, d);
        float t = pe / (pe - e);
        float zr = pz + (z - pz) * t;
        vec2 r = u - pDisp(zr, u);
        float dr = layerDepth(which, r);
        if (dr - zr <= thick) return vec3(r, dr);
        pz = z;
        pe = -1e-4;
    }
    vec2 p = u - pDisp(0.0, u);
    return vec3(p, layerDepth(which, p));
}
