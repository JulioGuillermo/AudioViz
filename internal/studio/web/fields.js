import { html, useState, Component } from './vendor/preact-htm.js';
import { basename, splitHex, joinHex, gradientCSS, clone } from './util.js';
import { Icon } from './icons.js';
import { t } from './i18n.js';
import { createStore } from './store.js';
import { openMenu } from './menu.js';
import { helpItem, tHelp } from './help.js';
import { openExpr, exprClip, ExprValue, exprLib, libNames } from './exprs.js';
import { animatable, valueAt, keyIndexAt } from './keys.js';

// Generic form built from the schema. value = config object; onChange(newObject).
// kf (optional, see keys.js keyApi): keyframes of the edited segment / text; prefix: path of
// `value` inside it (groups and layers).
export function Form({ fields, value, onChange, ctx, isFirst = true, kf = null, prefix = '' }) {
  // a key may be a dotted path (a group that edits a block inside a block)
  const set = (key, v) => {
    if (!key.includes('.')) { onChange({ ...value, [key]: v }); return; }
    const ps = key.split('.'), out = { ...value };
    let o = out;
    for (let i = 0; i < ps.length - 1; i++) { o[ps[i]] = { ...(o[ps[i]] || {}) }; o = o[ps[i]]; }
    o[ps[ps.length - 1]] = v;
    onChange(out);
  };
  const get = (key) => key.split('.').reduce((o, p) => (o == null ? undefined : o[p]), value);
  return html`${fields.map((f) => {
    if (f.showIf && !f.showIf(value)) return null;
    if (f.firstOnly && !isFirst) return null;
    // A section only groups fields of this same object under a title
    if (f.type === 'section') return html`<${Section} key=${f.key} id=${prefix + '|' + f.key} title=${t(f.label)} help=${f.help}><${Form} fields=${f.fields} value=${value}
      onChange=${onChange} ctx=${ctx} isFirst=${isFirst} kf=${kf} prefix=${prefix} /></${Section}>`;
    const path = prefix ? prefix + '.' + f.key : f.key;
    // dia: the keyframe diamond; 'gap' = a form with keyframes, field not animatable (aligned label)
    let v = get(f.key), setV = (x) => set(f.key, x), dia = kf ? 'gap' : null;
    if (kf && animatable(f)) {
      // Animated: shows the value at the cursor; a change sets a key at the cursor
      const l = kf.keys[path];
      if (l && l.length) {
        v = valueAt(l, kf.u);
        // shown with the precision of the field (the render uses the exact value)
        if (typeof v === 'number') v = f.type === 'int' ? Math.round(v) : +v.toFixed(Math.max(0, -Math.floor(Math.log10(f.step || 0.01))));
        setV = (x) => kf.setKey(path, x);
      }
      dia = html`<${KeyButton} kf=${kf} path=${path} list=${l} cur=${v} />`;
    }
    return html`<${Field} key=${f.id || f.key} f=${f} v=${v} set=${setV} ctx=${ctx} isFirst=${isFirst} kf=${kf} path=${path} dia=${dia} />`;
  })}`;
}

// Keyframe diamond of a property: empty = still, outline = animated, filled = key at the
// cursor. Click: add / remove the key at the cursor (the timeline shows the property's keys:
// click one to go there).
function KeyButton({ kf, path, list, cur }) {
  const animated = !!(list && list.length);
  const here = animated && keyIndexAt(list, kf.u) >= 0;
  const focus = kf.focus === path;
  // Right-click: show only its keys on the timeline, clear its keys or all of the element's
  const menu = (e) => {
    const any = Object.keys(kf.keys || {}).length > 0;
    const setFocus = (p) => kf.setFocus && kf.setFocus(p);
    openMenu(e, [
      { label: t(here ? 'Remove the key at the cursor' : 'Add a key at the cursor'), icon: here ? 'minus' : 'plus', onClick: () => kf.toggle(path, cur) },
      '-',
      focus ? { label: t('Show the keys of all the properties'), icon: 'eye', onClick: () => setFocus(null) }
        : { label: t('Show only its keys on the timeline'), icon: 'filter', disabled: !animated, onClick: () => setFocus(path) },
      '-',
      { label: t('Clear all its keys'), icon: 'eraser', danger: true, disabled: !animated, onClick: () => { kf.clear(path); if (focus) setFocus(null); } },
      { label: t('Clear all the keys of this element'), icon: 'eraser', danger: true, disabled: !any, onClick: () => { kf.clearAll(); setFocus(null); } },
    ]);
  };
  return html`<span class=${'kfbtn' + (animated ? ' anim' : '') + (focus ? ' focus' : '')} onContextMenu=${menu}>
    <button class=${'kfd' + (here ? ' on' : '')} onClick=${() => kf.toggle(path, cur)}
      title=${t(here ? 'Remove the key at the cursor' : animated ? 'Add a key at the cursor' : 'Animate: first key at the cursor (then change the value at other instants)')}></button>
  </span>`;
}

// Text / number box that keeps what you are typing while it has focus: the app re-renders
// several times a second while playing, and a controlled value would be reset on every render.
// The value is applied on change (Enter or leaving the field); Esc restores it.
export function Edit({ tag = 'input', value, onKeyDown, ...rest }) {
  const [focused, setFocused] = useState(false);
  return html`<${tag} ...${rest} value=${focused ? undefined : value}
    onFocus=${() => setFocused(true)} onBlur=${() => setFocused(false)}
    onKeyDown=${(e) => {
      if (e.key === 'Enter' && tag === 'input') e.target.blur();
      else if (e.key === 'Escape') { e.target.value = value ?? ''; e.target.blur(); }
      onKeyDown && onKeyDown(e);
    }} />`;
}

// The field being adjusted right now, for the ones that show something while they are
// (f.adjust: e.g. 'lightrange' shows the depth a light reaches; 'range' a background effect's
// depth range, f.rangeKeys its [from, to, soft] keys): { kind, keys } or null
export const adjusting = createStore(null);

// the types of properties an expression can give (numbers)
// a text's properties an expression can work out per letter or pixel (as lyrics.TextPxProps)
const TEXT_EACH = ['color', 'outline_color', 'glow.color', 'shadow.color', 'opacity', 'outline_width', 'glow.intensity',
  'glow.radius', 'shadow.opacity', 'shadow.blur'];
// a clip's properties an expression can work out per pixel (as effects.ClipPxProps)
const CLIP_EACH = ['chroma.color', 'opacity', 'brightness', 'contrast', 'saturation', 'hue', 'chroma.tolerance', 'chroma.softness'];
const EXPRABLE = new Set(['num', 'int', 'optnum', 'bool', 'color']);

function Row({ f, children, dia, kf, path }) {
  const [xinfo, setXinfo] = useState(null); // the expression's check (uses value?)
  const exprable = kf && kf.target && path && EXPRABLE.has(f.type);
  const src = exprable && kf.exprs ? kf.exprs[path] : null;
  const btn = dia && dia !== 'gap';
  // f.adjust: say so while the pointer is down on it or it has the focus
  const adj = f.adjust ? {
    onPointerDown: () => {
      adjusting.set({ kind: f.adjust, keys: f.rangeKeys });
      // released: back to normal, unless typing a number in it
      const up = () => {
        removeEventListener('pointerup', up);
        const el = document.activeElement;
        if (!(el && el.closest('.fld.adjust') && el.tagName === 'INPUT' && el.type !== 'range')) adjusting.set(null);
      };
      addEventListener('pointerup', up);
    },
    onFocusIn: (e) => { if (e.target.type !== 'range') adjusting.set({ kind: f.adjust, keys: f.rangeKeys }); },
    onFocusOut: (e) => { if (e.target.type !== 'range') adjusting.set(null); },
  } : {};
  // right-click: its advanced settings (an expression), its help (not on screen)
  const openAdv = () => openExpr({ title: t(f.label), path, src: src || 'value', had: !!src, target: kf.target, t0: kf.t0, dur: kf.dur,
    time: kf.t0 + kf.u, names: { lib: libNames(exprLib.get()), keys: kf.pathKeys || [] },
    // a text's colors: worked out for all of it, per letter or per pixel (or by what it reads)
    each: kf.target && ((kf.target.kind === 'text' && TEXT_EACH.includes(path)) || (kf.target.kind === 'clip' && !kf.emitter && CLIP_EACH.includes(path)))
      ? ((kf.exprEach || {})[path] || '') : null,
    eachKinds: kf.target && kf.target.kind === 'clip' ? ['text', 'pixel'] : ['text', 'letter', 'pixel'],
    onSave: (v, each) => { kf.setExpr(path, v); if (each != null) kf.setEach(path, v == null ? '' : each); } });
  const items = [];
  if (exprable) {
    items.push({ label: t('Advanced settings…'), icon: 'sigma', onClick: openAdv },
      { label: t('Copy the expression'), icon: 'copy', disabled: !src, onClick: () => exprClip.set(src) },
      { label: t('Paste the expression'), icon: 'clipboard', disabled: !exprClip.get(), onClick: () => kf.setExpr(path, exprClip.get()) });
    if (src) items.push({ label: t('Remove the expression'), icon: 'trash-2', danger: true, onClick: () => kf.setExpr(path, null) });
  }
  if (f.help) items.push(...(items.length ? ['-'] : []), helpItem(t(f.label), tHelp(f.help)));
  const menu = items.length ? (e) => openMenu(e, items) : undefined;
  const noValue = src && xinfo && !xinfo.error && !xinfo.uses_value;
  // click on its name: the timeline shows only its keys (if it has any); a click anywhere else
  // shows them all again (the app's kfFocus)
  const animated = btn && kf && kf.keys && (kf.keys[path] || []).length > 0;
  const focused = animated && kf.focus === path;
  const nameClick = animated ? (e) => { e.preventDefault(); kf.setFocus && kf.setFocus(path); } : undefined;
  return html`<div class=${'fld' + (f.type === 'bool' ? ' fld-bool' : '') + (dia ? ' kfable' : '') + (f.adjust ? ' adjust' : '') + (src ? ' has-expr' : '') + (noValue ? ' expr-novalue' : '') + (animated ? ' kf-named' : '') + (focused ? ' kf-focus' : '')}
      data-kfpath=${animated ? path : undefined} ...${adj} onContextMenu=${menu}>
    <label for=${btn ? 'kf-none' : undefined} onClick=${nameClick} title=${animated ? t('Click: show only its keys on the timeline') : undefined}>${btn && dia}${src && html`<span class="xbadge" title=${t('It has an expression (right-click: Advanced settings)')}
      onClick=${(e) => { e.preventDefault(); e.stopPropagation(); openAdv(); }}>ƒ</span>`}${t(f.label)}</label>
    <div class="ctl">${children}</div>
    ${src && html`<${ExprValue} kf=${kf} path=${path} src=${src} bool=${f.type === 'bool'} onInfo=${setXinfo} />`}
  </div>`;
}

export function Switch({ checked, onChange, title, disabled }) {
  return html`<label class="switch" title=${title || ''}>
    <input type="checkbox" checked=${!!checked} disabled=${disabled} onChange=${(e) => onChange(e.target.checked)} /><span></span>
  </label>`;
}

// <select> whose current value is always shown: Preact sets select.value before the options
// exist, so the selected option is marked on the <option> itself.
// Select only re-renders when what it shows changes: the playing App re-renders
// several times a second and Preact would otherwise rewrite every <option>,
// which flickers (or closes) an open dropdown.
function selectSig(p) {
  const tr = p.translate !== false;
  return [String(p.value ?? ''), p.cls, p.title, ...p.options.map(([v, l]) => v + '\u0001' + (tr ? t(l) : l))].join('\u0002');
}

export class Select extends Component {
  // compared against the signature of the last render (labels translated then),
  // so a language change also re-renders it
  shouldComponentUpdate(next) { return selectSig(next) !== this.sig; }
  render({ value, options, cls = 'grow', title, translate = true }) {
    this.sig = selectSig(this.props);
    const cur = String(value ?? '');
    const has = options.some(([v]) => String(v) === cur);
    // this.props.onChange is read at event time, so a skipped render never uses a stale handler
    return html`<select class=${cls} title=${title || ''} onChange=${(e) => this.props.onChange(e.target.value)}>
      ${!has && html`<option value=${cur} selected>${cur || '—'}</option>`}
      ${options.map(([v, lbl]) => html`<option value=${String(v)} selected=${String(v) === cur}>${translate ? t(lbl) : lbl}</option>`)}
    </select>`;
  }
}

function Num({ f, v, set, optional }) {
  const step = f.step || (f.type === 'int' ? 1 : 0.01);
  const val = v ?? f.min ?? 0;
  const conv = (x) => (f.type === 'int' ? Math.round(+x) : +x);
  const on = !optional || (v !== null && v !== undefined);
  return html`
    ${optional && html`<${Switch} title=${t('Use a custom value (off = automatic)')} checked=${on}
        onChange=${(c) => set(c ? (f.min ?? 0) : null)} />`}
    <input type="range" min=${f.min ?? 0} max=${f.max ?? 1} step=${step} value=${val} disabled=${!on}
      onInput=${(e) => set(conv(e.target.value))} />
    <${Edit} type="number" step=${step} value=${on ? val : ''} disabled=${!on} placeholder=${t('auto')}
      onChange=${(e) => e.target.value !== '' && set(conv(e.target.value))} />`;
}

function Color({ f, v, set }) {
  const empty = v === '' || v === undefined || v === null;
  const off = empty && f.optional;
  const { rgb, a } = splitHex(empty ? '#ffffff' : v);
  return html`
    ${f.optional && html`<${Switch} title=${t('Use a custom color')} checked=${!empty} onChange=${(c) => set(c ? '#ffffff' : '')} />`}
    <input type="color" value=${rgb} disabled=${off} onInput=${(e) => set(joinHex(e.target.value, a))} />
    <input type="range" min="0" max="255" step="1" value=${a} title=${t('Opacity')} disabled=${off}
      onInput=${(e) => set(joinHex(rgb, +e.target.value))} />
    <${Edit} type="text" value=${empty ? '' : v} style="width:88px" class="mono" placeholder=${f.optional ? t('automatic') : ''}
      disabled=${off} onChange=${(e) => set(e.target.value.trim())} />`;
}

function Gradient({ v, set }) {
  const stops = v || [];
  const upd = (i, k, x) => { const s = clone(stops); s[i][k] = x; set(s); };
  return html`<div class="stops">
    <div class="gradbar" style=${{ background: gradientCSS(stops) }}></div>
    ${stops.map((g, i) => {
      const { rgb, a } = splitHex(g.color);
      return html`<div class="stop">
        <input type="color" value=${rgb} onInput=${(e) => upd(i, 'color', joinHex(e.target.value, a))} />
        <${Edit} type="text" value=${g.color} onChange=${(e) => upd(i, 'color', e.target.value.trim())} />
        <input type="range" min="0" max="1" step="0.01" value=${g.offset} title=${t('Position')} onInput=${(e) => upd(i, 'offset', +e.target.value)} />
        <span class="pct">${Math.round(g.offset * 100)}%</span>
        <button class="btn sm icon ghost danger" title=${t('Remove color')} onClick=${() => set(stops.filter((_, j) => j !== i))}><${Icon} name="x" size=${14} /></button>
      </div>`;
    })}
    <div class="row">
      <button class="btn sm" onClick=${() => {
        const last = stops[stops.length - 1];
        set([...stops, { offset: last ? Math.min(1, last.offset + 0.25) : 0, color: last ? last.color : '#ffffff' }]);
      }}><${Icon} name="plus" size=${14} /> ${t('Color')}</button>
      ${stops.length > 1 && html`<button class="btn sm ghost" title=${t('Reverse the gradient')}
        onClick=${() => set(stops.map((g) => ({ ...g, offset: 1 - g.offset })).reverse())}><${Icon} name="arrow-left-right" size=${14} /> ${t('Reverse')}</button>`}
    </div>
  </div>`;
}

const BAND_LABEL = { bass: 'bass', mid: 'mids', treble: 'treble', overall: 'volume', transient: 'transients', beat: 'beats' };
const TYPE_LABEL = { rising_sparks: 'Sparks', radial_burst: 'Burst', ambient_dust: 'Dust' };

function Layers({ f, v, set, ctx, kf, path }) {
  const layers = v || [];
  const [open, setOpen] = useState(0);
  const upd = (i, lv) => { const l = clone(layers); l[i] = lv; set(l); };
  return html`<div style="width:100%">
    ${layers.map((l, i) => html`<div class="layer" key=${i}>
      <div class="lh" onClick=${() => setOpen(open === i ? -1 : i)}>
        <${Icon} name=${open === i ? 'chevron-down' : 'chevron-right'} size=${14} cls="faint" />
        <span class="sw" style=${{ background: l.color || '#fff' }}></span>
        <span>${t('Layer {n}', { n: i + 1 })}</span>
        <span class="faint">${t(TYPE_LABEL[l.type] || l.type)} · ${t(BAND_LABEL[l.react_to || 'bass'])}</span>
        <span class="spacer"></span>
        <button class="btn sm icon ghost" title=${t('Duplicate layer')} onClick=${(e) => { e.stopPropagation(); const n = clone(layers); n.splice(i + 1, 0, clone(l)); set(n); }}><${Icon} name="copy" size=${14} /></button>
        <button class="btn sm icon ghost danger" title=${t('Delete layer')} disabled=${layers.length < 2} onClick=${(e) => { e.stopPropagation(); set(layers.filter((_, j) => j !== i)); }}><${Icon} name="trash-2" size=${14} /></button>
      </div>
      ${open === i && html`<div class="lb"><${Form} fields=${f.fields} value=${l} onChange=${(lv) => upd(i, lv)} ctx=${ctx} kf=${kf} prefix=${path + '.' + i} /></div>`}
    </div>`)}
    <button class="btn sm" style="margin-top:4px" onClick=${() => { set([...layers, clone(layers[layers.length - 1] || {})]); setOpen(layers.length); }}>
      <${Icon} name="plus" size=${14} /> ${t('Layer')}</button>
  </div>`;
}

// A folding section: closed until opened; which ones are open is remembered (by id) while the
// studio is open, so they stay open when the selection changes
const openSections = new Set();
export function Section({ title, children, id, defOpen = false, extra = null, help = null }) {
  const key = id || title;
  const [open, setOpenState] = useState(() => openSections.has(key) || (defOpen && !openSections.has('!' + key)));
  const setOpen = (o) => {
    openSections.delete(key); openSections.delete('!' + key);
    openSections.add(o ? key : '!' + key);
    setOpenState(o);
  };
  return html`<div class=${'section' + (open ? ' open' : '')}>
    <div class="sh" onClick=${() => setOpen(!open)} onContextMenu=${help ? (e) => openMenu(e, [helpItem(title, tHelp(help))]) : undefined}><${Icon} name=${open ? 'chevron-down' : 'chevron-right'} size=${13} />${title}${extra}</div>
    ${open && html`<div class="sb">${children}</div>`}
  </div>`;
}

function Field({ f, v, set, ctx, isFirst, kf, path, dia }) {
  switch (f.type) {
    case 'num': case 'int':
      return html`<${Row} f=${f} dia=${dia} kf=${kf} path=${path}><${Num} f=${f} v=${v ?? f.def} set=${set} /></${Row}>`;
    case 'optnum':
      return html`<${Row} f=${f} dia=${dia} kf=${kf} path=${path}><${Num} f=${f} v=${v} set=${set} optional=${true} /></${Row}>`;
    case 'bool':
      return html`<${Row} f=${f} dia=${dia} kf=${kf} path=${path}><${Switch} checked=${v} onChange=${set} /></${Row}>`;
    case 'enum': {
      const numeric = typeof f.options[0][0] === 'number';
      const cur = v === undefined || v === null || (v === '' && f.def !== undefined) ? f.def : v;
      return html`<${Row} f=${f} dia=${dia} kf=${kf} path=${path}><${Select} value=${cur} options=${f.options} onChange=${(x) => set(numeric ? +x : x)} /></${Row}>`;
    }
    case 'color':
      return html`<${Row} f=${f} dia=${dia} kf=${kf} path=${path}><${Color} f=${f} v=${v} set=${set} /></${Row}>`;
    case 'gradient':
      return html`<${Row} f=${f} dia=${dia} kf=${kf} path=${path}><${Gradient} v=${v} set=${set} /></${Row}>`;
    case 'text':
      return html`<${Row} f=${f} dia=${dia} kf=${kf} path=${path}><${Edit} type="text" class="grow" value=${v ?? ''} onChange=${(e) => set(e.target.value)} /></${Row}>`;
    case 'textarea':
      return html`<${Row} f=${f} dia=${dia} kf=${kf} path=${path}><${Edit} tag="textarea" class="grow" rows="3" value=${v ?? ''} onChange=${(e) => set(e.target.value)} /></${Row}>`;
    case 'textpick': {
      // one of the project's fixed texts (its index)
      const opts = (ctx.textChoices || []).map(([i, name]) => [i, name]);
      if (!opts.length) return html`<${Row} f=${f} dia=${dia} kf=${kf} path=${path}><span class="faint" style="font-size:12px">${t('No fixed texts yet (Titles and texts)')}</span></${Row}>`;
      return html`<${Row} f=${f} dia=${dia} kf=${kf} path=${path}><${Select} value=${v ?? 0} options=${opts} translate=${false} onChange=${(x) => set(+x)} /></${Row}>`;
    }
    case 'zindex': {
      // Above all the layers (unset), above the background (0), or above a layer (its index + 1)
      const opts = [['', t('Above all the layers')], [0, t('Above the background, under the layers')],
        ...(ctx.layerChoices || []).map(([z, name]) => [z, t('Above “{n}”', { n: name })])];
      return html`<${Row} f=${f} dia=${dia} kf=${kf} path=${path}><${Select} value=${v ?? ''} options=${opts} translate=${false}
        onChange=${(x) => set(x === '' ? null : +x)} /></${Row}>`;
    }
    case 'file':
      return html`<${Row} f=${f} dia=${dia} kf=${kf} path=${path}><div class="filectl">
        <div class="fn" title=${v || ''}><${Icon} name=${FILE_ICON[f.kind] || 'file'} size=${14} cls="faint" />
          <span>${v ? basename(v) : html`<span class="faint">${t('None')}</span>`}</span></div>
        <button class="btn icon" title=${t('Choose file')} onClick=${() => ctx.pickFile(f.kind, set, v)}><${Icon} name="folder-open" size=${15} /></button>
        ${v && html`<button class="btn icon ghost danger" title=${t('Remove')} onClick=${() => set('')}><${Icon} name="x" size=${15} /></button>`}
      </div></${Row}>`;
    case 'group':
      return html`<${Section} id=${path + '|' + (f.id || '')} title=${t(f.label)} help=${f.help}><${Form} fields=${f.fields} value=${v || {}} onChange=${set} ctx=${ctx} isFirst=${isFirst} kf=${kf} prefix=${path} /></${Section}>`;
    case 'layers':
      return html`<${Section} id=${path} title=${t(f.label)} help=${f.help}><${Layers} f=${f} v=${v} set=${set} ctx=${ctx} kf=${kf} path=${path} /></${Section}>`;
  }
  return null;
}

export const FILE_ICON = { audio: 'file-audio', bg: 'image', image: 'image', lyrics: 'file-text', txt: 'file-text', font: 'type', project: 'file', dir: 'folder' };
