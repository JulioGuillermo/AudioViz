// The expressions' interpreter (package expr: the same bytecode, the same ops), shared by
// emit.comp (per particle) and render.comp (texts' colors per pixel). The shader including it
// defines first XPW(i): word i of the buffer the programs are in (uint), and after it
//   float xVar(int v)                       a variable (expr.Var…)
//   bool xKey(int kind, uint tu, out float v) a value of the path (0) / route (1) from its key
//                                           table entry (false: nil)
// and sets xSeed, xFrame, xIndex (rand()), xCh, xValA (the channel worked out, value.a: rgb()).
const int XSTK = 32, XSLOT = 16; // as gpu.EmitExprStack, EmitExprSlots
float xVar(int v);
bool xKey(int kind, uint tu, out float v);
uint xSeed = 0u, xFrame = 0u;
int xIndex = 0, xCh = 0;
float xValA = 0.0;

float xwf(int i) { return uintBitsToFloat(XPW(i)); }
uint xhash(uint n) {
    n = (n << 13u) ^ n;
    n = n * (n * n * 15731u + 789221u) + 1376312589u;
    return n & 0x7fffffffu;
}
float xhashf(uint n) { return float(xhash(n)) / 2147483647.0; }
float xnoise1(float x) {
    float i = floor(x), f = x - i, u = f * f * (3.0 - 2.0 * f);
    float a = xhashf(uint(int(i))), b = xhashf(uint(int(i) + 1));
    return a + (b - a) * u;
}
float xnoise2(float x, float y) {
    float ix = floor(x), iy = floor(y), fx = x - ix, fy = y - iy;
    float ux = fx * fx * (3.0 - 2.0 * fx), uy = fy * fy * (3.0 - 2.0 * fy);
    float a = xhashf(uint(int(ix)) ^ uint(int(iy)) * 2654435761u), b = xhashf(uint(int(ix) + 1) ^ uint(int(iy)) * 2654435761u);
    float c = xhashf(uint(int(ix)) ^ uint(int(iy) + 1) * 2654435761u), d = xhashf(uint(int(ix) + 1) ^ uint(int(iy) + 1) * 2654435761u);
    return a + (b - a) * ux + (c - a) * uy + (a - b - c + d) * ux * uy;
}
// an item's random number (as expr.ItemRand)
float xItemRand(uint seed, int index, bool perFrame, uint frame) {
    uint n = seed ^ uint(index) * 2654435761u;
    if (perFrame) n ^= frame * 40503u + 0x9e3779b9u;
    return xhashf(n);
}
const float XDEG = 3.14159265358979 / 180.0;
float xsafe(float v) { return (isnan(v) || isinf(v)) ? 0.0 : v; }
// a power as Go's math.Pow: a negative base only to a whole power (else 0, as NaN gives)
float xpow(float a, float b) {
    if (a >= 0.0) return xsafe(pow(a, b));
    if (fract(b) != 0.0) return 0.0;
    return xsafe(pow(-a, b) * (mod(b, 2.0) == 1.0 ? -1.0 : 1.0));
}
// hue (degrees), saturation, value as red, green, blue (as expr.hsvToRGB)
vec3 xhsv(float h, float s, float v) {
    h = mod(h, 360.0) / 60.0;
    s = clamp(s, 0.0, 1.0); v = clamp(v, 0.0, 1.0);
    float c = v * s, x = c * (1.0 - abs(mod(h, 2.0) - 1.0));
    int k = int(h);
    vec3 o = k == 0 ? vec3(c, x, 0.0) : k == 1 ? vec3(x, c, 0.0) : k == 2 ? vec3(0.0, c, x)
        : k == 3 ? vec3(0.0, x, c) : k == 4 ? vec3(x, 0.0, c) : vec3(c, 0.0, x);
    return o + (v - c);
}
// a built-in function (as expr.callFunc; the same numbers as expr.Funcs)
float xcall(int fi, float a0, float a1, float a2, float a3, float a4, bool anyNil) {
    switch (fi) {
    case 0: return sin(a0 * XDEG);
    case 1: return cos(a0 * XDEG);
    case 2: return xsafe(tan(a0 * XDEG));
    case 3: return xsafe(asin(a0) / XDEG);
    case 4: return xsafe(acos(a0) / XDEG);
    case 5: return atan(a0) / XDEG;
    case 6: return atan(a0, a1) / XDEG;
    case 7: return sin(a0);
    case 8: return cos(a0);
    case 9: return xsafe(tan(a0));
    case 10: return xsafe(asin(a0));
    case 11: return xsafe(acos(a0));
    case 12: return atan(a0);
    case 13: return atan(a0, a1);
    case 14: return a0 / XDEG;
    case 15: return a0 * XDEG;
    case 16: return abs(a0);
    case 17: return sign(a0);
    case 18: return floor(a0);
    case 19: return ceil(a0);
    case 20: return floor(a0 + 0.5);
    case 21: return a0 - floor(a0);
    case 22: return a0 < 0.0 ? 0.0 : sqrt(a0);
    case 23: return xsafe(exp(a0));
    case 24: return a0 <= 0.0 ? 0.0 : log(a0);
    case 25: return xpow(a0, a1);
    case 26: return a1 == 0.0 ? 0.0 : a0 - a1 * floor(a0 / a1);
    case 27: return min(a0, a1);
    case 28: return max(a0, a1);
    case 29: return max(a1, min(a2, a0));
    case 30: return a0 + (a1 - a0) * a2;
    case 31: {
        if (a1 == a0) return a2 < a0 ? 0.0 : 1.0;
        float t = clamp((a2 - a0) / (a1 - a0), 0.0, 1.0);
        return t * t * (3.0 - 2.0 * t);
    }
    case 32: return a1 < a0 ? 0.0 : 1.0;
    case 33: return a2 == a1 ? a3 : a3 + (a0 - a1) / (a2 - a1) * (a4 - a3);
    case 34: return xnoise1(a0);
    case 35: return xnoise2(a0, a1);
    case 36: return xItemRand(xSeed, xIndex, false, xFrame);
    case 37: return xItemRand(xSeed, xIndex, true, xFrame);
    case 38: return anyNil ? 1.0 : 0.0;
    case 39: case 40: case 41: {
        // a color: the channel being worked out (rgb keeps the color's alpha)
        vec4 c = vec4(a0, a1, a2, fi == 40 ? a3 : xValA);
        if (fi == 41) c.rgb = xhsv(a0, a1, a2);
        return c[clamp(xCh, 0, 3)];
    }
    }
    return 0.0;
}
// the arguments each function takes (expr.Funcs)
int xargs(int fi) {
    if (fi == 6 || fi == 13 || fi == 25 || fi == 26 || fi == 27 || fi == 28 || fi == 32 || fi == 35) return 2;
    if (fi == 29 || fi == 30 || fi == 31 || fi == 39 || fi == 41) return 3;
    if (fi == 40) return 4;
    if (fi == 33) return 5;
    if (fi == 36 || fi == 37) return 0;
    return 1;
}
// runs a program: its code (n words), constants, key table (2 words per key: route, path); nil
// gives 0
float xRun(int code, int n, int co, int kt) {
    float st[XSTK];
    bool nl[XSTK];
    float sl[XSLOT];
    bool sn[XSLOT];
    int sp = 0;
    int pc = 0;
    for (int step = 0; step < 8192 && pc < n; step++) {
        int op = int(XPW(code + pc));
        int a0 = pc + 1 < n ? int(XPW(code + pc + 1)) : 0;
        int nx = pc + 1;
        if (op == 0) { st[sp] = xwf(co + a0); nl[sp] = false; sp++; nx = pc + 2; }
        else if (op == 1) { st[sp] = 0.0; nl[sp] = true; sp++; }
        else if (op == 2) { st[sp] = xVar(a0); nl[sp] = false; sp++; nx = pc + 2; }
        else if (op == 7) {
            // a value along the route (kind 1) or path (0) where the item is
            int kind = a0, k = int(XPW(code + pc + 2));
            float v;
            bool ok = xKey(kind, XPW(kt + 2 * k + (kind == 1 ? 0 : 1)), v);
            st[sp] = ok ? v : 0.0; nl[sp] = !ok;
            sp++;
            nx = pc + 3;
        }
        else if (op == 8) { st[sp] = sl[a0]; nl[sp] = sn[a0]; sp++; nx = pc + 2; }
        else if (op == 9) { sp--; sl[a0] = st[sp]; sn[a0] = nl[sp]; nx = pc + 2; }
        else if (op == 10) { sp--; }
        else if (op >= 11 && op <= 22) {
            sp--;
            float b = nl[sp] ? 0.0 : st[sp];
            sp--;
            float a = nl[sp] ? 0.0 : st[sp];
            float r = 0.0;
            if (op == 11) r = a + b;
            else if (op == 12) r = a - b;
            else if (op == 13) r = a * b;
            else if (op == 14) r = b == 0.0 ? 0.0 : a / b;
            else if (op == 15) r = b == 0.0 ? 0.0 : a - b * floor(a / b);
            else if (op == 16) r = xpow(a, b);
            else if (op == 17) r = a == b ? 1.0 : 0.0;
            else if (op == 18) r = a != b ? 1.0 : 0.0;
            else if (op == 19) r = a < b ? 1.0 : 0.0;
            else if (op == 20) r = a <= b ? 1.0 : 0.0;
            else if (op == 21) r = a > b ? 1.0 : 0.0;
            else r = a >= b ? 1.0 : 0.0;
            st[sp] = xsafe(r); nl[sp] = false; sp++;
        }
        else if (op == 23) { float v = nl[sp - 1] ? 0.0 : st[sp - 1]; st[sp - 1] = -v; nl[sp - 1] = false; }
        else if (op == 24) { float v = nl[sp - 1] ? 0.0 : st[sp - 1]; st[sp - 1] = v > 0.0 ? 0.0 : 1.0; nl[sp - 1] = false; }
        else if (op == 25) { float v = nl[sp - 1] ? 0.0 : st[sp - 1]; st[sp - 1] = v > 0.0 ? 1.0 : 0.0; nl[sp - 1] = false; }
        else if (op == 26) { nx = a0; }
        else if (op == 27) { sp--; float v = nl[sp] ? 0.0 : st[sp]; nx = v > 0.0 ? pc + 2 : a0; }
        else if (op == 28) { if (!nl[sp - 1]) nx = a0; else { sp--; nx = pc + 2; } }
        else if (op == 29) {
            float v = st[sp - 1]; bool vn = nl[sp - 1];
            sp = a0; st[sp] = v; nl[sp] = vn; sp++;
            nx = int(XPW(code + pc + 2));
        }
        else if (op == 30) {
            int k = xargs(a0);
            float ar[5];
            bool anyNil = false;
            for (int q = 4; q >= 0; q--) {
                if (q >= k) { ar[q] = 0.0; continue; }
                sp--;
                anyNil = anyNil || nl[sp];
                ar[q] = nl[sp] ? 0.0 : st[sp];
            }
            st[sp] = xsafe(xcall(a0, ar[0], ar[1], ar[2], ar[3], ar[4], anyNil)); nl[sp] = false; sp++;
            nx = pc + 2;
        }
        else if (op == 31) { break; }
        else if (op == 33) { st[sp - 1] = xwf(co + a0); nl[sp - 1] = false; nx = pc + 2; }
        // 32 (nop): nothing
        pc = nx;
    }
    if (sp == 0 || nl[sp - 1]) return 0.0;
    return st[sp - 1];
}
