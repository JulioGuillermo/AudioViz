// The project's ComfyUI flows (internal/studio/comfy.go): where ComfyUI is (the user starts it),
// the flows the project keeps, importing them (the workflows saved in ComfyUI, its templates, a
// file), the editor of their roles (where the prompt, the images… go and where the result comes
// from), and the form a flow gets in the Images dialog.

import { html, useEffect, useState } from './vendor/preact-htm.js';
import { createStore, useStore } from './store.js';
import { Icon } from './icons.js';
import { t } from './i18n.js';
import { api } from './util.js';

// the project's flows (shared by the section, the dialogs and the Images dialog)
export const flowsStore = createStore([]);
// (fresh: checked against ComfyUI asked again, so a model installed since counts)
export const loadFlows = (fresh) => api('GET', '/api/comfy/flows' + (fresh ? '?fresh=1' : '')).then((r) => flowsStore.set(r.flows || [])).catch(() => flowsStore.set([]));

export const KINDS = [['image', 'Image (from a prompt)'], ['edit', 'Image edit (from images)'], ['depth', 'Depth map'], ['video', 'Video']];
const ROLE_LABEL = { prompt: 'Prompt', negative: 'Negative prompt', seed: 'Seed', width: 'Width', height: 'Height', image: 'Input image', param: 'Parameter', output: 'Result' };

// what keeps a flow from running in ComfyUI, in words
export function issueText(is) {
  switch (is.kind) {
    case 'node': return t('The node {c} is not installed in ComfyUI', { c: is.class });
    case 'model': return t('Missing model: {v}', { v: is.value });
    case 'value': return t('{n}: “{v}” is not one of the options of {i}', { n: is.title, v: is.value, i: is.input });
    case 'role': return t('“{r}” points to something the flow doesn’t have', { r: t(ROLE_LABEL[is.input] || is.input) });
    case 'output': return t('It has no result: choose the node it is taken from');
  }
  return is.kind;
}

export function Issues({ issues }) {
  if (!issues || !issues.length) return null;
  return html`<div class="cf-issues"><b>${t('It can’t run in your ComfyUI yet:')}</b>
    ${issues.map((is, i) => html`<p key=${i}><${Icon} name="triangle-alert" size=${12} /> <span>${issueText(is)}
      ${(is.kind === 'model' || is.kind === 'value') && html` <span class="faint">(${is.title} · ${is.input})</span>`}</span></p>`)}
    ${issues.some((is) => is.kind === 'model') && html`<p class="faint">${t('Put the missing models in ComfyUI’s models folder (or install them from its Manager) and check again.')}</p>`}
    ${issues.some((is) => is.kind === 'node') && html`<p class="faint">${t('Install the missing nodes in ComfyUI (its Manager finds them), restart it and import the flow again.')}</p>`}
  </div>`;
}

// a flow's state: ready, what it lacks, or not known (ComfyUI not answering)
function FlowState({ f }) {
  if (!f.issues) return html`<span class="cf-dot unk" title=${t('Not checked: ComfyUI is not answering')}><${Icon} name="circle-help" size=${13} /></span>`;
  if (!f.issues.length) return html`<span class="cf-dot ok" title=${t('Ready')}><${Icon} name="check" size=${13} /></span>`;
  return html`<span class="cf-dot bad" title=${f.issues.map(issueText).join('\n')}><${Icon} name="triangle-alert" size=${13} />${f.issues.length}</span>`;
}

// ---- the section in the Project panel
export function ComfySection({ comfy, onURL, Section, Edit, showErr }) {
  const flows = useStore(flowsStore);
  const [st, setSt] = useState(null);
  const [dlg, setDlg] = useState(null); // 'import' | {edit: id}
  const check = () => {
    setSt({ busy: true });
    api('GET', '/api/comfy/status').then((x) => { setSt(x); if (x.ok) loadFlows(true); }).catch((e) => setSt({ ok: false, error: e.message }));
  };
  useEffect(() => { loadFlows(); }, []);
  const del = async (f) => {
    if (!confirm(t('Remove the flow “{n}” from the project?', { n: f.name }))) return;
    try { await api('DELETE', '/api/comfy/flow?id=' + encodeURIComponent(f.id)); loadFlows(); } catch (e) { showErr(e); }
  };
  return html`<${Section} title=${t('ComfyUI')} id="comfy">
    <div class="fld"><label>${t('Address')}</label><div class="ctl">
      <${Edit} type="text" value=${(comfy && comfy.url) || ''} placeholder="http://127.0.0.1:8188" onChange=${(e) => onURL(e.target.value.trim())} />
      <button class="btn sm" onClick=${check}>${t('Test')}</button></div></div>
    ${st && (st.busy ? html`<p class="faint cf-st">…</p>` : st.ok
      ? html`<p class="cf-st st-ok"><${Icon} name="check" size=${13} /> ComfyUI ${(st.system && st.system.comfyui_version) || ''} · ${((st.devices || [])[0] || {}).name || ''}</p>`
      : html`<p class="cf-st st-err"><${Icon} name="triangle-alert" size=${13} /> ${t('Not answering (start ComfyUI there)')}: ${st.error}</p>`)}
    <div class="cf-flows">
      ${!flows.length && html`<p class="faint">${t('No flows yet: import your ComfyUI workflows or its templates.')}</p>`}
      ${flows.map((f) => html`<div class="cf-flow" key=${f.id}>
        <${FlowState} f=${f} /><span class="cf-kind">${t(Object.fromEntries(KINDS)[f.kind] || f.kind)}</span><b>${f.name}</b>
        <span class="spacer"></span>
        <button class="btn icon ghost sm" title=${t('Edit its roles')} onClick=${() => setDlg({ edit: f.id })}><${Icon} name="pencil" size=${13} /></button>
        <button class="btn icon ghost sm" title=${t('Remove')} onClick=${() => del(f)}><${Icon} name="trash-2" size=${13} /></button></div>`)}
    </div>
    <div class="row" style="gap:6px"><button class="btn sm" onClick=${() => setDlg('import')}><${Icon} name="download" size=${13} /> ${t('Import a flow…')}</button>
      ${flows.length > 0 && html`<button class="btn sm ghost" title=${t('Check the flows again against ComfyUI (after installing models or nodes)')}
        onClick=${() => loadFlows(true)}><${Icon} name="rotate-ccw" size=${13} /> ${t('Check again')}</button>`}</div>
    ${dlg === 'import' && html`<${ImportDialog} onClose=${() => setDlg(null)} onImported=${(f) => { loadFlows(); setDlg({ edit: f.id }); }} showErr=${showErr} />`}
    ${dlg && dlg.edit && html`<${FlowEditor} id=${dlg.edit} onClose=${() => { setDlg(null); loadFlows(); }} showErr=${showErr} />`}
  </${Section}>`;
}

// ---- importing: saved in ComfyUI, its templates, a file
function ImportDialog({ onClose, onImported, showErr }) {
  const [tab, setTab] = useState('workflows');
  const [list, setList] = useState(null);
  const [q, setQ] = useState('');
  const [busy, setBusy] = useState('');
  const [err, setErr] = useState('');
  useEffect(() => {
    setList(null); setErr('');
    if (tab === 'file') return;
    api('GET', '/api/comfy/browse?src=' + tab).then((r) => setList(tab === 'templates' ? r.templates : r.workflows)).catch((e) => setErr(e.message));
  }, [tab]);
  const imp = async (body, key) => {
    setBusy(key); setErr('');
    try { const f = await api('POST', '/api/comfy/import', body); onImported(f); } catch (e) { setErr(e.message); }
    setBusy('');
  };
  const qq = q.trim().toLowerCase();
  const match = (s) => !qq || (s || '').toLowerCase().includes(qq);
  let body;
  if (tab === 'file') {
    body = html`<div class="cf-file"><p class="faint">${t('A workflow saved from ComfyUI (as it saves them, or exported for the API).')}</p>
      <input type="file" accept=".json,application/json" onChange=${async (e) => {
        const f = e.target.files[0]; if (!f) return;
        try { await imp({ src: 'json', json: JSON.parse(await f.text()), title: f.name.replace(/\.json$/i, '') }, 'file'); } catch (x) { setErr(x.message); }
      }} /></div>`;
  } else if (!list) {
    body = err ? null : html`<p class="faint">${t('Asking ComfyUI…')}</p>`;
  } else if (tab === 'workflows') {
    body = html`<div class="cf-list">${list.filter(match).map((p) => html`<div class="cf-row" key=${p}>
      <${Icon} name="file" size=${14} cls="muted" /><span class="cf-path">${p.replace(/\.json$/i, '')}</span><span class="spacer"></span>
      <button class="btn sm" disabled=${!!busy} onClick=${() => imp({ src: 'workflow', path: p }, p)}>${busy === p ? '…' : t('Import')}</button></div>`)}
      ${!list.length && html`<p class="faint">${t('No workflows saved in ComfyUI.')}</p>`}</div>`;
  } else {
    const cats = [...new Set(list.map((x) => x.category))];
    body = html`<div class="cf-tpls">${cats.map((c) => {
      const ts = list.filter((x) => x.category === c && (match(x.title) || match(x.description) || (x.models || []).some(match)));
      if (!ts.length) return null;
      return html`<div class="cf-cat"><h4>${c}</h4><div class="cf-grid">${ts.map((x) => html`<div class="cf-tpl" key=${x.name}>
        <img src=${'/api/comfy/thumb?name=' + encodeURIComponent(x.name) + '&sub=' + encodeURIComponent(x.mediaSubtype || 'webp')} loading="lazy" alt="" />
        <b>${x.title}</b><span class="faint">${(x.models || []).join(', ')}</span>
        <button class="btn sm" disabled=${!!busy} onClick=${() => imp({ src: 'template', name: x.name, title: x.title }, x.name)}>${busy === x.name ? '…' : t('Import')}</button></div>`)}</div></div>`;
    })}</div>`;
  }
  return html`<div class="modal-bg" onMouseDown=${(e) => e.target === e.currentTarget && onClose()}>
    <div class="modal cf-dlg">
      <div class="modal-h"><${Icon} name="download" size=${16} cls="muted" />${t('Import a ComfyUI flow')}<span class="spacer"></span>
        <button class="btn icon ghost" onClick=${onClose}><${Icon} name="x" size=${16} /></button></div>
      <div class="cf-tabs">
        ${[['workflows', 'Saved in ComfyUI'], ['templates', 'ComfyUI templates'], ['file', 'A file']].map(([k, l]) => html`<button class=${'btn sm' + (tab === k ? ' on' : ' ghost')} onClick=${() => setTab(k)}>${t(l)}</button>`)}
        ${tab !== 'file' && html`<input type="search" placeholder=${t('Search…')} value=${q} onInput=${(e) => setQ(e.target.value)} />`}
      </div>
      ${err && html`<p class="st-err cf-err"><${Icon} name="triangle-alert" size=${13} /> ${err}</p>`}
      <div class="mb cf-body">${body}</div>
    </div></div>`;
}

// ---- a flow's roles
// The places of the flow AudioViz fills (and where it takes the result from), in sentences: each
// one a node of the flow and its input; only the nodes and inputs that fit are offered.
const SLOTS = [
  { kind: 'prompt', label: 'Your prompt goes to', want: 'text' },
  { kind: 'negative', label: 'The negative prompt goes to', want: 'text' },
  { kind: 'seed', label: 'The seed goes to', want: 'number' },
  { kind: 'width', label: 'The width goes to', want: 'number' },
  { kind: 'height', label: 'The height goes to', want: 'number' },
];
const fits = (want, v) => want === 'any' || (want === 'text' ? typeof v === 'string' : want === 'number' ? typeof v === 'number' : true);

function FlowEditor({ id, onClose, showErr }) {
  const [f, setF] = useState(null);
  const [roles, setRoles] = useState([]);
  const [open, setOpen] = useState(''); // the value shown whole
  useEffect(() => { api('GET', '/api/comfy/flow?id=' + encodeURIComponent(id)).then((x) => { setF(x); setRoles(x.roles || []); }).catch(showErr); }, [id]);
  if (!f) return null;
  const nodes = f.nodes || [];
  const nodeOf = (nid) => nodes.find((n) => n.id === nid);
  const inputsOf = (n, want) => (n ? Object.keys(n.inputs).filter((k) => fits(want, n.inputs[k])).sort() : []);
  const setRole = (i, patch) => setRoles(roles.map((r, j) => (j === i ? { ...r, ...patch } : r)));
  const del = (i) => setRoles(roles.filter((_, j) => j !== i));
  const add = (kind, want, label) => {
    const n = nodes.find((x) => inputsOf(x, want).length) || nodes[0] || { id: '', inputs: {} };
    const input = inputsOf(n, want)[0] || '';
    setRoles([...roles, { kind, node: n.id, input, label: label || '', value: n.inputs[input] }]);
  };
  const save = async () => {
    try { await api('PUT', '/api/comfy/flow', { id, name: f.name, kind: f.kind, roles }); onClose(); } catch (e) { showErr(e); }
  };
  const nodeLabel = (n) => n.title + (n.title !== n.class ? ' · ' + n.class : '') + '  #' + n.id;

  // one place: its node, its input, what it holds now
  const where = (r, i, want, tail) => {
    const n = nodeOf(r.node);
    const ns = nodes.filter((x) => x.id === r.node || (want === 'output' ? x.output : inputsOf(x, want).length));
    const val = n && r.input ? n.inputs[r.input] : undefined;
    const sv = val === undefined ? '' : String(val);
    const k = r.node + '|' + r.input;
    return html`<select class="cf-node" value=${r.node} onChange=${(e) => {
        const nn = nodeOf(e.target.value);
        const inp = want === 'output' ? '' : inputsOf(nn, want)[0] || '';
        setRole(i, { node: e.target.value, input: inp, value: nn && nn.inputs[inp] });
      }}>${ns.map((x) => html`<option value=${x.id}>${nodeLabel(x)}</option>`)}</select>
      ${want !== 'output' && html`<span class="faint">→</span><select class="cf-inp" value=${r.input || ''} onChange=${(e) => setRole(i, { input: e.target.value, value: n && n.inputs[e.target.value] })}>
        ${!r.input && html`<option value="">—</option>`}${inputsOf(n, want).concat(r.input && !inputsOf(n, want).includes(r.input) ? [r.input] : []).map((x) => html`<option value=${x}>${x}</option>`)}</select>`}
      ${tail}
      ${sv !== '' && html`<div class=${'cf-val' + (open === k ? ' open' : '')} title=${t('Click to see it whole')} onClick=${() => setOpen(open === k ? '' : k)}>
        ${t('Now')}: ${open === k || sv.length <= 90 ? sv : sv.slice(0, 90) + '…'}</div>`}`;
  };
  const slot = ({ kind, label, want }) => {
    const i = roles.findIndex((r) => r.kind === kind);
    return html`<div class="cf-slot" key=${kind}><div class="cf-sl">${t(label)}</div>
      ${i < 0 ? html`<div class="cf-sw"><span class="faint">${t('Not used')}</span>
          <button class="btn sm ghost" onClick=${() => add(kind, want)}><${Icon} name="plus" size=${12} /> ${t('Assign')}</button></div>`
        : html`<div class="cf-sw">${where(roles[i], i, want,
          html`<button class="btn icon ghost sm" title=${t('Not used')} onClick=${() => del(i)}><${Icon} name="x" size=${13} /></button>`)}</div>`}</div>`;
  };
  const many = (kind, want, title, hint, addLabel, namePh) => html`<div class="cf-group"><div class="cf-gh"><b>${t(title)}</b><span class="faint">${t(hint)}</span></div>
    ${roles.map((r, i) => r.kind === kind && html`<div class="cf-slot" key=${i}>
      <div class="cf-sl"><input type="text" placeholder=${t(namePh)} value=${r.label || ''} onInput=${(e) => setRole(i, { label: e.target.value })} /></div>
      <div class="cf-sw">${where(r, i, want, html`<button class="btn icon ghost sm" title=${t('Remove')} onClick=${() => del(i)}><${Icon} name="x" size=${13} /></button>`)}</div></div>`)}
    <button class="btn sm ghost" onClick=${() => add(kind, want)}><${Icon} name="plus" size=${12} /> ${t(addLabel)}</button></div>`;
  const oi = roles.findIndex((r) => r.kind === 'output');
  return html`<div class="modal-bg" onMouseDown=${(e) => e.target === e.currentTarget && onClose()}>
    <div class="modal cf-dlg">
      <div class="modal-h"><${Icon} name="pencil" size=${16} cls="muted" />${t('Flow')} · ${f.name}<span class="spacer"></span>
        <button class="btn icon ghost" onClick=${onClose}><${Icon} name="x" size=${16} /></button></div>
      <div class="mb cf-body">
        <div class="fld"><label>${t('Name')}</label><div class="ctl"><input type="text" value=${f.name} onInput=${(e) => setF({ ...f, name: e.target.value })} /></div></div>
        <div class="fld"><label>${t('What it makes')}</label><div class="ctl"><select value=${f.kind} onChange=${(e) => setF({ ...f, kind: e.target.value })}>
          ${KINDS.map(([k, l]) => html`<option value=${k}>${t(l)}</option>`)}</select></div></div>
        <${Issues} issues=${f.issues} />
        ${f.warnings && f.warnings.length > 0 && html`<div class="cf-warn">${f.warnings.map((w) => html`<p><${Icon} name="triangle-alert" size=${12} /> ${w}</p>`)}</div>`}
        <p class="faint cf-hint">${t('When you generate, AudioViz writes what you give it (in the Images dialog) into these places of the flow, runs it in ComfyUI and keeps the image of its result. They were guessed on import: check that they point where you expect. Each one is a node of the flow (as titled in ComfyUI, #its number) and one of its fields.')}</p>
        <div class="cf-group">${SLOTS.map(slot)}</div>
        ${many('image', 'any', 'Input images', 'The images you choose go to these image loaders, in order.', 'Input image', 'Its name')}
        ${many('param', 'any', 'More values in the form', 'Fields of the flow you want to change from AudioViz (steps, strength…).', 'Expose a value', 'Its name in the form')}
        <div class="cf-group"><div class="cf-slot"><div class="cf-sl"><b>${t('The result is taken from')}</b></div>
          ${oi < 0 ? html`<div class="cf-sw"><span class="st-err">${t('Nothing: choose the node that saves the image')}</span>
              <button class="btn sm ghost" onClick=${() => { const n = nodes.find((x) => x.output) || nodes[0]; if (n) setRoles([...roles, { kind: 'output', node: n.id }]); }}><${Icon} name="plus" size=${12} /> ${t('Assign')}</button></div>`
            : html`<div class="cf-sw">${where(roles[oi], oi, 'output')}</div>`}</div></div>
      </div>
      <div class="mf"><span class="spacer"></span>
        <button class="btn ghost" onClick=${onClose}>${t('Cancel')}</button>
        <button class="btn primary" onClick=${save}>${t('Save')}</button></div>
    </div></div>`;
}

// ---- a flow's form in the Images dialog: its prompt, negative, images, size, seed, params
export function FlowForm({ flow, values, setValues, images, pickImage, size }) {
  const has = (k) => (flow.roles || []).some((r) => r.kind === k);
  const imgRoles = (flow.roles || []).filter((r) => r.kind === 'image');
  const params = (flow.roles || []).filter((r) => r.kind === 'param');
  const set = (k, v) => setValues({ ...values, [k]: v });
  return html`<div class="cf-form">
    ${has('prompt') && html`<label class="ig-l">${t('Prompt')}</label>
      <textarea class="ig-prompt" rows="6" value=${values.prompt || ''} onInput=${(e) => set('prompt', e.target.value)}></textarea>`}
    ${has('negative') && html`<label class="ig-l">${t('Negative prompt')}</label>
      <textarea class="ig-neg" rows="2" value=${values.negative || ''} onInput=${(e) => set('negative', e.target.value)}></textarea>`}
    ${imgRoles.length > 0 && html`<label class="ig-l">${t('Input images')}</label><div class="ig-refs">
      ${imgRoles.map((r, i) => {
        const p = (images || [])[i];
        return html`<div class="cf-in" key=${i}><span class="faint">${r.label || t('Image {n}', { n: i + 1 })}</span>
          ${p ? html`<div class="ig-ref" title=${p}><img src=${'/api/thumb?s=320&path=' + encodeURIComponent(p)} alt="" />
            <button class="btn icon sm" onClick=${() => pickImage(i, '')}><${Icon} name="x" size=${12} /></button></div>`
            : html`<button class="ig-add" onClick=${() => pickImage(i, null)}><${Icon} name="plus" size=${18} /></button>`}</div>`;
      })}</div>`}
    ${has('width') && size}
    ${has('seed') && html`<div class="fld"><label>${t('Seed')}</label><div class="ctl"><input type="number" min="0" value=${values.seed ?? ''} placeholder=${t('Random')}
      onInput=${(e) => set('seed', e.target.value)} /></div></div>`}
    ${params.map((r) => {
      const k = r.node + '|' + r.input;
      const cur = (values.params || {})[k];
      const v = cur ?? r.value;
      const num = typeof r.value === 'number';
      const setP = (x) => setValues({ ...values, params: { ...(values.params || {}), [k]: x } });
      return html`<div class="fld" key=${k}><label>${r.label || r.input}</label><div class="ctl">
        ${typeof r.value === 'boolean' ? html`<input type="checkbox" checked=${!!v} onChange=${(e) => setP(e.target.checked)} />`
          : html`<input type=${num ? 'number' : 'text'} step="any" value=${v ?? ''} onInput=${(e) => setP(num ? +e.target.value : e.target.value)} />`}</div></div>`;
    })}
  </div>`;
}
