import { html, useEffect, useLayoutEffect, useRef, useState } from './vendor/preact-htm.js';
import { useStore } from './store.js';
import { fmtTime } from './util.js';
import { Icon } from './icons.js';
import { t } from './i18n.js';
import { useFrameStream } from './frames.js';
import { ClipHandles, TextHandles, textBoxOf, geoOf } from './layers.js';
import { MaskOverlay } from './masks.js';
import { EmitterHandles } from './emitters.js';
import { keyTimes } from './keys.js';
import { keyLabel, targetKey, isPicked, pickedTimes, pickKey, moveKeysAt, keyMenuItems, FocusChip } from './keymarks.js';
import { openMenu } from './menu.js';

// The editor: the preview over the whole browser, as a canvas that can be zoomed, panned and
// rotated (like Krita), with tools on the left, the inspector on the right and a small timeline
// below. Mouse, pen (pressure) and touch (two fingers: pan, zoom and rotate together).
//
// View transform (CSS on the frame, origin at its top-left corner):
//   screen = t + R(r) · z · frame     →     frame = R(−r) · (screen − t) / z

const TOOLS = [
  { id: 'select', icon: 'mouse-pointer-2', label: 'Select and transform (V)', key: 'v' },
  { id: 'hand', icon: 'hand', label: 'Pan the view (H, or middle button, or two fingers)', key: 'h' },
  { id: 'zoom', icon: 'zoom-in', label: 'Zoom: click to zoom in, Alt+click to zoom out, drag to zoom (Z)', key: 'z' },
  { id: 'rotate', icon: 'rotate-ccw', label: 'Rotate the view (R; Shift: steps of 15°)', key: 'r' },
  { sep: true },
  { id: 'shape', icon: 'lasso', label: 'Mask shape: draw it by hand (S)', key: 's', mask: 'vector' },
  { id: 'nodes', icon: 'spline', label: 'Mask nodes: drag them; double-click the line adds one, Alt+click removes one (N)', key: 'n', mask: 'vector' },
  { id: 'brush', icon: 'brush', label: 'Paint the mask (B; the pen\'s pressure can set size and opacity)', key: 'b', mask: 'raster' },
  { id: 'eraser', icon: 'eraser', label: 'Erase the painted mask (E)', key: 'e', mask: 'raster' },
  { sep: true },
  { id: 'path', icon: 'route', label: 'Motion path: draw it by hand, then drag its nodes (P)', key: 'p', path: true },
];

// Pointer capture keeps the events of a drag even outside the element (best effort)
export const capture = (el, id) => { try { el.setPointerCapture(id); } catch (err) { /* not an active pointer */ } };

const rot = (x, y, deg) => {
  const a = (deg * Math.PI) / 180, c = Math.cos(a), s = Math.sin(a);
  return { x: x * c - y * s, y: x * s + y * c };
};

export function Editor({ meta, timeStore, playing, setPlaying, seek, rev, selClip, selEmit = false, onClip, inspector, onClose, marks = null, onKeys, kf = {},
  tool: tool0 = 'select', selMask = null, editMask, onPainted, onNeedMask, selId, selClipIdx, selText, selTextIdx, onText, maskOwner = null, pathOwner = null, editPath }) {
  const time = useStore(timeStore);
  const W = meta.width, H = meta.height;
  const viewRef = useRef();
  const canvas = useRef();
  const [tool, setToolState] = useState(tool0);
  useEffect(() => setToolState(tool0), [tool0]);
  // A mask tool needs a mask of its kind on the selected clip (onNeedMask creates or picks one)
  const setTool = (id) => {
    const tl = TOOLS.find((x) => x.id === id);
    if (tl && tl.mask) onNeedMask && onNeedMask(tl.mask);
    setToolState(id);
  };
  // Brush settings (remembered by the browser)
  const [brush, setBrushState] = useState(() => {
    try { return { size: 60, hardness: 0.6, opacity: 1, pSize: true, pOpacity: false, ...JSON.parse(localStorage.getItem('audioviz.brush') || '{}') }; }
    catch (e) { return { size: 60, hardness: 0.6, opacity: 1, pSize: true, pOpacity: false }; }
  });
  const setToolRef = useRef(setTool);
  setToolRef.current = setTool;
  const maskRef = useRef(maskOwner); // what the mask tools work on (a clip, a text, a lyrics segment)
  maskRef.current = maskOwner;
  const pathRef = useRef(pathOwner); // what the path tool works on (a clip or a text)
  pathRef.current = pathOwner;
  const [pathDraw, setPathDraw] = useState(false); // drawing the path again (the nodes otherwise)
  const curPath = pathOwner && pathOwner.obj.motion && pathOwner.obj.motion.path && pathOwner.obj.motion.path.points ? pathOwner.obj.motion.path : null;
  const setBrush = (k, v) => setBrushState((b) => {
    const n = { ...b, [k]: v };
    try { localStorage.setItem('audioviz.brush', JSON.stringify(n)); } catch (e) { /* ignore */ }
    return n;
  });
  const [v, setV] = useState(null); // { z, r, tx, ty }
  const vRef = useRef(v);
  vRef.current = v;
  const [panel, setPanel] = useState(true);
  const [pen, setPen] = useState(null); // last pointer over the view: type, pressure, tilt, frame position

  // Quality of the frames: full when zoomed in enough, reduced when the whole frame is small
  const dpr = window.devicePixelRatio || 1;
  const scale = v ? Math.max(1, Math.min(4, Math.floor(1 / Math.max(0.01, v.z * dpr)))) : 2;
  const info = useFrameStream(canvas, { meta, time, scale, rev, playing });

  const rect = () => viewRef.current.getBoundingClientRect();
  const fit = () => {
    const r = rect();
    const z = Math.min(r.width / W, r.height / H) * 0.94;
    setV({ z, r: 0, tx: (r.width - W * z) / 2, ty: (r.height - H * z) / 2 });
  };
  useLayoutEffect(() => { fit(); }, [W, H]);
  // client → frame px
  const toFrame = (cx, cy) => {
    const r = rect(), s = vRef.current;
    const p = rot(cx - r.left - s.tx, cy - r.top - s.ty, -s.r);
    return { x: p.x / s.z, y: p.y / s.z };
  };
  // Keeps frame point f under view point (lx, ly) for zoom z and rotation r
  const anchor = (f, lx, ly, z, r) => {
    const p = rot(f.x * z, f.y * z, r);
    return { z, r, tx: lx - p.x, ty: ly - p.y };
  };
  const zoomAt = (factor, cx, cy) => {
    const r = rect(), s = vRef.current;
    const lx = cx - r.left, ly = cy - r.top;
    const z = Math.max(0.05, Math.min(32, s.z * factor));
    setV(anchor(toFrame(cx, cy), lx, ly, z, s.r));
  };
  const setZoom = (z) => { const r = rect(); zoomAt(z / vRef.current.z, r.left + r.width / 2, r.top + r.height / 2); };
  const rotateTo = (deg) => {
    const r = rect(), s = vRef.current;
    const cx = r.left + r.width / 2, cy = r.top + r.height / 2;
    setV(anchor(toFrame(cx, cy), r.width / 2, r.height / 2, s.z, ((deg % 360) + 540) % 360 - 180));
  };

  // Pointers: one = the tool (middle button = pan); two touches = pan + zoom + rotate
  const pts = useRef(new Map());
  const gesture = useRef(null);
  const onPointerDown = (e) => {
    capture(viewRef.current, e.pointerId);
    pts.current.set(e.pointerId, { x: e.clientX, y: e.clientY });
    const s = vRef.current;
    if (pts.current.size === 2) {
      const [a, b] = [...pts.current.values()];
      const mid = { x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 };
      gesture.current = { kind: 'pinch', v0: s, f: toFrame(mid.x, mid.y), d0: Math.hypot(b.x - a.x, b.y - a.y), a0: Math.atan2(b.y - a.y, b.x - a.x) };
      return;
    }
    // Only the view tools (and the middle button) act here: the select and mask tools have their
    // own handlers (capturing the pointer here would steal their clicks and double-clicks)
    const mode = e.button === 1 ? 'hand' : ['hand', 'zoom', 'rotate'].includes(tool) ? tool : null;
    if (!mode) return;
    e.preventDefault();
    const r = rect();
    const c = { x: r.left + r.width / 2, y: r.top + r.height / 2 };
    gesture.current = { kind: mode, x0: e.clientX, y0: e.clientY, v0: s, f: toFrame(e.clientX, e.clientY),
      fc: toFrame(c.x, c.y), c, a0: Math.atan2(e.clientY - c.y, e.clientX - c.x), alt: e.altKey, moved: false };
  };
  const onPointerMove = (e) => {
    const p = toFrame(e.clientX, e.clientY);
    setPen({ type: e.pointerType, pressure: e.pressure, tilt: [e.tiltX || 0, e.tiltY || 0], x: p.x, y: p.y });
    if (!pts.current.has(e.pointerId)) return;
    pts.current.set(e.pointerId, { x: e.clientX, y: e.clientY });
    const g = gesture.current;
    if (!g) return;
    const r = rect();
    if (g.kind === 'pinch' && pts.current.size >= 2) {
      const [a, b] = [...pts.current.values()];
      const mid = { x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 };
      const z = Math.max(0.05, Math.min(32, g.v0.z * Math.hypot(b.x - a.x, b.y - a.y) / (g.d0 || 1)));
      const rr = g.v0.r + ((Math.atan2(b.y - a.y, b.x - a.x) - g.a0) * 180) / Math.PI;
      setV(anchor(g.f, mid.x - r.left, mid.y - r.top, z, rr));
      return;
    }
    const dx = e.clientX - g.x0, dy = e.clientY - g.y0;
    if (Math.abs(dx) + Math.abs(dy) > 3) g.moved = true;
    if (g.kind === 'hand') setV({ ...g.v0, tx: g.v0.tx + dx, ty: g.v0.ty + dy });
    else if (g.kind === 'zoom' && g.moved) setV(anchor(g.f, g.x0 - r.left, g.y0 - r.top, Math.max(0.05, Math.min(32, g.v0.z * Math.exp(dx / 200))), g.v0.r));
    else if (g.kind === 'rotate') {
      let rr = g.v0.r + ((Math.atan2(e.clientY - g.c.y, e.clientX - g.c.x) - g.a0) * 180) / Math.PI;
      if (e.shiftKey) rr = Math.round(rr / 15) * 15;
      setV(anchor(g.fc, g.c.x - r.left, g.c.y - r.top, g.v0.z, rr));
    }
  };
  const onPointerUp = (e) => {
    pts.current.delete(e.pointerId);
    const g = gesture.current;
    if (g && g.kind === 'zoom' && !g.moved) zoomAt(g.alt ? 1 / 1.6 : 1.6, e.clientX, e.clientY);
    if (pts.current.size === 0) gesture.current = null;
  };
  const onWheel = (e) => {
    e.preventDefault();
    if (e.shiftKey) { setV({ ...vRef.current, tx: vRef.current.tx - e.deltaY }); return; }
    zoomAt(Math.exp(-e.deltaY * (e.deltaMode === 1 ? 0.05 : 0.0015)), e.clientX, e.clientY);
  };

  // Keys: tools, 0 = fit, 1 = 100 %, Esc = close (the app keeps space, arrows, undo...)
  useEffect(() => {
    const k = (e) => {
      if (/^(INPUT|TEXTAREA|SELECT)$/.test(e.target.tagName) || e.ctrlKey || e.metaKey) return;
      if (e.key === 'Escape') { onClose(); return; }
      if (e.key === '0') { fit(); return; }
      if (e.key === '1') { setZoom(1 / dpr); return; }
      const tl = TOOLS.find((x) => x.key === e.key.toLowerCase());
      if (tl && !(tl.mask && !maskRef.current) && !(tl.path && !pathRef.current)) { setToolRef.current(tl.id); e.stopPropagation(); }
    };
    addEventListener('keydown', k, true);
    return () => removeEventListener('keydown', k, true);
  }, []);

  const cursor = { hand: 'grab', zoom: 'zoom-in', rotate: 'alias' }[tool] || 'default';
  const world = v && { width: W + 'px', height: H + 'px', transform: `translate(${v.tx}px, ${v.ty}px) rotate(${v.r}deg) scale(${v.z})` };
  const showClip = selClip && (selClip.path || selEmit) && time >= selClip.start && time < selClip.end;
  return html`<div class="editor">
    <div class="ed-top">
      <span class="ed-title"><${Icon} name="pen-tool" size=${15} /> ${t('Editor')}</span>
      ${(tool === 'brush' || tool === 'eraser') && html`<div class="ed-opts">
        <label>${t('Size')}<input type="range" min="2" max="600" step="1" value=${brush.size} onInput=${(e) => setBrush('size', +e.target.value)} /><span class="mono">${brush.size}</span></label>
        <label>${t('Hardness')}<input type="range" min="0" max="1" step="0.01" value=${brush.hardness} onInput=${(e) => setBrush('hardness', +e.target.value)} /></label>
        <label>${t('Opacity')}<input type="range" min="0.02" max="1" step="0.01" value=${brush.opacity} onInput=${(e) => setBrush('opacity', +e.target.value)} /></label>
        <span class="faint">${t('Pressure →')}</span>
        <button class=${'btn sm' + (brush.pSize ? ' on' : '')} onClick=${() => setBrush('pSize', !brush.pSize)}>${t('size')}</button>
        <button class=${'btn sm' + (brush.pOpacity ? ' on' : '')} onClick=${() => setBrush('pOpacity', !brush.pOpacity)}>${t('opacity')}</button>
      </div>`}
      ${tool === 'path' && html`<div class="ed-opts faint">${t(curPath && !pathDraw
        ? 'The path it follows (start: the ring). Drag its nodes; right-click a node or the line: menu. Its progress over time is in the inspector.'
        : 'Draw the path by hand, from where it starts to where it ends.')}
        ${curPath && html`<button class=${'btn sm' + (pathDraw ? ' on' : '')} onClick=${() => setPathDraw(!pathDraw)}><${Icon} name="pencil" size=${13} /> ${t('Draw again')}</button>`}</div>`}
      ${(tool === 'shape' || tool === 'nodes') && html`<div class="ed-opts faint">${t(tool === 'shape'
        ? (maskOwner && maskOwner.screen ? 'Draw a closed shape on the screen: the text is seen inside it (or outside, in the mask settings). Several shapes add up.' : 'Draw a closed shape over the clip: the clip is seen inside it (or outside, in the mask settings). Several shapes add up.')
        : 'Click a node (Shift: several), drag to move · right-click a node or the line: menu (add a node, delete a segment, close, join) · double-click the line: a node there · Del: delete · C: node type · J: join two ends')}</div>`}
      <span class="spacer"></span>
      <div class="btngroup">
        <button class="btn sm icon" title=${t('Zoom out')} onClick=${() => setZoom((v ? v.z : 1) / 1.5)}><${Icon} name="zoom-out" size=${14} /></button>
        <span class="ed-zoom mono">${v ? Math.round(v.z * dpr * 100) : 100}%</span>
        <button class="btn sm icon" title=${t('Zoom in')} onClick=${() => setZoom((v ? v.z : 1) * 1.5)}><${Icon} name="zoom-in" size=${14} /></button>
      </div>
      <button class="btn sm" title=${t('Fit the frame (0)')} onClick=${fit}><${Icon} name="maximize-2" size=${14} /> ${t('Fit')}</button>
      <button class="btn sm" title=${t('Actual pixels (1)')} onClick=${() => setZoom(1 / dpr)}>1:1</button>
      <button class=${'btn sm' + (v && v.r ? ' on' : '')} title=${t('Rotation of the view: click to reset')} onClick=${() => rotateTo(0)}>
        <${Icon} name="rotate-ccw" size=${14} /> ${v ? Math.round(v.r) : 0}°</button>
      <button class=${'btn sm icon' + (panel ? ' on' : '')} title=${t('Show / hide the inspector')} onClick=${() => setPanel(!panel)}><${Icon} name="sliders-horizontal" size=${14} /></button>
      <button class="btn sm" title=${t('Close the editor (Esc)')} onClick=${onClose}><${Icon} name="x" size=${14} /> ${t('Close')}</button>
    </div>
    <div class="ed-main">
      <div class="ed-tools">
        ${TOOLS.map((tl) => tl.sep ? html`<div class="ed-sep"></div>` : html`<button class=${'btn icon' + (tool === tl.id ? ' on' : '')}
          title=${t(tl.label)} disabled=${(tl.mask && !maskOwner) || (tl.path && !pathOwner)} onClick=${() => setTool(tl.id)}>
          <${Icon} name=${tl.icon} size=${17} /></button>`)}
      </div>
      <div class="ed-view" ref=${viewRef} style=${{ cursor }} onPointerDown=${onPointerDown} onPointerMove=${onPointerMove}
          onPointerUp=${onPointerUp} onPointerCancel=${onPointerUp} onWheel=${onWheel}>
        ${v && html`<div class="ed-world" style=${world}>
          <canvas ref=${canvas} width=${Math.round(W / scale)} height=${Math.round(H / scale)}></canvas>
          ${selText && tool === 'select' && textBoxOf(info.texts, selTextIdx) && html`<${TextHandles} text=${selText} box=${textBoxOf(info.texts, selTextIdx)}
            time=${time} meta=${meta} k=${1} hs=${1 / v.z} toFrame=${toFrame} onChange=${onText} />`}
          ${showClip && tool === 'select' && html`<${selEmit ? EmitterHandles : ClipHandles} clip=${selClip} time=${time} meta=${meta} k=${1} hs=${1 / v.z}
            toFrame=${toFrame} onChange=${onClip} geo=${geoOf(info.layers, selId, selClipIdx)} />`}
          ${tool === 'path' && pathOwner && html`<${MaskOverlay} clip=${pathOwner.obj} k=${0} pathShape=${(pathOwner.obj.motion && pathOwner.obj.motion.path) || null}
            time=${time} meta=${meta} tool=${curPath && !pathDraw ? 'nodes' : 'shape'} brush=${brush} toFrame=${toFrame} z=${v.z}
            editMask=${(fn) => { setPathDraw(false); editPath(fn); }} />`}
          ${tool !== 'path' && maskOwner && (maskOwner.screen || showClip) && selMask != null && maskOwner.obj.masks && maskOwner.obj.masks[selMask] && html`<${MaskOverlay}
            clip=${maskOwner.obj} k=${selMask} screen=${maskOwner.screen}
            time=${time} meta=${meta} tool=${tool} brush=${brush} toFrame=${toFrame} z=${v.z} editMask=${editMask} onPainted=${onPainted}
            geo=${geoOf(info.layers, selId, selClipIdx)} />`}
        </div>`}
        <div class="ed-status mono">
          ${pen && html`${t(pen.type === 'pen' ? 'Pen' : pen.type === 'touch' ? 'Touch' : 'Mouse')}
            ${pen.type === 'pen' ? ' · ' + t('pressure') + ' ' + pen.pressure.toFixed(2) + ' · ' + t('tilt') + ' ' + pen.tilt.join('/') : ''}
            · ${Math.round(pen.x)}, ${Math.round(pen.y)}`}
          <span class="faint"> · 1/${scale} · ${info.ms} ms</span>
        </div>
      </div>
      ${panel && html`<div class="ed-panel">${inspector}</div>`}
    </div>
    <${MiniTimeline} meta=${meta} time=${time} seek=${seek} playing=${playing} setPlaying=${setPlaying} marks=${marks} onKeys=${onKeys} kf=${kf} />
  </div>`;
}

// Small timeline of the editor: transport and a bar to move through the song, with the
// selection's time range and its keys (marks: { start, end, keys, target }). Keys as on the
// timeline: click = go there, drag = move them (within the range), right-click = curve / delete.
// kf: the keys' property filter and selection, shared with the timeline ({ focus, setFocus, sel, setSel })
function MiniTimeline({ meta, time, seek, playing, setPlaying, marks, onKeys, kf }) {
  const bar = useRef();
  const d = meta.duration || 1;
  const xToT = (cx) => { const r = bar.current.getBoundingClientRect(); return Math.max(0, Math.min(d, ((cx - r.left) / r.width) * d)); };
  const { focus = null, setFocus = () => {}, sel: kfSel = null, setSel: setKfSel = () => {} } = kf;
  const down = (e) => {
    if (e.button !== 0) return;
    const el = bar.current;
    capture(el, e.pointerId);
    seek(xToT(e.clientX));
    el.onpointermove = (ev) => seek(xToT(ev.clientX));
    el.onpointerup = () => { el.onpointermove = null; el.onpointerup = null; };
  };
  const keyDown = (e, k) => {
    if (e.button !== 0) return;
    e.preventDefault();
    e.stopPropagation();
    if (!pickKey(e, kfSel, setKfSel, marks.target, k)) return;
    const ts = pickedTimes(kfSel, marks.target, k);
    const orig = marks.keys, base = marks.start, x0 = e.clientX;
    let moved = false, last = 0;
    const mv = (ev) => {
      if (!moved && Math.abs(ev.clientX - x0) < 3) return;
      moved = true;
      const r = moveKeysAt(orig, ts, focus, xToT(ev.clientX) - base - k.t, marks.end - base);
      last = r.dt;
      onKeys(marks.target, () => r.keys);
    };
    const up = () => {
      removeEventListener('pointermove', mv); removeEventListener('pointerup', up);
      if (!moved) seek(base + k.t);
      else setKfSel({ key: targetKey(marks.target), ts: ts.map((x) => +(x + last).toFixed(3)) });
    };
    addEventListener('pointermove', mv); addEventListener('pointerup', up);
  };
  const keyMenu = (e, k) => {
    const picked = isPicked(kfSel, marks.target, k.t);
    if (!picked) setKfSel({ key: targetKey(marks.target), ts: [k.t] });
    openMenu(e, keyMenuItems({ target: marks.target, keys: marks.keys, k, focus, setFocus,
      kfSel: picked ? kfSel : { key: targetKey(marks.target), ts: [k.t] }, setKfSel, onKeys }));
  };
  return html`<div class="ed-bottom">
    <div class="btngroup">
      <button class="btn icon" title=${t('Previous frame (←)')} onClick=${() => seek(time - 1 / meta.fps)}><${Icon} name="chevron-left" size=${16} /></button>
    </div>
    <button class="btn primary play" title=${t('Play / pause (Space)')} onClick=${() => setPlaying(!playing)}><${Icon} name=${playing ? 'pause' : 'play'} size=${16} /></button>
    <div class="btngroup">
      <button class="btn icon" title=${t('Next frame (→)')} onClick=${() => seek(time + 1 / meta.fps)}><${Icon} name="chevron-right" size=${16} /></button>
    </div>
    <span class="time mono">${fmtTime(time)} <small>/ ${fmtTime(meta.duration)}</small></span>
    <${FocusChip} focus=${focus} setFocus=${setFocus} />
    <div class="ed-bar" ref=${bar} onPointerDown=${down}>
      ${marks && html`<div class="ed-range" style=${{ left: (marks.start / d) * 100 + '%', width: ((marks.end - marks.start) / d) * 100 + '%' }}></div>`}
      ${marks && keyTimes(marks.keys, focus).map((k) => html`<div class=${'ed-key' + (k.ease === 'step' ? ' hold' : '') + (isPicked(kfSel, marks.target, k.t) ? ' picked' : '')} key=${'k' + k.t}
          style=${{ left: ((marks.start + k.t) / d) * 100 + '%' }} title=${fmtTime(marks.start + k.t) + ' · ' + k.paths.map(keyLabel).join(', ') + ' — ' + t('click: select (Shift: several) · drag: move · right-click: menu')}
          onPointerDown=${(e) => keyDown(e, k)} onContextMenu=${(e) => keyMenu(e, k)}></div>`)}
      <div class="ed-head" style=${{ left: (time / d) * 100 + '%' }}></div>
    </div>
  </div>`;
}
