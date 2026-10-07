import { html, useEffect, useRef, useState } from './vendor/preact-htm.js';
import { Form } from './fields.js';
import { Icon } from './icons.js';
import { t } from './i18n.js';
import { valueAt, getIn } from './keys.js';
import { openMenu } from './menu.js';
import { clipValue, clipSize, useNatural, newLayerId, shownGeom } from './layers.js';

// Masks of a clip (config.MaskConfig): vector (closed smooth shapes, 0..1 of the clip's image)
// or raster (a painted PNG). They move, scale and turn with the clip. The editor draws them:
// shape (freehand), nodes, brush, eraser. Fixed texts and the lyrics have masks too, on the screen
// (0..1 of the frame): their owner is anything with { masks, keys, start }.

// A lyrics segment as a mask owner (its masks are in its config, its keys beside it) and the way
// to change it (inside an update)
export const segMaskOwner = (s) => ({ masks: (s.cfg && s.cfg.masks) || [], keys: s.keys || {}, start: s.at });
export function editSegMasks(s, fn) {
  const o = segMaskOwner(s);
  fn(o);
  if (o.masks.length || s.cfg.masks) s.cfg.masks = o.masks;
  if (Object.keys(o.keys).length || s.keys) s.keys = o.keys;
}
// The whole frame as the geometry of screen masks
export const frameGeom = (meta) => ({ cx: meta.width / 2, cy: meta.height / 2, w: meta.width, h: meta.height, rot: 0, fx: 1, fy: 1 });

export const MASK_FIELDS = [
  { key: 'mode', label: 'Clip seen', type: 'enum', def: 'inside', anim: false,
    options: [['inside', 'Only inside the mask'], ['outside', 'Only outside the mask']] },
  { key: 'feather', label: 'Soft edge (px)', type: 'num', min: 0, max: 200, step: 1 },
  { key: 'opacity', label: 'Strength', type: 'num', min: 0, max: 1, step: 0.01 },
];

export const TEXT_MASK_FIELDS = [{ ...MASK_FIELDS[0], label: 'Text seen' }, ...MASK_FIELDS.slice(1)];

// The shapes of mask k at time tt (its keys, or its still shapes)
export function maskShapes(c, k, tt) {
  const l = c.keys && c.keys[`masks.${k}.shapes`];
  return l && l.length ? valueAt(l, tt - c.start) : (c.masks[k].shapes || []);
}

// Sets the shapes of mask k at time tt: a key at the cursor when they are animated (mutates c,
// inside an update)
export function setMaskShapes(c, k, tt, shapes) {
  const key = `masks.${k}.shapes`, l = c.keys && c.keys[key];
  if (!l || !l.length) { c.masks[k].shapes = shapes; return; }
  const u = +Math.max(0, tt - c.start).toFixed(3);
  const nl = l.map((x) => ({ ...x }));
  const i = nl.findIndex((x) => Math.abs(x.t - u) < 0.002);
  if (i >= 0) nl[i].v = shapes; else nl.push({ t: u, v: shapes });
  c.keys = { ...c.keys, [key]: nl.sort((a, b) => a.t - b.t) };
}

// Where the clip is at time tt (frame px): center, size, rotation (rad), flips
export function clipGeom(c, nat, meta, tt) {
  const sz = clipSize(c, nat, meta.width, meta.height, clipValue(c, 'scale', tt));
  return { cx: clipValue(c, 'x', tt) * meta.width, cy: clipValue(c, 'y', tt) * meta.height, w: sz.w, h: sz.h,
    rot: (clipValue(c, 'rotation', tt) * Math.PI) / 180, fx: c.flip_x ? -1 : 1, fy: c.flip_y ? -1 : 1 };
}
// clip image 0..1 → frame px, and back
export function toFramePt(g, u, v) {
  const lx = (u - 0.5) * g.w * g.fx, ly = (v - 0.5) * g.h * g.fy;
  const c = Math.cos(g.rot), s = Math.sin(g.rot);
  return { x: g.cx + lx * c - ly * s, y: g.cy + lx * s + ly * c };
}
export function toClipPt(g, x, y) {
  const dx = x - g.cx, dy = y - g.cy, c = Math.cos(-g.rot), s = Math.sin(-g.rot);
  const lx = dx * c - dy * s, ly = dx * s + dy * c;
  return [lx / (g.w * g.fx) + 0.5, ly / (g.h * g.fy) + 0.5];
}

// Node types (as effects/masks.go): [x, y] smooth (automatic), [x, y, 1] corner, [x, y, 2, inX,
// inY, outX, outY] symmetric handles, [x, y, 3, ...] free handles, [x, y, 4, ...] aligned handles (in
// line, each its own length); offsets from the node
export const NODE = { smooth: 0, corner: 1, symmetric: 2, free: 3, aligned: 4 };
// The order C goes through
const CYCLE = [NODE.smooth, NODE.corner, NODE.symmetric, NODE.aligned, NODE.free];
export const nodeType = (p) => (p[2] ? p[2] : 0);

// The in / out handle offsets of node i of a shape (clip space); an open shape's ends have no
// neighbor beyond them
export function nodeHandles(pts, i, open = false) {
  const n = pts.length, at = (k) => (open ? pts[Math.min(Math.max(k, 0), n - 1)] : pts[((k % n) + n) % n]);
  const p = at(i), typ = nodeType(p);
  if (typ === NODE.corner) return { in: [0, 0], out: [0, 0] };
  if (typ >= NODE.symmetric && p.length >= 7) return { in: [p[3], p[4]], out: [p[5], p[6]] };
  const a = at(i - 1), c = at(i + 1), dx = (c[0] - a[0]) / 6, dy = (c[1] - a[1]) / 6;
  return { in: [-dx, -dy], out: [dx, dy] };
}

// The four control points (clip space) of segment i (from node i to the next)
export function segment(pts, i, open = false) {
  const n = pts.length, b = pts[i], c = pts[(i + 1) % n];
  const o = nodeHandles(pts, i, open).out, inn = nodeHandles(pts, (i + 1) % n, open).in;
  return [[b[0], b[1]], [b[0] + o[0], b[1] + o[1]], [c[0] + inn[0], c[1] + inn[1]], [c[0], c[1]]];
}
const segCount = (pts, open) => (open ? pts.length - 1 : pts.length);

// A shape (clip space points) as an SVG path in frame px: map(u, v) → {x, y}
export function shapeSVG(pts, map, open = false) {
  const n = pts.length;
  if (n < (open ? 2 : 3)) return '';
  const f0 = map(pts[0][0], pts[0][1]);
  let d = `M${f0.x},${f0.y}`;
  for (let i = 0; i < segCount(pts, open); i++) {
    const [, c1, c2, e] = segment(pts, i, open).map((q) => map(q[0], q[1]));
    d += ` C${c1.x},${c1.y} ${c2.x},${c2.y} ${e.x},${e.y}`;
  }
  return open ? d : d + ' Z';
}
// One segment as an SVG path
export function segmentSVG(pts, i, map, open = false) {
  const [b, c1, c2, e] = segment(pts, i, open).map((q) => map(q[0], q[1]));
  return `M${b.x},${b.y} C${c1.x},${c1.y} ${c2.x},${c2.y} ${e.x},${e.y}`;
}
const bez = (P, t) => {
  const u = 1 - t;
  return [0, 1].map((k) => u * u * u * P[0][k] + 3 * u * u * t * P[1][k] + 3 * u * t * t * P[2][k] + t * t * t * P[3][k]);
};
// Where along segment i (0..1) the curve passes closest to frame point f
export function nearestT(pts, i, open, map, f) {
  const P = segment(pts, i, open);
  let bt = 0, bd = Infinity;
  for (let k = 0; k <= 64; k++) {
    const q = bez(P, k / 64), m = map(q[0], q[1]), d = Math.hypot(m.x - f.x, m.y - f.y);
    if (d < bd) { bd = d; bt = k / 64; }
  }
  return bt;
}
const r5 = (x) => +x.toFixed(5);
// Custom handles for a node: keeps it as it is, making smooth / symmetric nodes "aligned" so
// their handles can change independently without moving the rest of the curve
function customize(pts, i, open, h) {
  const p = pts[i], typ = nodeType(p);
  if (typ === NODE.corner && !h) return;
  const cur = nodeHandles(pts, i, open), hh = { ...cur, ...(h || {}) };
  pts[i] = [p[0], p[1], typ === NODE.free ? NODE.free : NODE.aligned, r5(hh.in[0]), r5(hh.in[1]), r5(hh.out[0]), r5(hh.out[1])];
}
// Splits segment i at t (de Casteljau): a new node on the curve, the shape stays the same.
// Returns the index of the new node.
export function splitSegment(shape, i, t) {
  const pts = shape.points, open = !!shape.open, n = pts.length, j = (i + 1) % n;
  const [P0, C1, C2, P3] = segment(pts, i, open);
  const L = (a, b) => [a[0] + (b[0] - a[0]) * t, a[1] + (b[1] - a[1]) * t];
  const Q0 = L(P0, C1), Q1 = L(C1, C2), Q2 = L(C2, P3), R0 = L(Q0, Q1), R1 = L(Q1, Q2), S = L(R0, R1);
  // the neighbors keep their other handle; their handle on this segment shortens
  const hi = nodeHandles(pts, i, open), hj = nodeHandles(pts, j, open);
  const cornerI = nodeType(pts[i]) === NODE.corner, cornerJ = nodeType(pts[j]) === NODE.corner;
  if (!cornerI) customize(pts, i, open, { in: hi.in, out: [Q0[0] - P0[0], Q0[1] - P0[1]] });
  if (!cornerJ) customize(pts, j, open, { in: [Q2[0] - P3[0], Q2[1] - P3[1]], out: hj.out });
  const node = [r5(S[0]), r5(S[1]), NODE.aligned, r5(R0[0] - S[0]), r5(R0[1] - S[1]), r5(R1[0] - S[0]), r5(R1[1] - S[1])];
  pts.splice(i + 1, 0, node);
  return i + 1;
}
// The shape the other way round (handles swapped)
export function reverseShape(shape) {
  const pts = shape.points.map((p) => (p.length >= 7 ? [p[0], p[1], p[2], p[5], p[6], p[3], p[4]] : [...p])).reverse();
  return { ...shape, points: pts };
}

// Kept for other callers: a closed smooth curve through frame points
export function smoothPath(fpts) {
  return shapeSVG(fpts.map((p) => [p.x, p.y, ...(p.c ? [1] : [])]), (x, y) => ({ x, y }));
}

// Ramer–Douglas–Peucker: fewer points along a freehand line (tolerance in px)
export function simplify(pts, tol) {
  if (pts.length < 3) return pts;
  const dist = (p, a, b) => {
    const dx = b.x - a.x, dy = b.y - a.y, l = dx * dx + dy * dy;
    const k = l ? Math.max(0, Math.min(1, ((p.x - a.x) * dx + (p.y - a.y) * dy) / l)) : 0;
    return Math.hypot(p.x - a.x - k * dx, p.y - a.y - k * dy);
  };
  const rec = (a, b, out) => {
    let best = -1, bi = -1;
    for (let i = a + 1; i < b; i++) { const d = dist(pts[i], pts[a], pts[b]); if (d > best) { best = d; bi = i; } }
    if (best > tol) { rec(a, bi, out); out.push(pts[bi]); rec(bi, b, out); }
  };
  const out = [pts[0]];
  rec(0, pts.length - 1, out);
  out.push(pts[pts.length - 1]);
  return out;
}

// The masks part of the clip inspector
export function MaskSection({ clip, kf, selMask, setSelMask, editClip, ctx, openEditor, screen = false }) {
  const masks = clip.masks || [];
  const add = (kind) => {
    editClip((c) => { c.masks = [...(c.masks || []), { id: newLayerId(), kind, mode: 'inside', feather: 0, opacity: 1, shapes: [], path: '' }]; });
    setSelMask(masks.length);
    openEditor(kind === 'raster' ? 'brush' : 'shape');
  };
  return html`<div class="section"><div class="sh">${t('Masks')}</div><div class="sb">
    ${masks.length === 0 && html`<p class="faint" style="margin:2px 0 8px;font-size:12px">${t(screen
      ? 'A mask limits where on the screen the text is seen (it stays still while the text moves). Draw it in the editor: a shape by hand (then its nodes), or paint it with the brush.'
      : 'A mask limits where the clip is seen. Draw it in the editor: a shape by hand (then its nodes), or paint it with the brush.')}</p>`}
    ${masks.map((m, k) => html`<div class=${'maskcard' + (selMask === k ? ' on' : '')} key=${m.id || k} onClick=${() => setSelMask(k)}>
      <div class="mh">
        <${Icon} name=${m.kind === 'raster' ? 'brush' : 'pen-tool'} size=${14} />
        <span class="lbl">${m.name || t(m.kind === 'raster' ? 'Painted mask {n}' : 'Shape mask {n}', { n: k + 1 })}</span>
        <span class="faint">${m.kind === 'raster' ? '' : t('{n} shapes', { n: (m.shapes || []).length })}</span>
        <span class="spacer"></span>
        <button class="btn sm" title=${t('Draw it in the editor')} onClick=${(e) => { e.stopPropagation(); setSelMask(k); openEditor(m.kind === 'raster' ? 'brush' : (m.shapes || []).length ? 'nodes' : 'shape'); }}>
          <${Icon} name="pencil" size=${13} /> ${t('Draw')}</button>
        <button class=${'btn sm icon' + (m.off ? '' : ' on')} title=${t(m.off ? 'Turn on' : 'Turn off')}
          onClick=${(e) => { e.stopPropagation(); editClip((c) => { c.masks[k].off = !c.masks[k].off; }); }}><${Icon} name=${m.off ? 'eye-off' : 'eye'} size=${13} /></button>
        <button class="btn sm icon ghost danger" title=${t('Delete mask')}
          onClick=${(e) => { e.stopPropagation(); editClip((c) => {
            c.masks.splice(k, 1);
            // keys of later masks move down one index
            if (c.keys) {
              const ks = {};
              for (const p in c.keys) {
                const mm = /^masks\.(\d+)\.(.*)$/.exec(p);
                if (!mm) ks[p] = c.keys[p];
                else if (+mm[1] < k) ks[p] = c.keys[p];
                else if (+mm[1] > k) ks[`masks.${+mm[1] - 1}.${mm[2]}`] = c.keys[p];
              }
              c.keys = ks;
            }
          }); setSelMask(null); }}><${Icon} name="trash-2" size=${13} /></button>
      </div>
      ${selMask === k && html`<div class="mb">
        <${Form} fields=${screen ? TEXT_MASK_FIELDS : MASK_FIELDS} value=${m} ctx=${ctx} kf=${kf} prefix=${'masks.' + k}
          onChange=${(nm) => editClip((c) => { c.masks[k] = nm; })} />
        ${m.kind !== 'raster' && kf && html`<div class="fld kfable"><label for="kf-none">
          <span class="kfbtn${kf.keys['masks.' + k + '.shapes'] ? ' anim' : ''}"><button class=${'kfd' + (kf.keys['masks.' + k + '.shapes'] && kf.keys['masks.' + k + '.shapes'].some((x) => Math.abs(x.t - kf.u) < 0.002) ? ' on' : '')}
            onClick=${() => kf.toggle('masks.' + k + '.shapes', maskShapes(clip, k, clip.start + kf.u))}></button></span>${t('Shape over time')}</label>
          <div class="ctl faint" style="font-size:11.5px">${t('Key the shape, move the cursor and edit its nodes: it morphs between keys (same number of points).')}</div></div>`}
      </div>`}
    </div>`)}
    <div class="row" style="margin-top:6px">
      <button class="btn sm" onClick=${() => add('vector')}><${Icon} name="pen-tool" size=${13} /> ${t('Shape mask')}</button>
      <button class="btn sm" onClick=${() => add('raster')}><${Icon} name="brush" size=${13} /> ${t('Painted mask')}</button>
    </div>
  </div></div>`;
}

// Editor overlay for the masks of the selected clip: shows them and does the mask tools.
// tool: shape | nodes | brush | eraser; brush: { size, hardness, opacity, pSize, pOpacity }
// pathShape (not undefined): edits a motion path instead (one open or closed line on the screen,
// null = none yet; editMask then gets and returns [path])
export function MaskOverlay({ clip, k, time, meta, tool, brush, toFrame, z, editMask, onPainted, geo, screen = false, pathShape }) {
  const pathMode = pathShape !== undefined;
  if (pathMode) screen = true;
  const natC = useNatural(screen ? null : clip.path);
  const nat = screen ? { w: meta.width, h: meta.height } : natC;
  const [live, setLive] = useState(null); // freehand points being drawn, or nodes being dragged
  const paint = useRef(null); // raster: { canvas, ctx } at the mask's size
  const [paintVer, setPaintVer] = useState(0);
  const [selNodes, setSelNodes] = useState([]); // nodes tool: [{ si, pi }]
  const m = pathMode ? PATH_MASK : clip.masks && clip.masks[k];
  const W = meta.width, H = meta.height;
  // Raster: the painted image in an offscreen canvas (loaded from its file the first time)
  useEffect(() => {
    if (!m || m.kind !== 'raster' || !nat) return;
    const s = Math.min(1, 1024 / Math.max(nat.w, nat.h));
    const cv = document.createElement('canvas');
    cv.width = Math.max(1, Math.round(nat.w * s)); cv.height = Math.max(1, Math.round(nat.h * s));
    const cx = cv.getContext('2d');
    paint.current = { canvas: cv, ctx: cx };
    if (m.path) {
      const im = new Image();
      im.onload = () => { cx.drawImage(im, 0, 0, cv.width, cv.height); setPaintVer((x) => x + 1); };
      im.src = '/api/media?path=' + encodeURIComponent(m.path) + '&v=' + Date.now();
    }
    setPaintVer((x) => x + 1);
  }, [m && m.id, m && m.kind, nat && nat.w]);
  if (!nat || !m) return null;
  // where the clip is shown in this frame (its background effects and pulse included)
  const g = screen ? frameGeom(meta) : shownGeom(clip, nat, meta, time, geo);
  const shapes = m.kind === 'raster' ? [] : (live && live.shapes) || (pathMode ? (pathShape ? [pathShape] : []) : maskShapes(clip, k, time));
  const toF = (p) => toFramePt(g, p[0], p[1]);
  const map = (u, v) => toFramePt(g, u, v);
  const hs = 1 / z; // handle size compensation

  const down = (e) => {
    if (e.button !== 0) return; // middle button: the view pans
    e.preventDefault();
    e.stopPropagation();
    try { e.currentTarget.setPointerCapture(e.pointerId); } catch (err) { /* synthetic */ }
    const p0 = toFrame(e.clientX, e.clientY);
    if (tool === 'shape') {
      const pts = [p0];
      setLive({ draw: pts });
      const mv = (ev) => { pts.push(toFrame(ev.clientX, ev.clientY)); setLive({ draw: [...pts] }); };
      const up = () => {
        removeEventListener('pointermove', mv); removeEventListener('pointerup', up);
        setLive(null);
        const sp = simplify(pts, 2.5 / Math.max(z, 0.2));
        const sh = { points: sp.map((p) => toClipPt(g, p.x, p.y).map((x) => r5(x))) };
        // a path: one line, drawn again replaces it
        if (pathMode) { if (sp.length >= 2) editMask(() => [{ ...sh, open: true }]); }
        else if (sp.length >= 3) editMask((cur) => [...cur, sh]);
      };
      addEventListener('pointermove', mv); addEventListener('pointerup', up);
    } else if (tool === 'brush' || tool === 'eraser') {
      const P = paint.current;
      if (!P) return;
      const cv = P.canvas, cx = P.ctx;
      let last = null;
      const stamp = (ev) => {
        const f = toFrame(ev.clientX, ev.clientY);
        const [u, v] = toClipPt(g, f.x, f.y);
        const px = u * cv.width, py = v * cv.height;
        const pr = ev.pointerType === 'pen' ? Math.max(0.05, ev.pressure || 0.5) : 1;
        // brush size in frame px → mask px
        const k2 = cv.width / g.w;
        const r = Math.max(0.5, (brush.size / 2) * (brush.pSize ? pr : 1) * k2);
        const a = brush.opacity * (brush.pOpacity ? pr : 1);
        const dot = (x, y) => {
          const gr = cx.createRadialGradient(x, y, 0, x, y, r);
          gr.addColorStop(0, `rgba(255,255,255,${a})`);
          gr.addColorStop(Math.min(0.99, brush.hardness), `rgba(255,255,255,${a})`);
          gr.addColorStop(1, 'rgba(255,255,255,0)');
          cx.fillStyle = gr;
          cx.beginPath(); cx.arc(x, y, r, 0, Math.PI * 2); cx.fill();
        };
        cx.globalCompositeOperation = tool === 'eraser' ? 'destination-out' : 'source-over';
        if (last) {
          const d = Math.hypot(px - last.x, py - last.y), steps = Math.max(1, Math.ceil(d / Math.max(1, r * 0.35)));
          for (let i = 1; i <= steps; i++) dot(last.x + ((px - last.x) * i) / steps, last.y + ((py - last.y) * i) / steps);
        } else dot(px, py);
        last = { x: px, y: py };
        setPaintVer((x) => x + 1);
      };
      stamp(e);
      const mv = (ev) => stamp(ev);
      const up = () => {
        removeEventListener('pointermove', mv); removeEventListener('pointerup', up);
        cx.globalCompositeOperation = 'source-over';
        cv.toBlob((b) => b && onPainted(b));
      };
      addEventListener('pointermove', mv); addEventListener('pointerup', up);
    }
  };

  // Node operations (on a copy of the shapes, then saved). Selection: [{ si, pi }]
  const copyShapes = () => shapes.map((s) => ({ ...s, points: s.points.map((p) => [...p]) }));
  const isSel = (si, pi) => selNodes.some((x) => x.si === si && x.pi === pi);
  const save = (cur) => editMask(() => cur.filter((s) => s.points.length >= (s.open ? 2 : 3)));
  // Removes nodes (a closed shape left with 2 nodes becomes a line; shapes too small go)
  const removeNodes = (list) => {
    const cur = copyShapes();
    [...list].sort((a, b) => b.pi - a.pi).forEach(({ si, pi }) => cur[si] && cur[si].points.splice(pi, 1));
    cur.forEach((s) => { if (!s.open && s.points.length === 2) s.open = true; });
    save(cur);
    setSelNodes([]);
  };
  // Changes a node's type; custom handles start from the curve as it is (no jump)
  const setType = (si, pi, typ) => {
    const cur = copyShapes();
    const sh = cur[si], pts = sh.points, p = pts[pi];
    let h = nodeHandles(pts, pi, sh.open);
    if (typ >= NODE.symmetric && nodeType(p) === NODE.corner) {
      // from a corner: the automatic handles of a smooth node
      h = nodeHandles(pts.map((q, k) => (k === pi ? [q[0], q[1]] : q)), pi, sh.open);
    }
    if (typ === NODE.symmetric) h = { in: [-h.out[0], -h.out[1]], out: h.out };
    if (typ === NODE.aligned) {
      // in line with the out handle, keeping the in handle's length
      const lo = Math.hypot(h.out[0], h.out[1]) || 1, li = Math.hypot(h.in[0], h.in[1]);
      h = { in: [-h.out[0] / lo * li, -h.out[1] / lo * li], out: h.out };
    }
    pts[pi] = typ === NODE.smooth ? [p[0], p[1]] : typ === NODE.corner ? [p[0], p[1], 1]
      : [p[0], p[1], typ, r5(h.in[0]), r5(h.in[1]), r5(h.out[0]), r5(h.out[1])];
    save(cur);
  };
  const cycleType = (si, pi) => setType(si, pi, CYCLE[(CYCLE.indexOf(nodeType(shapes[si].points[pi])) + 1) % CYCLE.length]);
  const removeShape = (si) => { const cur = copyShapes(); cur.splice(si, 1); save(cur); setSelNodes([]); };
  const closeShape = (si) => { const cur = copyShapes(); cur[si].open = false; save(cur); };
  // Deleting segment i (node i → next): a closed shape opens there; an open one splits in two
  const removeSegment = (si, i) => {
    const cur = copyShapes(), sh = cur[si], n = sh.points.length;
    if (!sh.open) {
      sh.points = [...sh.points.slice(i + 1), ...sh.points.slice(0, i + 1)];
      sh.open = true;
    } else {
      const a = { ...sh, points: sh.points.slice(0, i + 1) }, b = { ...sh, points: sh.points.slice(i + 1, n) };
      cur.splice(si, 1, a, b);
    }
    save(cur);
    setSelNodes([]);
  };
  // A node at a point of a segment, on the curve (the shape does not change)
  const addNodeAt = (si, i, f) => {
    const cur = copyShapes(), sh = cur[si];
    const pi = splitSegment(sh, i, Math.min(0.98, Math.max(0.02, nearestT(sh.points, i, sh.open, map, f))));
    save(cur);
    setSelNodes([{ si, pi }]);
  };
  // Joining two selected ends: of the same open shape, it closes; of two open shapes, they become one
  const ends = (si) => (shapes[si] && shapes[si].open ? [0, shapes[si].points.length - 1] : []);
  const canJoin = selNodes.length === 2 && selNodes.every(({ si, pi }) => ends(si).includes(pi))
    && (selNodes[0].si !== selNodes[1].si || shapes[selNodes[0].si].points.length >= 3);
  const join = () => {
    const [x, y] = selNodes, cur = copyShapes();
    if (x.si === y.si) { cur[x.si].open = false; save(cur); setSelNodes([]); return; }
    let A = cur[x.si], B = cur[y.si];
    if (x.pi === 0) A = reverseShape(A);              // the selected end of A last
    if (y.pi !== 0) B = reverseShape(B);              // the selected end of B first
    const merged = { ...A, points: [...A.points, ...B.points], open: true };
    const rest = cur.filter((_, k) => k !== x.si && k !== y.si);
    save([...rest, merged]);
    setSelNodes([]);
  };
  // Keys of the nodes tool: Delete / Backspace removes the selected nodes, C changes the type, J joins
  useEffect(() => {
    if (tool !== 'nodes' || !selNodes.length) return;
    const k = (e) => {
      if (/^(INPUT|TEXTAREA|SELECT)$/.test(e.target.tagName)) return;
      if (e.key === 'Delete' || e.key === 'Backspace') { e.preventDefault(); e.stopPropagation(); removeNodes(selNodes); }
      else if ((e.key === 'c' || e.key === 'C') && selNodes.length === 1) { e.stopPropagation(); cycleType(selNodes[0].si, selNodes[0].pi); }
      else if ((e.key === 'j' || e.key === 'J') && canJoin) { e.stopPropagation(); join(); }
    };
    addEventListener('keydown', k, true);
    return () => removeEventListener('keydown', k, true);
  });
  // Nodes: click selects (Shift: adds to the selection), drag moves (all the selected ones);
  // Alt+click removes; right-click: menu
  const nodeDown = (e, si, pi) => {
    if (e.button !== 0) return;
    e.preventDefault();
    e.stopPropagation();
    if (e.altKey) { removeNodes([{ si, pi }]); return; }
    let sel = selNodes;
    if (e.shiftKey) sel = isSel(si, pi) ? sel.filter((x) => !(x.si === si && x.pi === pi)) : [...sel, { si, pi }];
    else if (!isSel(si, pi)) sel = [{ si, pi }];
    setSelNodes(sel);
    const cur = copyShapes(), base = copyShapes(), f0 = toFrame(e.clientX, e.clientY);
    const [u0, v0] = toClipPt(g, f0.x, f0.y);
    let moved = false;
    const mv = (ev) => {
      moved = true;
      const f = toFrame(ev.clientX, ev.clientY);
      const [u, v] = toClipPt(g, f.x, f.y);
      for (const { si: a, pi: b } of sel) {
        const o = base[a] && base[a].points[b];
        if (o) cur[a].points[b] = [r5(o[0] + u - u0), r5(o[1] + v - v0), ...o.slice(2)];
      }
      setLive({ shapes: cur.map((s) => ({ ...s, points: s.points.map((p) => [...p]) })) });
    };
    const up = () => {
      removeEventListener('pointermove', mv); removeEventListener('pointerup', up);
      setLive(null);
      if (moved) save(cur);
    };
    addEventListener('pointermove', mv); addEventListener('pointerup', up);
  };
  const nodeMenu = (e, si, pi) => {
    if (!isSel(si, pi)) setSelNodes([{ si, pi }]);
    const typ = nodeType(shapes[si].points[pi]);
    const multi = isSel(si, pi) && selNodes.length > 1;
    openMenu(e, [
      ...(canJoin && isSel(si, pi) ? [{ label: t(selNodes[0].si === selNodes[1].si ? 'Join the ends: close the shape (J)' : 'Join the two lines (J)'), icon: 'merge', onClick: join }, '-'] : []),
      { header: t('Node type (C: next)') },
      { label: t('Smooth (automatic)'), icon: 'spline', checked: typ === NODE.smooth, onClick: () => setType(si, pi, NODE.smooth) },
      { label: t('Corner'), icon: 'chevron-up', checked: typ === NODE.corner, onClick: () => setType(si, pi, NODE.corner) },
      { label: t('Symmetric handles'), icon: 'arrow-left-right', checked: typ === NODE.symmetric, onClick: () => setType(si, pi, NODE.symmetric) },
      { label: t('Aligned handles (own lengths)'), icon: 'minus', checked: typ === NODE.aligned, onClick: () => setType(si, pi, NODE.aligned) },
      { label: t('Free handles'), icon: 'move', checked: typ === NODE.free, onClick: () => setType(si, pi, NODE.free) },
      '-',
      { label: t(multi ? 'Delete the selected nodes (Del)' : 'Delete node (Del)'), icon: 'minus', onClick: () => removeNodes(multi ? selNodes : [{ si, pi }]) },
      ...(shapes[si].open ? [{ label: t('Close the shape'), icon: 'circle', onClick: () => closeShape(si) }] : []),
      '-',
      { label: t('Delete shape'), icon: 'trash-2', danger: true, onClick: () => removeShape(si) },
    ]);
  };
  // Right-click on the line: a node there, delete this segment, close, delete the shape
  const segMenu = (e, si, i) => {
    const f = toFrame(e.clientX, e.clientY);
    openMenu(e, [
      { header: t('Line') },
      { label: t('Add a node here'), icon: 'plus', onClick: () => addNodeAt(si, i, f) },
      { label: t('Delete this segment (opens the shape)'), icon: 'scissors', onClick: () => removeSegment(si, i) },
      ...(shapes[si].open ? [{ label: t('Close the shape'), icon: 'circle', onClick: () => closeShape(si) }] : []),
      '-',
      { label: t('Delete shape'), icon: 'trash-2', danger: true, onClick: () => removeShape(si) },
    ]);
  };
  // Dragging a handle of a custom node (h: 'in' | 'out'): symmetric mirrors the other one,
  // aligned turns it to stay in line
  const handleDown = (e, si, pi, h) => {
    if (e.button !== 0) return;
    e.preventDefault();
    e.stopPropagation();
    const cur = copyShapes();
    const mv = (ev) => {
      const f = toFrame(ev.clientX, ev.clientY);
      const p = cur[si].points[pi];
      const [u, v] = toClipPt(g, f.x, f.y);
      const d = [r5(u - p[0]), r5(v - p[1])];
      const io = h === 'in' ? 3 : 5, oo = h === 'in' ? 5 : 3;
      p[io] = d[0]; p[io + 1] = d[1];
      if (nodeType(p) === NODE.symmetric) { p[oo] = -d[0]; p[oo + 1] = -d[1]; }
      else if (nodeType(p) === NODE.aligned) {
        const len = Math.hypot(p[oo], p[oo + 1]), dl = Math.hypot(d[0], d[1]) || 1;
        p[oo] = r5(-d[0] / dl * len); p[oo + 1] = r5(-d[1] / dl * len);
      }
      setLive({ shapes: cur.map((s) => ({ ...s, points: s.points.map((q) => [...q]) })) });
    };
    const up = () => {
      removeEventListener('pointermove', mv); removeEventListener('pointerup', up);
      setLive(null);
      save(cur);
    };
    addEventListener('pointermove', mv); addEventListener('pointerup', up);
  };

  const drawing = tool === 'shape' || tool === 'brush' || tool === 'eraser';
  const P = paint.current;
  return html`<div class=${'maskov' + (drawing ? ' active' : '')} onPointerDown=${drawing ? down : null}
      style=${{ cursor: tool === 'shape' ? 'crosshair' : drawing ? 'none' : '' }}>
    ${m.kind === 'raster' && P && html`<${PaintView} canvas=${P.canvas} g=${g} ver=${paintVer} />`}
    <svg class="masksvg" width=${W} height=${H} viewBox=${`0 0 ${W} ${H}`}>
      ${!pathMode && (clip.masks || []).map((om, ok) => ok !== k && om.kind !== 'raster' && !om.off && maskShapes(clip, ok, time).map((s, si) => html`<path key=${'o' + ok + '-' + si}
        d=${shapeSVG(s.points, map, s.open)} class="mask-other" style=${{ strokeWidth: hs }} />`))}
      ${shapes.map((s, si) => {
        const d = shapeSVG(s.points, map, s.open);
        const nodes = tool === 'nodes';
        return html`<g key=${si}>
          ${!s.open && !pathMode && html`<path d=${d} class="mask-fill" />`}
          <path d=${d} class=${'mask-line' + (pathMode ? ' path' : s.open ? ' open' : '')} style=${{ strokeWidth: (pathMode ? 2 : 1.5) * hs }} />
          ${pathMode && s.points.length > 0 && html`<circle class="path-start" cx=${toF(s.points[0]).x} cy=${toF(s.points[0]).y} r=${9 * hs} style=${{ strokeWidth: 1.5 * hs }} />`}
          ${nodes && Array.from({ length: s.open ? s.points.length - 1 : s.points.length }, (_, i) => html`<path key=${'s' + i}
            d=${segmentSVG(s.points, i, map, s.open)} class="mask-hit" style=${{ strokeWidth: 10 * hs }}
            onDblClick=${(e) => { e.stopPropagation(); addNodeAt(si, i, toFrame(e.clientX, e.clientY)); }}
            onContextMenu=${(e) => segMenu(e, si, i)} />`)}
          ${nodes && s.points.map((p, pi) => {
            if (!isSel(si, pi) || selNodes.length > 1 || nodeType(p) < NODE.symmetric) return null;
            // the handles of the selected custom node
            const f = toF(p), hh = nodeHandles(s.points, pi, s.open);
            return ['in', 'out'].map((side) => {
              const q = map(p[0] + hh[side][0], p[1] + hh[side][1]);
              return html`<g key=${'h' + side}><line class="mask-hline" x1=${f.x} y1=${f.y} x2=${q.x} y2=${q.y} style=${{ strokeWidth: hs }} />
                <circle class="mask-handle" cx=${q.x} cy=${q.y} r=${4 * hs} style=${{ strokeWidth: hs }} onPointerDown=${(e) => handleDown(e, si, pi, side)} /></g>`;
            });
          })}
          ${nodes && s.points.map((p, pi) => {
            const f = toF(p), typ = nodeType(p);
            const ev = { onPointerDown: (e) => nodeDown(e, si, pi), onContextMenu: (e) => nodeMenu(e, si, pi) };
            const cls = 'mask-node' + (isSel(si, pi) ? ' on' : '') + (s.open && (pi === 0 || pi === s.points.length - 1) ? ' end' : '');
            // smooth: circle · corner: square · symmetric: diamond · aligned: hexagon · free: triangle
            if (typ === NODE.aligned) {
              const r = 5.5 * hs, hx = [0, 1, 2, 3, 4, 5].map((i) => `${f.x + r * Math.cos((i * Math.PI) / 3)},${f.y + r * Math.sin((i * Math.PI) / 3)}`);
              return html`<polygon key=${pi} class=${cls} points=${hx.join(' ')} style=${{ strokeWidth: hs }} ...${ev} />`;
            }
            if (typ === NODE.corner) return html`<rect key=${pi} class=${cls} x=${f.x - 4.5 * hs} y=${f.y - 4.5 * hs} width=${9 * hs} height=${9 * hs} style=${{ strokeWidth: hs }} ...${ev} />`;
            if (typ === NODE.symmetric) return html`<rect key=${pi} class=${cls} x=${f.x - 4 * hs} y=${f.y - 4 * hs} width=${8 * hs} height=${8 * hs}
              transform=${`rotate(45 ${f.x} ${f.y})`} style=${{ strokeWidth: hs }} ...${ev} />`;
            if (typ === NODE.free) return html`<path key=${pi} class=${cls} d=${`M${f.x},${f.y - 6 * hs} L${f.x + 5.5 * hs},${f.y + 4 * hs} L${f.x - 5.5 * hs},${f.y + 4 * hs} Z`} style=${{ strokeWidth: hs }} ...${ev} />`;
            return html`<circle key=${pi} class=${cls} cx=${f.x} cy=${f.y} r=${5 * hs} style=${{ strokeWidth: hs }} ...${ev} />`;
          })}
        </g>`;
      })}
      ${live && live.draw && html`<polyline class="mask-draw" points=${live.draw.map((p) => p.x + ',' + p.y).join(' ')} style=${{ strokeWidth: 1.5 * hs }} />`}
    </svg>
    ${(tool === 'brush' || tool === 'eraser') && html`<${BrushCursor} size=${brush.size} hs=${hs} toFrame=${toFrame} />`}
  </div>`;
}

const PATH_MASK = { kind: 'vector', id: 'path' };

// The painted mask shown over the clip (a red tint where it is painted)
function PaintView({ canvas, g, ver }) {
  const ref = useRef();
  useEffect(() => {
    const c = ref.current;
    if (!c) return;
    if (c.width !== canvas.width || c.height !== canvas.height) { c.width = canvas.width; c.height = canvas.height; }
    const x = c.getContext('2d');
    x.clearRect(0, 0, c.width, c.height);
    x.drawImage(canvas, 0, 0);
    x.globalCompositeOperation = 'source-in';
    x.fillStyle = 'rgba(255,60,90,0.45)';
    x.fillRect(0, 0, c.width, c.height);
    x.globalCompositeOperation = 'source-over';
  }, [ver, canvas]);
  return html`<canvas ref=${ref} class="maskpaint" style=${{ left: g.cx - g.w / 2 + 'px', top: g.cy - g.h / 2 + 'px', width: g.w + 'px', height: g.h + 'px',
    transform: `rotate(${g.rot}rad) scale(${g.fx}, ${g.fy})` }}></canvas>`;
}

// A circle of the brush's size following the pointer
function BrushCursor({ size, hs, toFrame }) {
  const [p, setP] = useState(null);
  useEffect(() => {
    const mv = (e) => setP(toFrame(e.clientX, e.clientY));
    addEventListener('pointermove', mv);
    return () => removeEventListener('pointermove', mv);
  }, [toFrame]);
  if (!p) return null;
  return html`<div class="brushcur" style=${{ left: p.x - size / 2 + 'px', top: p.y - size / 2 + 'px', width: size + 'px', height: size + 'px', borderWidth: hs + 'px' }}></div>`;
}

export { getIn };
