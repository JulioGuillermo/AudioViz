// Collaboration (as internal/studio/collab.go): the project changes by operations on its JSON
// (JSON Pointer: set, insert, remove), never all of it at once. Each change made here is the
// difference between what the server will have and what is on screen; the server applies the
// operations in order and sends everyone's to everyone; those of others are applied here moved
// past what is still on its way or not sent yet, so that nothing being done here is lost.

import { clone } from './util.js';

// the project's keys that are not shared: the studio's view (each one's) and read-only defaults
const LOCAL_KEYS = new Set(['view', 'clipDefaults', 'textDefaults', 'emitterPresets']);

export const tokens = (p) => (p === '' || p === '/' ? [] : p.slice(1).split('/').map((s) => s.replace(/~1/g, '/').replace(/~0/g, '~')));
export const pointer = (ts) => '/' + ts.map((s) => String(s).replace(/~/g, '~0').replace(/\//g, '~1')).join('/');

const isObj = (v) => v !== null && typeof v === 'object' && !Array.isArray(v);

export function deepEqual(a, b) {
  if (a === b) return true;
  if (typeof a !== typeof b || a === null || b === null || typeof a !== 'object') return false;
  if (Array.isArray(a) !== Array.isArray(b)) return false;
  if (Array.isArray(a)) {
    if (a.length !== b.length) return false;
    for (let i = 0; i < a.length; i++) if (!deepEqual(a[i], b[i])) return false;
    return true;
  }
  const ka = Object.keys(a), kb = Object.keys(b);
  if (ka.length !== kb.length) return false;
  for (const k of ka) if (!(k in b) || !deepEqual(a[k], b[k])) return false;
  return true;
}

// diff: the operations that turn a into b (in order); list items by their place: what was
// inserted or removed in a run, the rest item by item
export function diff(a, b, path = [], out = []) {
  if (deepEqual(a, b)) return out;
  if (isObj(a) && isObj(b)) {
    for (const k of Object.keys(a)) {
      if (!path.length && LOCAL_KEYS.has(k)) continue;
      if (!(k in b)) out.push({ op: 'remove', path: pointer([...path, k]) });
    }
    for (const k of Object.keys(b)) {
      if (!path.length && LOCAL_KEYS.has(k)) continue;
      if (!(k in a)) out.push({ op: 'set', path: pointer([...path, k]), value: clone(b[k]) });
      else diff(a[k], b[k], [...path, k], out);
    }
    return out;
  }
  if (Array.isArray(a) && Array.isArray(b)) {
    let p = 0;
    while (p < a.length && p < b.length && deepEqual(a[p], b[p])) p++;
    let s = 0;
    while (s < a.length - p && s < b.length - p && deepEqual(a[a.length - 1 - s], b[b.length - 1 - s])) s++;
    const ma = a.length - p - s, mb = b.length - p - s;
    if (ma > mb) {
      // removed: which ones (the run that leaves the rest most alike), then the others item by item
      let best = 0, bestScore = -1;
      for (let o = 0; o <= ma - mb; o++) {
        let sc = 0;
        for (let i = 0; i < mb; i++) if (deepEqual(a[p + (i < o ? i : i + ma - mb)], b[p + i])) sc++;
        if (sc > bestScore) { best = o; bestScore = sc; }
      }
      for (let i = 0; i < ma - mb; i++) out.push({ op: 'remove', path: pointer([...path, p + best]) });
      for (let i = 0; i < mb; i++) diff(a[p + (i < best ? i : i + ma - mb)], b[p + i], [...path, p + i], out);
    } else {
      for (let i = 0; i < ma; i++) diff(a[p + i], b[p + i], [...path, p + i], out);
      for (let i = ma; i < mb; i++) out.push({ op: 'insert', path: pointer([...path, p + i]), value: clone(b[p + i]) });
    }
    return out;
  }
  out.push({ op: 'set', path: pointer(path), value: clone(b) });
  return out;
}

// applyOp: o on root (changed in place; a new root when it is the root that changes); throws if
// its place is not there
export function applyOp(root, o) {
  const ts = tokens(o.path);
  if (!ts.length) return clone(o.value);
  let cur = root;
  for (let i = 0; i < ts.length - 1; i++) {
    const t = ts[i];
    let nx = Array.isArray(cur) ? cur[+t] : cur[t];
    if (nx === undefined || nx === null) {
      if (o.op === 'remove' || Array.isArray(cur)) throw new Error('no ' + o.path);
      nx = cur[t] = {};
    }
    cur = nx;
  }
  const last = ts[ts.length - 1];
  if (Array.isArray(cur)) {
    const k = last === '-' ? cur.length : +last;
    if (!(k >= 0 && k <= cur.length)) throw new Error('no ' + o.path);
    if (o.op === 'insert') cur.splice(k, 0, clone(o.value));
    else if (o.op === 'remove') { if (k >= cur.length) throw new Error('no ' + o.path); cur.splice(k, 1); }
    else cur[k] = o.op === 'merge' ? merge(cur[k], o.value) : clone(o.value);
  } else if (isObj(cur)) {
    if (o.op === 'remove') delete cur[last];
    else cur[last] = o.op === 'merge' ? merge(cur[last], o.value) : clone(o.value);
  } else throw new Error('no ' + o.path);
  return root;
}

function merge(a, b) {
  if (!isObj(a) || !isObj(b)) return clone(b);
  const o = { ...a };
  for (const k in b) o[k] = merge(a[k], b[k]);
  return o;
}

// applyOps: ops on a copy of root (those whose place is gone are left out)
export function applyOps(root, ops) {
  let r = clone(root);
  for (const o of ops || []) {
    try { r = applyOp(r, o); } catch (e) { /* its place is gone */ }
  }
  return r;
}

// transformOp: o moved past a (applied before it on the same state); null when its place is gone.
// first: o goes first where both insert at the same place. As collab.go.
function transformOp(o, a, first) {
  if (a.op !== 'insert' && a.op !== 'remove') return o;
  const at = tokens(a.path), ot = tokens(o.path);
  if (!at.length || ot.length < at.length) return o;
  const n = at.length - 1;
  for (let i = 0; i < n; i++) if (at[i] !== ot[i]) return o;
  const ai = +at[n], oi0 = +ot[n];
  if (!Number.isInteger(ai) || !Number.isInteger(oi0) || at[n] === '' || ot[n] === '') return o;
  const exact = ot.length === at.length;
  let oi = oi0;
  if (a.op === 'insert') {
    if (oi > ai || (oi === ai && !(first && exact && o.op === 'insert'))) oi++;
  } else if (oi > ai) oi--;
  else if (oi === ai && !(exact && o.op === 'insert')) return null;
  if (oi === oi0) return o;
  const t2 = [...ot];
  t2[n] = String(oi);
  return { ...o, path: pointer(t2) };
}

// transformPair: ops (made one after another) moved past against (applied first, on the same
// state), and against moved past ops: [ops', against']
export function transformPair(ops, against) {
  const ag = [...(against || [])];
  const out = [];
  for (let o of ops || []) {
    for (let j = 0; j < ag.length && o; j++) {
      if (!ag[j]) continue;
      const o2 = transformOp(o, ag[j], false);
      ag[j] = transformOp(ag[j], o, true);
      o = o2;
    }
    if (o) out.push(o);
  }
  return [out, ag.filter(Boolean)];
}

// ---- who I am (asked once per browser) and this tab's id

const COLORS = ['#f97316', '#22c55e', '#3b82f6', '#e11d48', '#eab308', '#14b8a6', '#8b5cf6', '#ec4899', '#84cc16', '#06b6d4'];

export function me() {
  let u = null;
  try { u = JSON.parse(localStorage.getItem('audioviz.user')); } catch (e) { /* none */ }
  if (!u || !u.color) u = { name: (u && u.name) || '', color: COLORS[Math.floor(Math.random() * COLORS.length)] };
  let id = '';
  try { id = sessionStorage.getItem('audioviz.client'); } catch (e) { /* none */ }
  if (!id) {
    id = Math.random().toString(36).slice(2, 10) + Date.now().toString(36).slice(-4);
    try { sessionStorage.setItem('audioviz.client', id); } catch (e) { /* none */ }
  }
  return { ...u, id };
}

export function saveMe(u) {
  try { localStorage.setItem('audioviz.user', JSON.stringify({ name: u.name, color: u.color })); } catch (e) { /* none */ }
}
