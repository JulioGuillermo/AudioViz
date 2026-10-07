import { html, useState } from './vendor/preact-htm.js';
import { EMITTER_PRESETS } from './schema.js';
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
const START = { fire: [0.5, 0.95], sparks: [0.5, 0.8], smoke: [0.5, 0.8], dust: [0.5, 0.5], light: [0.5, 0.5],
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
export const emitterMenuItems = (onPick) => EMITTER_PRESETS.map(([k, label, icon]) => ({ label: t(label), icon, onClick: () => onPick(k) }));

// Choosing another preset replaces the emitter's settings (its place on the clip stays)
export function PresetPicker({ clip, onPick }) {
  const cur = clip.emitter && clip.emitter.preset;
  return html`<div class="presets">${EMITTER_PRESETS.map(([k, label, icon]) => html`<button key=${k}
      class=${'btn sm' + (cur === k ? ' on' : '')} title=${t('Start again from this preset (its settings replace the current ones)')}
      onClick=${() => onPick(k)}><${Icon} name=${icon} size=${13} /> ${t(label)}</button>`)}</div>`;
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
