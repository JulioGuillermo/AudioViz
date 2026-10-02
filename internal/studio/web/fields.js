import { html, useState } from './vendor/preact-htm.js';
import { basename, splitHex, joinHex, gradientCSS, clone } from './util.js';

// Formulario genérico a partir del esquema. value = objeto de config; onChange(nuevoObjeto)
export function Form({ fields, value, onChange, ctx, isFirst = true }) {
  const set = (key, v) => onChange({ ...value, [key]: v });
  return html`${fields.map((f) => {
    if (f.showIf && !f.showIf(value)) return null;
    if (f.firstOnly && !isFirst) return null;
    return html`<${Field} key=${f.key} f=${f} v=${value[f.key]} set=${(v) => set(f.key, v)} ctx=${ctx} parent=${value} isFirst=${isFirst} />`;
  })}`;
}

function Row({ f, children }) {
  return html`<div class="fld">
    <label title=${f.help || ''}>${f.label}</label>
    <div class="ctl">${children}</div>
    ${f.help && html`<div class="help">${f.help}</div>`}
  </div>`;
}

function Num({ f, v, set, optional }) {
  const step = f.step || (f.type === 'int' ? 1 : 0.01);
  const val = v ?? f.min ?? 0;
  const conv = (x) => (f.type === 'int' ? Math.round(+x) : +x);
  const on = !optional || (v !== null && v !== undefined);
  return html`
    ${optional && html`<input type="checkbox" title="Usar valor propio" checked=${on}
        onChange=${(e) => set(e.target.checked ? (f.min ?? 0) : null)} />`}
    <input type="range" min=${f.min ?? 0} max=${f.max ?? 1} step=${step} value=${val} disabled=${!on}
      onInput=${(e) => set(conv(e.target.value))} />
    <input type="number" step=${step} value=${on ? val : ''} disabled=${!on} placeholder="auto"
      onChange=${(e) => e.target.value !== '' && set(conv(e.target.value))} />`;
}

function Color({ f, v, set }) {
  const empty = v === '' || v === undefined || v === null;
  const { rgb, a } = splitHex(empty ? '#ffffff' : v);
  return html`
    ${f.optional && html`<input type="checkbox" title="Usar color propio" checked=${!empty}
        onChange=${(e) => set(e.target.checked ? '#ffffff' : '')} />`}
    <input type="color" value=${rgb} disabled=${empty && f.optional} onInput=${(e) => set(joinHex(e.target.value, a))} />
    <input type="range" min="0" max="255" step="1" value=${a} title="Opacidad" disabled=${empty && f.optional}
      style="max-width:70px" onInput=${(e) => set(joinHex(rgb, +e.target.value))} />
    <input type="text" value=${empty ? '' : v} style="width:96px" class="mono" placeholder=${f.optional ? 'ninguno' : ''}
      onChange=${(e) => set(e.target.value.trim())} />`;
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
        <input type="range" min="0" max="1" step="0.01" value=${g.offset} onInput=${(e) => upd(i, 'offset', +e.target.value)} />
        <span class="mono muted" style="width:34px">${Math.round(g.offset * 100)}%</span>
        <button class="icon ghost danger" title="Quitar" onClick=${() => set(stops.filter((_, j) => j !== i))}>✕</button>
      </div>`;
    })}
    <div class="row">
      <button onClick=${() => {
        const last = stops[stops.length - 1];
        set([...stops, { offset: last ? Math.min(1, last.offset + 0.25) : 0, color: last ? last.color : '#ffffff' }]);
      }}>＋ Color</button>
      ${stops.length > 1 && html`<button class="ghost" title="Invertir" onClick=${() => set(stops.map((g) => ({ ...g, offset: 1 - g.offset })).reverse())}>⇄</button>`}
    </div>
  </div>`;
}

function Layers({ f, v, set, ctx }) {
  const layers = v || [];
  const [open, setOpen] = useState(0);
  const upd = (i, lv) => { const l = clone(layers); l[i] = lv; set(l); };
  return html`<div style="width:100%">
    ${layers.map((l, i) => html`<div class="group" key=${i}>
      <div class="gh" onClick=${() => setOpen(open === i ? -1 : i)}>
        <span style=${{ width: 10, height: 10, borderRadius: 5, background: l.color || '#fff', display: 'inline-block' }}></span>
        Capa ${i + 1} · <span class="muted">${l.type} · ${l.react_to || 'bass'}</span>
        <span class="spacer"></span>
        <button class="icon ghost" title="Duplicar" onClick=${(e) => { e.stopPropagation(); const n = clone(layers); n.splice(i + 1, 0, clone(l)); set(n); }}>⧉</button>
        <button class="icon ghost danger" title="Eliminar" disabled=${layers.length < 2} onClick=${(e) => { e.stopPropagation(); set(layers.filter((_, j) => j !== i)); }}>✕</button>
      </div>
      ${open === i && html`<div class="gb"><${Form} fields=${f.fields} value=${l} onChange=${(lv) => upd(i, lv)} ctx=${ctx} /></div>`}
    </div>`)}
    <button onClick=${() => { set([...layers, clone(layers[layers.length - 1] || {})]); setOpen(layers.length); }}>＋ Capa</button>
  </div>`;
}

function Field({ f, v, set, ctx, isFirst }) {
  switch (f.type) {
    case 'num': case 'int':
      return html`<${Row} f=${f}><${Num} f=${f} v=${v} set=${set} /></${Row}>`;
    case 'optnum':
      return html`<${Row} f=${f}><${Num} f=${f} v=${v} set=${set} optional=${true} /></${Row}>`;
    case 'bool':
      return html`<${Row} f=${f}><input type="checkbox" checked=${!!v} onChange=${(e) => set(e.target.checked)} /></${Row}>`;
    case 'enum': {
      const numeric = typeof f.options[0][0] === 'number';
      return html`<${Row} f=${f}><select value=${v ?? ''} onChange=${(e) => set(numeric ? +e.target.value : e.target.value)}>
        ${f.options.map(([val, lbl]) => html`<option value=${val}>${lbl}</option>`)}
      </select></${Row}>`;
    }
    case 'color':
      return html`<${Row} f=${f}><${Color} f=${f} v=${v} set=${set} /></${Row}>`;
    case 'gradient':
      return html`<${Row} f=${f}><${Gradient} v=${v} set=${set} /></${Row}>`;
    case 'text':
      return html`<${Row} f=${f}><input type="text" class="grow" value=${v ?? ''} onChange=${(e) => set(e.target.value)} /></${Row}>`;
    case 'file':
      return html`<${Row} f=${f}><div class="filectl">
        <div class="fn" title=${v || ''}>${v ? basename(v) : html`<span class="muted">ninguno</span>`}</div>
        <button onClick=${() => ctx.pickFile(f.kind, set, v)}>Elegir…</button>
        ${v && html`<button class="icon ghost danger" title="Quitar" onClick=${() => set('')}>✕</button>`}
      </div></${Row}>`;
    case 'group':
      return html`<div class="group">
        <div class="gh">${f.label}</div>
        <div class="gb"><${Form} fields=${f.fields} value=${v || {}} onChange=${set} ctx=${ctx} isFirst=${isFirst} /></div>
      </div>`;
    case 'layers':
      return html`<${Layers} f=${f} v=${v} set=${set} ctx=${ctx} />`;
  }
  return null;
}
