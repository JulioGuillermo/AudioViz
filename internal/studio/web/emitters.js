import { html, useState } from './vendor/preact-htm.js';
import { EMITTER_PRESETS, EMITTER_GROUPS } from './schema.js';
import { Icon } from './icons.js';
import { t } from './i18n.js';
import { clone } from './util.js';
import { clipValue } from './layers.js';
import { valueAt } from './keys.js';

// Particle emitters (config.EmitterConfig): a layer of kind "emitter" whose clips throw particles.
// The clip places the emitter (x, y or its motion path; rotation turns its shape and direction;
// scale makes everything bigger); its emitter block says what it throws.

export const isEmitterLayer = (l) => !!l && l.kind === 'emitter';
export const presetLabel = (k) => { const p = EMITTER_PRESETS.find((x) => x[0] === k); return p ? t(p[1]) : t('Particles'); };
export const presetIcon = (k) => { const p = EMITTER_PRESETS.find((x) => x[0] === k); return p ? p[2] : 'sparkles'; };

// Where a preset starts on the frame (0..1)
const START = { fire: [0.5, 0.95], fire_real: [0.5, 0.95], sparks: [0.5, 0.8], smoke: [0.5, 0.8], smoke_real: [0.5, 0.85], fire_gas: [0.5, 0.9], clouds_gas: [0.5, 0.03], water_sim: [0.5, 0.3], water_sim_clear: [0.5, 0.3], water_3d: [0.5, 0.05], water_3d_clear: [0.5, 0.05], splash_sim: [0.5, 0.75], fog_gas: [0.5, 0.88], fog_proc: [0.5, 0.5], clouds_sky: [0.5, 0.5], clouds_bank: [0.5, 0.5], clouds_storm: [0.5, 0.5], ocean: [0.5, 0.5], ocean_sunset: [0.5, 0.5], ocean_storm: [0.5, 0.5], lake_calm: [0.5, 0.5], fog_3d: [0.5, 0.88], smoke_3d: [0.5, 0.85], fire_gas_haze: [0.5, 0.9], fire_clean: [0.5, 0.9], fire_clean_haze: [0.5, 0.9], heat_haze: [0.5, 0.95], steam: [0.5, 0.85], dust: [0.5, 0.5], light: [0.5, 0.5],
  explosion: [0.5, 0.5], snow: [0.5, -0.02] };

// A new emitter clip from a preset, from st for dur seconds
export function newEmitterClip(proj, preset, st, dur) {
  const e = clone((proj.emitterPresets || {})[preset] || {});
  e.preset = preset;
  const [x, y] = START[preset] || [0.5, 0.5];
  return { ...clone(proj.clipDefaults || {}), path: '', start: st, end: +(st + dur).toFixed(3), x, y,
    blend: e.mode === 'soft' ? 'normal' : 'add', emitter: e };
}

// The menu items to add an emitter (one per preset)
export const emitterMenuItems = (onPick) => EMITTER_GROUPS.flatMap(([g, ps]) => [{ header: t(g) },
  ...ps.map(([k, label, icon]) => ({ label: t(label), icon, onClick: () => onPick(k) }))]);

// Choosing another preset replaces the emitter's settings (its place on the clip stays)
export function PresetPicker({ clip, onPick }) {
  const cur = clip.emitter && clip.emitter.preset;
  return html`<div class="presets">${EMITTER_GROUPS.map(([g, ps]) => [html`<div class="presets-h" key=${'h' + g}>${t(g)}</div>`,
    ...ps.map(([k, label, icon]) => html`<button key=${k}
      class=${'btn sm' + (cur === k ? ' on' : '')} title=${t(label) + ' · ' + t('Start again from this preset (its settings replace the current ones)')}
      onClick=${() => onPick(k)}><${Icon} name=${icon} size=${13} /><span class="pl">${t(label)}</span></button>`)])}</div>`;
}

// The circles of a light around its center (frame px cx, cy) over a view of the frame: its
// reach (where it is still strong), where it fades out (fainter) and, with rim light, where only
// the edges are lit (blue); drag a handle to change it. light: the light's settings; prefix: its
// keys' path in obj ("emitter.light." for an emitter clip, "light." for a text); keys from obj's
// start; sc: frame px per px of its settings; k: view px per frame px.
export function LightCircles({ obj, light, prefix, time, cx, cy, sc, k, ox = 0, oy = 0, hs = 1, W, H, toFrame, onChange }) {
  const [drag, setDrag] = useState(null);
  if (!light || !light.enabled) return null;
  const kv = (key, def) => { const l = obj.keys && obj.keys[prefix + key]; return l && l.length ? valueAt(l, time - (obj.start || 0)) : light[key] ?? def; };
  const dv = (key, def) => (drag && drag[prefix + key] != null ? drag[prefix + key] : def);
  const reach = dv('reach', kv('reach', 0));
  const end0 = kv('end', 0);
  const end = dv('end', end0 > 0 ? end0 : reach * 2.5);
  const rimR = light.rim ? dv('rim_from', kv('rim_from', 500)) : -1;
  const c = { x: ox + cx * k, y: oy + cy * k };
  const lr = reach > 0 ? reach * sc * k : 0;
  if (lr <= 0) return null;
  const le = end * sc * k, lrim = rimR >= 0 ? rimR * sc * k : -1;
  const start = (e0, key, min) => {
    if (e0.button !== 0) return;
    e0.preventDefault();
    e0.stopPropagation();
    try { e0.currentTarget.setPointerCapture(e0.pointerId); } catch (err) { /* synthetic */ }
    let cur = {};
    const mv = (m) => {
      const p = toFrame(m.clientX, m.clientY);
      cur = { [prefix + key]: Math.max(min, Math.round(Math.hypot(p.x - cx, p.y - cy) / sc)) };
      setDrag(cur);
    };
    const up = () => {
      removeEventListener('pointermove', mv); removeEventListener('pointerup', up);
      setDrag(null);
      if (Object.keys(cur).length) onChange(cur);
    };
    addEventListener('pointermove', mv); addEventListener('pointerup', up);
  };
  // a handle on each circle where it is inside the frame: towards the middle of the frame first,
  // then turning away from it bit by bit (a circle bigger than the frame may cross it elsewhere)
  const ka = Math.hypot(W / 2 - cx, H / 2 - cy) > 1 ? Math.atan2(H / 2 - cy, W / 2 - cx) : -Math.PI / 4;
  const onCircle = (r) => {
    const rf = r / k; // frame px
    for (let i = 0; i <= 24; i++) {
      const a = ka + (i % 2 ? 1 : -1) * Math.ceil(i / 2) * (Math.PI / 12);
      const x = cx + rf * Math.cos(a), y = cy + rf * Math.sin(a);
      if (x >= 8 && y >= 8 && x <= W - 8 && y <= H - 8) return { x: c.x + r * Math.cos(a), y: c.y + r * Math.sin(a) };
    }
    return null;
  };
  const knob = onCircle(lr) || { x: c.x + lr * Math.cos(ka), y: c.y + lr * Math.sin(ka) };
  const knob2 = onCircle(le);
  const knob3 = lrim >= 0 ? onCircle(Math.max(lrim, 1)) : null;
  const dot = 12 * hs;
  const handle = (kn, cls, key, min, value, title) => kn && html`<div class=${'eb-reach' + cls}
      style=${{ left: kn.x - dot / 2 + 'px', top: kn.y - dot / 2 + 'px', width: dot + 'px', height: dot + 'px' }}
      onPointerDown=${(m) => start(m, key, min)} title=${t(title)}>
      ${drag && drag[prefix + key] != null && html`<span class="eb-val">${Math.round(value)} px</span>`}</div>`;
  return html`<svg class="eb-svg"><g style=${{ strokeWidth: 1.5 * hs }}>
      <circle class="eb-light" cx=${c.x} cy=${c.y} r=${lr} />
      <circle class="eb-light far" cx=${c.x} cy=${c.y} r=${le} />
      ${lrim > 0 && html`<circle class="eb-light rim" cx=${c.x} cy=${c.y} r=${lrim} />`}
    </g></svg>
    ${handle(knob, '', 'reach', 20, reach, 'Reach of its light: drag to change it')}
    ${handle(knob2, ' far', 'end', 20, end, 'Where its light fades out: drag to change it')}
    ${handle(knob3, ' rim', 'rim_from', 0, rimR, 'Farther than this circle only the edges that face it are lit: drag to change it')}`;
}

// Handles of the selected emitter over a view of the frame (as ClipHandles): drag the point to
// move it, the arrow's tip to turn it; its shape is outlined. k: view px per frame px; ox, oy: the
// frame's origin in the parent; toFrame(clientX, clientY) → frame px; geo: its X-Layers entry
// (where it is now: OX, OY, ORot).
export function EmitterHandles({ clip, time, meta, k, ox = 0, oy = 0, toFrame, hs = 1, geo, onChange }) {
  const [drag, setDrag] = useState(null);
  const W = meta.width, H = meta.height;
  const e = clip.emitter || {};
  const val = (key) => clipValue(clip, key, time);
  const ev = (key) => { const l = clip.keys && clip.keys['emitter.' + key]; return l && l.length ? valueAt(l, time - clip.start) : e[key]; };
  const own = { x: val('x'), y: val('y'), rotation: val('rotation') || 0 };
  const v = { ...own, ...(drag || {}) };
  // now: the server's place (with its keys, its motion path), moved by the drag
  const gx = geo && geo.OX != null ? geo.OX : own.x * W, gy = geo && geo.OY != null ? geo.OY : own.y * H;
  const grot = geo && geo.ORot != null ? geo.ORot : (own.rotation * Math.PI) / 180;
  const cx = gx + (v.x - own.x) * W, cy = gy + (v.y - own.y) * H;
  const rot = grot + ((v.rotation - own.rotation) * Math.PI) / 180;
  const sc = (H / 1080) * (val('scale') || 1);
  const dir = rot + ((ev('direction') || 0) * Math.PI) / 180;
  const arrow = 70 / k * hs; // frame px
  const tip = { x: cx + Math.sin(dir) * arrow, y: cy - Math.cos(dir) * arrow };
  const spread = (Math.min(360, ev('spread') || 0) * Math.PI) / 180 / 2;
  const S = (p) => ({ x: ox + p.x * k, y: oy + p.y * k });
  const start = (ev0, mode) => {
    if (ev0.button !== 0) return;
    ev0.preventDefault();
    ev0.stopPropagation();
    try { ev0.currentTarget.setPointerCapture(ev0.pointerId); } catch (err) { /* synthetic */ }
    const p0 = toFrame(ev0.clientX, ev0.clientY), v0 = { ...v };
    let cur = {};
    const mv = (m) => {
      const p = toFrame(m.clientX, m.clientY);
      if (mode === 'move') cur = { x: +(v0.x + (p.x - p0.x) / W).toFixed(4), y: +(v0.y + (p.y - p0.y) / H).toFixed(4) };
      else {
        let a = (Math.atan2(p.x - cx, -(p.y - cy)) * 180) / Math.PI - (ev('direction') || 0) - ((grot * 180) / Math.PI - own.rotation);
        a += Math.round((v0.rotation - a) / 360) * 360;
        if (m.shiftKey) a = Math.round(a / 15) * 15;
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
  // the shape, in screen px
  const c = S({ x: cx, y: cy });
  const sw = (ev('width') || 0) * sc * k, sh = (ev('height') || 0) * sc * k;
  const shape = e.shape || 'point';
  const deg = (rot * 180) / Math.PI;
  const t1 = S(tip);
  const cone = [-spread, spread].map((s) => S({ x: cx + Math.sin(dir + s) * arrow * 0.8, y: cy - Math.cos(dir + s) * arrow * 0.8 }));
  const dot = 12 * hs;
  return html`<div class=${'emitbox' + (drag ? ' drag' : '')}>
    <${LightCircles} obj=${clip} light=${e.light} prefix="emitter.light." time=${time} cx=${cx} cy=${cy} sc=${sc} k=${k} ox=${ox} oy=${oy}
      hs=${hs} W=${W} H=${H} toFrame=${toFrame} onChange=${onChange} />
    ${(shape === 'circle' || shape === 'ring') && sw > 2 && html`<div class="eb-shape round" style=${{ left: c.x - sw / 2 + 'px', top: c.y - sw / 2 + 'px', width: sw + 'px', height: sw + 'px' }}></div>`}
    ${(shape === 'line' || shape === 'box') && sw > 2 && html`<div class=${'eb-shape' + (shape === 'line' ? ' line' : '')}
      style=${{ left: c.x - sw / 2 + 'px', top: c.y - (shape === 'line' ? 0 : sh / 2) + 'px', width: sw + 'px', height: (shape === 'line' ? 0 : sh) + 'px', transform: `rotate(${deg}deg)`, transformOrigin: `${sw / 2}px ${shape === 'line' ? 0 : sh / 2}px` }}></div>`}
    <svg class="eb-svg"><g style=${{ strokeWidth: 1.5 * hs }}>
      ${spread > 0.01 && spread < Math.PI && cone.map((q) => html`<line class="eb-cone" x1=${c.x} y1=${c.y} x2=${q.x} y2=${q.y} />`)}
      <line class="eb-arrow" x1=${c.x} y1=${c.y} x2=${t1.x} y2=${t1.y} />
    </g></svg>
    <div class="eb-dot" style=${{ left: c.x - dot / 2 + 'px', top: c.y - dot / 2 + 'px', width: dot + 'px', height: dot + 'px' }}
      onPointerDown=${(m) => start(m, 'move')} title=${t('Drag to move the emitter')}></div>
    <div class="eb-tip" style=${{ left: t1.x - dot / 2 + 'px', top: t1.y - dot / 2 + 'px', width: dot + 'px', height: dot + 'px' }}
      onPointerDown=${(m) => start(m, 'rotate')} title=${t('Drag to turn it (Shift: steps of 15°)')}></div>
  </div>`;
}
