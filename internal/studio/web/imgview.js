// Images: one image big over the whole window (zoom, pan and turn the view; the previous / next
// one), and an editor: turn, flip and crop it, paint on it with brushes (paint, restore the
// original, blur, sharpen, smudge, lighten / darken, clone) and its light and color; the result is
// saved as a new image in the project (assets/), the original untouched.
import { html, useEffect, useLayoutEffect, useRef, useState } from './vendor/preact-htm.js';
import { Icon } from './icons.js';
import { Select } from './fields.js';
import { t } from './i18n.js';
import { basename } from './util.js';

const media = (p) => '/api/media?path=' + encodeURIComponent(p);
const ZMIN = 0.02, ZMAX = 40;
const UNDO_MAX = 24;

// the light and color settings: key, label, range (0 = unchanged)
const ADJ = [['exposure', 'Exposure', -1, 1], ['contrast', 'Contrast', -1, 1], ['saturation', 'Saturation', -1, 1],
  ['warmth', 'Warmth', -1, 1], ['tint', 'Tint', -1, 1], ['hue', 'Hue', -180, 180]];
const ADJ0 = { exposure: 0, contrast: 0, saturation: 0, warmth: 0, tint: 0, hue: 0 };
const adjOn = (a) => ADJ.some(([k]) => a[k]);
const CROP_ASPECTS = [['free', 'Free'], ['orig', 'Original'], ['video', 'Like the video'], ['16:9', '16:9'], ['1:1', '1:1'], ['9:16', '9:16'], ['4:3', '4:3'], ['3:4', '3:4']];

// the editor's tools (key: its shortcut)
const TOOLS = [
  { id: 'hand', icon: 'hand', key: 'h', label: 'Move the view (H, or hold Space, or the middle button)' },
  { id: 'brush', icon: 'brush', key: 'b', label: 'Brush (B): paints a color (Alt+click: take the color)' },
  { id: 'eraser', icon: 'eraser', key: 'e', label: 'Restore (E): paints the original image back' },
  { id: 'blur', icon: 'droplet', key: 'l', label: 'Blur (L): softens where it passes' },
  { id: 'sharpen', icon: 'triangle', key: 'n', label: 'Sharpen (N): makes details and edges stand out' },
  { id: 'smudge', icon: 'pointer', key: 'm', label: 'Smudge (M): drags the colors along, like a finger on wet paint' },
  { id: 'dodge', icon: 'sun', key: 'o', label: 'Lighten / darken (O): brightens or darkens where it passes (Alt: the other)' },
  { id: 'clone', icon: 'copy', key: 's', label: 'Clone (S): Alt+click where to copy from, then paint it elsewhere' },
  { id: 'picker', icon: 'pipette', key: 'i', label: 'Eyedropper (I, or Alt+click with the brush): takes a color' },
  { id: 'crop', icon: 'crop', key: 'c', label: 'Crop (C)' },
];
const PAINT = new Set(['brush', 'eraser', 'blur', 'sharpen', 'smudge', 'dodge', 'clone']);
const BRUSH0 = { size: 40, hard: 0.6, strength: 0.6, color: '#ffffff', dodge: 'lighten' };
const loadBrush = () => { try { return { ...BRUSH0, ...JSON.parse(localStorage.getItem('audioviz.iv.brush') || '{}') }; } catch (e) { return { ...BRUSH0 }; } };

// ---- canvases
function canvas(w, h) {
  const c = document.createElement('canvas');
  c.width = Math.max(1, Math.round(w));
  c.height = Math.max(1, Math.round(h));
  return c;
}
const ctx2d = (c) => c.getContext('2d', { willReadFrequently: true });
function copyCanvas(src) {
  const c = canvas(src.width, src.height);
  ctx2d(c).drawImage(src, 0, 0);
  return c;
}
// src turned a quarter clockwise (dir 1) or counterclockwise (-1)
function turned(src, dir) {
  const c = canvas(src.height, src.width);
  const x = ctx2d(c);
  x.translate(c.width / 2, c.height / 2);
  x.rotate(dir * Math.PI / 2);
  x.drawImage(src, -src.width / 2, -src.height / 2);
  return c;
}
function flipped(src, horiz) {
  const c = canvas(src.width, src.height);
  const x = ctx2d(c);
  x.translate(horiz ? c.width : 0, horiz ? 0 : c.height);
  x.scale(horiz ? -1 : 1, horiz ? 1 : -1);
  x.drawImage(src, 0, 0);
  return c;
}
// the part of src inside crop rect cr (0…1)
function cropped(src, cr) {
  const x0 = Math.round(cr.x * src.width), y0 = Math.round(cr.y * src.height);
  const c = canvas(Math.max(1, Math.round(cr.w * src.width)), Math.max(1, Math.round(cr.h * src.height)));
  ctx2d(c).drawImage(src, -x0, -y0);
  return c;
}

// ---- light and color: one 3×3 matrix and an offset (on 0…1 values): exposure (±2 stops), white
// balance (warmth: red vs blue; tint: magenta vs green), contrast around the middle gray,
// saturation and hue (turning the colors round the gray axis, keeping their luminance)
function colorMatrix(a) {
  const ex = Math.pow(2, 2 * a.exposure);
  const d = [ex * (1 + 0.25 * a.warmth) * (1 + 0.12 * a.tint), ex * (1 - 0.2 * a.tint), ex * (1 - 0.25 * a.warmth) * (1 + 0.12 * a.tint)];
  const ct = 1 + a.contrast;
  const s = 1 + a.saturation;
  const lr = 0.2126, lg = 0.7152, lb = 0.0722;
  const S = [lr + (1 - lr) * s, lg - lg * s, lb - lb * s, lr - lr * s, lg + (1 - lg) * s, lb - lb * s, lr - lr * s, lg - lg * s, lb + (1 - lb) * s];
  const h = a.hue * Math.PI / 180, cs = Math.cos(h), sn = Math.sin(h);
  const Hm = [ // as CSS hue-rotate
    0.213 + cs * 0.787 - sn * 0.213, 0.715 - cs * 0.715 - sn * 0.715, 0.072 - cs * 0.072 + sn * 0.928,
    0.213 - cs * 0.213 + sn * 0.143, 0.715 + cs * 0.285 + sn * 0.140, 0.072 - cs * 0.072 - sn * 0.283,
    0.213 - cs * 0.213 - sn * 0.787, 0.715 - cs * 0.715 + sn * 0.715, 0.072 + cs * 0.928 + sn * 0.072];
  const mul = (A, B) => { const o = []; for (let i = 0; i < 3; i++) for (let j = 0; j < 3; j++) o.push(A[i * 3] * B[j] + A[i * 3 + 1] * B[3 + j] + A[i * 3 + 2] * B[6 + j]); return o; };
  const HS = mul(Hm, S);
  // (the contrast's offset is a gray: S and H keep grays, their rows add up to 1)
  return { M: HS.map((v, k) => v * ct * d[k % 3]), off: 0.5 * (1 - ct) };
}
// the same as an SVG feColorMatrix (the live view)
function svgMatrix(a) {
  const { M, off } = colorMatrix(a);
  return [M[0], M[1], M[2], 0, off, M[3], M[4], M[5], 0, off, M[6], M[7], M[8], 0, off, 0, 0, 0, 1, 0].map((v) => +v.toFixed(5)).join(' ');
}
// applies the light and color to a canvas, in place
function adjust(c, a) {
  if (!adjOn(a)) return c;
  const x = ctx2d(c);
  const id = x.getImageData(0, 0, c.width, c.height);
  const p = id.data;
  const { M, off } = colorMatrix(a);
  const o = off * 255;
  for (let i = 0; i < p.length; i += 4) {
    const r = p[i], g = p[i + 1], b = p[i + 2];
    p[i] = M[0] * r + M[1] * g + M[2] * b + o;
    p[i + 1] = M[3] * r + M[4] * g + M[5] * b + o;
    p[i + 2] = M[6] * r + M[7] * g + M[8] * b + o;
  }
  x.putImageData(id, 0, 0);
  return c;
}

// ---- brushes
// a box blur of the RGB of p (w×h RGBA) with radius k, twice (close to a gaussian)
function boxBlur(p, w, h, k) {
  const a = new Float32Array(w * h * 3), b = new Float32Array(w * h * 3);
  for (let i = 0, j = 0; i < p.length; i += 4, j += 3) { a[j] = p[i]; a[j + 1] = p[i + 1]; a[j + 2] = p[i + 2]; }
  const pass = (src, dst, horiz) => {
    const n = horiz ? w : h, m = horiz ? h : w;
    for (let l = 0; l < m; l++) {
      for (let c = 0; c < 3; c++) {
        let sum = 0;
        const at = (i) => { const q = Math.min(n - 1, Math.max(0, i)); return (horiz ? l * w + q : q * w + l) * 3 + c; };
        for (let i = -k; i <= k; i++) sum += src[at(i)];
        for (let i = 0; i < n; i++) {
          dst[(horiz ? l * w + i : i * w + l) * 3 + c] = sum / (2 * k + 1);
          sum += src[at(i + k + 1)] - src[at(i - k)];
        }
      }
    }
  };
  pass(a, b, true); pass(b, a, false); pass(a, b, true); pass(b, a, false);
  return a;
}
const hexRGB = (s) => { const m = /^#?([0-9a-f]{6})$/i.exec(s || ''); const v = m ? parseInt(m[1], 16) : 0xffffff; return [v >> 16 & 255, v >> 8 & 255, v & 255]; };
const toHex = (r, g, b) => '#' + [r, g, b].map((v) => Math.max(0, Math.min(255, Math.round(v))).toString(16).padStart(2, '0')).join('');

// the brush's weight at distance d from its center (radius r, hardness 0…1)
const falloff = (d, r, hard) => {
  if (d >= r) return 0;
  const r0 = Math.min(r * hard, r - 1);
  if (d <= r0) return 1;
  const x = (d - r0) / (r - r0);
  return 1 - x * x * (3 - 2 * x);
};

// one dab of a brush at (cx, cy) on the work canvas. st: the stroke (tool, settings, the
// original for restore, what clone copies from and its offset, the smudge's finger)
function dab(work, st, cx, cy, r, k) {
  const W = work.width, H = work.height;
  const tool = st.tool;
  const br = tool === 'blur' || tool === 'sharpen' ? Math.max(1, Math.round(r / 5)) : 0;
  const x0 = Math.max(0, Math.floor(cx - r) - 2 * br), y0 = Math.max(0, Math.floor(cy - r) - 2 * br);
  const x1 = Math.min(W, Math.ceil(cx + r) + 2 * br + 1), y1 = Math.min(H, Math.ceil(cy + r) + 2 * br + 1);
  if (x1 <= x0 || y1 <= y0) return;
  const w = x1 - x0, h = y1 - y0;
  const wx = ctx2d(work);
  const id = wx.getImageData(x0, y0, w, h);
  const p = id.data;
  let src = null, blur = null;
  if (tool === 'eraser') src = ctx2d(st.orig).getImageData(x0, y0, w, h).data;
  else if (tool === 'clone') src = ctx2d(st.from).getImageData(x0 + st.ox, y0 + st.oy, w, h).data;
  else if (tool === 'blur' || tool === 'sharpen') blur = boxBlur(p, w, h, br);
  const col = st.rgb;
  const s = st.strength * k;
  const F = st.finger, FR = st.fr, fw = 2 * FR + 1;
  for (let y = 0; y < h; y++) {
    const dy = y0 + y + 0.5 - cy;
    for (let x = 0; x < w; x++) {
      const dx = x0 + x + 0.5 - cx;
      const f = falloff(Math.hypot(dx, dy), r, st.hard);
      if (f <= 0) continue;
      const i = (y * w + x) * 4, j = (y * w + x) * 3;
      let wgt = s * f, r2, g2, b2;
      switch (tool) {
        case 'brush': [r2, g2, b2] = col; break;
        case 'eraser': r2 = src[i]; g2 = src[i + 1]; b2 = src[i + 2]; break;
        case 'clone': r2 = src[i]; g2 = src[i + 1]; b2 = src[i + 2]; wgt *= src[i + 3] / 255; break;
        case 'blur': r2 = blur[j]; g2 = blur[j + 1]; b2 = blur[j + 2]; break;
        case 'sharpen': {
          const a = 1.2;
          r2 = p[i] + (p[i] - blur[j]) * a; g2 = p[i + 1] + (p[i + 1] - blur[j + 1]) * a; b2 = p[i + 2] + (p[i + 2] - blur[j + 2]) * a;
          wgt *= 0.25;
          break;
        }
        case 'dodge': {
          const up = st.dodge === 'lighten';
          r2 = up ? p[i] + (255 - p[i]) * 0.35 : p[i] * 0.7;
          g2 = up ? p[i + 1] + (255 - p[i + 1]) * 0.35 : p[i + 1] * 0.7;
          b2 = up ? p[i + 2] + (255 - p[i + 2]) * 0.35 : p[i + 2] * 0.7;
          wgt *= 0.12; // (the dabs overlap: it builds up as it passes)
          break;
        }
        case 'smudge': {
          // the finger's paint here (it carries what it picked up), then it picks up what is here
          const fx = Math.round(x0 + x - cx) + FR, fy = Math.round(y0 + y - cy) + FR;
          if (fx < 0 || fy < 0 || fx >= fw || fy >= fw) continue;
          const q = (fy * fw + fx) * 3;
          r2 = F[q]; g2 = F[q + 1]; b2 = F[q + 2];
          wgt = Math.min(1, 0.25 + 0.75 * st.strength) * f * k;
          break;
        }
        default: continue;
      }
      p[i] += (r2 - p[i]) * wgt;
      p[i + 1] += (g2 - p[i + 1]) * wgt;
      p[i + 2] += (b2 - p[i + 2]) * wgt;
    }
  }
  wx.putImageData(id, x0, y0);
  if (tool === 'smudge') pickUp(st, p, x0, y0, w, h, cx, cy, false);
}
// the smudge's finger takes the colors under it (first: all of them; then mixing in)
function pickUp(st, p, x0, y0, w, h, cx, cy, first) {
  const F = st.finger, FR = st.fr, fw = 2 * FR + 1;
  const keep = first ? 0 : Math.min(0.95, 0.5 + 0.45 * st.strength);
  for (let fy = 0; fy < fw; fy++) {
    for (let fx = 0; fx < fw; fx++) {
      const x = Math.round(cx) - FR + fx - x0, y = Math.round(cy) - FR + fy - y0;
      if (x < 0 || y < 0 || x >= w || y >= h) continue;
      const i = (y * w + x) * 4, q = (fy * fw + fx) * 3;
      F[q] = F[q] * keep + p[i] * (1 - keep);
      F[q + 1] = F[q + 1] * keep + p[i + 1] * (1 - keep);
      F[q + 2] = F[q + 2] * keep + p[i + 2] * (1 - keep);
    }
  }
}

// the biggest rect of aspect ar (width / height, in pixels) centered on an ow×oh image
function aspectRect(ar, ow, oh) {
  let w = 1, h = (ow / ar) / oh;
  if (h > 1) { h = 1; w = (oh * ar) / ow; }
  return { x: (1 - w) / 2, y: (1 - h) / 2, w, h };
}

export function ImageViewer({ items, path, setPath, onClose, onSaved, videoAR }) {
  const idx = Math.max(0, items.findIndex((it) => it.path === path));
  const it = items[idx] || { path, name: basename(path) };
  const stageRef = useRef();
  const [img, setImg] = useState(null);
  const [err, setErr] = useState('');
  const [view, setViewState] = useState({ x: 0, y: 0, z: 1, r: 0 });
  const viewRef = useRef(view);
  const setView = (v) => { viewRef.current = v; setViewState(v); };
  // the edit: the pixels being edited and the original turned / cropped with them (canvases), the
  // undo and redo steps; rev changes when they do
  const E = useRef(null);
  const [editing, setEditing] = useState(false);
  const [rev, setRev] = useState(0);
  const bump = () => setRev((r) => r + 1);
  const [adj, setAdj] = useState({ ...ADJ0 });
  const [tool, setToolState] = useState('hand');
  const [brush, setBrushState] = useState(loadBrush);
  const setBrush = (b) => { setBrushState(b); try { localStorage.setItem('audioviz.iv.brush', JSON.stringify(b)); } catch (e) { /* private */ } };
  const [crop, setCrop] = useState(null); // the crop rect while cropping (0…1)
  const [aspect, setAspect] = useState('free');
  const [orig, setOrig] = useState(false); // showing the original (Compare)
  const [saving, setSaving] = useState(false);
  const [hover, setHover] = useState(null); // the pointer on the image (the brush's circle)
  const [clone, setClone] = useState(null); // where clone copies from: { x, y, ox, oy } (offset once painting)
  const space = useRef(false);
  const host = useRef(null);
  const fitted = useRef('');
  const cropping = editing && tool === 'crop';

  // the image (full size: the editor works on its pixels)
  useEffect(() => {
    let live = true;
    setImg(null); setErr(''); setEditing(false); E.current = null; setOrig(false); fitted.current = '';
    const im = new Image();
    im.onload = () => live && setImg(im);
    im.onerror = () => live && setErr(t('Could not load the image.'));
    im.src = media(it.path);
    return () => { live = false; };
  }, [it.path]);

  // the world: what is shown and its size in image pixels
  const ww = editing && E.current ? E.current.work.width : img ? img.naturalWidth : 1;
  const wh = editing && E.current ? E.current.work.height : img ? img.naturalHeight : 1;

  // the edited pixels (or the original, comparing) shown in the world
  useLayoutEffect(() => {
    const h = host.current;
    if (!h || !E.current) return;
    const c = orig ? E.current.orig : E.current.work;
    if (h.firstChild !== c) { h.textContent = ''; c.className = 'iv-canvas'; h.appendChild(c); }
  });

  // fit the world in the stage (keeping the view's turn)
  const fit = (r = viewRef.current.r) => {
    const st = stageRef.current;
    if (!st) return;
    const W = st.clientWidth, H = st.clientHeight;
    const a = r * Math.PI / 180, c = Math.abs(Math.cos(a)), s = Math.abs(Math.sin(a));
    const bw = ww * c + wh * s, bh = ww * s + wh * c; // the turned world's box
    const z = Math.min(ZMAX, Math.max(ZMIN, Math.min(W / bw, H / bh) * 0.94));
    const cx = (ww / 2) * z, cy = (wh / 2) * z;
    const ca = Math.cos(a), sa = Math.sin(a);
    setView({ z, r, x: W / 2 - (cx * ca - cy * sa), y: H / 2 - (cx * sa + cy * ca) });
  };
  // fitted when the image or the world's size changes
  useLayoutEffect(() => {
    const key = img ? `${it.path}|${ww}x${wh}` : '';
    if (img && fitted.current !== key) { fitted.current = key; fit(editing ? viewRef.current.r : 0); }
  }, [img, ww, wh]);
  useEffect(() => {
    const o = () => fit();
    addEventListener('resize', o);
    return () => removeEventListener('resize', o);
  }, [ww, wh]);

  // screen (stage px) ↔ world (image px)
  const toWorld = (sx, sy, v = viewRef.current) => {
    const a = -v.r * Math.PI / 180, dx = (sx - v.x) / v.z, dy = (sy - v.y) / v.z;
    return [dx * Math.cos(a) - dy * Math.sin(a), dx * Math.sin(a) + dy * Math.cos(a)];
  };
  const fromWorld = (wx, wy, v) => {
    const a = v.r * Math.PI / 180;
    return [v.x + v.z * (wx * Math.cos(a) - wy * Math.sin(a)), v.y + v.z * (wx * Math.sin(a) + wy * Math.cos(a))];
  };
  // zoom by f / turn by da degrees around stage point (sx, sy)
  const zoomAt = (f, sx, sy) => {
    const v = viewRef.current;
    const z = Math.min(ZMAX, Math.max(ZMIN, v.z * f));
    const k = z / v.z;
    setView({ ...v, z, x: sx - k * (sx - v.x), y: sy - k * (sy - v.y) });
  };
  const turnAt = (da, sx, sy) => {
    const v = viewRef.current;
    const a = da * Math.PI / 180, dx = v.x - sx, dy = v.y - sy;
    setView({ ...v, r: ((v.r + da) % 360 + 360) % 360, x: sx + dx * Math.cos(a) - dy * Math.sin(a), y: sy + dx * Math.sin(a) + dy * Math.cos(a) });
  };
  const center = () => { const st = stageRef.current; return st ? [st.clientWidth / 2, st.clientHeight / 2] : [0, 0]; };
  const oneToOne = () => { const [cx, cy] = center(); zoomAt(1 / viewRef.current.z, cx, cy); };

  // ---- the edit's steps (undo / redo)
  const snap = (geom) => ({ work: copyCanvas(E.current.work), orig: geom ? copyCanvas(E.current.orig) : null });
  const pushUndo = (geom) => {
    const e = E.current;
    e.undo.push(snap(geom));
    if (e.undo.length > UNDO_MAX) e.undo.shift();
    e.redo = [];
    e.dirty = true;
  };
  const restoreStep = (s) => {
    const e = E.current;
    e.work = s.work;
    if (s.orig) e.orig = s.orig;
  };
  const undo = () => {
    const e = E.current;
    if (!e || !e.undo.length) return;
    const s = e.undo.pop();
    e.redo.push(snap(!!s.orig));
    restoreStep(s);
    bump();
  };
  const redo = () => {
    const e = E.current;
    if (!e || !e.redo.length) return;
    const s = e.redo.pop();
    e.undo.push(snap(!!s.orig));
    restoreStep(s);
    bump();
  };
  // a change of the whole image (turn, flip, crop): the edited pixels and the original with them
  const geom = (fn) => {
    pushUndo(true);
    E.current.work = fn(E.current.work);
    E.current.orig = fn(E.current.orig);
    bump();
  };

  const startEdit = () => {
    if (!img) return;
    const c = canvas(img.naturalWidth, img.naturalHeight);
    ctx2d(c).drawImage(img, 0, 0);
    E.current = { work: c, orig: copyCanvas(c), undo: [], redo: [], dirty: false };
    setAdj({ ...ADJ0 }); setTool('brush'); setCrop(null); setClone(null); setEditing(true);
  };
  const changed = () => !!E.current && (E.current.dirty && E.current.undo.length > 0 || adjOn(adj));
  const endEdit = () => { setEditing(false); E.current = null; setCrop(null); setHover(null); setTool('hand'); };
  const cancelEdit = () => {
    if (changed() && !confirm(t('Discard the changes to this image?'))) return;
    endEdit();
  };
  const save = async () => {
    if (!E.current || saving) return;
    setSaving(true); setErr('');
    try {
      const c = adjust(copyCanvas(E.current.work), adj);
      const blob = await new Promise((res) => c.toBlob(res, 'image/png'));
      const base = basename(it.path).replace(/\.[^.]+$/, '');
      const taken = new Set(items.map((x) => basename(x.path)));
      let name = base + '-edit.png';
      for (let n = 2; taken.has(name); n++) name = `${base}-edit-${n}.png`;
      const fd = new FormData();
      fd.append('file', blob, name);
      const r = await fetch('/api/upload', { method: 'POST', body: fd });
      const j = await r.json();
      if (!r.ok) throw new Error(j.error || r.statusText);
      endEdit();
      onSaved(j.path);
    } catch (e) { setErr(e.message); }
    setSaving(false);
  };

  // ---- crop
  const aspectOf = () => {
    if (aspect === 'free') return 0;
    if (aspect === 'orig') return ww / wh;
    if (aspect === 'video') return videoAR || 16 / 9;
    const [a, b] = aspect.split(':').map(Number);
    return a / b;
  };
  function setTool(id) {
    if (id === 'crop' && E.current) setCrop((c) => c || { x: 0, y: 0, w: 1, h: 1 });
    if (id !== 'crop') setCrop(null);
    setToolState(id);
  }
  const applyCrop = () => {
    const c = crop;
    if (c && (c.w < 0.999 || c.h < 0.999)) geom((src) => cropped(src, c));
    setTool('brush');
  };
  const setCropAspect = (a) => {
    setAspect(a);
    if (a === 'free' || !E.current) return;
    const ar = a === 'orig' ? ww / wh : a === 'video' ? (videoAR || 16 / 9) : (([p, q]) => p / q)(a.split(':').map(Number));
    setCrop(aspectRect(ar, ww, wh));
    setToolState('crop');
  };
  const cropHit = (wx, wy) => {
    const c = crop || { x: 0, y: 0, w: 1, h: 1 };
    const x0 = c.x * ww, y0 = c.y * wh, x1 = (c.x + c.w) * ww, y1 = (c.y + c.h) * wh;
    const tol = 12 / viewRef.current.z;
    const nx = Math.abs(wx - x0) < tol ? 'w' : Math.abs(wx - x1) < tol ? 'e' : '';
    const ny = Math.abs(wy - y0) < tol ? 'n' : Math.abs(wy - y1) < tol ? 's' : '';
    const inX = wx > x0 - tol && wx < x1 + tol, inY = wy > y0 - tol && wy < y1 + tol;
    if ((nx && inY) || (ny && inX)) return (ny && inX ? ny : '') + (nx && inY ? nx : '');
    if (wx > x0 && wx < x1 && wy > y0 && wy < y1) return 'move';
    return 'new';
  };
  // the crop rect dragged: by a side or corner (h: n, s, e, w…), moved, or drawn anew; kept to the
  // aspect chosen (Shift: free for a moment)
  function dragCrop(g, wx, wy, free) {
    const nx = Math.max(0, Math.min(1, wx / ww)), ny = Math.max(0, Math.min(1, wy / wh));
    const sx = g.w0[0] / ww, sy = g.w0[1] / wh;
    let { x, y, w, h } = g.c0;
    let x1 = x + w, y1 = y + h;
    const min = 16 / Math.max(ww, wh);
    if (g.h === 'move') {
      const dx = Math.max(-x, Math.min(1 - x1, nx - sx)), dy = Math.max(-y, Math.min(1 - y1, ny - sy));
      return { x: x + dx, y: y + dy, w, h };
    }
    let hd = g.h;
    if (hd === 'new') {
      x = Math.min(sx, nx); y = Math.min(sy, ny); x1 = Math.max(sx, nx); y1 = Math.max(sy, ny);
      hd = (ny < sy ? 'n' : 's') + (nx < sx ? 'w' : 'e');
    } else {
      if (hd.includes('w')) x = Math.min(nx, x1 - min);
      if (hd.includes('e')) x1 = Math.max(nx, x + min);
      if (hd.includes('n')) y = Math.min(ny, y1 - min);
      if (hd.includes('s')) y1 = Math.max(ny, y + min);
    }
    w = Math.max(min, x1 - x); h = Math.max(min, y1 - y);
    const ar = free ? 0 : aspectOf();
    if (ar > 0) {
      const k = ar * wh / ww; // w / h in 0…1 units
      const byW = hd === 'e' || hd === 'w' || (hd.length === 2 && w / k >= h);
      if (byW) h = w / k; else w = h * k;
      if (w > 1) { w = 1; h = w / k; }
      if (h > 1) { h = 1; w = h * k; }
      if (hd.includes('w')) x = x1 - w;
      if (hd.includes('n')) y = y1 - h;
      if (hd === 'n' || hd === 's') x = x + ((x1 - x) - w) / 2;
      if (hd === 'e' || hd === 'w') y = y + ((y1 - y) - h) / 2;
      x = Math.max(0, Math.min(1 - w, x)); y = Math.max(0, Math.min(1 - h, y));
    }
    return { x: Math.max(0, x), y: Math.max(0, y), w: Math.min(w, 1 - Math.max(0, x)), h: Math.min(h, 1 - Math.max(0, y)) };
  }

  // ---- painting: a stroke of dabs along the pointer (spaced by a fifth of the brush); a pen's
  // pressure makes it thinner and lighter
  const strokeRef = useRef(null);
  const pickColor = (wx, wy) => {
    const e = E.current;
    const x = Math.max(0, Math.min(e.work.width - 1, Math.floor(wx))), y = Math.max(0, Math.min(e.work.height - 1, Math.floor(wy)));
    const p = ctx2d(e.work).getImageData(Math.max(0, x - 1), Math.max(0, y - 1), 3, 3).data;
    let r = 0, g = 0, b = 0, n = 0;
    for (let i = 0; i < p.length; i += 4) { r += p[i]; g += p[i + 1]; b += p[i + 2]; n++; }
    r /= n; g /= n; b /= n;
    if (adjOn(adj)) { // the color as seen
      const { M, off } = colorMatrix(adj);
      [r, g, b] = [M[0] * r + M[1] * g + M[2] * b + off * 255, M[3] * r + M[4] * g + M[5] * b + off * 255, M[6] * r + M[7] * g + M[8] * b + off * 255];
    }
    setBrush({ ...brush, color: toHex(r, g, b) });
  };
  const startStroke = (wx, wy, e) => {
    const ed = E.current;
    const alt = e.altKey;
    if (tool === 'picker' || (tool === 'brush' && alt)) { pickColor(wx, wy); return false; }
    if (tool === 'clone' && alt) { setClone({ x: wx, y: wy }); return false; }
    if (tool === 'clone' && !clone) { setErr(t('Clone: first Alt+click where to copy from.')); return false; }
    pushUndo(false);
    const r = brush.size / 2;
    const st = { tool, hard: brush.hard, strength: brush.strength, rgb: hexRGB(brush.color), orig: ed.orig, last: [wx, wy], rest: 0,
      dodge: tool === 'dodge' && alt ? (brush.dodge === 'lighten' ? 'darken' : 'lighten') : brush.dodge };
    if (tool === 'clone') {
      let c = clone;
      if (c.ox == null) { c = { ...c, ox: Math.round(c.x - wx), oy: Math.round(c.y - wy) }; setClone(c); }
      st.from = copyCanvas(ed.work); st.ox = c.ox; st.oy = c.oy;
    }
    if (tool === 'smudge') {
      st.fr = Math.ceil(r);
      const fw = 2 * st.fr + 1;
      st.finger = new Float32Array(fw * fw * 3);
      const x0 = Math.max(0, Math.round(wx) - st.fr), y0 = Math.max(0, Math.round(wy) - st.fr);
      const w = Math.min(ed.work.width, Math.round(wx) + st.fr + 1) - x0, h = Math.min(ed.work.height, Math.round(wy) + st.fr + 1) - y0;
      if (w > 0 && h > 0) pickUp(st, ctx2d(ed.work).getImageData(x0, y0, w, h).data, x0, y0, w, h, wx, wy, true);
    }
    strokeRef.current = st;
    const k = e.pointerType === 'pen' && e.pressure > 0 ? e.pressure : 1;
    if (tool !== 'smudge') dab(ed.work, st, wx, wy, Math.max(1, r * (0.3 + 0.7 * k)), k);
    return true;
  };
  const moveStroke = (wx, wy, e) => {
    const st = strokeRef.current;
    if (!st) return;
    const work = E.current.work;
    const k = e.pointerType === 'pen' && e.pressure > 0 ? e.pressure : 1;
    const r = Math.max(1, brush.size / 2 * (0.3 + 0.7 * k));
    const step = Math.max(1, r * 0.2);
    const [lx, ly] = st.last;
    const d = Math.hypot(wx - lx, wy - ly);
    let s = step - st.rest;
    while (s <= d) {
      dab(work, st, lx + (wx - lx) * s / d, ly + (wy - ly) * s / d, r, k);
      s += step;
    }
    st.rest = d - (s - step);
    st.last = [wx, wy];
  };
  const endStroke = () => { if (strokeRef.current) { strokeRef.current = null; bump(); } };
  // a stroke dropped (a second finger came: it is a gesture)
  const dropStroke = () => {
    if (!strokeRef.current) return;
    strokeRef.current = null;
    const e = E.current;
    const s = e.undo.pop();
    if (s) restoreStep(s);
    bump();
  };

  // ---- pointers: the tool, or moving the view (hand, Space, middle button); two fingers move,
  // zoom and turn the view together
  const ptrs = useRef(new Map());
  const gest = useRef(null);
  const local = (e) => { const b = stageRef.current.getBoundingClientRect(); return [e.clientX - b.left, e.clientY - b.top]; };
  const onDown = (e) => {
    if (e.button === 2) return;
    try { stageRef.current.setPointerCapture(e.pointerId); } catch (x) { /* (not a live pointer) */ }
    ptrs.current.set(e.pointerId, local(e));
    const ps = [...ptrs.current.values()];
    if (ps.length >= 2) {
      dropStroke();
      const [a, b] = ps;
      const m = [(a[0] + b[0]) / 2, (a[1] + b[1]) / 2];
      gest.current = { kind: 'pinch', v: viewRef.current, w: toWorld(m[0], m[1]), d0: Math.hypot(b[0] - a[0], b[1] - a[1]) || 1, a0: Math.atan2(b[1] - a[1], b[0] - a[0]) };
      return;
    }
    const [sx, sy] = ps[0];
    const panning = !editing || tool === 'hand' || e.button === 1 || space.current;
    if (!panning && e.button === 0) {
      const [wx, wy] = toWorld(sx, sy);
      if (cropping) {
        gest.current = { kind: 'crop', h: cropHit(wx, wy), w0: [wx, wy], c0: crop || { x: 0, y: 0, w: 1, h: 1 } };
        return;
      }
      if (PAINT.has(tool) || tool === 'picker') {
        setErr('');
        gest.current = startStroke(wx, wy, e) ? { kind: 'paint' } : null;
        if (!gest.current) bump();
        return;
      }
    }
    gest.current = { kind: 'pan', s0: [sx, sy], v: viewRef.current };
  };
  const onMove = (e) => {
    const pt = local(e);
    if (editing && PAINT.has(tool)) {
      const [wx, wy] = toWorld(pt[0], pt[1]);
      setHover([wx, wy]);
    }
    if (!ptrs.current.has(e.pointerId)) return;
    ptrs.current.set(e.pointerId, pt);
    const g = gest.current;
    if (!g) return;
    const ps = [...ptrs.current.values()];
    if (g.kind === 'pinch' && ps.length >= 2) {
      const [a, b] = ps;
      const m = [(a[0] + b[0]) / 2, (a[1] + b[1]) / 2];
      const z = Math.min(ZMAX, Math.max(ZMIN, g.v.z * Math.hypot(b[0] - a[0], b[1] - a[1]) / g.d0));
      const r = g.v.r + (Math.atan2(b[1] - a[1], b[0] - a[0]) - g.a0) * 180 / Math.PI;
      const nv = { z, r, x: 0, y: 0 };
      const [px, py] = fromWorld(g.w[0], g.w[1], nv);
      setView({ ...nv, x: m[0] - px, y: m[1] - py });
    } else if (g.kind === 'pan') {
      const [sx, sy] = ps[0];
      setView({ ...g.v, x: g.v.x + sx - g.s0[0], y: g.v.y + sy - g.s0[1] });
    } else if (g.kind === 'crop') {
      const [wx, wy] = toWorld(ps[0][0], ps[0][1]);
      setCrop(dragCrop(g, wx, wy, e.shiftKey));
    } else if (g.kind === 'paint') {
      // (the coalesced events: a fast stroke stays smooth)
      const evs = e.getCoalescedEvents ? e.getCoalescedEvents() : [];
      for (const ce of evs.length ? evs : [e]) {
        const [wx, wy] = toWorld(...local(ce));
        moveStroke(wx, wy, ce);
      }
    }
  };
  const onUp = (e) => {
    ptrs.current.delete(e.pointerId);
    const g = gest.current;
    if (g && g.kind === 'paint') endStroke();
    const ps = [...ptrs.current.values()];
    gest.current = ps.length === 1 && g && g.kind === 'pinch' ? { kind: 'pan', s0: ps[0], v: viewRef.current } : null;
  };

  // wheel: zoom where the pointer is (a trackpad pinch too); Shift: turn the view
  useEffect(() => {
    const st = stageRef.current;
    if (!st) return;
    const w = (e) => {
      e.preventDefault();
      const [sx, sy] = local(e);
      if (e.shiftKey) { turnAt((e.deltaY || e.deltaX) > 0 ? 15 : -15, sx, sy); return; }
      zoomAt(Math.exp(-e.deltaY * (e.ctrlKey ? 0.01 : 0.0015)), sx, sy);
    };
    st.addEventListener('wheel', w, { passive: false });
    return () => st.removeEventListener('wheel', w);
  }, [img]);

  // ---- keys (before the rest of the studio: Esc, arrows… are the viewer's here)
  const go = (d) => { if (items.length > 1) setPath(items[(idx + d + items.length) % items.length].path); };
  useEffect(() => {
    const k = (e) => {
      const tag = (e.target && e.target.tagName) || '';
      const typing = (tag === 'INPUT' && !['range', 'checkbox', 'button', 'color'].includes(e.target.type)) || tag === 'TEXTAREA' || tag === 'SELECT';
      if (typing) { if (e.key === 'Escape') e.target.blur(); return; }
      const key = e.key, low = key.toLowerCase();
      let used = true;
      const [cx, cy] = center();
      if (key === ' ') { space.current = true; }
      else if (key === 'Escape') { if (cropping) setTool('brush'); else if (editing) cancelEdit(); else onClose(); }
      else if (key === 'Enter' && cropping) applyCrop();
      else if ((e.ctrlKey || e.metaKey) && low === 's') { if (editing) save(); }
      else if ((e.ctrlKey || e.metaKey) && low === 'z') { if (e.shiftKey) redo(); else undo(); }
      else if ((e.ctrlKey || e.metaKey) && low === 'y') redo();
      else if (e.ctrlKey || e.metaKey || e.altKey) used = false;
      else if (key === 'ArrowLeft' && !editing) go(-1);
      else if (key === 'ArrowRight' && !editing) go(1);
      else if (key === '+' || key === '=') zoomAt(1.25, cx, cy);
      else if (key === '-' || key === '_') zoomAt(0.8, cx, cy);
      else if (key === '0') fit();
      else if (key === '1') oneToOne();
      else if (key === 'r') turnAt(90, cx, cy);
      else if (key === 'R') turnAt(-90, cx, cy);
      else if (key === '[' && editing) setBrush({ ...brush, size: Math.max(1, Math.round(brush.size / 1.2)) });
      else if (key === ']' && editing) setBrush({ ...brush, size: Math.min(1000, Math.round(brush.size * 1.2 + 1)) });
      else if (low === 'e' && !editing) startEdit();
      else if (editing && TOOLS.some((x) => x.key === low)) setTool(TOOLS.find((x) => x.key === low).id);
      else used = false;
      if (used) { e.preventDefault(); e.stopImmediatePropagation(); }
    };
    const ku = (e) => { if (e.key === ' ') space.current = false; };
    addEventListener('keydown', k, true);
    addEventListener('keyup', ku, true);
    return () => { removeEventListener('keydown', k, true); removeEventListener('keyup', ku, true); };
  });

  // ---- drawing
  const v = view;
  const zpct = Math.round(v.z * 100);
  const cr = crop || { x: 0, y: 0, w: 1, h: 1 };
  const hs = 7 / v.z; // a crop handle's half size, in image px
  const overlay = editing && html`<svg class="iv-crop" viewBox=${`0 0 ${ww} ${wh}`} width=${ww} height=${wh}>
    ${cropping && html`<path fill="rgba(0,0,0,.55)" fill-rule="evenodd" d=${`M0 0H${ww}V${wh}H0Z M${cr.x * ww} ${cr.y * wh}h${cr.w * ww}v${cr.h * wh}h${-cr.w * ww}Z`} />
      <rect x=${cr.x * ww} y=${cr.y * wh} width=${cr.w * ww} height=${cr.h * wh} fill="none" stroke="#fff" stroke-width=${1.5 / v.z} />
      ${[1, 2].map((k) => html`<line x1=${(cr.x + cr.w * k / 3) * ww} y1=${cr.y * wh} x2=${(cr.x + cr.w * k / 3) * ww} y2=${(cr.y + cr.h) * wh} stroke="rgba(255,255,255,.35)" stroke-width=${1 / v.z} />
        <line x1=${cr.x * ww} y1=${(cr.y + cr.h * k / 3) * wh} x2=${(cr.x + cr.w) * ww} y2=${(cr.y + cr.h * k / 3) * wh} stroke="rgba(255,255,255,.35)" stroke-width=${1 / v.z} />`)}
      ${[[0, 0], [0.5, 0], [1, 0], [0, 0.5], [1, 0.5], [0, 1], [0.5, 1], [1, 1]].map(([a, b]) => html`<rect x=${(cr.x + cr.w * a) * ww - hs} y=${(cr.y + cr.h * b) * wh - hs}
        width=${2 * hs} height=${2 * hs} fill="#fff" stroke="#000" stroke-width=${1 / v.z} />`)}`}
    ${PAINT.has(tool) && hover && html`<circle cx=${hover[0]} cy=${hover[1]} r=${brush.size / 2} fill="none" stroke="#000" stroke-opacity=".6" stroke-width=${2.5 / v.z} />
      <circle cx=${hover[0]} cy=${hover[1]} r=${brush.size / 2} fill="none" stroke="#fff" stroke-width=${1 / v.z} />`}
    ${tool === 'clone' && clone && (() => {
      // where it copies from: fixed until painting, then following the brush
      const [sx, sy] = clone.ox == null || !hover ? [clone.x, clone.y] : [hover[0] + clone.ox, hover[1] + clone.oy];
      const l = 8 / v.z;
      return html`<g stroke="#fff" stroke-width=${1.5 / v.z}><circle cx=${sx} cy=${sy} r=${brush.size / 2} fill="none" stroke-dasharray=${`${4 / v.z} ${4 / v.z}`} />
        <line x1=${sx - l} y1=${sy} x2=${sx + l} y2=${sy} /><line x1=${sx} y1=${sy - l} x2=${sx} y2=${sy + l} /></g>`;
    })()}
  </svg>`;
  const outSize = editing ? `${ww}×${wh}` + (cropping && crop ? ` → ${Math.round(cr.w * ww)}×${Math.round(cr.h * wh)}` : '') : img ? `${img.naturalWidth}×${img.naturalHeight}` : '';
  const e = E.current;
  const slider = (k, label, lo, hi, step, fmt) => html`<div class="iv-adj"><label>${t(label)}</label>
    <input type="range" min=${lo} max=${hi} step=${step} value=${brush[k]} onInput=${(ev) => setBrush({ ...brush, [k]: +ev.target.value })} />
    <span class="mono faint">${fmt(brush[k])}</span></div>`;
  const toolDef = TOOLS.find((x) => x.id === tool);

  const panel = editing && html`<div class="iv-panel">
    <div class="iv-ph">${t('Edit')}<span class="spacer"></span>
      <button class="btn sm icon ghost" disabled=${!e || !e.undo.length} title=${t('Undo (Ctrl+Z)')} onClick=${undo}><${Icon} name="undo-2" size=${14} /></button>
      <button class="btn sm icon ghost" disabled=${!e || !e.redo.length} title=${t('Redo (Ctrl+Shift+Z)')} onClick=${redo}><${Icon} name="redo-2" size=${14} /></button></div>
    ${PAINT.has(tool) && html`<div class="iv-sec">
      <div class="iv-l">${t(toolDef.label.split(' (')[0])}</div>
      ${slider('size', 'Size', 1, 600, 1, (x) => x + ' px')}
      ${slider('hard', 'Hardness', 0, 1, 0.01, (x) => Math.round(x * 100) + '%')}
      ${slider('strength', 'Strength', 0.02, 1, 0.01, (x) => Math.round(x * 100) + '%')}
      ${tool === 'brush' && html`<div class="iv-adj"><label>${t('Color')}</label>
        <input type="color" value=${brush.color} onInput=${(ev) => setBrush({ ...brush, color: ev.target.value })} /><span class="mono faint">${brush.color}</span></div>`}
      ${tool === 'dodge' && html`<div class="row" style="gap:4px;margin-top:4px">${[['lighten', 'Lighten'], ['darken', 'Darken']].map(([m, l]) => html`
        <button class=${'btn sm' + (brush.dodge === m ? ' on' : '')} onClick=${() => setBrush({ ...brush, dodge: m })}>${t(l)}</button>`)}</div>`}
      <p class="faint iv-hint">${t(tool === 'clone' ? (clone ? 'Paint to copy; Alt+click picks another place to copy from.' : 'Alt+click where to copy from, then paint.')
        : tool === 'eraser' ? 'Brings back the original image where it passes.' : '[ and ]: brush size. A pen\'s pressure makes it thinner and lighter.')}</p>
    </div>`}
    ${tool === 'picker' && html`<div class="iv-sec"><div class="iv-l">${t('Eyedropper')}</div>
      <div class="row" style="gap:8px"><span class="iv-swatch" style=${{ background: brush.color }}></span><span class="mono faint">${brush.color}</span></div>
      <p class="faint iv-hint">${t('Click on the image to take its color for the brush.')}</p></div>`}
    <div class="iv-sec">
      <div class="iv-l">${t('Turn, flip and crop')}</div>
      <div class="row" style="gap:4px;flex-wrap:wrap">
        <button class="btn sm icon" title=${t('Turn left')} onClick=${() => geom((c) => turned(c, -1))}><${Icon} name="rotate-ccw" size=${14} /></button>
        <button class="btn sm icon" title=${t('Turn right')} onClick=${() => geom((c) => turned(c, 1))}><${Icon} name="rotate-cw" size=${14} /></button>
        <button class="btn sm icon" title=${t('Flip horizontally')} onClick=${() => geom((c) => flipped(c, true))}><${Icon} name="flip-horizontal-2" size=${14} /></button>
        <button class="btn sm icon" title=${t('Flip vertically')} onClick=${() => geom((c) => flipped(c, false))}><${Icon} name="flip-vertical-2" size=${14} /></button>
        <button class=${'btn sm' + (cropping ? ' on' : '')} title="C" onClick=${() => (cropping ? applyCrop() : setTool('crop'))}><${Icon} name="crop" size=${14} /> ${t(cropping ? 'Apply' : 'Crop')}</button>
        <${Select} value=${aspect} options=${CROP_ASPECTS} onChange=${setCropAspect} />
      </div>
      ${cropping && html`<p class="faint iv-hint">${t('Drag its corners and sides, drag inside to move it, or drag outside to draw a new one (Shift: any shape). Enter applies it, Esc leaves it.')}</p>`}
    </div>
    <div class="iv-sec">
      <div class="iv-l">${t('Light and color')}</div>
      ${ADJ.map(([k, l, lo, hi]) => html`<div class="iv-adj">
        <label onDblClick=${() => setAdj({ ...adj, [k]: 0 })} title=${t('Double-click: back to 0')}>${t(l)}</label>
        <input type="range" min=${lo} max=${hi} step=${(hi - lo) / 200} value=${adj[k]} onInput=${(ev) => setAdj({ ...adj, [k]: +ev.target.value })}
          onDblClick=${() => setAdj({ ...adj, [k]: 0 })} />
        <span class="mono faint">${k === 'hue' ? Math.round(adj[k]) + '°' : (adj[k] > 0 ? '+' : '') + Math.round(adj[k] * 100)}</span></div>`)}
      ${adjOn(adj) && html`<button class="btn sm ghost" style="margin-top:4px" onClick=${() => setAdj({ ...ADJ0 })}><${Icon} name="rotate-ccw" size=${13} /> ${t('Reset')}</button>`}
    </div>
    <div class="iv-sec row" style="gap:6px;flex-wrap:wrap">
      <button class="btn sm" onPointerDown=${() => setOrig(true)} onPointerUp=${() => setOrig(false)} onPointerLeave=${() => setOrig(false)}
        title=${t('Hold to see the original')}><${Icon} name="eye" size=${14} /> ${t('Compare')}</button>
    </div>
    <div class="iv-foot">
      <p class="faint iv-hint">${t('Saved as a new image in the project (assets/); the original is not changed.')}</p>
      <div class="row" style="gap:6px">
        <button class="btn" onClick=${cancelEdit}>${t('Cancel')}</button>
        <span class="spacer"></span>
        <button class="btn primary" disabled=${saving || !changed()} onClick=${save} title="Ctrl+S">
          <${Icon} name=${saving ? 'loader-circle' : 'save'} size=${14} cls=${saving ? 'spin' : ''} /> ${t('Save as new image')}</button>
      </div>
    </div>
  </div>`;

  const showAdj = editing && !orig && adjOn(adj);
  return html`<div class="iv-bg" onContextMenu=${(ev) => ev.preventDefault()}>
    <svg class="iv-defs" width="0" height="0" aria-hidden="true"><filter id="iv-adj" color-interpolation-filters="sRGB">
      <feColorMatrix type="matrix" values=${svgMatrix(adj)} /></filter></svg>
    <div class="iv-top">
      <b class="iv-name" title=${it.path}>${it.name || basename(it.path)}</b>
      ${items.length > 1 && !editing && html`<span class="faint mono" style="font-size:11.5px">${idx + 1} / ${items.length}</span>`}
      <span class="faint mono" style="font-size:11.5px">${outSize}</span>
      <span class="spacer"></span>
      <button class="btn sm icon ghost" title=${t('Zoom out (−)')} onClick=${() => zoomAt(0.8, ...center())}><${Icon} name="zoom-out" size=${15} /></button>
      <button class="btn sm ghost mono iv-z" title=${t('Fit (0)')} onClick=${() => fit()}>${zpct}%</button>
      <button class="btn sm icon ghost" title=${t('Zoom in (+)')} onClick=${() => zoomAt(1.25, ...center())}><${Icon} name="zoom-in" size=${15} /></button>
      <button class="btn sm ghost" title=${t('Actual size (1)')} onClick=${oneToOne}>1:1</button>
      <button class="btn sm icon ghost" title=${t('Fit (0)')} onClick=${() => fit()}><${Icon} name="maximize-2" size=${14} /></button>
      <span class="iv-sep"></span>
      <button class="btn sm icon ghost" title=${t('Turn the view left (Shift+R)')} onClick=${() => turnAt(-90, ...center())}><${Icon} name="rotate-ccw" size=${15} /></button>
      <button class=${'btn sm ghost mono' + (v.r ? ' on' : '')} title=${t('Rotation of the view: click to put it straight')}
        onClick=${() => { const [cx, cy] = center(); turnAt(-(v.r > 180 ? v.r - 360 : v.r), cx, cy); }}>${Math.round(v.r > 180 ? v.r - 360 : v.r)}°</button>
      <button class="btn sm icon ghost" title=${t('Turn the view right (R)')} onClick=${() => turnAt(90, ...center())}><${Icon} name="rotate-cw" size=${15} /></button>
      <span class="iv-sep"></span>
      ${!editing && html`<button class="btn sm" disabled=${!img} title="E" onClick=${startEdit}><${Icon} name="sliders-horizontal" size=${14} /> ${t('Edit')}</button>`}
      <button class="btn sm icon ghost" title=${t('Close (Esc)')} onClick=${() => (editing ? cancelEdit() : onClose())}><${Icon} name="x" size=${16} /></button>
    </div>
    ${err && html`<div class="alert err" style="margin:8px 12px 0"><${Icon} name="triangle-alert" size=${15} />${err}</div>`}
    <div class="iv-main">
      ${editing && html`<div class="de-tools iv-tools">${TOOLS.map((x) => html`<button class=${'btn icon' + (tool === x.id ? ' on' : ' ghost')} title=${t(x.label)}
        onClick=${() => setTool(x.id)}><${Icon} name=${x.icon} size=${16} /></button>`)}</div>`}
      <div class=${'iv-stage' + (cropping ? ' crop' : '') + (editing && (PAINT.has(tool) || tool === 'picker') ? ' paint' : '')} ref=${stageRef}
          onPointerDown=${onDown} onPointerMove=${onMove} onPointerUp=${onUp} onPointerCancel=${onUp} onPointerLeave=${() => setHover(null)}
          onDblClick=${(ev) => { if (editing && tool !== 'hand') return; const [sx, sy] = local(ev); if (v.z < 0.99 || v.z > 1.01) zoomAt(1 / v.z, sx, sy); else fit(); }}>
        ${!img && !err && html`<div class="iv-wait faint"><${Icon} name="loader-circle" size=${26} cls="spin" /></div>`}
        ${img && html`<div class="iv-world" style=${{ width: ww + 'px', height: wh + 'px', transform: `translate(${v.x}px,${v.y}px) rotate(${v.r}deg) scale(${v.z})` }}>
          ${editing ? html`<div class="iv-pv" ref=${host} style=${{ filter: showAdj ? 'url(#iv-adj)' : 'none' }}></div>` : html`<img src=${img.src} alt="" draggable=${false} />`}
          ${overlay}
        </div>`}
        ${items.length > 1 && !editing && html`<button class="iv-nav l" title=${t('Previous (←)')} onClick=${() => go(-1)} onPointerDown=${(ev) => ev.stopPropagation()}><${Icon} name="chevron-left" size=${26} /></button>
          <button class="iv-nav r" title=${t('Next (→)')} onClick=${() => go(1)} onPointerDown=${(ev) => ev.stopPropagation()}><${Icon} name="chevron-right" size=${26} /></button>`}
        <div class="iv-help faint">${t(editing ? 'Wheel or two fingers: zoom · Space or middle button: move · Shift+wheel: turn · Ctrl+Z: undo'
          : 'Wheel or two fingers: zoom · drag: move · Shift+wheel or two fingers: turn · double-click: actual size / fit')}</div>
      </div>
      ${panel}
    </div>
  </div>`;
}
