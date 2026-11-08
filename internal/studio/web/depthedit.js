import { html, useEffect, useRef, useState } from './vendor/preact-htm.js';
import { Icon } from './icons.js';
import { t } from './i18n.js';
import { api } from './util.js';

// The depth editor: a background's depth map painted by hand over its picture (a still's one map,
// or each frame of a video's depth, one by one). The map is nearness 0 (far, black) … 1 (near,
// white), worked on as floats and kept as 16-bit (/api/depth/edit/map; the edits win over the
// worked out map). Tools: brush (with tips for textures: spray, particles, grain, clouds), blur,
// sharpen, smudge, gradient, eyedropper; how much of the map shows over the picture is only for
// looking (the map itself stays opaque).

const TOOLS = [
  { id: 'brush', icon: 'brush', key: 'b', label: 'Brush (B): paints a depth level; its tip can add textures' },
  { id: 'blur', icon: 'droplet', key: 'l', label: 'Blur (L): softens the depth where it passes' },
  { id: 'sharpen', icon: 'triangle', key: 'n', label: 'Sharpen (N): makes the depth\'s edges stand out' },
  { id: 'smudge', icon: 'pointer', key: 'm', label: 'Smudge (M): drags the depth along' },
  { id: 'gradient', icon: 'blend', key: 'g', label: 'Gradient (G): drag from one level to the other (a floor, a sky…)' },
  { id: 'picker', icon: 'pipette', key: 'i', label: 'Eyedropper (I, or Alt+click): takes the level from the map' },
  { id: 'hand', icon: 'hand', key: 'h', label: 'Move the view (H, or hold Space, or the middle button)' },
];
const TIPS = [['round', 'Round'], ['spray', 'Spray'], ['particles', 'Particles'], ['grain', 'Grain'], ['clouds', 'Clouds'], ['streaks', 'Streaks']];
const MODES = [['set', 'Paint the level'], ['nearer', 'Bring nearer'], ['farther', 'Push farther'],
  ['lift', 'Only bring nearer (up to the level)'], ['sink', 'Only push farther (down to the level)']];

const DEF = { size: 60, hardness: 0.5, opacity: 1, level: 1, level2: 0, amount: 0.1, tip: 'round', mode: 'set',
  density: 0.5, grain: 2, jitter: 0.2, scale: 40, blur: 6, sharpen: 1, strength: 0.6, pSize: true, pOpacity: false,
  show: 0.6, colors: false };
const loadOpts = () => { try { return { ...DEF, ...JSON.parse(localStorage.getItem('audioviz.depthedit') || '{}') }; } catch (e) { return { ...DEF }; } };

// --- small helpers
const clamp01 = (v) => (v < 0 ? 0 : v > 1 ? 1 : v);
const sstep = (a, b, x) => { const k = clamp01((x - a) / (b - a)); return k * k * (3 - 2 * k); };
// a random number from a seed (stable textures: the same spot always gives the same grain)
const hash2 = (x, y) => { let h = (x * 374761393 + y * 668265263) | 0; h = Math.imul(h ^ (h >>> 13), 1274126177); return ((h ^ (h >>> 16)) >>> 0) / 4294967296; };
const vnoise = (x, y) => {
  const ix = Math.floor(x), iy = Math.floor(y), fx = x - ix, fy = y - iy;
  const u = fx * fx * (3 - 2 * fx), v = fy * fy * (3 - 2 * fy);
  const a = hash2(ix, iy), b = hash2(ix + 1, iy), c = hash2(ix, iy + 1), d = hash2(ix + 1, iy + 1);
  return a + (b - a) * u + (c - a) * v + (a - b - c + d) * u * v;
};
const fbm = (x, y) => (vnoise(x, y) * 0.5 + vnoise(x * 2.03, y * 2.03) * 0.25 + vnoise(x * 4.1, y * 4.1) * 0.125 + vnoise(x * 8.3, y * 8.3) * 0.0625) / 0.9375;

// three box blurs of radius r (≈ gaussian) of src (w×h) into a new array
function blurred(src, w, h, r) {
  const a = Float32Array.from(src), b = new Float32Array(src.length);
  const pass = (s, d, n, stride, count, step) => {
    const k = 1 / (2 * r + 1);
    for (let c = 0; c < count; c++) {
      const base = c * step;
      let acc = 0;
      for (let i = -r; i <= r; i++) acc += s[base + Math.max(0, Math.min(n - 1, i)) * stride];
      for (let i = 0; i < n; i++) {
        d[base + i * stride] = acc * k;
        acc += s[base + Math.min(n - 1, i + r + 1) * stride] - s[base + Math.max(0, i - r) * stride];
      }
    }
  };
  for (let it = 0; it < 3; it++) { pass(a, b, w, 1, h, w); pass(b, a, h, w, w, 1); }
  return a;
}

// grey (or colors: far blue … near red) for each of 256 levels, as RGBA words
const LUT_GREY = new Uint32Array(256), LUT_COL = new Uint32Array(256);
for (let i = 0; i < 256; i++) {
  LUT_GREY[i] = (255 << 24) | (i << 16) | (i << 8) | i;
  // (turbo, approximated)
  const x = i / 255;
  const r = clamp01(0.1357 + x * (4.5974 - x * (42.3277 - x * (130.5887 - x * (150.5666 - x * 58.1375)))));
  const g = clamp01(0.0914 + x * (2.1856 + x * (4.8052 - x * (14.0195 - x * (4.2109 + x * 2.7747)))));
  const b = clamp01(0.1067 + x * (12.5925 - x * (60.1097 - x * (109.0745 - x * (88.5066 - x * 26.8183)))));
  LUT_COL[i] = (255 << 24) | (Math.round(b * 255) << 16) | (Math.round(g * 255) << 8) | Math.round(r * 255);
}

const qs = (o) => new URLSearchParams(o).toString();

// req: { image, source, model, detail, file, flow, all }; startFrame: the frame to open at
export function DepthEditor({ req, startFrame = 0, onClose, showErr }) {
  const [info, setInfo] = useState(null);
  const [frame, setFrame] = useState(startFrame);
  const [frameIn, setFrameIn] = useState(String(startFrame + 1));
  const [tool, setToolState] = useState('brush');
  const [o, setOptsState] = useState(loadOpts);
  const [view, setView] = useState({ z: 1, x: 0, y: 0 });
  const [loading, setLoading] = useState(true);
  const [dirty, setDirty] = useState(false);
  const [saving, setSaving] = useState(false);
  const [edited, setEdited] = useState([]);
  const [hist, setHist] = useState({ u: 0, r: 0 });
  const [cur, setCur] = useState(null); // the pointer over the picture (screen px) for the brush circle
  const [pick, setPick] = useState(null); // the level under the pointer
  const stageRef = useRef(), canvasRef = useRef();
  // the working state (not for rendering): the map, the stroke, undo
  const S = useRef({ w: 0, h: 0, map: null, img: null, ctx: null, undo: [], redo: [], dirtyR: null, raf: 0,
    stroke: null, blurCache: null, ver: 0, space: false, pan: null }).current;
  const oRef = useRef(o); oRef.current = o;
  const toolRef = useRef(tool); toolRef.current = tool;
  const viewRef = useRef(view); viewRef.current = view;
  const frameRef = useRef(frame); frameRef.current = frame;
  const dirtyRef = useRef(dirty); dirtyRef.current = dirty;
  const prevTool = useRef('brush');

  const setOpt = (k, v) => setOptsState((p) => {
    const n = { ...p, [k]: v };
    try { localStorage.setItem('audioviz.depthedit', JSON.stringify(n)); } catch (e) { /* ignore */ }
    return n;
  });
  const setTool = (id) => { if (id !== 'picker') prevTool.current = id; setToolState(id); };
  const base = { image: req.image, source: req.source || '', model: req.model || 'small', detail: req.detail || 'high',
    file: req.file || '', flow: req.flow || '', all: req.all ? '1' : '' };

  // --- drawing the map into the canvas (only what changed, once per screen frame)
  const touch = (x0, y0, x1, y1) => {
    x0 = Math.max(0, Math.floor(x0)); y0 = Math.max(0, Math.floor(y0));
    x1 = Math.min(S.w, Math.ceil(x1)); y1 = Math.min(S.h, Math.ceil(y1));
    if (x1 <= x0 || y1 <= y0) return;
    const r = S.dirtyR;
    S.dirtyR = r ? [Math.min(r[0], x0), Math.min(r[1], y0), Math.max(r[2], x1), Math.max(r[3], y1)] : [x0, y0, x1, y1];
    if (!S.raf) S.raf = requestAnimationFrame(paint);
  };
  const paint = () => {
    S.raf = 0;
    const r = S.dirtyR;
    S.dirtyR = null;
    if (!r || !S.ctx) return;
    const lut = oRef.current.colors ? LUT_COL : LUT_GREY;
    const px = S.px, w = S.w, map = S.map;
    for (let y = r[1]; y < r[3]; y++) {
      for (let x = r[0], i = y * w + r[0]; x < r[2]; x++, i++) px[i] = lut[(clamp01(map[i]) * 255 + 0.5) | 0];
    }
    S.ctx.putImageData(S.img, 0, 0, r[0], r[1], r[2] - r[0], r[3] - r[1]);
  };
  useEffect(() => { if (S.map) touch(0, 0, S.w, S.h); }, [o.colors]);

  // --- loading a frame (its map and its picture)
  const [picURL, setPicURL] = useState('');
  const loadFrame = async (i, orig) => {
    setLoading(true);
    try {
      const r = await fetch('/api/depth/edit/map?' + qs({ ...base, frame: i, orig: orig ? '1' : '' }));
      if (!r.ok) throw new Error(await r.text());
      const w = +r.headers.get('X-Width'), h = +r.headers.get('X-Height');
      const u16 = new Uint16Array(await r.arrayBuffer());
      const map = new Float32Array(w * h);
      for (let k = 0; k < map.length; k++) map[k] = u16[k] / 65535;
      const sizeChanged = w !== S.w || h !== S.h;
      Object.assign(S, { w, h, map, undo: [], redo: [], stroke: null, blurCache: null });
      S.ver++;
      const cv = canvasRef.current;
      cv.width = w; cv.height = h;
      S.ctx = cv.getContext('2d');
      S.img = S.ctx.createImageData(w, h);
      S.px = new Uint32Array(S.img.data.buffer);
      touch(0, 0, w, h);
      setHist({ u: 0, r: 0 });
      setDirty(!!orig);
      setPicURL('/api/depth/edit/image?' + qs({ image: req.image, frame: i, fps: info ? info.fps : 0, w, h }));
      if (sizeChanged) fit(w, h);
    } catch (e) { showErr(e); }
    setLoading(false);
  };
  const fit = (w = S.w, h = S.h) => {
    const el = stageRef.current;
    if (!el || !w) return;
    const z = Math.min(el.clientWidth / w, el.clientHeight / h) * 0.96;
    setView({ z, x: (el.clientWidth - w * z) / 2, y: (el.clientHeight - h * z) / 2 });
  };
  useEffect(() => {
    api('GET', '/api/depth/edit/info?' + qs(base)).then((r) => {
      if (!r.ready) { showErr(new Error(t('Work out the depth first'))); onClose(); return; }
      setInfo(r);
      setEdited(r.edited || []);
      const f = Math.max(0, Math.min(r.frames - 1, startFrame));
      setFrame(f); setFrameIn(String(f + 1));
    }).catch((e) => { showErr(e); onClose(); });
  }, []);
  useEffect(() => { if (info) loadFrame(frame); }, [info, frame]);

  // --- saving (the frame's map as 16-bit)
  const save = async () => {
    if (!S.map) return;
    setSaving(true);
    try {
      const u16 = new Uint16Array(S.map.length);
      for (let k = 0; k < u16.length; k++) u16[k] = (clamp01(S.map[k]) * 65535 + 0.5) | 0;
      const r = await fetch('/api/depth/edit/map?' + qs({ ...base, frame: frameRef.current, w: S.w, h: S.h }),
        { method: 'PUT', body: u16.buffer, headers: { 'Content-Type': 'application/octet-stream' } });
      if (!r.ok) throw new Error((await r.json().catch(() => ({}))).error || r.statusText);
      setDirty(false);
      setEdited((e) => (e.includes(frameRef.current) ? e : [...e, frameRef.current].sort((a, b) => a - b)));
    } catch (e) { showErr(e); setSaving(false); return false; }
    setSaving(false);
    return true;
  };
  const go = async (i) => {
    if (!info) return;
    i = Math.max(0, Math.min(info.frames - 1, i));
    setFrameIn(String(i + 1));
    if (i === frameRef.current) return;
    if (dirtyRef.current && !(await save())) return; // (each frame kept as you leave it)
    setFrame(i);
  };
  const close = async () => {
    if (dirtyRef.current && !(await save())) return;
    onClose();
  };
  const revertFrame = async () => {
    if (!confirm(t('Back to the depth that was worked out for this frame? Its hand edits are lost.'))) return;
    try {
      await api('DELETE', '/api/depth/edit/map?' + qs({ ...base, frame: frameRef.current }));
      setEdited((e) => e.filter((x) => x !== frameRef.current));
      await loadFrame(frameRef.current, false);
      setDirty(false);
    } catch (e) { showErr(e); }
  };
  const revertAll = async () => {
    if (!confirm(t('Back to the worked out depth on every frame? All the hand edits are lost.'))) return;
    try {
      await api('DELETE', '/api/depth/edit/map?' + qs({ ...base, frame: 'all' }));
      setEdited([]);
      await loadFrame(frameRef.current, false);
      setDirty(false);
    } catch (e) { showErr(e); }
  };
  // the previous (or next) frame's map on this one (to carry an edit along)
  const copyFrom = async (j) => {
    if (!info || j < 0 || j >= info.frames) return;
    try {
      const r = await fetch('/api/depth/edit/map?' + qs({ ...base, frame: j }));
      if (!r.ok) throw new Error(await r.text());
      const w = +r.headers.get('X-Width'), h = +r.headers.get('X-Height');
      if (w !== S.w || h !== S.h) throw new Error(t('The frames are not the same size'));
      const u16 = new Uint16Array(await r.arrayBuffer());
      const before = Float32Array.from(S.map);
      for (let k = 0; k < S.map.length; k++) S.map[k] = u16[k] / 65535;
      pushUndo(0, 0, S.w, S.h, before);
      touch(0, 0, S.w, S.h);
    } catch (e) { showErr(e); }
  };

  // --- undo (the region a stroke changed, before and after)
  const pushUndo = (x0, y0, x1, y1, before /* whole map before */) => {
    const rw = x1 - x0, rh = y1 - y0;
    if (rw <= 0 || rh <= 0) return;
    const a = new Float32Array(rw * rh), b = new Float32Array(rw * rh);
    for (let y = 0; y < rh; y++) {
      const o0 = (y0 + y) * S.w + x0;
      a.set(before.subarray(o0, o0 + rw), y * rw);
      b.set(S.map.subarray(o0, o0 + rw), y * rw);
    }
    S.undo.push({ x0, y0, rw, rh, a, b });
    let bytes = 0;
    for (let k = S.undo.length - 1; k >= 0; k--) { bytes += S.undo[k].a.length * 8; if (bytes > 400e6) { S.undo.splice(0, k + 1); break; } }
    S.redo = [];
    S.blurCache = null; S.ver++;
    setHist({ u: S.undo.length, r: 0 });
    setDirty(true);
  };
  const applyStep = (st, which) => {
    const src = st[which];
    for (let y = 0; y < st.rh; y++) S.map.set(src.subarray(y * st.rw, (y + 1) * st.rw), (st.y0 + y) * S.w + st.x0);
    touch(st.x0, st.y0, st.x0 + st.rw, st.y0 + st.rh);
    S.blurCache = null; S.ver++;
    setDirty(true);
  };
  const undo = () => { const st = S.undo.pop(); if (!st) return; applyStep(st, 'a'); S.redo.push(st); setHist({ u: S.undo.length, r: S.redo.length }); };
  const redo = () => { const st = S.redo.pop(); if (!st) return; applyStep(st, 'b'); S.undo.push(st); setHist({ u: S.undo.length, r: S.redo.length }); };

  // --- sampling the map
  const levelAt = (x, y) => {
    let s = 0, n = 0;
    for (let dy = -1; dy <= 1; dy++) for (let dx = -1; dx <= 1; dx++) {
      const xx = Math.round(x) + dx, yy = Math.round(y) + dy;
      if (xx >= 0 && yy >= 0 && xx < S.w && yy < S.h) { s += S.map[yy * S.w + xx]; n++; }
    }
    return n ? s / n : 0;
  };

  // --- strokes. A stroke keeps the map as it was (base), how much each pixel is covered (cov,
  // at most the opacity: going over a spot again in the same stroke adds nothing) and where each
  // pixel goes (tgt); the map is base → tgt by cov (lift / sink: only one way)
  const begin = (x, y, pr) => {
    const op = oRef.current, tl = toolRef.current;
    const n = S.w * S.h;
    // (the stroke's buffers, made once for the map's size)
    if (!S.bufs || S.bufs.n !== n) S.bufs = { n, base: new Float32Array(n), cov: new Float32Array(n), tgt: new Float32Array(n) };
    else S.bufs.cov.fill(0);
    S.bufs.base.set(S.map);
    const st = { tool: tl, base: S.bufs.base, cov: S.bufs.cov, tgt: null, last: null, bb: [1e9, 1e9, -1, -1],
      seed: (Math.random() * 1e6) | 0, start: [x, y], acc: 0 };
    if (tl === 'blur' || tl === 'sharpen') {
      const r = tl === 'blur' ? Math.max(1, Math.round(op.blur)) : 2;
      const key = S.ver + '|' + r;
      let bl = S.blurCache && S.blurCache.key === key ? S.blurCache.v : null;
      if (!bl) { bl = blurred(st.base, S.w, S.h, r); S.blurCache = { key, v: bl }; }
      if (tl === 'blur') st.tgt = bl;
      else {
        st.tgt = S.bufs.tgt;
        const k = op.sharpen;
        for (let i = 0; i < n; i++) st.tgt[i] = clamp01(st.base[i] + k * (st.base[i] - bl[i]));
      }
    } else if (tl === 'brush') st.tgt = S.bufs.tgt;
    S.stroke = st;
    if (tl === 'gradient') { gradient(x, y); return; }
    dabTo(x, y, pr);
  };
  // dabs along the way from the last point to (x, y), every quarter of the radius
  const dabTo = (x, y, pr) => {
    const st = S.stroke, op = oRef.current;
    const r = Math.max(0.5, (op.size / 2) * (op.pSize ? pr : 1));
    if (!st.last) { dab(x, y, r, pr); st.last = [x, y]; return; }
    const [lx, ly] = st.last, d = Math.hypot(x - lx, y - ly);
    const step = Math.max(0.5, r * (st.tool === 'smudge' ? 0.15 : 0.25));
    st.acc += d;
    if (st.acc < step) return;
    const k = Math.floor(st.acc / step);
    for (let j = 1; j <= k; j++) {
      const f = (j * step - (st.acc - d)) / d;
      if (f < 0 || f > 1) continue;
      const px = lx + (x - lx) * f, py = ly + (y - ly) * f;
      if (st.tool === 'smudge') smudge(px, py, r, st.prevDab || st.last);
      else dab(px, py, r, pr);
      st.prevDab = [px, py];
    }
    st.acc -= k * step;
    st.last = [x, y];
  };
  const grow = (st, x0, y0, x1, y1) => {
    st.bb[0] = Math.min(st.bb[0], x0); st.bb[1] = Math.min(st.bb[1], y0);
    st.bb[2] = Math.max(st.bb[2], x1); st.bb[3] = Math.max(st.bb[3], y1);
  };
  // one dab of the brush / blur / sharpen at (cx, cy), radius r
  const dab = (cx, cy, r, pr) => {
    const st = S.stroke, op = oRef.current, w = S.w, h = S.h;
    if (st.tool === 'smudge') return;
    const opac = op.opacity * (op.pOpacity ? pr : 1) * (st.tool === 'brush' ? 1 : op.strength);
    const hard = Math.min(0.99, op.hardness);
    const tip = st.tool === 'brush' ? op.tip : 'round';
    const mode = op.mode;
    // where a covered pixel goes (brush): the level, or the map moved nearer / farther; tex is
    // the texture there (-1 … 1)
    const target = (i, tex, lvl) => {
      if (mode === 'nearer') return st.base[i] + op.amount * (1 + tex * op.jitter);
      if (mode === 'farther') return st.base[i] - op.amount * (1 + tex * op.jitter);
      return lvl + tex * op.jitter;
    };
    const cover = (i, c, tv) => {
      if (c <= st.cov[i]) return;
      st.cov[i] = c;
      if (st.tgt && st.tool === 'brush') st.tgt[i] = clamp01(tv);
    };
    const x0 = Math.max(0, Math.floor(cx - r)), y0 = Math.max(0, Math.floor(cy - r));
    const x1 = Math.min(w, Math.ceil(cx + r + 1)), y1 = Math.min(h, Math.ceil(cy + r + 1));
    if (x1 <= x0 || y1 <= y0) return;
    if (tip === 'spray' || tip === 'particles') {
      // random dots (spray: tiny, many; particles: round blobs of several sizes and levels)
      const area = Math.PI * r * r;
      const g = Math.max(0.5, op.grain);
      const count = tip === 'spray' ? Math.max(1, Math.round(op.density * area / (g * g * 6)))
        : Math.max(1, Math.round(op.density * 6));
      for (let k = 0; k < count; k++) {
        const ang = Math.random() * Math.PI * 2;
        const rad = r * Math.sqrt(Math.random()) * (tip === 'spray' ? Math.sqrt(Math.random()) * 1.15 : 1);
        const px = cx + Math.cos(ang) * rad, py = cy + Math.sin(ang) * rad;
        const pr2 = tip === 'spray' ? g * (0.5 + Math.random()) : Math.max(1, g * 4 * (0.35 + Math.random() * 0.9));
        const lvl = op.level + (Math.random() * 2 - 1) * (tip === 'particles' ? op.jitter : 0);
        const fall = tip === 'spray' ? 1 - sstep(0.6, 1, rad / r) : 1;
        const bx0 = Math.max(0, Math.floor(px - pr2)), by0 = Math.max(0, Math.floor(py - pr2));
        const bx1 = Math.min(w, Math.ceil(px + pr2 + 1)), by1 = Math.min(h, Math.ceil(py + pr2 + 1));
        for (let y = by0; y < by1; y++) for (let x = bx0; x < bx1; x++) {
          const d = Math.hypot(x + 0.5 - px, y + 0.5 - py) / pr2;
          if (d >= 1) continue;
          const c = opac * fall * (1 - sstep(hard, 1, d));
          const i = y * w + x;
          cover(i, c, target(i, tip === 'particles' ? 0 : (Math.random() * 2 - 1) * 0.5, lvl));
        }
      }
    } else {
      for (let y = y0; y < y1; y++) for (let x = x0; x < x1; x++) {
        const d = Math.hypot(x + 0.5 - cx, y + 0.5 - cy) / r;
        if (d >= 1) continue;
        const i = y * w + x;
        let c = opac * (1 - sstep(hard, 1, d));
        let tex = 0;
        if (tip === 'grain') tex = hash2(x + st.seed, y) * 2 - 1;
        else if (tip === 'clouds') tex = fbm(x / op.scale + st.seed % 997, y / op.scale) * 2 - 1;
        else if (tip === 'streaks') { tex = vnoise(x / (op.scale * 0.15) + st.seed % 997, y / (op.scale * 3)) * 2 - 1; c *= 0.55 + 0.45 * (tex * 0.5 + 0.5); }
        cover(i, c, target(i, tex, op.level));
      }
    }
    // the map in this dab's box: base → tgt by cov
    for (let y = y0; y < y1; y++) for (let x = x0, i = y * w + x0; x < x1; x++, i++) {
      const c = st.cov[i];
      if (c <= 0) continue;
      const b = st.base[i];
      let v = b + (st.tgt[i] - b) * c;
      if (st.tool === 'brush') {
        if (mode === 'lift') v = Math.max(b, Math.min(v, Math.max(b, op.level)));
        else if (mode === 'sink') v = Math.min(b, Math.max(v, Math.min(b, op.level)));
      }
      S.map[i] = clamp01(v);
    }
    grow(st, x0, y0, x1, y1);
    touch(x0, y0, x1, y1);
  };
  // smudge: the depth under the brush pushed from where it was a moment ago
  const smudge = (cx, cy, r, from) => {
    const st = S.stroke, op = oRef.current, w = S.w, h = S.h;
    const dx = cx - from[0], dy = cy - from[1];
    const x0 = Math.max(0, Math.floor(cx - r)), y0 = Math.max(0, Math.floor(cy - r));
    const x1 = Math.min(w, Math.ceil(cx + r + 1)), y1 = Math.min(h, Math.ceil(cy + r + 1));
    if (x1 <= x0 || y1 <= y0) return;
    const sx0 = Math.max(0, Math.floor(x0 - dx - 1)), sy0 = Math.max(0, Math.floor(y0 - dy - 1));
    const sx1 = Math.min(w, Math.ceil(x1 - dx + 1)), sy1 = Math.min(h, Math.ceil(y1 - dy + 1));
    const sw = sx1 - sx0, sh = sy1 - sy0;
    if (sw <= 0 || sh <= 0) return;
    const src = new Float32Array(sw * sh);
    for (let y = 0; y < sh; y++) src.set(S.map.subarray((sy0 + y) * w + sx0, (sy0 + y) * w + sx1), y * sw);
    const at = (x, y) => {
      x = Math.max(0, Math.min(sw - 1, x - sx0)); y = Math.max(0, Math.min(sh - 1, y - sy0));
      const ix = Math.floor(x), iy = Math.floor(y), fx = x - ix, fy = y - iy;
      const ix1 = Math.min(sw - 1, ix + 1), iy1 = Math.min(sh - 1, iy + 1);
      const a = src[iy * sw + ix] + (src[iy * sw + ix1] - src[iy * sw + ix]) * fx;
      const b = src[iy1 * sw + ix] + (src[iy1 * sw + ix1] - src[iy1 * sw + ix]) * fx;
      return a + (b - a) * fy;
    };
    const hard = Math.min(0.99, op.hardness), k0 = op.strength * op.opacity;
    for (let y = y0; y < y1; y++) for (let x = x0; x < x1; x++) {
      const d = Math.hypot(x + 0.5 - cx, y + 0.5 - cy) / r;
      if (d >= 1) continue;
      const k = k0 * (1 - sstep(hard, 1, d));
      const i = y * w + x;
      S.map[i] += (at(x - dx, y - dy) - S.map[i]) * k;
    }
    grow(st, x0, y0, x1, y1);
    touch(x0, y0, x1, y1);
  };
  // the gradient from the stroke's start to (x, y): level → level 2, over the whole map
  const gradient = (x, y) => {
    const st = S.stroke, op = oRef.current, w = S.w, h = S.h;
    const [ax, ay] = st.start, vx = x - ax, vy = y - ay, L2 = vx * vx + vy * vy;
    st.end = [x, y];
    const mode = op.mode;
    for (let yy = 0; yy < h; yy++) for (let xx = 0, i = yy * w; xx < w; xx++, i++) {
      const f = L2 < 1 ? 0 : clamp01(((xx + 0.5 - ax) * vx + (yy + 0.5 - ay) * vy) / L2);
      const tv = op.level + (op.level2 - op.level) * f, b = st.base[i];
      let v = b + (tv - b) * op.opacity;
      if (mode === 'lift' || mode === 'nearer') v = Math.max(b, v);
      else if (mode === 'sink' || mode === 'farther') v = Math.min(b, v);
      S.map[i] = v;
    }
    grow(st, 0, 0, w, h);
    touch(0, 0, w, h);
    setGradLine([ax, ay, x, y]);
  };
  const [gradLine, setGradLine] = useState(null);
  const end = () => {
    const st = S.stroke;
    S.stroke = null;
    setGradLine(null);
    if (!st || st.bb[2] < 0) return;
    pushUndo(Math.max(0, st.bb[0]), Math.max(0, st.bb[1]), Math.min(S.w, st.bb[2]), Math.min(S.h, st.bb[3]), st.base);
  };

  // --- pointer: screen → map px
  const toMap = (e) => {
    const r = canvasRef.current.getBoundingClientRect();
    return [(e.clientX - r.left) / r.width * S.w, (e.clientY - r.top) / r.height * S.h];
  };
  const onDown = (e) => {
    if (!S.map || loading) return;
    const tl = toolRef.current;
    if (e.button === 1 || tl === 'hand' || S.space) {
      e.preventDefault();
      S.pan = { x: e.clientX, y: e.clientY, v: viewRef.current };
      e.currentTarget.setPointerCapture(e.pointerId);
      return;
    }
    if (e.button !== 0) return;
    const [x, y] = toMap(e);
    if (tl === 'picker' || e.altKey) {
      setOpt('level', +levelAt(x, y).toFixed(3));
      if (tl === 'picker') setToolState(prevTool.current);
      return;
    }
    e.currentTarget.setPointerCapture(e.pointerId);
    begin(x, y, e.pointerType === 'pen' ? Math.max(0.05, e.pressure || 0.5) : 1);
  };
  const onMove = (e) => {
    const r = stageRef.current.getBoundingClientRect();
    setCur({ x: e.clientX - r.left, y: e.clientY - r.top });
    if (S.pan) {
      const p = S.pan;
      setView({ ...p.v, x: p.v.x + e.clientX - p.x, y: p.v.y + e.clientY - p.y });
      return;
    }
    if (S.map) { const [mx, my] = toMap(e); setPick(mx >= 0 && my >= 0 && mx < S.w && my < S.h ? levelAt(mx, my) : null); }
    if (!S.stroke) return;
    const evs = e.getCoalescedEvents ? e.getCoalescedEvents() : [e];
    for (const ev of (evs.length ? evs : [e])) {
      const [x, y] = toMap(ev);
      if (S.stroke.tool === 'gradient') { gradient(x, y); continue; }
      dabTo(x, y, ev.pointerType === 'pen' ? Math.max(0.05, ev.pressure || 0.5) : 1);
    }
  };
  const onUp = () => { if (S.pan) { S.pan = null; return; } end(); };
  const onWheel = (e) => {
    e.preventDefault();
    const r = stageRef.current.getBoundingClientRect();
    const mx = e.clientX - r.left, my = e.clientY - r.top;
    const v = viewRef.current;
    const z = Math.max(0.05, Math.min(32, v.z * Math.pow(1.0015, -e.deltaY)));
    setView({ z, x: mx - (mx - v.x) * z / v.z, y: my - (my - v.y) * z / v.z });
  };
  useEffect(() => {
    const el = stageRef.current;
    el.addEventListener('wheel', onWheel, { passive: false });
    return () => el.removeEventListener('wheel', onWheel);
  }, []);

  // --- keys
  useEffect(() => {
    const kd = (e) => {
      if (e.target.closest && e.target.closest('input, textarea, select')) { if (e.key !== 'Escape') return; }
      e.stopPropagation(); // (the studio's own keys wait while the editor is open)
      const k = e.key.toLowerCase();
      if ((e.ctrlKey || e.metaKey) && k === 'z') { e.preventDefault(); e.shiftKey ? redo() : undo(); return; }
      if ((e.ctrlKey || e.metaKey) && k === 'y') { e.preventDefault(); redo(); return; }
      if ((e.ctrlKey || e.metaKey) && k === 's') { e.preventDefault(); save(); return; }
      if (e.ctrlKey || e.metaKey || e.altKey) return;
      if (e.key === 'Escape') { e.preventDefault(); close(); return; }
      if (e.key === ' ') { S.space = true; e.preventDefault(); return; }
      if (e.key === '[') { setOpt('size', Math.max(1, Math.round(oRef.current.size / 1.2))); return; }
      if (e.key === ']') { setOpt('size', Math.min(1000, Math.round(oRef.current.size * 1.2 + 1))); return; }
      if (e.key === 'ArrowLeft' || e.key === ',') { e.preventDefault(); go(frameRef.current - 1); return; }
      if (e.key === 'ArrowRight' || e.key === '.') { e.preventDefault(); go(frameRef.current + 1); return; }
      if (e.key === '0') { fit(); return; }
      const tl = TOOLS.find((x) => x.key === k);
      if (tl) setTool(tl.id);
    };
    const ku = (e) => { if (e.key === ' ') S.space = false; e.stopPropagation(); };
    addEventListener('keydown', kd, true);
    addEventListener('keyup', ku, true);
    return () => { removeEventListener('keydown', kd, true); removeEventListener('keyup', ku, true); };
  }, [info]);

  // --- the panel
  const n = info ? info.frames : 1;
  const video = n > 1;
  const range = (k, label, min, max, step, fmt) => html`<label title=${t(label)}>${t(label)}
    <input type="range" min=${min} max=${max} step=${step} value=${o[k]} onInput=${(e) => setOpt(k, +e.target.value)} />
    <input class="de-num mono" type="number" min=${min} max=${max} step=${step} value=${fmt ? fmt(o[k]) : o[k]}
      onChange=${(e) => { const v = +e.target.value; if (isFinite(v)) setOpt(k, Math.max(min, Math.min(max, v))); }} /></label>`;
  const levelCtl = (k, label) => html`<label class="de-level" title=${t('0 = the farthest (black), 1 = the nearest (white)')}>${t(label)}
    <span class="de-swatch" style=${{ background: `rgb(${Math.round(o[k] * 255)},${Math.round(o[k] * 255)},${Math.round(o[k] * 255)})` }}></span>
    <input type="range" class="de-grey" min="0" max="1" step="0.001" value=${o[k]} onInput=${(e) => setOpt(k, +e.target.value)} />
    <input class="de-num mono" type="number" min="0" max="1" step="0.01" value=${(+o[k]).toFixed(3)}
      onChange=${(e) => { const v = +e.target.value; if (isFinite(v)) setOpt(k, clamp01(v)); }} /></label>`;
  const sizeCtl = html`${range('size', 'Size', 1, 600, 1)}${range('hardness', 'Hardness', 0, 1, 0.01)}`;
  const pen = html`<span class="faint">${t('Pen')}:</span>
    <button class=${'btn sm' + (o.pSize ? ' on' : '')} title=${t('The pen\'s pressure sets the size')} onClick=${() => setOpt('pSize', !o.pSize)}>${t('size')}</button>
    <button class=${'btn sm' + (o.pOpacity ? ' on' : '')} title=${t('The pen\'s pressure sets the opacity')} onClick=${() => setOpt('pOpacity', !o.pOpacity)}>${t('opacity')}</button>`;
  let opts = null;
  if (tool === 'brush') {
    opts = html`
      <div class="de-row">${sizeCtl}${range('opacity', 'Opacity', 0.01, 1, 0.01)}${pen}</div>
      <div class="de-row">
        <label>${t('Paints')}<select value=${o.mode} onChange=${(e) => setOpt('mode', e.target.value)}>
          ${MODES.map(([v, l]) => html`<option value=${v}>${t(l)}</option>`)}</select></label>
        ${o.mode === 'nearer' || o.mode === 'farther' ? range('amount', 'How much', 0.005, 0.5, 0.005) : levelCtl('level', 'Level')}
        <label>${t('Tip')}<select value=${o.tip} onChange=${(e) => setOpt('tip', e.target.value)}>
          ${TIPS.map(([v, l]) => html`<option value=${v}>${t(l)}</option>`)}</select></label>
        ${(o.tip === 'spray' || o.tip === 'particles') && range('density', 'Density', 0.02, 1, 0.01)}
        ${(o.tip === 'spray' || o.tip === 'particles') && range('grain', o.tip === 'spray' ? 'Dot size' : 'Particle size', 0.5, 20, 0.5)}
        ${(o.tip === 'clouds' || o.tip === 'streaks') && range('scale', 'Texture size', 4, 300, 1)}
        ${o.tip !== 'round' && o.tip !== 'spray' && range('jitter', o.tip === 'particles' ? 'Level variation' : 'Texture depth', 0, 0.5, 0.005)}
      </div>`;
  } else if (tool === 'blur') {
    opts = html`<div class="de-row">${sizeCtl}${range('strength', 'Strength', 0.01, 1, 0.01)}${range('blur', 'Blur radius', 1, 60, 1)}${pen}</div>`;
  } else if (tool === 'sharpen') {
    opts = html`<div class="de-row">${sizeCtl}${range('strength', 'Strength', 0.01, 1, 0.01)}${range('sharpen', 'Amount', 0.1, 4, 0.05)}${pen}</div>`;
  } else if (tool === 'smudge') {
    opts = html`<div class="de-row">${sizeCtl}${range('strength', 'Strength', 0.01, 1, 0.01)}${pen}</div>`;
  } else if (tool === 'gradient') {
    opts = html`<div class="de-row">${levelCtl('level', 'From')}${levelCtl('level2', 'To')}${range('opacity', 'Opacity', 0.01, 1, 0.01)}
      <label>${t('Paints')}<select value=${o.mode === 'lift' || o.mode === 'sink' ? o.mode : 'set'} onChange=${(e) => setOpt('mode', e.target.value)}>
        ${MODES.filter(([v]) => v === 'set' || v === 'lift' || v === 'sink').map(([v, l]) => html`<option value=${v}>${t(l)}</option>`)}</select></label></div>`;
  } else if (tool === 'picker') {
    opts = html`<div class="de-row"><span class="faint">${t('Click on the map to take its level for the brush')}</span>${levelCtl('level', 'Level')}</div>`;
  } else {
    opts = html`<div class="de-row"><span class="faint">${t('Drag to move; the wheel zooms')}</span></div>`;
  }
  const brushTool = ['brush', 'blur', 'sharpen', 'smudge'].includes(tool);
  const csz = o.size * view.z;
  const pct = n > 1 ? (i) => (i / (n - 1)) * 100 : () => 0;
  return html`<div class="depthed">
    <div class="de-top">
      <${Icon} name="layers" size=${16} cls="muted" /><b>${t('Depth editor')}</b>
      ${video && html`<div class="de-frames">
        <button class="btn icon sm ghost" title=${t('First frame')} onClick=${() => go(0)}><${Icon} name="chevron-first" size=${15} /></button>
        <button class="btn icon sm ghost" title=${t('Previous frame (← or ,)')} onClick=${() => go(frame - 1)}><${Icon} name="chevron-left" size=${15} /></button>
        <span class="faint">${t('Frame')}</span>
        <input class="de-num mono" type="number" min="1" max=${n} value=${frameIn} onInput=${(e) => setFrameIn(e.target.value)}
          onChange=${(e) => go((+e.target.value || 1) - 1)} />
        <span class="faint">/ ${n}</span>
        <button class="btn icon sm ghost" title=${t('Next frame (→ or .)')} onClick=${() => go(frame + 1)}><${Icon} name="chevron-right" size=${15} /></button>
        <button class="btn icon sm ghost" title=${t('Last frame')} onClick=${() => go(n - 1)}><${Icon} name="chevron-last" size=${15} /></button>
        <button class="btn sm ghost" title=${t('This frame takes the previous frame\'s map (to carry an edit along; undo takes it back)')}
          disabled=${frame === 0} onClick=${() => copyFrom(frame - 1)}><${Icon} name="copy" size=${13} /> ${t('Previous one\'s map')}</button>
      </div>`}
      <span class="spacer"></span>
      <label class="de-show" title=${t('How much of the depth shows over the picture (only to see it here: the map stays as it is)')}>
        <${Icon} name="eye" size=${14} />${t('Map seen')}
        <input type="range" min="0" max="1" step="0.01" value=${o.show} onInput=${(e) => setOpt('show', +e.target.value)} />
        <span class="mono faint">${Math.round(o.show * 100)}%</span></label>
      <button class=${'btn sm' + (o.colors ? ' on' : '')} title=${t('Colors instead of grey (blue far, red near): small steps show better')}
        onClick=${() => setOpt('colors', !o.colors)}><${Icon} name="sun" size=${13} /> ${t('Colors')}</button>
      <button class="btn icon sm ghost" title=${t('Fit (0)')} onClick=${() => fit()}><${Icon} name="maximize-2" size=${14} /></button>
      <button class="btn icon sm ghost" disabled=${!hist.u} title=${t('Undo (Ctrl+Z)')} onClick=${undo}><${Icon} name="undo-2" size=${15} /></button>
      <button class="btn icon sm ghost" disabled=${!hist.r} title=${t('Redo (Ctrl+Shift+Z)')} onClick=${redo}><${Icon} name="redo-2" size=${15} /></button>
      <button class="btn sm ghost" disabled=${!edited.includes(frame)} title=${t('Back to the depth that was worked out for this frame')} onClick=${revertFrame}>
        <${Icon} name="rotate-ccw" size=${13} /> ${t(video ? 'Revert frame' : 'Revert')}</button>
      ${video && html`<button class="btn sm ghost danger" disabled=${!edited.length} title=${t('Back to the worked out depth on every frame')} onClick=${revertAll}>
        <${Icon} name="trash-2" size=${13} /> ${t('Revert all')}</button>`}
      <button class="btn sm primary" disabled=${!dirty || saving} title=${t('Keep this frame\'s map (Ctrl+S; it is also kept when you go to another frame or close)')} onClick=${save}>
        <${Icon} name="save" size=${13} /> ${t(saving ? 'Saving…' : dirty ? 'Save' : 'Saved')}</button>
      <button class="btn icon ghost" title=${t('Close (Esc; the changes are kept)')} onClick=${close}><${Icon} name="x" size=${16} /></button>
    </div>
    <div class="de-opts">${opts}</div>
    <div class="de-main">
      <div class="de-tools">${TOOLS.map((x) => html`<button class=${'btn icon' + (tool === x.id ? ' on' : ' ghost')} title=${t(x.label)}
        onClick=${() => setTool(x.id)}><${Icon} name=${x.icon} size=${17} /></button>`)}
        <div class="de-curlevel" title=${t('The brush\'s level')} style=${{ background: `rgb(${Math.round(o.level * 255)},${Math.round(o.level * 255)},${Math.round(o.level * 255)})` }}></div>
      </div>
      <div class=${'de-stage' + (tool === 'hand' ? ' pan' : '') + (brushTool ? ' brush' : '')} ref=${stageRef}
        onPointerDown=${onDown} onPointerMove=${onMove} onPointerUp=${onUp} onPointerCancel=${onUp}
        onPointerLeave=${() => { setCur(null); setPick(null); }} onContextMenu=${(e) => e.preventDefault()}>
        <div class="de-world" style=${{ width: (S.w || 0) + 'px', height: (S.h || 0) + 'px', transform: `translate(${view.x}px,${view.y}px) scale(${view.z})` }}>
          ${picURL && html`<img class="de-pic" src=${picURL} draggable="false" />`}
          <canvas ref=${canvasRef} class=${'de-map' + (view.z > 2 ? ' px' : '')} style=${{ opacity: o.show }}></canvas>
          ${gradLine && html`<svg class="de-grad" viewBox=${`0 0 ${S.w} ${S.h}`}><line x1=${gradLine[0]} y1=${gradLine[1]} x2=${gradLine[2]} y2=${gradLine[3]}
            stroke="#fff" stroke-width=${2 / view.z} stroke-dasharray=${`${6 / view.z} ${4 / view.z}`} /></svg>`}
        </div>
        ${cur && brushTool && html`<div class="brushcur" style=${{ left: cur.x - csz / 2 + 'px', top: cur.y - csz / 2 + 'px', width: csz + 'px', height: csz + 'px' }}></div>`}
        ${loading && html`<div class="de-loading faint"><${Icon} name="loader-circle" size=${16} cls="spin" /> ${t('Loading…')}</div>`}
        <div class="de-status mono faint">${pick != null ? `${t('Level')} ${pick.toFixed(3)}` : ''} · ${Math.round(view.z * 100)}%${info ? ` · ${S.w}×${S.h}` : ''}</div>
      </div>
    </div>
    ${video && html`<div class="de-strip" title=${t('Marks: the frames edited by hand')}>
      <input type="range" min="0" max=${n - 1} step="1" value=${frame} onChange=${(e) => go(+e.target.value)} onInput=${(e) => setFrameIn(String(+e.target.value + 1))} />
      <div class="de-marks">${edited.map((i) => html`<i style=${{ left: pct(i) + '%' }} onClick=${() => go(i)}></i>`)}</div>
    </div>`}
  </div>`;
}
