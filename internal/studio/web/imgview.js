// Images: one image big over the whole window (zoom, pan and turn the view; the previous / next
// one), and a basic editor (crop, turn, flip, light and color) that saves the result as a new
// image in the project (assets/), the original untouched.
import { html, useEffect, useLayoutEffect, useRef, useState } from './vendor/preact-htm.js';
import { Icon } from './icons.js';
import { Select } from './fields.js';
import { t } from './i18n.js';
import { basename } from './util.js';

const media = (p) => '/api/media?path=' + encodeURIComponent(p);
const ZMIN = 0.02, ZMAX = 40;

// the light and color settings: key, label, range (0 = unchanged)
const ADJ = [['exposure', 'Exposure', -1, 1], ['contrast', 'Contrast', -1, 1], ['saturation', 'Saturation', -1, 1],
  ['warmth', 'Warmth', -1, 1], ['tint', 'Tint', -1, 1], ['hue', 'Hue', -180, 180]];
// an edit: quarter turns clockwise, flips (after the turn), crop (0…1 of the turned image; null:
// all of it) and the light and color
const ED0 = { q: 0, fh: false, fv: false, crop: null, exposure: 0, contrast: 0, saturation: 0, warmth: 0, tint: 0, hue: 0 };
const CROP_ASPECTS = [['free', 'Free'], ['orig', 'Original'], ['video', 'Like the video'], ['16:9', '16:9'], ['1:1', '1:1'], ['9:16', '9:16'], ['4:3', '4:3'], ['3:4', '3:4']];
const changed = (ed) => !!ed && JSON.stringify(ed) !== JSON.stringify(ED0);

// the image's size once turned
const turnedSize = (img, q) => (q % 2 ? [img.naturalHeight, img.naturalWidth] : [img.naturalWidth, img.naturalHeight]);

// the image turned and flipped as ed says, its crop rect (null: all) at scale s, on a canvas
function drawBase(img, ed, crop, s) {
  const [ow, oh] = turnedSize(img, ed.q);
  const cr = crop || { x: 0, y: 0, w: 1, h: 1 };
  const c = document.createElement('canvas');
  c.width = Math.max(1, Math.round(ow * cr.w * s));
  c.height = Math.max(1, Math.round(oh * cr.h * s));
  const x = c.getContext('2d');
  x.imageSmoothingQuality = 'high';
  // the turned image placed so that its crop rect fills the canvas
  x.translate(-cr.x * ow * s, -cr.y * oh * s);
  x.translate(ow * s / 2, oh * s / 2);
  x.scale(ed.fh ? -1 : 1, ed.fv ? -1 : 1);
  x.rotate(ed.q * Math.PI / 2);
  const w = img.naturalWidth * s, h = img.naturalHeight * s;
  x.drawImage(img, -w / 2, -h / 2, w, h);
  return c;
}

// the light and color as one 3×3 matrix and an offset (on 0…1 values): exposure (±2 stops), white
// balance (warmth: red vs blue; tint: magenta vs green), contrast around the middle gray,
// saturation and hue (turning the colors round the gray axis, keeping their luminance)
function colorMatrix(ed) {
  const ex = Math.pow(2, 2 * ed.exposure);
  const d = [ex * (1 + 0.25 * ed.warmth) * (1 + 0.12 * ed.tint), ex * (1 - 0.2 * ed.tint), ex * (1 - 0.25 * ed.warmth) * (1 + 0.12 * ed.tint)];
  const ct = 1 + ed.contrast;
  const s = 1 + ed.saturation;
  const lr = 0.2126, lg = 0.7152, lb = 0.0722;
  const S = [lr + (1 - lr) * s, lg - lg * s, lb - lb * s, lr - lr * s, lg + (1 - lg) * s, lb - lb * s, lr - lr * s, lg - lg * s, lb + (1 - lb) * s];
  const a = ed.hue * Math.PI / 180, cs = Math.cos(a), sn = Math.sin(a);
  const Hm = [ // as CSS hue-rotate
    0.213 + cs * 0.787 - sn * 0.213, 0.715 - cs * 0.715 - sn * 0.715, 0.072 - cs * 0.072 + sn * 0.928,
    0.213 - cs * 0.213 + sn * 0.143, 0.715 + cs * 0.285 + sn * 0.140, 0.072 - cs * 0.072 - sn * 0.283,
    0.213 - cs * 0.213 - sn * 0.787, 0.715 - cs * 0.715 + sn * 0.715, 0.072 + cs * 0.928 + sn * 0.072];
  const mul = (A, B) => { const o = []; for (let i = 0; i < 3; i++) for (let j = 0; j < 3; j++) o.push(A[i * 3] * B[j] + A[i * 3 + 1] * B[3 + j] + A[i * 3 + 2] * B[6 + j]); return o; };
  const HS = mul(Hm, S);
  // (the contrast's offset is a gray: S and H keep grays, their rows add up to 1)
  return { M: HS.map((v, k) => v * ct * d[k % 3]), off: 0.5 * (1 - ct) };
}

// applies the light and color to a canvas, in place
function adjust(c, ed) {
  if (!ADJ.some(([k]) => ed[k])) return c;
  const x = c.getContext('2d');
  const id = x.getImageData(0, 0, c.width, c.height);
  const p = id.data;
  const { M, off } = colorMatrix(ed);
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

// the crop rect after turning the image a quarter clockwise, or flipping it
const turnCrop = (c) => c && { x: 1 - c.y - c.h, y: c.x, w: c.h, h: c.w };
const flipCrop = (c, h) => c && (h ? { ...c, x: 1 - c.x - c.w } : { ...c, y: 1 - c.y - c.h });

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
  const [ed, setEd] = useState(null); // the edit (null: just viewing)
  const [cropping, setCropping] = useState(false);
  const [aspect, setAspect] = useState('free');
  const [orig, setOrig] = useState(false); // showing it without its light and color (Compare)
  const [saving, setSaving] = useState(false);
  const [pv, setPv] = useState(null); // the edit's preview canvas
  const pvBox = useRef(null); // its host in the page
  const fitted = useRef('');

  // the image (full size: the editor works on its pixels)
  useEffect(() => {
    let live = true;
    setImg(null); setErr(''); setEd(null); setCropping(false); setOrig(false); fitted.current = '';
    const im = new Image();
    im.onload = () => live && setImg(im);
    im.onerror = () => live && setErr(t('Could not load the image.'));
    im.src = media(it.path);
    return () => { live = false; };
  }, [it.path]);

  // the world: what is shown and its size in image pixels
  const editing = !!ed;
  let ww = img ? img.naturalWidth : 1, wh = img ? img.naturalHeight : 1;
  if (img && editing) {
    const [ow, oh] = turnedSize(img, ed.q);
    const cr = cropping || !ed.crop ? { w: 1, h: 1 } : ed.crop;
    ww = Math.max(1, Math.round(ow * cr.w)); wh = Math.max(1, Math.round(oh * cr.h));
  }

  // the edit's preview (at most ~1600 px; its light and color on it)
  useEffect(() => {
    if (!img || !ed) { setPv(null); return; }
    const [ow, oh] = turnedSize(img, ed.q);
    const crop = cropping ? null : ed.crop;
    const cw = ow * (crop ? crop.w : 1), ch = oh * (crop ? crop.h : 1);
    const s = Math.min(1, 1600 / Math.max(cw, ch));
    const id = requestAnimationFrame(() => setPv(orig ? drawBase(img, ed, crop, s) : adjust(drawBase(img, ed, crop, s), ed)));
    return () => cancelAnimationFrame(id);
  }, [img, ed, cropping, orig]);
  useLayoutEffect(() => {
    const host = pvBox.current;
    if (!host) return;
    host.textContent = '';
    if (pv) { pv.className = 'iv-canvas'; host.appendChild(pv); }
  }, [pv, editing]);

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
    if (img && fitted.current !== key) { fitted.current = key; fit(viewRef.current.r && ed ? viewRef.current.r : 0); }
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
  // actual size (1 image px = 1 screen px) at the center
  const oneToOne = () => { const [cx, cy] = center(); zoomAt(1 / viewRef.current.z, cx, cy); };

  // ---- pointers: one drags the view (or the crop rect); two move, zoom and turn it together
  const ptrs = useRef(new Map());
  const gest = useRef(null);
  const local = (e) => { const b = stageRef.current.getBoundingClientRect(); return [e.clientX - b.left, e.clientY - b.top]; };
  const cropHit = (wx, wy) => {
    const c = ed.crop || { x: 0, y: 0, w: 1, h: 1 };
    const x0 = c.x * ww, y0 = c.y * wh, x1 = (c.x + c.w) * ww, y1 = (c.y + c.h) * wh;
    const tol = 12 / viewRef.current.z;
    const nx = Math.abs(wx - x0) < tol ? 'w' : Math.abs(wx - x1) < tol ? 'e' : '';
    const ny = Math.abs(wy - y0) < tol ? 'n' : Math.abs(wy - y1) < tol ? 's' : '';
    const inX = wx > x0 - tol && wx < x1 + tol, inY = wy > y0 - tol && wy < y1 + tol;
    if ((nx && inY) || (ny && inX)) return (ny && inX ? ny : '') + (nx && inY ? nx : '');
    if (wx > x0 && wx < x1 && wy > y0 && wy < y1) return 'move';
    return 'new';
  };
  const onDown = (e) => {
    if (e.button === 2) return;
    try { stageRef.current.setPointerCapture(e.pointerId); } catch (x) { /* (not a live pointer) */ }
    ptrs.current.set(e.pointerId, local(e));
    const ps = [...ptrs.current.values()];
    if (ps.length >= 2) {
      const [a, b] = ps;
      const m = [(a[0] + b[0]) / 2, (a[1] + b[1]) / 2];
      gest.current = { kind: 'pinch', v: viewRef.current, w: toWorld(m[0], m[1]), d0: Math.hypot(b[0] - a[0], b[1] - a[1]) || 1, a0: Math.atan2(b[1] - a[1], b[0] - a[0]) };
      return;
    }
    const [sx, sy] = ps[0];
    if (cropping && e.button === 0 && !e.altKey) {
      const [wx, wy] = toWorld(sx, sy);
      const h = cropHit(wx, wy);
      gest.current = { kind: 'crop', h, w0: [wx, wy], c0: ed.crop || { x: 0, y: 0, w: 1, h: 1 } };
      return;
    }
    gest.current = { kind: 'pan', s0: [sx, sy], v: viewRef.current };
  };
  const onMove = (e) => {
    if (!ptrs.current.has(e.pointerId)) return;
    ptrs.current.set(e.pointerId, local(e));
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
      setEd((d) => ({ ...d, crop: dragCrop(g, wx, wy, e.shiftKey) }));
    }
  };
  const onUp = (e) => {
    ptrs.current.delete(e.pointerId);
    const g = gest.current;
    if (g && g.kind === 'crop') setEd((d) => (d.crop && d.crop.w > 0.999 && d.crop.h > 0.999 ? { ...d, crop: null } : d));
    // one finger left after a pinch: it drags from here
    const ps = [...ptrs.current.values()];
    gest.current = ps.length === 1 && g && g.kind === 'pinch' ? { kind: 'pan', s0: ps[0], v: viewRef.current } : null;
  };
  // the crop rect dragged: by a side or corner (h: n, s, e, w…), moved, or drawn anew; kept to the
  // aspect chosen (Shift: free for a moment)
  const aspectOf = () => {
    if (aspect === 'free') return 0;
    if (aspect === 'orig') { const [ow, oh] = turnedSize(img, ed.q); return ow / oh; }
    if (aspect === 'video') return videoAR || 16 / 9;
    const [a, b] = aspect.split(':').map(Number);
    return a / b;
  };
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
      // the side dragged leads; a corner: the one that moved most
      const byW = hd === 'e' || hd === 'w' || (hd.length === 2 && w / k >= h);
      if (byW) h = w / k; else w = h * k;
      if (w > 1) { w = 1; h = w / k; }
      if (h > 1) { h = 1; w = h * k; }
      // anchored at the opposite side / corner
      if (hd.includes('w')) x = x1 - w;
      if (hd.includes('n')) y = y1 - h;
      if (hd === 'n' || hd === 's') x = x + ((x1 - x) - w) / 2;
      if (hd === 'e' || hd === 'w') y = y + ((y1 - y) - h) / 2;
      x = Math.max(0, Math.min(1 - w, x)); y = Math.max(0, Math.min(1 - h, y));
    }
    return { x: Math.max(0, x), y: Math.max(0, y), w: Math.min(w, 1 - Math.max(0, x)), h: Math.min(h, 1 - Math.max(0, y)) };
  }
  const setCropAspect = (a) => {
    setAspect(a);
    if (a === 'free' || !img) return;
    const [ow, oh] = turnedSize(img, ed.q);
    const ar = a === 'orig' ? ow / oh : a === 'video' ? (videoAR || 16 / 9) : (([p, q]) => p / q)(a.split(':').map(Number));
    setEd((d) => ({ ...d, crop: aspectRect(ar, ow, oh) }));
    setCropping(true);
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

  // ---- the edit
  const startEdit = () => { setEd({ ...ED0 }); setAspect('free'); };
  const turn = (dir) => setEd((d) => {
    let c = d.crop;
    for (let i = 0; i < (dir > 0 ? 1 : 3); i++) c = turnCrop(c);
    return { ...d, q: (d.q + (dir > 0 ? 1 : 3)) % 4, crop: c };
  });
  const flip = (h) => setEd((d) => ({ ...d, [h ? 'fh' : 'fv']: !d[h ? 'fh' : 'fv'], crop: flipCrop(d.crop, h) }));
  const cancelEdit = () => {
    if (changed(ed) && !confirm(t('Discard the changes to this image?'))) return;
    setEd(null); setCropping(false);
  };
  const save = async () => {
    if (!img || !ed || saving) return;
    setSaving(true); setErr('');
    try {
      const c = adjust(drawBase(img, ed, ed.crop, 1), ed);
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
      setEd(null); setCropping(false);
      onSaved(j.path);
    } catch (e) { setErr(e.message); }
    setSaving(false);
  };

  // ---- keys (before the rest of the studio: Esc, arrows… are the viewer's here)
  const go = (d) => { if (!ed && items.length > 1) setPath(items[(idx + d + items.length) % items.length].path); };
  useEffect(() => {
    const k = (e) => {
      const tag = (e.target && e.target.tagName) || '';
      const typing = (tag === 'INPUT' && !['range', 'checkbox', 'button'].includes(e.target.type)) || tag === 'TEXTAREA' || tag === 'SELECT';
      if (typing) { if (e.key === 'Escape') e.target.blur(); return; }
      const key = e.key;
      let used = true;
      const [cx, cy] = center();
      if (key === 'Escape') { if (cropping) setCropping(false); else if (ed) cancelEdit(); else onClose(); }
      else if ((e.ctrlKey || e.metaKey) && key.toLowerCase() === 's') { if (ed) save(); }
      else if (e.ctrlKey || e.metaKey || e.altKey) used = false;
      else if (key === 'ArrowLeft' && !ed) go(-1);
      else if (key === 'ArrowRight' && !ed) go(1);
      else if (key === '+' || key === '=') zoomAt(1.25, cx, cy);
      else if (key === '-' || key === '_') zoomAt(0.8, cx, cy);
      else if (key === '0') fit();
      else if (key === '1') oneToOne();
      else if (key === 'r') turnAt(90, cx, cy);
      else if (key === 'R') turnAt(-90, cx, cy);
      else if (key === 'e' && !ed) startEdit();
      else if (key === 'c' && ed) setCropping((v) => !v);
      else used = false;
      if (used) { e.preventDefault(); e.stopImmediatePropagation(); }
    };
    addEventListener('keydown', k, true);
    return () => removeEventListener('keydown', k, true);
  });

  // ---- drawing
  const v = view;
  const zpct = Math.round(v.z * 100);
  const cr = ed && (ed.crop || { x: 0, y: 0, w: 1, h: 1 });
  const hs = 7 / v.z; // a handle's half size, in image px
  const cropSvg = editing && cropping && html`<svg class="iv-crop" viewBox=${`0 0 ${ww} ${wh}`} width=${ww} height=${wh}>
    <path fill="rgba(0,0,0,.55)" fill-rule="evenodd" d=${`M0 0H${ww}V${wh}H0Z M${cr.x * ww} ${cr.y * wh}h${cr.w * ww}v${cr.h * wh}h${-cr.w * ww}Z`} />
    <rect x=${cr.x * ww} y=${cr.y * wh} width=${cr.w * ww} height=${cr.h * wh} fill="none" stroke="#fff" stroke-width=${1.5 / v.z} />
    ${[1, 2].map((k) => html`<line x1=${(cr.x + cr.w * k / 3) * ww} y1=${cr.y * wh} x2=${(cr.x + cr.w * k / 3) * ww} y2=${(cr.y + cr.h) * wh} stroke="rgba(255,255,255,.35)" stroke-width=${1 / v.z} />
      <line x1=${cr.x * ww} y1=${(cr.y + cr.h * k / 3) * wh} x2=${(cr.x + cr.w) * ww} y2=${(cr.y + cr.h * k / 3) * wh} stroke="rgba(255,255,255,.35)" stroke-width=${1 / v.z} />`)}
    ${[[0, 0], [0.5, 0], [1, 0], [0, 0.5], [1, 0.5], [0, 1], [0.5, 1], [1, 1]].map(([a, b]) => html`<rect x=${(cr.x + cr.w * a) * ww - hs} y=${(cr.y + cr.h * b) * wh - hs}
      width=${2 * hs} height=${2 * hs} fill="#fff" stroke="#000" stroke-width=${1 / v.z} />`)}
  </svg>`;
  const [ow, oh] = img && ed ? turnedSize(img, ed.q) : [0, 0];
  const outSize = ed && img ? `${Math.round(ow * cr.w)}×${Math.round(oh * cr.h)}` : img ? `${img.naturalWidth}×${img.naturalHeight}` : '';

  const panel = ed && html`<div class="iv-panel">
    <div class="iv-ph">${t('Edit')}<span class="spacer"></span><span class="faint mono" style="font-size:11px">${outSize}</span></div>
    <div class="iv-sec">
      <div class="iv-l">${t('Turn and flip')}</div>
      <div class="row" style="gap:4px">
        <button class="btn sm icon" title=${t('Turn left')} onClick=${() => turn(-1)}><${Icon} name="rotate-ccw" size=${14} /></button>
        <button class="btn sm icon" title=${t('Turn right')} onClick=${() => turn(1)}><${Icon} name="rotate-cw" size=${14} /></button>
        <button class=${'btn sm icon' + (ed.fh ? ' on' : '')} title=${t('Flip horizontally')} onClick=${() => flip(true)}><${Icon} name="flip-horizontal-2" size=${14} /></button>
        <button class=${'btn sm icon' + (ed.fv ? ' on' : '')} title=${t('Flip vertically')} onClick=${() => flip(false)}><${Icon} name="flip-vertical-2" size=${14} /></button>
      </div>
    </div>
    <div class="iv-sec">
      <div class="iv-l">${t('Crop')}</div>
      <div class="row" style="gap:4px;flex-wrap:wrap">
        <button class=${'btn sm' + (cropping ? ' on' : '')} title="C" onClick=${() => setCropping(!cropping)}><${Icon} name="crop" size=${14} /> ${t(cropping ? 'Done' : 'Crop')}</button>
        <${Select} value=${aspect} options=${CROP_ASPECTS} onChange=${setCropAspect} />
        ${ed.crop && html`<button class="btn sm ghost" title=${t('The whole image')} onClick=${() => setEd({ ...ed, crop: null })}><${Icon} name="x" size=${13} /></button>`}
      </div>
      ${cropping && html`<p class="faint iv-hint">${t('Drag its corners and sides, drag inside to move it, or drag outside to draw a new one (Shift: any shape).')}</p>`}
    </div>
    <div class="iv-sec">
      <div class="iv-l">${t('Light and color')}</div>
      ${ADJ.map(([k, l, lo, hi]) => html`<div class="iv-adj">
        <label onDblClick=${() => setEd({ ...ed, [k]: 0 })} title=${t('Double-click: back to 0')}>${t(l)}</label>
        <input type="range" min=${lo} max=${hi} step=${(hi - lo) / 200} value=${ed[k]} onInput=${(e) => setEd({ ...ed, [k]: +e.target.value })}
          onDblClick=${() => setEd({ ...ed, [k]: 0 })} />
        <span class="mono faint">${k === 'hue' ? Math.round(ed[k]) + '°' : (ed[k] > 0 ? '+' : '') + Math.round(ed[k] * 100)}</span></div>`)}
    </div>
    <div class="iv-sec row" style="gap:6px;flex-wrap:wrap">
      <button class="btn sm" onPointerDown=${() => setOrig(true)} onPointerUp=${() => setOrig(false)} onPointerLeave=${() => setOrig(false)}
        title=${t('Hold to see it without the light and color changes')}><${Icon} name="eye" size=${14} /> ${t('Compare')}</button>
      <button class="btn sm ghost" disabled=${!changed(ed)} onClick=${() => { setEd({ ...ED0 }); setAspect('free'); }}><${Icon} name="rotate-ccw" size=${13} /> ${t('Reset')}</button>
    </div>
    <div class="iv-foot">
      <p class="faint iv-hint">${t('Saved as a new image in the project (assets/); the original is not changed.')}</p>
      <div class="row" style="gap:6px">
        <button class="btn" onClick=${cancelEdit}>${t('Cancel')}</button>
        <span class="spacer"></span>
        <button class="btn primary" disabled=${saving || !changed(ed)} onClick=${save} title="Ctrl+S">
          <${Icon} name=${saving ? 'loader-circle' : 'save'} size=${14} cls=${saving ? 'spin' : ''} /> ${t('Save as new image')}</button>
      </div>
    </div>
  </div>`;

  return html`<div class="iv-bg" onContextMenu=${(e) => e.preventDefault()}>
    <div class="iv-top">
      <b class="iv-name" title=${it.path}>${it.name || basename(it.path)}</b>
      ${items.length > 1 && !ed && html`<span class="faint mono" style="font-size:11.5px">${idx + 1} / ${items.length}</span>`}
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
      ${!ed && html`<button class="btn sm" disabled=${!img} title="E" onClick=${startEdit}><${Icon} name="sliders-horizontal" size=${14} /> ${t('Edit')}</button>`}
      <button class="btn sm icon ghost" title=${t('Close (Esc)')} onClick=${() => (ed ? cancelEdit() : onClose())}><${Icon} name="x" size=${16} /></button>
    </div>
    ${err && html`<div class="alert err" style="margin:8px 12px 0"><${Icon} name="triangle-alert" size=${15} />${err}</div>`}
    <div class="iv-main">
      <div class=${'iv-stage' + (cropping ? ' crop' : '')} ref=${stageRef} onPointerDown=${onDown} onPointerMove=${onMove} onPointerUp=${onUp} onPointerCancel=${onUp}
          onDblClick=${(e) => { if (cropping) return; const [sx, sy] = local(e); if (v.z < 0.99 || v.z > 1.01) zoomAt(1 / v.z, sx, sy); else fit(); }}>
        ${!img && !err && html`<div class="iv-wait faint"><${Icon} name="loader-circle" size=${26} cls="spin" /></div>`}
        ${img && html`<div class="iv-world" style=${{ width: ww + 'px', height: wh + 'px', transform: `translate(${v.x}px,${v.y}px) rotate(${v.r}deg) scale(${v.z})` }}>
          ${editing ? html`<div class="iv-pv" ref=${pvBox}></div>` : html`<img src=${img.src} alt="" draggable=${false} />`}
          ${cropSvg}
        </div>`}
        ${items.length > 1 && !ed && html`<button class="iv-nav l" title=${t('Previous (←)')} onClick=${() => go(-1)} onPointerDown=${(e) => e.stopPropagation()}><${Icon} name="chevron-left" size=${26} /></button>
          <button class="iv-nav r" title=${t('Next (→)')} onClick=${() => go(1)} onPointerDown=${(e) => e.stopPropagation()}><${Icon} name="chevron-right" size=${26} /></button>`}
        <div class="iv-help faint">${t('Wheel or two fingers: zoom · drag: move · Shift+wheel or two fingers: turn · double-click: actual size / fit')}</div>
      </div>
      ${panel}
    </div>
  </div>`;
}
