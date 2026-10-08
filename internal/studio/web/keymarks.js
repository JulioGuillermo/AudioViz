import { html } from './vendor/preact-htm.js';
import { TRACKS, TEXT_FIELDS, CLIP_FIELDS, EMITTER_CLIP_FIELDS } from './schema.js';
import { t } from './i18n.js';
import { EASES, EaseIcon, keyTimes, editKeysAt, KEY_EPS } from './keys.js';

// The keyframe diamonds of the timeline and of the editor's bar share this: the property filter
// (kfFocus: only the keys of one property), the selection of several keys (kfSel: { key, ts },
// the target's id and the selected times) and the right-click menu.

// The name of a property path, as the inspector shows it ("Glow · Intensity", "Mask 1 · Shape")
const MASK_PARTS = { shapes: 'Shape', feather: 'Soft edge (px)', opacity: 'Strength', mode: 'Clip seen' };
function find(fields, parts) {
  for (const f of fields || []) {
    if (f.type === 'section' && f.fields) { const r = find(f.fields, parts); if (r) return r; continue; }
    const kp = f.key.split('.'); // a group may edit a block inside a block ("emitter.glow")
    if (kp.some((k, i) => parts[i] !== k)) continue;
    const rest = parts.slice(kp.length);
    if (!rest.length) return [f.label];
    if (f.fields && /^\d+$/.test(rest[0])) {
      const r = find(f.fields, rest.slice(1));
      if (r) return [f.label + ' ' + (+rest[0] + 1), ...r];
      continue;
    }
    if (f.fields) { const r = find(f.fields, rest); if (r) return [f.label, ...r]; continue; }
  }
  return null;
}
export function keyLabel(path) {
  const ps = path.split('.');
  let parts;
  if (ps[0] === 'masks' && ps.length >= 3) parts = [t('Mask {n}', { n: +ps[1] + 1 }), t(MASK_PARTS[ps[2]] || ps[2])];
  else if (path === 'motion.progress') parts = [t('Progress along the path')];
  else {
    for (const fs of [TEXT_FIELDS, CLIP_FIELDS, EMITTER_CLIP_FIELDS, ...TRACKS.map((x) => x.fields)]) { const r = find(fs, ps); if (r) { parts = r.map((x) => t(x)); break; } }
  }
  return parts ? parts.join(' · ') : path;
}

export const targetKey = (target) => JSON.stringify(target);
const near = (a, b) => Math.abs(a - b) < KEY_EPS;
export const isPicked = (kfSel, target, tt) => !!(kfSel && kfSel.key === targetKey(target) && kfSel.ts.some((x) => near(x, tt)));
// The selected times of a target (those of a key k: all the selected ones if k is among them, else k's)
export function pickedTimes(kfSel, target, k) {
  return isPicked(kfSel, target, k.t) ? kfSel.ts : [k.t];
}

// Click on a key: select it (Shift / Ctrl: add or take it out of the selection)
export function pickKey(e, kfSel, setKfSel, target, k) {
  const key = targetKey(target);
  const cur = kfSel && kfSel.key === key ? kfSel.ts : [];
  if (e.shiftKey || e.ctrlKey || e.metaKey) {
    setKfSel({ key, ts: cur.some((x) => near(x, k.t)) ? cur.filter((x) => !near(x, k.t)) : [...cur, k.t] });
    return false; // no drag
  }
  if (!cur.some((x) => near(x, k.t))) setKfSel({ key, ts: [k.t] });
  return true;
}

// The keys at the given times (of the shown property, or of all) moved by dt, keeping their spacing
// inside 0..max (a key landing on another one of the same property replaces it)
export function moveKeysAt(keys, times, only, dt, max) {
  const lo = Math.min(...times), hi = Math.max(...times);
  dt = Math.max(-lo, Math.min(max - hi, dt));
  const ks = {};
  for (const p in keys) {
    let l = keys[p].map((k) => ({ ...k }));
    if (!only || p === only) {
      const moved = l.filter((k) => times.some((x) => near(x, k.t)));
      moved.forEach((k) => { k.t = +(k.t + dt).toFixed(3); });
      l = l.filter((k) => moved.includes(k) || !moved.some((m) => near(m.t, k.t))).sort((a, b) => a.t - b.t);
    }
    if (l.length) ks[p] = l;
  }
  return { keys: ks, dt };
}
// editKeysAt at several times (ease, remove), of the shown property or of all
export function editKeysTimes(keys, times, only, op) {
  let ks = keys;
  for (const tt of times) {
    const e = keyTimes(ks, only).find((x) => near(x.t, tt));
    if (e) ks = editKeysAt(ks, e.t, e.paths, op);
  }
  return ks;
}

// The right-click menu of a key k of a target: its curve, delete, the filter, clearing
export function keyMenuItems({ target, keys, k, focus, setFocus, kfSel, setKfSel, onKeys }) {
  const ts = pickedTimes(kfSel, target, k);
  const many = ts.length > 1;
  const props = keyTimes(keys).find((x) => near(x.t, k.t));
  const paths = props ? props.paths : k.paths;
  const items = [
    { header: t(many ? 'Curve to the next key ({n} keys)' : 'Curve to the next key', { n: ts.length }) },
    ...EASES.map(([id, label]) => ({ label: t(label), icon: html`<${EaseIcon} name=${id} />`, checked: !many && k.ease === id,
      onClick: () => onKeys(target, (ks) => editKeysTimes(ks, ts, focus, { ease: id })) })),
    '-',
    { label: t(many ? 'Delete the selected keys (Del)' : k.paths.length > 1 ? 'Delete these keys (Del)' : 'Delete the key (Del)'), icon: 'trash-2', danger: true,
      onClick: () => { onKeys(target, (ks) => editKeysTimes(ks, ts, focus, { remove: true })); setKfSel(null); } },
    { label: t('Select all the keys'), icon: 'mouse-pointer-2',
      onClick: () => setKfSel({ key: targetKey(target), ts: keyTimes(keys, focus).map((x) => x.t) }) },
    '-',
    { header: t('Show on the timeline') },
    ...(focus ? [{ label: t('All the properties'), icon: 'eye', onClick: () => setFocus(null) }] : []),
    ...paths.filter((p) => p !== focus).slice(0, 10).map((p) => ({ label: t('Only: {p}', { p: keyLabel(p) }), icon: 'filter', onClick: () => { setFocus(p); setKfSel(null); } })),
    '-',
    ...(focus ? [{ label: t('Clear all the keys of {p}', { p: keyLabel(focus) }), icon: 'eraser', danger: true,
      onClick: () => { onKeys(target, (ks) => { const n = { ...ks }; delete n[focus]; return n; }); setFocus(null); setKfSel(null); } }]
      : paths.length === 1 ? [{ label: t('Clear all the keys of {p}', { p: keyLabel(paths[0]) }), icon: 'eraser', danger: true,
        onClick: () => { onKeys(target, (ks) => { const n = { ...ks }; delete n[paths[0]]; return n; }); setKfSel(null); } }] : []),
    { label: t('Clear all the keys of this element'), icon: 'eraser', danger: true,
      onClick: () => { onKeys(target, () => ({})); setFocus(null); setKfSel(null); } },
  ];
  return items;
}

// The chip that shows the property filter
export function FocusChip({ focus, setFocus }) {
  if (!focus) return null;
  return html`<span class="kf-chip" title=${t('Only the keys of this property are shown')}><i class="kfdot"></i>${keyLabel(focus)}
    <button class="btn sm icon ghost" title=${t('Show the keys of all the properties')} onClick=${() => setFocus(null)}>✕</button></span>`;
}
