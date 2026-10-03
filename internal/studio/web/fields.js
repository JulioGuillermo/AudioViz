import { html, useState, Component } from './vendor/preact-htm.js';
import { basename, splitHex, joinHex, gradientCSS, clone } from './util.js';
import { Icon } from './icons.js';
import { t } from './i18n.js';

// Generic form built from the schema. value = config object; onChange(newObject)
export function Form({ fields, value, onChange, ctx, isFirst = true }) {
  const set = (key, v) => onChange({ ...value, [key]: v });
  return html`${fields.map((f) => {
    if (f.showIf && !f.showIf(value)) return null;
    if (f.firstOnly && !isFirst) return null;
    return html`<${Field} key=${f.key} f=${f} v=${value[f.key]} set=${(v) => set(f.key, v)} ctx=${ctx} isFirst=${isFirst} />`;
  })}`;
}

function Row({ f, children }) {
  return html`<div class="fld">
    <label title=${f.help ? t(f.help) : ''}>${t(f.label)}</label>
    <div class="ctl">${children}</div>
    ${f.help && html`<div class="help">${t(f.help)}</div>`}
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
    <input type="number" step=${step} value=${on ? val : ''} disabled=${!on} placeholder=${t('auto')}
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
    <input type="text" value=${empty ? '' : v} style="width:88px" class="mono" placeholder=${f.optional ? t('automatic') : ''}
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
        <input type="text" value=${g.color} onChange=${(e) => upd(i, 'color', e.target.value.trim())} />
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

function Layers({ f, v, set, ctx }) {
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
      ${open === i && html`<div class="lb"><${Form} fields=${f.fields} value=${l} onChange=${(lv) => upd(i, lv)} ctx=${ctx} /></div>`}
    </div>`)}
    <button class="btn sm" style="margin-top:4px" onClick=${() => { set([...layers, clone(layers[layers.length - 1] || {})]); setOpen(layers.length); }}>
      <${Icon} name="plus" size=${14} /> ${t('Layer')}</button>
  </div>`;
}

function Section({ title, children, defOpen = true }) {
  const [open, setOpen] = useState(defOpen);
  return html`<div class="section">
    <div class="sh" onClick=${() => setOpen(!open)}><${Icon} name=${open ? 'chevron-down' : 'chevron-right'} size=${13} />${title}</div>
    ${open && html`<div class="sb">${children}</div>`}
  </div>`;
}

function Field({ f, v, set, ctx, isFirst }) {
  switch (f.type) {
    case 'num': case 'int':
      return html`<${Row} f=${f}><${Num} f=${f} v=${v} set=${set} /></${Row}>`;
    case 'optnum':
      return html`<${Row} f=${f}><${Num} f=${f} v=${v} set=${set} optional=${true} /></${Row}>`;
    case 'bool':
      return html`<${Row} f=${f}><${Switch} checked=${v} onChange=${set} /></${Row}>`;
    case 'enum': {
      const numeric = typeof f.options[0][0] === 'number';
      const cur = v === undefined || v === null || (v === '' && f.def !== undefined) ? f.def : v;
      return html`<${Row} f=${f}><${Select} value=${cur} options=${f.options} onChange=${(x) => set(numeric ? +x : x)} /></${Row}>`;
    }
    case 'color':
      return html`<${Row} f=${f}><${Color} f=${f} v=${v} set=${set} /></${Row}>`;
    case 'gradient':
      return html`<${Row} f=${f}><${Gradient} v=${v} set=${set} /></${Row}>`;
    case 'text':
      return html`<${Row} f=${f}><input type="text" class="grow" value=${v ?? ''} onChange=${(e) => set(e.target.value)} /></${Row}>`;
    case 'textarea':
      return html`<${Row} f=${f}><textarea class="grow" rows="3" value=${v ?? ''} onChange=${(e) => set(e.target.value)}></textarea></${Row}>`;
    case 'file':
      return html`<${Row} f=${f}><div class="filectl">
        <div class="fn" title=${v || ''}><${Icon} name=${FILE_ICON[f.kind] || 'file'} size=${14} cls="faint" />
          <span>${v ? basename(v) : html`<span class="faint">${t('None')}</span>`}</span></div>
        <button class="btn icon" title=${t('Choose file')} onClick=${() => ctx.pickFile(f.kind, set, v)}><${Icon} name="folder-open" size=${15} /></button>
        ${v && html`<button class="btn icon ghost danger" title=${t('Remove')} onClick=${() => set('')}><${Icon} name="x" size=${15} /></button>`}
      </div></${Row}>`;
    case 'group':
      return html`<${Section} title=${t(f.label)}><${Form} fields=${f.fields} value=${v || {}} onChange=${set} ctx=${ctx} isFirst=${isFirst} /></${Section}>`;
    case 'layers':
      return html`<${Section} title=${t(f.label)}><${Layers} f=${f} v=${v} set=${set} ctx=${ctx} /></${Section}>`;
  }
  return null;
}

export const FILE_ICON = { audio: 'file-audio', bg: 'image', lyrics: 'file-text', txt: 'file-text', font: 'type', project: 'file', dir: 'folder' };
