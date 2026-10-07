import { html, useEffect, useState } from './vendor/preact-htm.js';
import { CLIP_FIELDS } from './schema.js';
import { fmtTime, parseTime, basename, clone } from './util.js';
import { Form, Edit } from './fields.js';
import { Icon } from './icons.js';
import { t } from './i18n.js';
import { keyApi, valueAt, keyIndexAt, KEY_EPS } from './keys.js';

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
  for (const k in props) {
    const l = c.keys && c.keys[k];
    if (l && l.length) {
      const nl = l.map((x) => ({ ...x }));
      const i = keyIndexAt(nl, u);
      if (i >= 0) nl[i].v = props[k]; else nl.push({ t: u, v: props[k] });
      c.keys = { ...c.keys, [k]: nl.sort((a, b) => a.t - b.t) };
    } else c[k] = props[k];
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

// Handles of the selected clip over the preview: drag inside to move, a corner to scale, the
// round handle to rotate. cr / sr: client rects of the canvas and of the stage.
export function ClipHandles({ clip, time, meta, cr, sr, onChange }) {
  const nat = useNatural(clip.path);
  const [drag, setDrag] = useState(null); // live values while dragging
  if (!nat || !cr) return null;
  const W = meta.width, H = meta.height;
  const v = { x: clipValue(clip, 'x', time), y: clipValue(clip, 'y', time), scale: clipValue(clip, 'scale', time),
    rotation: clipValue(clip, 'rotation', time), ...(drag || {}) };
  const sz = clipSize(clip, nat, W, H, v.scale);
  const k = cr.width / W; // screen px per frame px
  const cx = cr.left - sr.left + v.x * cr.width, cy = cr.top - sr.top + v.y * cr.height;
  const w = sz.w * k, h = sz.h * k;
  const start = (e, mode) => {
    if (e.button !== 0) return;
    e.preventDefault();
    e.stopPropagation();
    const v0 = { ...v };
    const ccx = cr.left + v0.x * cr.width, ccy = cr.top + v0.y * cr.height; // center in client coords
    const d0 = Math.hypot(e.clientX - ccx, e.clientY - ccy) || 1;
    let cur = {};
    const mv = (ev) => {
      if (mode === 'move') cur = { x: +(v0.x + (ev.clientX - e.clientX) / cr.width).toFixed(4), y: +(v0.y + (ev.clientY - e.clientY) / cr.height).toFixed(4) };
      else if (mode === 'scale') cur = { scale: +Math.max(0.01, v0.scale * Math.hypot(ev.clientX - ccx, ev.clientY - ccy) / d0).toFixed(4) };
      else {
        let a = Math.atan2(ev.clientY - ccy, ev.clientX - ccx) * 180 / Math.PI + 90;
        // whole turns kept; Shift: steps of 15°
        a += Math.round((v0.rotation - a) / 360) * 360;
        if (ev.shiftKey) a = Math.round(a / 15) * 15;
        cur = { rotation: +a.toFixed(2) };
      }
      setDrag(cur);
    };
    const up = () => {
      removeEventListener('mousemove', mv); removeEventListener('mouseup', up);
      setDrag(null);
      if (Object.keys(cur).length) onChange(cur);
    };
    addEventListener('mousemove', mv); addEventListener('mouseup', up);
  };
  const corner = (sx, sy) => html`<div class="ch-corner" style=${{ left: (sx * w) / 2 + w / 2 - 5 + 'px', top: (sy * h) / 2 + h / 2 - 5 + 'px' }}
    onMouseDown=${(e) => start(e, 'scale')} title=${t('Drag to scale')}></div>`;
  return html`<div class=${'clipbox' + (drag ? ' drag' : '')} style=${{ left: cx - w / 2 + 'px', top: cy - h / 2 + 'px', width: w + 'px', height: h + 'px',
      transform: `rotate(${v.rotation}deg)` }}>
    <div class="ch-body" onMouseDown=${(e) => start(e, 'move')} title=${t('Drag to move')}></div>
    ${corner(-1, -1)}${corner(1, -1)}${corner(-1, 1)}${corner(1, 1)}
    <div class="ch-rot" onMouseDown=${(e) => start(e, 'rotate')} title=${t('Drag to rotate (Shift: steps of 15°)')}></div>
  </div>`;
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
    seek: (u) => ctx.seek(c.start + u), edit: (fn) => editLayer((l) => { const cc = l.clips[j]; fn(cc, cc); }) });
  return html`<div>
    <div class="insp-h">
      <div class="insp-title"><${Icon} name="layers" size=${16} cls="muted" />${layerName(layer, li)}<span class="sub">${t('Layer')}</span></div>
      ${layer.clips.length > 0 && html`<div class="segtabs">${layer.clips.map((cl, k) => html`<button class=${'segtab' + (k === j ? ' on' : '')}
          onClick=${() => { setSel({ kind: 'layer', id: sel.id, clip: k }); if (time < cl.start || time >= cl.end) ctx.seek(cl.start); }}>
          <span class="mono">${fmtTime(cl.start, false)}</span><span class="nm">${cl.label || basename(cl.path) || t('Clip {n}', { n: k + 1 })}</span>
          <span class="sum mono">${fmtTime(cl.end - cl.start, false)}</span></button>`)}</div>`}
      <div class="row">
        <button class="btn sm" onClick=${() => ctx.addClip(sel.id, time)} title=${t('New clip starting at the cursor: choose its file')}>
          <${Icon} name="plus" size=${14} /> ${t('Clip at {t}', { t: fmtTime(time) })}</button>
        <span class="spacer"></span>
        <button class="btn sm ghost danger" disabled=${!c} title=${t('Delete this clip')}
          onClick=${() => { editLayer((l) => { l.clips.splice(j, 1); }); setSel({ ...sel, clip: Math.max(0, j - 1) }); }}><${Icon} name="trash-2" size=${14} /> ${t('Delete')}</button>
      </div>
    </div>
    <div class="insp-b">
      <div class="card">
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
          <span class="faint" style="font-size:11.5px">${t('{s} s', { s: (c.end - c.start).toFixed(2) })}</span></div></div>
        <div class="card-note">${t('Drag it on the preview to move it; its corners scale it and the round handle rotates it.')}</div>`}
      </div>
      ${c ? html`<${Form} fields=${CLIP_FIELDS} value=${c} ctx=${ctx} kf=${kf}
          onChange=${(nc) => editLayer((l) => { l.clips[j] = { ...nc, start: l.clips[j].start, end: l.clips[j].end }; })} />`
        : html`<p class="faint" style="font-size:12px">${t('No clips yet. Add one at the cursor, or double-click the layer\'s track on the timeline.')}</p>`}
      <div class="section"><div class="sh">${t('Layer')}</div><div class="sb">
        <p class="faint" style="margin:2px 0 8px;font-size:12px">${t('Layers higher in the list are drawn in front. Drag them in the left panel to change the order.')}</p>
        <button class="btn sm ghost danger" onClick=${() => { if (confirm(t('Delete the layer “{n}” and its clips?', { n: layerName(layer, li) }))) {
          update((p) => { p.layers = p.layers.filter((l) => l.id !== sel.id); }); setSel({ kind: 'output' }); } }}>
          <${Icon} name="trash-2" size=${14} /> ${t('Delete layer')}</button>
      </div></div>
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
