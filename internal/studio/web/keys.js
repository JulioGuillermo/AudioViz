// Keyframes in the studio. A segment (or a fixed text) has keys: { path: [{t, v, ease}] }, times
// in seconds from its start, path = config keys joined by dots ("glow.radius", "layers.1.speed").
// The evaluation is the same as internal/config/keys.go.

import { html } from './vendor/preact-htm.js';

export const EASES = [['linear', 'Linear'], ['step', 'Hold (step)'], ['ease_in', 'Ease in'],
  ['ease_out', 'Ease out'], ['ease_in_out', 'Ease in-out'], ['back', 'Anticipation and overshoot'], ['spring', 'Spring']];

// Two keys closer than this are the same instant
export const KEY_EPS = 0.002;

// Field types that can be animated
export const animatable = (f) => ['num', 'int', 'optnum', 'color', 'bool'].includes(f.type) && f.anim !== false;

export function ease(name, k) {
  k = Math.max(0, Math.min(1, k));
  switch (name) {
    case 'step': return 0;
    case 'ease_in': return k * k * k;
    case 'ease_out': return 1 - Math.pow(1 - k, 3);
    case 'ease_in_out': return k < 0.5 ? 4 * k * k * k : 1 - Math.pow(-2 * k + 2, 3) / 2;
    case 'back': { // anticipation and overshoot (as keys.go)
      const c = 1.70158 * 1.525;
      return k < 0.5 ? (2 * k) ** 2 * ((c + 1) * 2 * k - c) / 2 : ((2 * k - 2) ** 2 * ((c + 1) * (2 * k - 2) + c) + 2) / 2;
    }
    case 'spring': return k >= 1 ? 1 : 1 - Math.exp(-6 * k) * Math.cos(4.5 * Math.PI * k);
  }
  return k;
}

function parseHex(s) {
  if (typeof s !== 'string' || s[0] !== '#') return null;
  let h = s.slice(1);
  if (h.length === 3) h = h.split('').map((c) => c + c).join('');
  if (h.length !== 6 && h.length !== 8) return null;
  const n = parseInt(h, 16);
  if (Number.isNaN(n)) return null;
  if (h.length === 6) return [(n >> 16) & 255, (n >> 8) & 255, n & 255, 255];
  return [Math.floor(n / 2 ** 24) & 255, (n >> 16) & 255, (n >> 8) & 255, n & 255];
}

function formatHex(c, alpha) {
  const b = (x) => Math.max(0, Math.min(255, Math.round(x))).toString(16).padStart(2, '0');
  return '#' + b(c[0]) + b(c[1]) + b(c[2]) + (alpha ? b(c[3]) : '');
}

// Value of a (sorted) key list at u seconds
export function valueAt(l, u) {
  if (!l || !l.length) return undefined;
  if (u <= l[0].t) return l[0].v;
  let i = l.length - 1;
  while (i > 0 && l[i].t > u) i--;
  if (i >= l.length - 1) return l[l.length - 1].v;
  const a = l[i], b = l[i + 1];
  const k = b.t > a.t ? ease(a.ease, (u - a.t) / (b.t - a.t)) : 0;
  const v = lerpAny(a.v, b.v, k);
  return v === undefined ? (k >= 1 ? b.v : a.v) : v;
}

// Interpolates two values like config.lerpAny: numbers, colors, and lists / objects of them
// element by element (a mask's shapes morph); undefined when they cannot be mixed
function lerpAny(a, b, k) {
  if (typeof a === 'number' && typeof b === 'number') return a + (b - a) * k;
  if (typeof a === 'boolean' && a === b) return a; // the same switch on both sides (a shape's open)
  if (typeof a === 'string' && typeof b === 'string') {
    const ca = parseHex(a), cb = parseHex(b);
    if (ca && cb) return formatHex(ca.map((x, j) => x + (cb[j] - x) * k), a.length === 9 || b.length === 9);
    return a === b ? a : undefined; // the same text (a shape's group)
  }
  if (Array.isArray(a) && Array.isArray(b)) {
    const n = lerpNode(a, b, k);
    if (n) return n;
    if (a.length !== b.length) return undefined;
    const out = [];
    for (let i = 0; i < a.length; i++) { const v = lerpAny(a[i], b[i], k); if (v === undefined) return undefined; out.push(v); }
    return out;
  }
  if (a && b && typeof a === 'object' && typeof b === 'object') {
    const ka = Object.keys(a);
    if (ka.length !== Object.keys(b).length) return undefined;
    const out = {};
    for (const key of ka) { if (!(key in b)) return undefined; const v = lerpAny(a[key], b[key], k); if (v === undefined) return undefined; out[key] = v; }
    return out;
  }
  return undefined;
}

// Two nodes of a mask shape (as config.lerpNode): the position moves, the type changes halfway,
// the handles move when both have them
function lerpNode(a, b, k) {
  const node = (l) => [2, 3, 7].includes(l.length) && l.every((x) => typeof x === 'number');
  if (!node(a) || !node(b) || (a.length === 2 && b.length === 2)) return null;
  const out = [...(k >= 0.5 ? b : a)];
  const n = a.length === 7 && b.length === 7 ? 7 : 2;
  for (let i = 0; i < n; i++) if (i !== 2) out[i] = a[i] + (b[i] - a[i]) * k;
  return out;
}

const sortKeys = (l) => l.sort((a, b) => a.t - b.t);
export const keyIndexAt = (l, u) => (l || []).findIndex((k) => Math.abs(k.t - u) < KEY_EPS);

// Reads / writes a value at a dotted path inside a config object (writing mutates it)
export function getIn(o, path) {
  for (const p of path.split('.')) { if (o == null) return undefined; o = o[p]; }
  return o;
}
export function setIn(o, path, v) {
  const ps = path.split('.');
  for (let i = 0; i < ps.length - 1; i++) {
    if (o[ps[i]] == null) o[ps[i]] = /^\d+$/.test(ps[i + 1]) ? [] : {};
    o = o[ps[i]];
  }
  o[ps[ps.length - 1]] = v;
}

// Keyframe actions for one target (a segment or a text). `edit(fn)` changes the project:
// fn(holder, cfg) gets the object that holds `keys` and the config with the static values.
// u: the cursor, in seconds from the target's start; seek(u) moves the cursor there.
export function keyApi({ keys = {}, u, edit, seek, focus, setFocus, max = Infinity }) {
  u = Math.max(0, Math.min(max, +u.toFixed(3)));
  const write = (fn) => edit((h, cfg) => {
    const ks = { ...(h.keys || {}) };
    fn(ks, cfg);
    for (const p in ks) if (!ks[p] || !ks[p].length) delete ks[p];
    if (Object.keys(ks).length) h.keys = ks; else delete h.keys;
  });
  return {
    keys, u, focus, setFocus,
    // Value changed in the inspector of an animated property: key at the cursor
    setKey(path, v) {
      write((ks) => {
        const l = (ks[path] || []).map((k) => ({ ...k }));
        const i = keyIndexAt(l, u);
        if (i >= 0) l[i].v = v; else l.push({ t: u, v });
        ks[path] = sortKeys(l);
      });
    },
    // The diamond: animate (first key) / add a key here / remove the key here. Removing the last
    // key leaves the property still, with that value.
    toggle(path, cur) {
      setFocus && setFocus(path);
      write((ks, cfg) => {
        const l = (ks[path] || []).map((k) => ({ ...k }));
        const i = keyIndexAt(l, u);
        if (i >= 0) {
          const [gone] = l.splice(i, 1);
          if (!l.length) setIn(cfg, path, gone.v);
        } else l.push({ t: u, v: cur });
        ks[path] = sortKeys(l);
      });
    },
    // Removes all the keys of a property (it keeps its value at the cursor), or of all of them
    clear(path) {
      write((ks, cfg) => { if (ks[path]) { setIn(cfg, path, valueAt(ks[path], u)); delete ks[path]; } });
    },
    clearAll() {
      write((ks, cfg) => { for (const p in ks) { setIn(cfg, p, valueAt(ks[p], u)); delete ks[p]; } });
    },
    // Moves the cursor to the previous (-1) or next (+1) key of the property
    jump(path, dir) {
      const ts = (keys[path] || []).map((k) => k.t);
      const t = dir < 0 ? ts.filter((x) => x < u - KEY_EPS).pop() : ts.find((x) => x > u + KEY_EPS);
      if (t !== undefined) seek(t);
    },
  };
}

// All the key times of a target (or of one property), merged: [{t, paths}]
export function keyTimes(keys, only) {
  const out = [];
  for (const p in keys || {}) {
    if (only && p !== only) continue;
    for (const k of keys[p]) {
      const e = out.find((x) => Math.abs(x.t - k.t) < KEY_EPS);
      if (e) e.paths.push(p); else out.push({ t: k.t, paths: [p], ease: k.ease || 'linear' });
    }
  }
  return out.sort((a, b) => a.t - b.t);
}

// Changes the keys at time t (of the given paths): moved to nt, or their easing, or removed.
// Returns the new keys object (the given one is not changed).
export function editKeysAt(keys, t, paths, op) {
  const ks = {};
  for (const p in keys) {
    let l = keys[p].map((k) => ({ ...k }));
    if (paths.includes(p)) {
      if (op.remove) l = l.filter((k) => Math.abs(k.t - t) >= KEY_EPS);
      for (const k of l) {
        if (Math.abs(k.t - t) >= KEY_EPS) continue;
        if (op.ease) k.ease = op.ease === 'linear' ? undefined : op.ease;
        if (op.to !== undefined) k.t = +op.to.toFixed(3);
      }
      if (op.to !== undefined) {
        // A key moved onto another one of the same property replaces it
        const moved = l.find((k) => Math.abs(k.t - op.to) < KEY_EPS);
        l = l.filter((k) => k === moved || Math.abs(k.t - op.to) >= KEY_EPS);
      }
      sortKeys(l);
    }
    if (l.length) ks[p] = l;
  }
  return ks;
}

// Small picture of a curve (value over time from one key to the next), drawn with ease() itself
// so it is exactly the motion: the dashed lines are the two keys' values (start below, end above)
export function EaseIcon({ name, size = 18 }) {
  const X = (k) => 2 + k * 16, Y = (v) => 16.5 - v * 13; // value 0 → y 16.5, 1 → y 3.5 (room to overshoot)
  let d = '';
  if (name === 'step') d = `M${X(0)},${Y(0)} H${X(1)} V${Y(1)}`;
  else for (let i = 0; i <= 32; i++) {
    const k = i / 32;
    d += (i ? ' L' : 'M') + X(k).toFixed(2) + ',' + Math.max(0.5, Math.min(19.5, Y(ease(name, k)))).toFixed(2);
  }
  return html`<svg class="ease-ic" width=${size} height=${size} viewBox="0 0 20 20" fill="none" aria-hidden="true">
    <path d=${`M2,${Y(0)} H18 M2,${Y(1)} H18`} stroke="currentColor" stroke-opacity=".28" stroke-width="1" stroke-dasharray="1.5 1.5" />
    <path d=${d} stroke="var(--key)" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" />
  </svg>`;
}
