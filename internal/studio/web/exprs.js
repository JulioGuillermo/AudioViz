import { html, useEffect, useRef, useState } from './vendor/preact-htm.js';
import { createStore, useStore } from './store.js';
import { Icon } from './icons.js';
import { t } from './i18n.js';
import { api } from './util.js';

// The properties' advanced settings (expressions, package expr): the editor (highlighting,
// completion), the dialog (its check and its graph over time), the value shown under a property

// What the language knows (as expr.Vars, Bands, Funcs)
export const EXPR_VARS = ['value', 'clip_time', 'clip_progress', 'project_time', 'project_progress', 'frame', 'fps', 'index', 'count'];
export const EXPR_BANDS = ['bass', 'mid', 'treble', 'volume', 'transient', 'beat', 'drums', 'kick', 'snare', 'hats', 'bassline', 'vocals', 'other'];
export const EXPR_FUNCS = ['sin', 'cos', 'tan', 'asin', 'acos', 'atan', 'atan2', 'rad_sin', 'rad_cos', 'rad_tan', 'rad_asin', 'rad_acos',
  'rad_atan', 'rad_atan2', 'deg', 'rad', 'abs', 'sign', 'floor', 'ceil', 'round', 'fract', 'sqrt', 'exp', 'log', 'pow', 'mod', 'min',
  'max', 'clamp', 'mix', 'lerp', 'smoothstep', 'step', 'map', 'noise', 'noise2', 'rand', 'random', 'is_nil', 'lag', 'peak'];
const KEYWORDS = ['if', 'let', 'ret', 'nil', 'and', 'or', 'not', 'true', 'false'];
const CONSTS = ['pi', 'tau', 'e'];
const OUR = new Set([...EXPR_VARS, ...EXPR_BANDS, ...CONSTS, 'time', 'progress']); // (older names still highlighted)
const FUNCS = new Set(EXPR_FUNCS);

// tokens for highlighting: [{ s, cls }]
export function exprTokens(src) {
  const out = [];
  let i = 0;
  const push = (s, cls) => out.push({ s, cls });
  while (i < src.length) {
    const c = src[i];
    if (c === '#') { let j = src.indexOf('\n', i); if (j < 0) j = src.length; push(src.slice(i, j), 'x-com'); i = j; continue; }
    if (/[0-9]/.test(c) || (c === '.' && /[0-9]/.test(src[i + 1] || ''))) {
      let j = i; while (j < src.length && /[0-9.]/.test(src[j])) j++;
      const s = src.slice(i, j);
      push(s, s.includes('.') ? 'x-float' : 'x-int'); i = j; continue;
    }
    if (c === '$' || /[A-Za-z_]/.test(c)) {
      let j = i + 1; while (j < src.length && /[A-Za-z0-9_]/.test(src[j])) j++;
      const s = src.slice(i, j);
      let cls = '';
      if (s[0] === '$') cls = 'x-user';
      else if ((s === 'path' || s === 'route') && src[j] === '.') {
        push(s, 'x-var'); push('.', 'x-op');
        let k = j + 1; while (k < src.length && /[A-Za-z0-9_]/.test(src[k])) k++;
        push(src.slice(j + 1, k), 'x-key'); i = k; continue;
      } else if (KEYWORDS.includes(s)) cls = 'x-kw';
      else if (FUNCS.has(s) && src[j] === '(') cls = 'x-fn';
      else if (OUR.has(s)) cls = 'x-var';
      push(s, cls); i = j; continue;
    }
    if ('()'.includes(c)) { push(c, 'x-par'); i++; continue; }
    const two = src.slice(i, i + 2);
    if (['==', '!=', '<=', '>=', '&&', '||', '??', '->'].includes(two)) { push(two, 'x-op'); i += 2; continue; }
    if ('+-*/%^<>!=@.'.includes(c)) { push(c, 'x-op'); i++; continue; }
    push(c, ''); i++;
  }
  return out;
}

// The project's own names: its constants and functions ($…)
export function libNames(lib) {
  const consts = Object.keys((lib && lib.constants) || {}).map((k) => '$' + k.replace(/^\$/, ''));
  const funcs = [...String((lib && lib.functions) || '').matchAll(/^\s*\$([A-Za-z_][A-Za-z0-9_]*)\s*\(/gm)].map((m) => '$' + m[1]);
  return { consts, funcs };
}

// ExprEditor: a text with highlighting (a pre under a transparent textarea) and completion.
// names: { lib: {consts, funcs}, keys: path's value names }; err: { line, pos, msg }
export function ExprEditor({ value, onInput, names = {}, err, rows = 6, autofocus }) {
  const ta = useRef(), pre = useRef();
  const [comp, setComp] = useState(null); // { items, sel, start, end, x, y }
  useEffect(() => { if (autofocus && ta.current) ta.current.focus(); }, []);
  const sync = () => { if (pre.current && ta.current) { pre.current.scrollTop = ta.current.scrollTop; pre.current.scrollLeft = ta.current.scrollLeft; } };
  const candidates = (src, pos) => {
    const before = src.slice(0, pos);
    const mk = before.match(/\b(path|route)\.([A-Za-z0-9_]*)$/);
    if (mk) return { start: pos - mk[2].length, word: mk[2], items: (names.keys || []).map((k) => ({ s: k, cls: 'x-key' })) };
    const m = before.match(/\$?[A-Za-z_][A-Za-z0-9_]*$/) || (before.endsWith('$') ? ['$'] : null);
    if (!m) return null;
    const w = m[0];
    const lib = names.lib || {};
    const all = w[0] === '$'
      ? [...(lib.consts || []).map((s) => ({ s, cls: 'x-user' })), ...(lib.funcs || []).map((s) => ({ s: s + '(', cls: 'x-user' }))]
      : [...EXPR_VARS.map((s) => ({ s, cls: 'x-var' })), ...EXPR_BANDS.map((s) => ({ s, cls: 'x-var' })),
        ...EXPR_FUNCS.map((s) => ({ s: s + '(', cls: 'x-fn' })), ...KEYWORDS.map((s) => ({ s, cls: 'x-kw' })),
        { s: 'path.', cls: 'x-var' }, { s: 'route.', cls: 'x-var' }, ...CONSTS.map((s) => ({ s, cls: 'x-var' }))];
    return { start: pos - w.length, word: w, items: all };
  };
  const update = (src, pos) => {
    const c = candidates(src, pos);
    if (!c) { setComp(null); return; }
    const items = c.items.filter((x) => x.s.startsWith(c.word) && x.s !== c.word).slice(0, 12);
    if (!items.length || (c.word === '' && !/\.$/.test(src.slice(0, pos)))) { setComp(null); return; }
    setComp({ items, sel: 0, start: c.start, end: pos });
  };
  const accept = (it) => {
    const el = ta.current, src = el.value;
    const nv = src.slice(0, comp.start) + it.s + src.slice(comp.end);
    const np = comp.start + it.s.length;
    onInput(nv);
    setComp(null);
    requestAnimationFrame(() => { el.focus(); el.setSelectionRange(np, np); });
  };
  const toks = exprTokens(value || '');
  // the mistake: underlined from its position to the end of its word
  let errStart = -1, errEnd = -1;
  if (err && err.line >= 0 && err.pos >= 0) {
    errStart = err.pos;
    errEnd = err.pos + Math.max(1, ((value || '').slice(err.pos).match(/^[$A-Za-z0-9_.]+/) || ['x'])[0].length);
  }
  let at = 0;
  return html`<div class="xed" style=${{ '--rows': rows }}>
    <pre ref=${pre} aria-hidden="true">${toks.map((tk) => {
      const a = at; at += tk.s.length;
      const bad = errStart >= 0 && a < errEnd && at > errStart;
      return html`<span class=${tk.cls + (bad ? ' x-err' : '')}>${tk.s}</span>`;
    })}${'\n'}</pre>
    <textarea ref=${ta} spellcheck="false" value=${value} onScroll=${sync}
      onInput=${(e) => { onInput(e.target.value); update(e.target.value, e.target.selectionStart); }}
      onBlur=${() => setTimeout(() => setComp(null), 150)}
      onKeyDown=${(e) => {
        if (comp) {
          if (e.key === 'ArrowDown') { e.preventDefault(); setComp({ ...comp, sel: (comp.sel + 1) % comp.items.length }); return; }
          if (e.key === 'ArrowUp') { e.preventDefault(); setComp({ ...comp, sel: (comp.sel + comp.items.length - 1) % comp.items.length }); return; }
          if (e.key === 'Enter' || e.key === 'Tab') { e.preventDefault(); accept(comp.items[comp.sel]); return; }
          if (e.key === 'Escape') { e.preventDefault(); e.stopPropagation(); setComp(null); return; }
        }
        if (e.key === 'Tab') {
          // two spaces (a function's body)
          e.preventDefault();
          const el = e.target, p = el.selectionStart;
          onInput(el.value.slice(0, p) + '  ' + el.value.slice(el.selectionEnd));
          requestAnimationFrame(() => el.setSelectionRange(p + 2, p + 2));
        }
        // the editor's own keys (no app shortcuts while typing); Esc and Ctrl+Enter go to the dialog
        if (e.key !== 'Escape' && !(e.key === 'Enter' && (e.ctrlKey || e.metaKey))) e.stopPropagation();
      }}></textarea>
    ${comp && html`<div class="xcomp">${comp.items.map((it, k) => html`<div class=${'xci ' + it.cls + (k === comp.sel ? ' on' : '')}
      onMouseDown=${(e) => { e.preventDefault(); accept(it); }}>${it.s}</div>`)}</div>`}
  </div>`;
}

// The dialog (mounted once by the app): { title, path, src, target, t0, dur, time, names, onSave }
export const exprDialog = createStore(null);
export const openExpr = (spec) => exprDialog.set(spec);

// the samples of the graph: over the element's time, or ±6 s around the cursor
function graphTimes(d) {
  let a = d.t0 ?? 0, b = d.dur > 0 ? a + d.dur : (d.time ?? 0) + 6;
  if (!(d.dur > 0)) a = Math.max(0, (d.time ?? 0) - 6);
  const n = 160, out = [];
  for (let i = 0; i < n; i++) out.push(+(a + (b - a) * i / (n - 1)).toFixed(4));
  return out;
}

export function ExprDialog() {
  const d = useStore(exprDialog);
  const [src, setSrc] = useState('');
  const [res, setRes] = useState(null); // ExprSeries
  const cv = useRef();
  useEffect(() => { if (d) { setSrc(d.src || 'value'); setRes(null); } }, [d]);
  // checked and plotted as it is typed
  useEffect(() => {
    if (!d) return;
    const h = setTimeout(() => {
      api('POST', '/api/expr', { src, target: d.target, path: d.path, times: graphTimes(d) }).then(setRes).catch(() => {});
    }, 220);
    return () => clearTimeout(h);
  }, [src, d]);
  useEffect(() => {
    const c = cv.current;
    if (!c || !res || res.error || !res.samples) return;
    const W = c.width = c.clientWidth * devicePixelRatio, H = c.height = c.clientHeight * devicePixelRatio;
    const g = c.getContext('2d');
    g.clearRect(0, 0, W, H);
    const s = res.samples, vs = s.filter((x) => !x.nil).map((x) => x.v);
    let lo = Math.min(0, ...vs), hi = Math.max(1, ...vs);
    if (hi - lo < 1e-6) hi = lo + 1;
    const X = (i) => (i / Math.max(1, s.length - 1)) * W, Y = (v) => H - 6 - (v - lo) / (hi - lo) * (H - 12);
    g.strokeStyle = 'rgba(255,255,255,0.15)'; g.lineWidth = 1;
    g.beginPath(); g.moveTo(0, Y(0)); g.lineTo(W, Y(0)); g.stroke();
    if (d && d.time != null && s.length > 1) {
      const k = (d.time - s[0].t) / (s[s.length - 1].t - s[0].t);
      if (k >= 0 && k <= 1) { g.strokeStyle = 'rgba(255,140,60,0.7)'; g.beginPath(); g.moveTo(k * W, 0); g.lineTo(k * W, H); g.stroke(); }
    }
    g.strokeStyle = '#5ec8ff'; g.lineWidth = 2 * devicePixelRatio;
    g.beginPath();
    let pen = false;
    s.forEach((x, i) => { if (x.nil) { pen = false; return; } if (!pen) g.moveTo(X(i), Y(x.v)); else g.lineTo(X(i), Y(x.v)); pen = true; });
    g.stroke();
    g.fillStyle = 'rgba(255,255,255,0.55)'; g.font = `${11 * devicePixelRatio}px sans-serif`;
    g.fillText(hi.toFixed(2), 4, 12 * devicePixelRatio); g.fillText(lo.toFixed(2), 4, H - 4);
  }, [res]);
  if (!d) return null;
  const close = () => exprDialog.set(null);
  const save = (v) => { d.onSave(v); close(); };
  const err = res && res.error;
  return html`<div class="modal-bg" onMouseDown=${(e) => e.target === e.currentTarget && close()}>
    <div class="modal xdlg" onKeyDown=${(e) => {
      if (e.key === 'Escape') { e.stopPropagation(); close(); }
      if (e.key === 'Enter' && (e.ctrlKey || e.metaKey) && !err) { e.stopPropagation(); save(src); }
    }}>
      <div class="modal-h"><${Icon} name="sigma" size=${16} cls="muted" />${t('Advanced settings')} · ${d.title}<span class="spacer"></span>
        <button class="btn icon ghost" title=${t('Close (Esc)')} onClick=${close}><${Icon} name="x" size=${16} /></button></div>
      <div class="mb">
        <${ExprEditor} value=${src} onInput=${setSrc} names=${d.names} err=${err} rows=${7} autofocus=${true} />
        <div class=${'xstat' + (err ? ' bad' : '')}>${err ? html`<${Icon} name="triangle-alert" size=${13} /> ${err.line >= 0 ? t('Line {n}', { n: err.line + 1 }) + ': ' : ''}${err.msg}`
          : res ? html`<${Icon} name="check" size=${13} /> ${res.uses_value ? t('Uses the slider\'s value') : t('Doesn\'t use value: the slider has no effect')}${res.warn ? html` · <span class="xwarn">${t('Something impossible came up (a division by 0…): 0 there')}</span>` : ''}` : '…'}</div>
        <canvas ref=${cv} class="xgraph"></canvas>
      </div>
      <div class="mf">
        ${d.had && html`<button class="btn ghost danger" onClick=${() => save(null)}><${Icon} name="trash-2" size=${14} /> ${t('Remove the expression')}</button>`}
        <span class="spacer"></span>
        <button class="btn ghost" onClick=${close}>${t('Cancel')}</button>
        <button class="btn primary" disabled=${!!err} onClick=${() => save(src)}>${t('Save')}</button>
      </div>
    </div>
  </div>`;
}

// copied expression (Copy / Paste on a property)
export const exprClip = createStore(null);
// the project's library (Project → Functions and constants), for completion
export const exprLib = createStore({});

// the value names on a path (its nodes' named values)
export function pathKeyNames(shape) {
  const out = new Set();
  for (const v of (shape && shape.vals) || []) for (const k in (v && v.keys) || {}) out.add(k);
  return [...out].sort();
}

// ExprValue: the value an expression gives now, under its property
export function ExprValue({ kf, path, src, onInfo }) {
  const [v, setV] = useState(null);
  const T = kf.t0 + kf.u;
  useEffect(() => {
    let live = true;
    const h = setTimeout(() => {
      api('POST', '/api/expr', { src, target: kf.target, path, times: [T] }).then((r) => {
        if (!live) return;
        setV(r);
        onInfo && onInfo(r);
      }).catch(() => {});
    }, 120);
    return () => { live = false; clearTimeout(h); };
  }, [src, T, path]);
  if (!v) return null;
  if (v.error) return html`<div class="xval bad" title=${v.error.msg}>ƒ ${t('has a mistake: the slider\'s value is used')}</div>`;
  const s = v.samples && v.samples[0];
  return html`<div class="xval">ƒ = ${s && !s.nil ? (+s.v.toFixed(4)) : 'nil (0)'}</div>`;
}

// LibrarySection: the project's own constants ($name = number) and functions ($f(…) = …), checked
// as they are written (the server compiles them with the project)
export function LibrarySection({ lib, onChange, Section, Edit }) {
  const consts = (lib && lib.constants) || {};
  const [funcs, setFuncs] = useState((lib && lib.functions) || '');
  const [err, setErr] = useState(null);
  useEffect(() => { setFuncs((lib && lib.functions) || ''); }, [lib && lib.functions]);
  // checked once saved (the server's session has them)
  useEffect(() => {
    const h = setTimeout(() => {
      api('POST', '/api/expr', { src: '0', target: { kind: 'none' }, path: '', times: [] })
        .then((r) => setErr(r.error ? r.error.msg : null)).catch(() => {});
    }, 400);
    return () => clearTimeout(h);
  }, [lib && lib.functions, JSON.stringify(consts)]);
  const setConsts = (c) => onChange({ ...lib, constants: Object.keys(c).length ? c : undefined });
  const names = Object.keys(consts).sort();
  return html`<${Section} id="exprlib" title=${t('Functions and constants')}
    help=${'Your own constants ($name) and functions ($name(a, b) = …) for the advanced settings (expressions) of any property. A function\'s body can go on the lines under it, indented: let steps, ret(x) to give a result early, the last line its result.'}>
    <div class="xlib-consts">
      ${names.map((k) => html`<div class="row xlib-c" key=${k}>
        <code class="x-user">$${k}</code>
        <${Edit} type="number" class="mono" style="width:96px" step="any" value=${consts[k]}
          onChange=${(e) => setConsts({ ...consts, [k]: +e.target.value || 0 })} />
        <button class="btn sm ghost danger" title=${t('Remove')} onClick=${() => { const c = { ...consts }; delete c[k]; setConsts(c); }}><${Icon} name="x" size=${12} /></button>
      </div>`)}
      <${Edit} type="text" class="mono grow" placeholder=${t('new constant: name')}
        onChange=${(e) => {
          const k = e.target.value.trim().replace(/^\$/, '').replace(/[^A-Za-z0-9_]/g, '_');
          e.target.value = '';
          if (k && !/^[0-9]/.test(k) && !(k in consts)) setConsts({ ...consts, [k]: 1 });
        }} />
    </div>
    <hr class="node-sep" />
    <${ExprEditor} value=${funcs} rows=${8} names=${{ lib: libNames(lib) }} onInput=${setFuncs} />
    <div class="row" style="margin-top:6px;gap:8px">
      <span class=${'xstat' + (err ? ' bad' : '')} style="flex:1">${err || ''}</span>
      <button class="btn sm" disabled=${funcs === ((lib && lib.functions) || '')}
        onClick=${() => onChange({ ...lib, functions: funcs.trim() ? funcs : undefined })}><${Icon} name="check" size=${13} /> ${t('Apply')}</button>
    </div>
  </${Section}>`;
}
