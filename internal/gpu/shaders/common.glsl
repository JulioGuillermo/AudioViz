// Declarations shared by render.comp and fluid.comp: the per-frame parameters, the two
// background images and how they are sampled (each placed by its layer).
// FLUID_IMG_READ: the shader also reads the fluid pass's output as image 2.

const int MAX_GRAD = 8;
const int MAX_GLITCH = 64;
const int MAX_BARS = 256;
const int MAX_PARTS = 1024;
const int MAX_TEXT = 256;
const int MAX_TSTYLE = 8;

// One overlay clip (gpu.LayerP); its image is in the Layers buffer (render.comp)
const int MAX_TLIGHTS = MAX_TSTYLE; // gpu.MaxTextLights
const int TLIGHT_VEC4 = 900;  // gpu.TextLightVec4
struct LayerP {
    ivec4 info;    // image offset (pixels), width, height, flags (blend 0-3, key 4, glitch 5-6: 1 background's / 2 own, own count 8-11)
    vec4 xf;       // center x, y (px), screen px per image px x, y (negative = flipped)
    vec4 xf2;      // rotation (rad), opacity, blur (px), hue (rad)
    vec4 adj;      // brightness, contrast, saturation, -
    vec4 key;      // key color rgb, tolerance
    vec4 key2;     // softness
    vec4 warp0;    // zoom, ripple amplitude, frequency, phase
    vec4 warp1;    // shake x, y, chromatic shift, mode (0 none, 1 horizontal, 2 radial)
    vec4 warp2;    // flash contrast factor, brightness
    vec4 glitch[8];
    ivec4 mask;    // offset (words; one byte per mask px), width, height, on
    ivec4 light0;  // emitter light: field offset (words, in the emitter accumulations), grid w, h, mode (0 off, 1 soft, 2 add)
    vec4 light1;   // its color × strength
    vec4 depth;    // behind what is nearer: on, its depth, soft edge
    vec4 light2;   // 2.5D light: where it is x, y (px), its depth, fade by depth
    vec4 light3;   // shading of the surfaces, 2.5D on, lit from, to (soft edge in light1.w)
    vec4 light4;   // rim light: from (px), soft (px), width (px), gain (0 = off)
    vec4 light5;   // rim light: edge (depth step)
    vec4 ztrack;   // depth from its path: track offset + 1 (float bits; 0 none), center along it (px), direction x, y
    ivec4 xpr;     // properties per pixel (expressions): their table's offset in lay + 1 (0 none), seed, frame; in a transition on both images (0), only on A (1) or B (2)
    vec4 xps;      // along its path: its center (px), direction x, y; its opacity's factor (fades, reaction)
};

// One entry of the per-frame text style table (lyrics = 0, fixed texts after)
// a set of background effects (as the Params fields of the same names; n: the glitch's bands,
// the chromatic aberration's mode, the camera's search steps, the depth of field's taps)
struct FXSet {
    vec4 zoomRipple;
    vec4 shake;
    vec4 chroma;
    vec4 colorAdj;
    vec4 fxFlash;
    vec4 parallax;
    vec4 dof;
    vec4 ranges[4];
    ivec4 n;
    vec4 glitch[MAX_GLITCH];
};

struct TStyle {
    vec4 info;        // outlineW, glowRadius, glowIntensity, chromaPx (0 = none)
    vec4 color;
    vec4 upcoming;
    vec4 outline;
    vec4 glowC;
    vec4 shadow;      // dx, dy, opacity, blur
    vec4 shadowC;
    ivec4 gradN;      // gradient stop count, stage, glows (bit 0) / casts a shadow (bit 1)
    ivec4 mask;       // screen mask: offset in lay (words; a byte per px), width, height, on
    vec4 depth;       // behind what is nearer (0/1), its depth, soft edge; blur (px, depth of field)
    vec4 ztrack;      // depth from its path: track offset + 1 (float bits; 0 none), margin (px); its twist as a ribbon: track offset + 1; wrapped round a cylinder (1: items' tD.z is their slant)
    ivec4 xpr;        // properties per letter / pixel (expressions): their table's offset in lay + 1 (0 none), its opacity (float bits), seed, frame
    vec4 grad[MAX_GRAD];
};

layout(std430, set = 0, binding = 0) readonly buffer Params {
    ivec4 dims;       // outW, outH, padW (stride Y en bytes), padH
    ivec4 flags;      // vizType (0 none,1 circ,2 horiz,3 vert,4 wave), barCount, particleCount, glitchCount
    ivec4 flags2;     // chromaMode (0 no,1 horiz,2 radial), colorMode (0 grad,1 solid), gradCount, writeRGBA
    ivec4 flags3;     // mirrorBottom, glowEnabled, particleGlow, drawRing
    vec4 zoomRipple;  // zoom, rippleAmp, rippleFreq, rippleTime
    vec4 colorAdj;    // contrastFactor, brightAdd (0..1), opacity, vignette
    vec4 chroma;      // shift, -, -, -
    vec4 viz0;        // cx, cy, baseRadius, baseY
    vec4 viz1;        // barWidth, span, roundness, ringAlpha
    vec4 viz2;        // glowRadius, glowIntensity, waveAmp, stepX
    vec4 viz3;        // startY (vertical), barHeight (vertical), center gap (horizontal), -
    vec4 solidColor;  // rgba
    vec4 partColor;   // rgb
    vec4 waveColL;
    vec4 waveColR;
    vec4 gradInfo;    // axis (0 index, 1 length), fit (0 bar, 1 max), maxHeight, -
    vec4 shake;       // camera offset dx, dy; the zoom's factors of the bass zoom and of the shake
    vec4 grad[MAX_GRAD];       // rgb + offset in w
    vec4 glitch[MAX_GLITCH];   // y0, y1, shiftX, -
    vec4 bars[MAX_BARS];       // heightL, heightR, magL, magR
    vec4 parts[MAX_PARTS];     // x, y, size, alpha
    vec4 partCol[MAX_PARTS];   // rgb, glow (0/1)

    ivec4 textInfo;            // nItems, atlasW, any chroma (0/1), -
    TStyle ts[MAX_TSTYLE];
    vec4 tA[MAX_TEXT];         // atlas x, y, w, h
    vec4 tB[MAX_TEXT];         // center x, y, scale, alpha
    vec4 tC[MAX_TEXT];         // karaoke fill, pad, style index, rotation (rad)
    vec4 tD[MAX_TEXT];         // along its path (depth from it): how far (px), its direction (rad); turned round: 1 − width, 1 − height
    vec4 tG[MAX_TEXT];         // in its text's box (0…1): its center x, y, per atlas px x, y (colors per pixel)
    vec4 tH[MAX_TEXT];         // its index in its text, how many (colors per pixel)
    vec4 bgMix;                // -, visualizer opacity
    ivec4 prev;                // preview: scale (1 = normal), output width and height
    vec4 layerA;               // background A (BG): scale, offX, offY, rot (segment motion)
    vec4 layerB;               // background B (BG2), only during a transition
    ivec4 trans;               // mode (0 = none, see config.TransitionModes), direction, has (bit0 A, bit1 B), seed
    vec4 transP;               // progress k; drawn once per image (1: clips or effects only on one of them; B's effects in fxB)
    vec4 burnP;                // burn: edge, roughness, max distance, point count
    vec4 burnCol;              // burn: ember color
    vec4 burnPts[8];           // burn: ignition points (x, y in units of the frame height)
    // Fluid / liquid (effects.FluidFrame, in the FluidData buffer): word offsets of the bin
    // starts, - and the coarse level (the particles start at 0), bin side (0 = none)
    ivec4 fluidI;
    ivec4 fluidN;              // bins across, bins down, coarse width, coarse height
    vec4 fluidF;               // piece half width, half height, progress k, debug (gaps magenta)
    // Overlay layers: count; stages of the particles, the visualizer, mask of the stages with
    // texts (each style's stage in ts[s].gradN.y); background flash (contrast factor, brightness)
    ivec4 layerN;
    ivec4 zIdx;
    vec4 fxFlash;
    ivec4 depthI;  // backgrounds' depth in lay: word offsets of A, B; which are there (bit 0 A, bit 1 B)
    ivec4 normI;   // their surface normals in lay (two int8 per pixel): word offsets of A, B; which are there
    vec4 parallax;    // the 2.5D camera: shift x, y (px), dolly, focus
    ivec4 parallaxN;  // its search steps (0 = off)
    vec4 dof;         // depth of field: in focus from (near), to (far), soft, blur (px)
    vec4 ranges[4];   // flash, chromatic aberration, ripple, glitch only on depths: from, to, soft, on
    // texts lighting the scene (gpu.Params.TLight*): grid w, h, cell px, style (w 0 = off);
    // color × strength, add; 2.5D x, y, depth, fade by depth (0 = flat); shading; the cells
    ivec4 tlightI[MAX_TLIGHTS];
    vec4 tlightC[MAX_TLIGHTS];
    vec4 tlightD[MAX_TLIGHTS];
    vec4 tlightE[MAX_TLIGHTS];
    vec4 tlightF[MAX_TLIGHTS];
    vec4 tlightG[MAX_TLIGHTS];
    vec4 tlightV[MAX_TLIGHTS * TLIGHT_VEC4];
    ivec4 viewI;      // studio view instead of the frame: 0 the frame, 1 depth, 2 lit range, 3 object, 4 solid
    vec4 viewP;       // 2: from, to, soft; 3: its depth, soft; 4: its depth, band
    vec4 viewQ;       // 4: where it is born: x, y, radius
    // drawn once per image of a transition (transP.y): the zoom and shake on both (for the mix),
    // and image B's background effects (the fields above are A's)
    vec4 mixZS;
    FXSet fxB;
    LayerP layers[];  // the overlay clips drawn (layerN.x; as many as the renderer has room for)
} P;

layout(std430, set = 0, binding = 1) readonly buffer BG { uint bg[]; };
layout(std430, set = 0, binding = 6) readonly buffer BG2 { uint bg2[]; };

int W, H;

// ---- Background images: each one placed by its layer (motion) and mixed by the transition.
// Same math as internal/effects/transition.go (CPU path).

vec3 texel(int img, ivec2 c) {
    c = clamp(c, ivec2(0), ivec2(W - 1, H - 1));
#ifdef FLUID_IMG_READ
    if (img == 2) return unpackUnorm4x8(fimg[c.y * W + c.x]).rgb;
#endif
    uint v = img == 0 ? bg[c.y * W + c.x] : bg2[c.y * W + c.x];
    return unpackUnorm4x8(v).rgb;
}

vec3 bilinearImg(int img, vec2 q) {
    q = clamp(q, vec2(0.0), vec2(float(W - 1), float(H - 1)));
    ivec2 i0 = ivec2(floor(q));
    vec2 f = q - vec2(i0);
    vec3 a = texel(img, i0);
    vec3 b = texel(img, i0 + ivec2(1, 0));
    vec3 c = texel(img, i0 + ivec2(0, 1));
    vec3 d = texel(img, i0 + ivec2(1, 1));
    return mix(mix(a, b, f.x), mix(c, d, f.x), f.y);
}

// Image img (0 = A, 1 = B) at output point q: source = c + rotate(q - c, -rot) / scale + off
vec3 layerAt(int img, vec2 q) {
    if ((P.trans.z & (img == 0 ? 1 : 2)) == 0) return vec3(0.0);
#ifdef BG_PARALLAX
    q -= img == 0 ? gParA : gParB; // seen by the 2.5D camera (render.comp: parallaxPixel)
#endif
    vec4 L = img == 0 ? P.layerA : P.layerB;
    vec2 c = vec2(float(W), float(H)) * 0.5;
    vec2 d = q - c;
    if (L.w != 0.0) {
        float cs = cos(-L.w), sn = sin(-L.w);
        d = vec2(d.x * cs - d.y * sn, d.x * sn + d.y * cs);
    }
    float sc = L.x > 0.0 ? L.x : 1.0;
    return bilinearImg(img, c + d / sc + L.yz);
}
