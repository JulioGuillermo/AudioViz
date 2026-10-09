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
export const loadFlows = () => api('GET', '/api/comfy/flows').then((r) => flowsStore.set(r.flows || [])).catch(() => flowsStore.set([]));

export const KINDS = [['image', 'Image (from a prompt)'], ['edit', 'Image edit (from images)'], ['depth', 'Depth map'], ['video', 'Video']];
const ROLE_KINDS = [['prompt', 'Prompt'], ['negative', 'Negative prompt'], ['seed', 'Seed'], ['width', 'Width'], ['height', 'Height'],
  ['image', 'Input image'], ['param', 'Parameter'], ['output', 'Result (output)']];

// ---- the section in the Project panel
export function ComfySection({ comfy, onURL, Section, Edit, showErr }) {
  const flows = useStore(flowsStore);
  const [st, setSt] = useState(null);
  const [dlg, setDlg] = useState(null); // 'import' | {edit: id}
  const check = () => { setSt({ busy: true }); api('GET', '/api/comfy/status').then(setSt).catch((e) => setSt({ ok: false, error: e.message })); };
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
        <span class="cf-kind">${t(Object.fromEntries(KINDS)[f.kind] || f.kind)}</span><b>${f.name}</b>
        ${f.warnings && f.warnings.length > 0 && html`<${Icon} name="triangle-alert" size=${12} cls="st-warn" title=${f.warnings.join('\n')} />`}
        <span class="spacer"></span>
        <button class="btn icon ghost sm" title=${t('Edit its roles')} onClick=${() => setDlg({ edit: f.id })}><${Icon} name="pencil" size=${13} /></button>
        <button class="btn icon ghost sm" title=${t('Remove')} onClick=${() => del(f)}><${Icon} name="trash-2" size=${13} /></button></div>`)}
    </div>
    <button class="btn sm" onClick=${() => setDlg('import')}><${Icon} name="download" size=${13} /> ${t('Import a flow…')}</button>
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
function FlowEditor({ id, onClose, showErr }) {
  const [f, setF] = useState(null);
  const [roles, setRoles] = useState([]);
  useEffect(() => { api('GET', '/api/comfy/flow?id=' + encodeURIComponent(id)).then((x) => { setF(x); setRoles(x.roles || []); }).catch(showErr); }, [id]);
  if (!f) return null;
  const nodes = f.nodes || [];
  const nodeOf = (nid) => nodes.find((n) => n.id === nid);
  const setRole = (i, patch) => setRoles(roles.map((r, j) => (j === i ? { ...r, ...patch } : r)));
  const save = async () => {
    try { await api('PUT', '/api/comfy/flow', { id, name: f.name, kind: f.kind, roles }); onClose(); } catch (e) { showErr(e); }
  };
  return html`<div class="modal-bg" onMouseDown=${(e) => e.target === e.currentTarget && onClose()}>
    <div class="modal cf-dlg">
      <div class="modal-h"><${Icon} name="pencil" size=${16} cls="muted" />${t('Flow')} · ${f.name}<span class="spacer"></span>
        <button class="btn icon ghost" onClick=${onClose}><${Icon} name="x" size=${16} /></button></div>
      <div class="mb cf-body">
        <div class="fld"><label>${t('Name')}</label><div class="ctl"><input type="text" value=${f.name} onInput=${(e) => setF({ ...f, name: e.target.value })} /></div></div>
        <div class="fld"><label>${t('What it makes')}</label><div class="ctl"><select value=${f.kind} onChange=${(e) => setF({ ...f, kind: e.target.value })}>
          ${KINDS.map(([k, l]) => html`<option value=${k}>${t(l)}</option>`)}</select></div></div>
        ${f.warnings && f.warnings.length > 0 && html`<div class="cf-warn">${f.warnings.map((w) => html`<p><${Icon} name="triangle-alert" size=${12} /> ${w}</p>`)}</div>`}
        <p class="faint cf-hint">${t('Where AudioViz puts what it is given and where it takes the result from. Proposed when importing: check them.')}</p>
        <div class="cf-roles">
          ${roles.map((r, i) => {
            const n = nodeOf(r.node);
            const inputs = n ? Object.keys(n.inputs) : [];
            return html`<div class="cf-role" key=${i}>
              <select value=${r.kind} onChange=${(e) => setRole(i, { kind: e.target.value })}>${ROLE_KINDS.map(([k, l]) => html`<option value=${k}>${t(l)}</option>`)}</select>
              <select value=${r.node} onChange=${(e) => setRole(i, { node: e.target.value, input: '' })}>
                ${nodes.map((x) => html`<option value=${x.id}>${x.title} (${x.id})</option>`)}</select>
              ${r.kind !== 'output' && html`<select value=${r.input || ''} onChange=${(e) => setRole(i, { input: e.target.value, value: n && n.inputs[e.target.value] })}>
                <option value="">—</option>${inputs.map((k) => html`<option value=${k}>${k}</option>`)}</select>`}
              ${(r.kind === 'param' || r.kind === 'image') && html`<input type="text" placeholder=${t('Its name')} value=${r.label || ''} onInput=${(e) => setRole(i, { label: e.target.value })} />`}
              <button class="btn icon ghost sm" title=${t('Remove')} onClick=${() => setRoles(roles.filter((_, j) => j !== i))}><${Icon} name="x" size=${13} /></button>
              ${n && r.input && n.inputs[r.input] !== undefined && html`<span class="faint cf-val">= ${String(n.inputs[r.input]).slice(0, 60)}</span>`}
            </div>`;
          })}
        </div>
        <div class="cf-add">
          <button class="btn sm ghost" onClick=${() => setRoles([...roles, { kind: 'param', node: (nodes[0] || {}).id || '', input: '', label: '' }])}><${Icon} name="plus" size=${13} /> ${t('Expose a value')}</button>
          <button class="btn sm ghost" onClick=${() => setRoles([...roles, { kind: 'image', node: (nodes[0] || {}).id || '', input: 'image', label: '' }])}><${Icon} name="plus" size=${13} /> ${t('Input image')}</button>
        </div>
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
