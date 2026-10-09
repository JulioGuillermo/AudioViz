import { html, useEffect, useState } from './vendor/preact-htm.js';
import { CLIP_FIELDS, EMITTER_CLIP_FIELDS } from './schema.js';
import { isEmitterLayer, presetLabel, presetIcon, PresetPicker, LightCircles } from './emitters.js';
import { fmtTime, parseTime, basename, clone } from './util.js';
import { Form, Edit, Section } from './fields.js';
import { Icon } from './icons.js';
import { t } from './i18n.js';
import { keyApi, valueAt, keyIndexAt, KEY_EPS, setIn } from './keys.js';
import { MaskSection } from './masks.js';
import { helpMenu } from './help.js';
import { pathKeyNames } from './exprs.js';
import { MotionSection, RouteSection, NodeSection, hasPath, movePath } from './motion.js';

// Overlay layers (config.LayerConfig): proj.layers is bottom to top (drawing order); the studio
// shows them front first, as a stack of cards.

export const newLayerId = () => Math.random().toString(36).slice(2, 10);
// Layers front first: [{ l, i }] with i = index in proj.layers
export const frontFirst = (proj) => (proj.layers || []).map((l, i) => ({ l, i })).reverse();
export const layerById = (proj, id) => (proj.layers || []).find((l) => l.id === id);
export const layerName = (l, i) => l.name || t('Layer {n}', { n: i + 1 });

// A clip's value of a property at time tt (its keys, or its still value)
export function clipValue(c, key, tt) {
  const l = c.keys && c.keys[key];
  return l && l.length ? valueAt(l, tt - c.start) : c[key];
}

// Sets properties of a clip at time tt: a key at the cursor for the animated ones, the still
// value for the others (mutates c, inside an update)
export function setClipProps(c, props, tt) {
  const u = +Math.max(0, tt - c.start).toFixed(3);
  if (hasPath(c)) {
    // it follows a path: moving it moves the path
    const kx = 'x' in props ? 'x' : 'position_x' in props ? 'position_x' : null;
    const ky = 'y' in props ? 'y' : 'position_y' in props ? 'position_y' : null;
    if (kx || ky) {
      movePath(c, kx ? props[kx] - clipValue(c, kx, tt) : 0, ky ? props[ky] - clipValue(c, ky, tt) : 0);
      props = { ...props };
      delete props[kx]; delete props[ky];
    }
  }
  for (const k in props) {
    const l = c.keys && c.keys[k];
    if (l && l.length) {
      const nl = l.map((x) => ({ ...x }));
      const i = keyIndexAt(nl, u);
      if (i >= 0) nl[i].v = props[k]; else nl.push({ t: u, v: props[k] });
      c.keys = { ...c.keys, [k]: nl.sort((a, b) => a.t - b.t) };
    } else if (k.includes('.')) setIn(c, k, props[k]); // inside a block ("emitter.light.reach")
    else c[k] = props[k];
  }
}

// Natural size of a media file (image or video), loaded by the browser once
const natural = new Map();
export function useNatural(path) {
  const [, setTick] = useState(0);
  useEffect(() => {
    if (!path || natural.has(path)) return;
    natural.set(path, null);
    const src = '/api/media?path=' + encodeURIComponent(path);
    const done = (w, h) => { natural.set(path, w && h ? { w, h } : null); setTick((x) => x + 1); };
    if (/\.(mp4|mkv|mov|webm|avi|m4v|ts)$/i.test(path)) {
      const v = document.createElement('video');
      v.preload = 'metadata';
      v.onloadedmetadata = () => done(v.videoWidth, v.videoHeight);
      v.onerror = () => done(0, 0);
      v.src = src;
    } else {
      const im = new Image();
      im.onload = () => done(im.naturalWidth, im.naturalHeight);
      im.onerror = () => done(0, 0);
      im.src = src;
    }
  }, [path]);
  return path ? natural.get(path) : null;
}

// Size on screen (frame px) of a clip with its fit and scale (as engine/layers.go)
export function clipSize(c, nat, W, H, scale) {
  let bw = nat.w, bh = nat.h;
  const fit = c.fit || 'contain';
  if (fit === 'contain' || fit === 'cover') {
    const k = fit === 'contain' ? Math.min(W / bw, H / bh) : Math.max(W / bw, H / bh);
    bw *= k; bh *= k;
  } else if (fit === 'stretch') { bw = W; bh = H; }
  return { w: bw * scale, h: bh * scale };
}

// Where a clip is shown in the frame (frame px): from the server's geometry of the frame on screen
// (geo: the X-Layers entry of the clip, with the background effects it follows and its audio
// pulse) or, without it, from its own values. { cx, cy, w, h, rot (rad), fx, fy } plus the warp
// (zoom, shx, shy: screen = c + (frame + shake − c)·zoom) and k = shown size / own size.
export function shownGeom(c, nat, meta, tt, geo) {
  const W = meta.width, H = meta.height;
  const own = clipSize(c, nat, W, H, clipValue(c, 'scale', tt));
  const flips = { fx: c.flip_x ? -1 : 1, fy: c.flip_y ? -1 : 1 };
  if (!geo) return { cx: clipValue(c, 'x', tt) * W, cy: clipValue(c, 'y', tt) * H, w: own.w, h: own.h,
    rot: (clipValue(c, 'rotation', tt) * Math.PI) / 180, ...flips, zoom: 1, shx: 0, shy: 0, k: 1 };
  const z = geo.Zoom || 1;
  const w = geo.TW * Math.abs(geo.SX), h = geo.TH * Math.abs(geo.SY);
  return { cx: W / 2 + (geo.CX + geo.ShX - W / 2) * z, cy: H / 2 + (geo.CY + geo.ShY - H / 2) * z, w: w * z, h: h * z,
    rot: geo.rot, fx: Math.sign(geo.SX) || 1, fy: Math.sign(geo.SY) || 1, zoom: z, shx: geo.ShX, shy: geo.ShY, k: own.w ? (w / own.w) : 1 };
}
// The X-Layers entry of a clip
export const geoOf = (layers, id, clip) => (layers || []).find((g) => g.id === id && g.clip === clip);

// Handles of the selected clip over a view of the frame: drag inside to move, a corner to scale,
// the round handle to rotate. k: view px per frame px; ox, oy: where the frame's origin is in the
// parent; toFrame(clientX, clientY): a pointer position in frame px (any pan / zoom / rotation of
// the view); hs: size of the handles (1 = normal; the editor compensates its zoom); geo: where the
// clip is shown in the frame (see shownGeom).
export function ClipHandles({ clip, time, meta, k, ox = 0, oy = 0, toFrame, hs = 1, geo, onChange }) {
  const nat = useNatural(clip.path);
  const [drag, setDrag] = useState(null); // live values while dragging
  if (!nat) return null;
  const W = meta.width, H = meta.height;
  const g0 = shownGeom(clip, nat, meta, time, geo);
  const own = { x: clipValue(clip, 'x', time), y: clipValue(clip, 'y', time), scale: clipValue(clip, 'scale', time), rotation: clipValue(clip, 'rotation', time) };
  const v = { ...own, ...(drag || {}) };
  // shown = the server's geometry moved by the change of the own values while dragging
  const cxF = g0.cx + (v.x - own.x) * W * g0.zoom, cyF = g0.cy + (v.y - own.y) * H * g0.zoom;
  const sc = own.scale ? v.scale / own.scale : 1;
  const cx = ox + cxF * k, cy = oy + cyF * k;
  const w = g0.w * sc * k, h = g0.h * sc * k;
  const rotDeg = (g0.rot * 180) / Math.PI + (v.rotation - own.rotation);
  const start = (e, mode) => {
    if (e.button !== 0) return;
    e.preventDefault();
    e.stopPropagation();
    const el = e.currentTarget;
    try { el.setPointerCapture(e.pointerId); } catch (err) { /* not an active pointer */ }
    const v0 = { ...v };
    const c = { x: cxF, y: cyF }; // shown center in frame px
    const p0 = toFrame(e.clientX, e.clientY);
    const d0 = Math.hypot(p0.x - c.x, p0.y - c.y) || 1;
    let cur = {};
    const mv = (ev) => {
      const p = toFrame(ev.clientX, ev.clientY);
      if (mode === 'move') cur = { x: +(v0.x + (p.x - p0.x) / (W * g0.zoom)).toFixed(4), y: +(v0.y + (p.y - p0.y) / (H * g0.zoom)).toFixed(4) };
      else if (mode === 'scale') cur = { scale: +Math.max(0.01, v0.scale * Math.hypot(p.x - c.x, p.y - c.y) / d0).toFixed(4) };
      else {
        let a = Math.atan2(p.y - c.y, p.x - c.x) * 180 / Math.PI + 90 - ((g0.rot * 180) / Math.PI - own.rotation);
        // whole turns kept; Shift: steps of 15°
        a += Math.round((v0.rotation - a) / 360) * 360;
        if (ev.shiftKey) a = Math.round(a / 15) * 15;
        cur = { rotation: +a.toFixed(2) };
      }
      setDrag(cur);
    };
    const up = () => {
      removeEventListener('pointermove', mv); removeEventListener('pointerup', up);
      setDrag(null);
      if (Object.keys(cur).length) onChange(cur);
    };
    addEventListener('pointermove', mv); addEventListener('pointerup', up);
  };
  const hpx = 10 * hs;
  const corner = (sx, sy) => html`<div class="ch-corner" style=${{ left: (sx * w) / 2 + w / 2 - hpx / 2 + 'px', top: (sy * h) / 2 + h / 2 - hpx / 2 + 'px',
      width: hpx + 'px', height: hpx + 'px' }}
    onPointerDown=${(e) => start(e, 'scale')} title=${t('Drag to scale')}></div>`;
  return html`<div class=${'clipbox' + (drag ? ' drag' : '')} style=${{ left: cx - w / 2 + 'px', top: cy - h / 2 + 'px', width: w + 'px', height: h + 'px',
      transform: `rotate(${rotDeg}deg)`, borderWidth: hs + 'px' }}>
    <div class="ch-body" onPointerDown=${(e) => start(e, 'move')} title=${t('Drag to move')}></div>
    ${corner(-1, -1)}${corner(1, -1)}${corner(-1, 1)}${corner(1, 1)}
    <div class="ch-rot" style=${{ top: -26 * hs + 'px', width: 12 * hs + 'px', height: 12 * hs + 'px', marginLeft: -6 * hs + 'px' }}
      onPointerDown=${(e) => start(e, 'rotate')} title=${t('Drag to rotate (Shift: steps of 15°)')}></div>
  </div>`;
}

// The X-Texts entry of a fixed text (its index in proj.texts)
export const textBoxOf = (texts, i) => (texts || []).find((b) => b.i === i);

// Handles of the selected fixed text (as ClipHandles): drag inside to move it, a corner to change
// its size, the round handle to rotate it; it scales and turns around its position (the dot).
// box: its X-Texts entry (anchor ax, ay and rot as drawn, box X0..Y1 of the letters around the
// anchor before the rotation, frame px).
export function TextHandles({ text, box, time, meta, k, ox = 0, oy = 0, toFrame, hs = 1, onChange }) {
  const [drag, setDrag] = useState(null);
  const W = meta.width, H = meta.height;
  const own = { position_x: clipValue(text, 'position_x', time), position_y: clipValue(text, 'position_y', time),
    size: clipValue(text, 'size', time), rotation: clipValue(text, 'rotation', time) || 0 };
  const v = { ...own, ...(drag || {}) };
  const ax = box.ax + (v.position_x - own.position_x) * W, ay = box.ay + (v.position_y - own.position_y) * H;
  const sc = own.size ? v.size / own.size : 1;
  const rot = box.rot + ((v.rotation - own.rotation) * Math.PI) / 180;
  const m = 6; // frame px around the letters
  const lx = ((box.X0 + box.X1) / 2) * sc, ly = ((box.Y0 + box.Y1) / 2) * sc;
  const cs = Math.cos(rot), sn = Math.sin(rot);
  const cxF = ax + lx * cs - ly * sn, cyF = ay + lx * sn + ly * cs;
  const w = ((box.X1 - box.X0) * sc + 2 * m) * k, h = ((box.Y1 - box.Y0) * sc + 2 * m) * k;
  const cx = ox + cxF * k, cy = oy + cyF * k;
  const start = (e, mode) => {
    if (e.button !== 0) return;
    e.preventDefault();
    e.stopPropagation();
    try { e.currentTarget.setPointerCapture(e.pointerId); } catch (err) { /* not an active pointer */ }
    const v0 = { ...v };
    const a = { x: ax, y: ay }; // the anchor, frame px
    const p0 = toFrame(e.clientX, e.clientY);
    const d0 = Math.hypot(p0.x - a.x, p0.y - a.y) || 1;
    const a0 = Math.atan2(p0.y - a.y, p0.x - a.x);
    let cur = {};
    const mv = (ev) => {
      const p = toFrame(ev.clientX, ev.clientY);
      if (mode === 'move') cur = { position_x: +(v0.position_x + (p.x - p0.x) / W).toFixed(4), position_y: +(v0.position_y + (p.y - p0.y) / H).toFixed(4) };
      else if (mode === 'scale') cur = { size: +Math.max(1, v0.size * Math.hypot(p.x - a.x, p.y - a.y) / d0).toFixed(1) };
      else {
        let r = v0.rotation + ((Math.atan2(p.y - a.y, p.x - a.x) - a0) * 180) / Math.PI;
        r += Math.round((v0.rotation - r) / 360) * 360;
        if (ev.shiftKey) r = Math.round(r / 15) * 15;
        cur = { rotation: +r.toFixed(2) };
      }
      setDrag(cur);
    };
    const up = () => {
      removeEventListener('pointermove', mv); removeEventListener('pointerup', up);
      setDrag(null);
      if (Object.keys(cur).length) onChange(cur);
    };
    addEventListener('pointermove', mv); addEventListener('pointerup', up);
  };
  const hpx = 10 * hs;
  const corner = (sx, sy) => html`<div class="ch-corner" style=${{ left: (sx * w) / 2 + w / 2 - hpx / 2 + 'px', top: (sy * h) / 2 + h / 2 - hpx / 2 + 'px',
      width: hpx + 'px', height: hpx + 'px' }}
    onPointerDown=${(e) => start(e, 'scale')} title=${t('Drag to change the size')}></div>`;
  const dot = 8 * hs;
  return html`<div class=${'clipbox textbox' + (drag ? ' drag' : '')} style=${{ left: cx - w / 2 + 'px', top: cy - h / 2 + 'px', width: w + 'px', height: h + 'px',
      transform: `rotate(${(rot * 180) / Math.PI}deg)`, borderWidth: hs + 'px' }}>
    <div class="ch-body" onPointerDown=${(e) => start(e, 'move')} title=${t('Drag to move')}></div>
    ${corner(-1, -1)}${corner(1, -1)}${corner(-1, 1)}${corner(1, 1)}
    <div class="ch-rot" style=${{ top: -26 * hs + 'px', width: 12 * hs + 'px', height: 12 * hs + 'px', marginLeft: -6 * hs + 'px' }}
      onPointerDown=${(e) => start(e, 'rotate')} title=${t('Drag to rotate (Shift: steps of 15°)')}></div>
  </div>
  <div class="tb-anchor" style=${{ left: ox + ax * k - dot / 2 + 'px', top: oy + ay * k - dot / 2 + 'px', width: dot + 'px', height: dot + 'px' }}
    title=${t('Its position: it scales and turns around it')}></div>
  ${text.light && text.light.enabled && html`<div class="emitbox">
    <${LightCircles} obj=${text} light=${text.light} prefix="light." time=${time} cx=${ax} cy=${ay} sc=${H / 1080} k=${k} ox=${ox} oy=${oy}
      hs=${hs} W=${W} H=${H} toFrame=${toFrame} onChange=${onChange} /></div>`}`;
}

// Inspector of a layer: its name, its clips (tabs), the selected clip's times and settings
export function LayerInspector({ proj, sel, setSel, update, time, meta, ctx }) {
  const li = (proj.layers || []).findIndex((l) => l.id === sel.id);
  const layer = proj.layers[li];
  if (!layer) return null;
  const j = Math.min(sel.clip ?? 0, layer.clips.length - 1);
  const c = layer.clips[j];
  const editLayer = (fn) => update((p) => { fn(p.layers.find((l) => l.id === sel.id)); });
  const clampT = (v) => Math.max(0, Math.min(meta.duration, v));
  const kf = c && keyApi({ keys: c.keys, u: time - c.start, max: c.end - c.start, focus: ctx.kfFocus, setFocus: ctx.setKfFocus,
    exprs: c.exprs, target: { kind: 'clip', layer: layer.id, clip: j }, t0: c.start,
    pathKeys: [...new Set([...pathKeyNames(c.motion && c.motion.path), ...pathKeyNames(c.emitter && c.emitter.route)])],
    seek: (u) => ctx.seek(c.start + u), edit: (fn) => editLayer((l) => { const cc = l.clips[j]; fn(cc, cc); }) });
  const emit = isEmitterLayer(layer);
  const clipName = (cl, k) => cl.label || (emit ? presetLabel(cl.emitter && cl.emitter.preset) : basename(cl.path)) || t('Clip {n}', { n: k + 1 });
  return html`<div>
    <div class="insp-h">
      <div class="insp-title"><${Icon} name=${emit ? 'sparkles' : 'layers'} size=${16} cls="muted" />${layerName(layer, li)}<span class="sub">${t(emit ? 'Particle emitter' : 'Layer')}</span></div>
      ${layer.clips.length > 0 && html`<div class="segtabs">${layer.clips.map((cl, k) => html`<button class=${'segtab' + (k === j ? ' on' : '')}
          onClick=${() => { setSel({ kind: 'layer', id: sel.id, clip: k }); if (time < cl.start || time >= cl.end) ctx.seek(cl.start); }}>
          <span class="mono">${fmtTime(cl.start, false)}</span><span class="nm">${clipName(cl, k)}</span>
          <span class="sum mono">${fmtTime(cl.end - cl.start, false)}</span></button>`)}</div>`}
      <div class="row">
        <button class="btn sm" onClick=${() => ctx.addClip(sel.id, time)} title=${t(emit ? 'New emitter clip starting at the cursor (like this one)' : 'New clip starting at the cursor: choose its file')}>
          <${Icon} name="plus" size=${14} /> ${t('Clip at {t}', { t: fmtTime(time) })}</button>
        <span class="spacer"></span>
        <button class="btn sm ghost danger" disabled=${!c} title=${t('Delete this clip')}
          onClick=${() => { editLayer((l) => { l.clips.splice(j, 1); }); setSel({ ...sel, clip: Math.max(0, j - 1) }); }}><${Icon} name="trash-2" size=${14} /> ${t('Delete')}</button>
      </div>
    </div>
    <div class="insp-b">
      <div class="card" onContextMenu=${helpMenu(t('Clip'), emit ? 'Drag its point on the preview to move it, the tip of its arrow to turn it.'
        : 'Drag it on the preview to move it; its corners scale it and the round handle rotates it.')}>
        <div class="fld"><label>${t('Layer name')}</label><div class="ctl"><${Edit} type="text" class="grow" value=${layer.name || ''}
          placeholder=${layerName(layer, li)} onChange=${(e) => editLayer((l) => { l.name = e.target.value; })} /></div></div>
        ${c && html`
        <div class="fld"><label>${t('Clip name')}</label><div class="ctl"><${Edit} type="text" class="grow" value=${c.label || ''}
          placeholder=${t('Optional')} onChange=${(e) => editLayer((l) => { l.clips[j].label = e.target.value; })} /></div></div>
        <div class="fld"><label>${t('Starts at')}</label><div class="ctl">
          <${Edit} type="text" class="mono" style="width:96px" value=${fmtTime(c.start)}
            onChange=${(e) => { const v = clampT(parseTime(e.target.value)); editLayer((l) => { const cc = l.clips[j]; cc.start = v; cc.end = Math.max(cc.end, v + 0.1); }); }} />
          <span class="faint" style="font-size:11.5px">min:sec</span></div></div>
        <div class="fld"><label>${t('Ends at')}</label><div class="ctl">
          <${Edit} type="text" class="mono" style="width:96px" value=${fmtTime(c.end)}
            onChange=${(e) => editLayer((l) => { const cc = l.clips[j]; cc.end = Math.max(cc.start + 0.1, clampT(parseTime(e.target.value))); })} />
          <span class="faint" style="font-size:11.5px">${t('{s} s', { s: (c.end - c.start).toFixed(2) })}</span></div></div>`}
      </div>
      ${c && emit && html`<${Section} id="preset" title=${t('Preset')}>
        <${PresetPicker} clip=${c} onPick=${(k) => editLayer((l) => { const cc = l.clips[j];
          cc.emitter = { ...JSON.parse(JSON.stringify((ctx.emitterPresets || {})[k] || {})), preset: k };
          cc.blend = cc.emitter.mode === 'soft' ? 'normal' : 'add';
          if (cc.keys) { for (const p in cc.keys) if (p.startsWith('emitter.')) delete cc.keys[p]; } })} /></${Section}>`}
      ${c ? html`<${Form} fields=${emit ? EMITTER_CLIP_FIELDS : CLIP_FIELDS} value=${c} ctx=${ctx} kf=${kf}
          onChange=${(nc) => editLayer((l) => { l.clips[j] = { ...nc, start: l.clips[j].start, end: l.clips[j].end }; })} />`
        : html`<p class="faint" style="font-size:12px">${t('No clips yet. Add one at the cursor, or double-click the layer\'s track on the timeline.')}</p>`}
      ${c && emit && html`<${RouteSection} obj=${c} ctx=${ctx} kf=${kf} edit=${(fn) => editLayer((l) => fn(l.clips[j]))} />`}
      ${c && html`<${MotionSection} obj=${c} kf=${kf} ctx=${ctx} edit=${(fn) => editLayer((l) => fn(l.clips[j]))} />`}
      ${c && ctx.pathNode && html`<${NodeSection} obj=${c} what=${ctx.pathNode.what} pi=${ctx.pathNode.pi} edit=${(fn) => editLayer((l) => fn(l.clips[j]))} />`}
      ${c && html`<${MaskSection} clip=${c} kf=${kf} selMask=${ctx.selMask} setSelMask=${ctx.setSelMask} ctx=${ctx}
        openEditor=${ctx.openEditor} editClip=${(fn) => editLayer((l) => fn(l.clips[j]))} />`}
      <${Section} id="layer" title=${t('Layer')} help=${'Layers higher in the list are drawn in front. Drag them in the left panel to change the order.'}>
        <button class="btn sm ghost danger" onClick=${() => { if (confirm(t('Delete the layer “{n}” and its clips?', { n: layerName(layer, li) }))) {
          update((p) => { p.layers = p.layers.filter((l) => l.id !== sel.id); }); setSel({ kind: 'output' }); } }}>
          <${Icon} name="trash-2" size=${14} /> ${t('Delete layer')}</button>
      </${Section}>
    </div>
  </div>`;
}

// Duplicates a clip right after itself (same length), inside an update
export function duplicateClip(layer, j) {
  const c = clone(layer.clips[j]);
  const len = c.end - c.start;
  c.start = +layer.clips[j].end.toFixed(3);
  c.end = +(c.start + len).toFixed(3);
  layer.clips.splice(j + 1, 0, c);
  return j + 1;
}

export { KEY_EPS };
