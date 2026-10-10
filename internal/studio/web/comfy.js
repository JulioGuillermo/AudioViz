// The project's ComfyUI flows (internal/studio/comfy.go): where ComfyUI is (the user starts it),
// the flows the project keeps, importing them (the workflows saved in ComfyUI, its templates, a
// file), the editor of their roles (where the prompt, the images… go and where the result comes
// from), and the form a flow gets in the Images dialog.

import { html, useEffect, useState } from './vendor/preact-htm.js';
import { Modal, fmtDur } from './dialogs.js';
import { createStore, useStore } from './store.js';
import { Icon } from './icons.js';
import { t } from './i18n.js';
import { api, clientId } from './util.js';

// the project's flows (shared by the section, the dialogs and the Images dialog)
export const flowsStore = createStore([]);
// (fresh: checked against ComfyUI asked again, so a model installed since counts)
export const loadFlows = (fresh) => api('GET', '/api/comfy/flows' + (fresh ? '?fresh=1' : '')).then((r) => flowsStore.set(r.flows || [])).catch(() => flowsStore.set([]));

export const KINDS = [['image', 'Image (from a prompt)'], ['edit', 'Image edit (from images)'], ['depth', 'Depth map'], ['video', 'Video'],
  ['audio', 'Audio (music, voice…)']];
const MEDIA = [['image', 'Image'], ['video', 'Video'], ['audio', 'Audio']];
// the file picker's kind for an input of a flow
const PICK_KIND = { image: 'image', video: 'video', audio: 'audio' };
// an input file's name in the form (its own, or Image 1, Video 1…)
const fileLabel = (roles, r) => {
  if (r.label) return r.label;
  const same = roles.filter((x) => x.kind === 'image' && (x.media || 'image') === (r.media || 'image'));
  const n = same.indexOf(r) + 1;
  return t({ video: 'Video {n}', audio: 'Audio {n}' }[r.media] || 'Image {n}', { n });
};
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
export function ComfySection({ comfy, onURL, onRun, Section, Edit, showErr }) {
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
        <b class="cf-name" title=${f.name}>${f.name}</b>
        <div class="cf-line"><${FlowState} f=${f} /><span class="cf-kind">${t(Object.fromEntries(KINDS)[f.kind] || f.kind)}</span>
        <span class="spacer"></span>
        ${f.kind === 'depth' ? html`<span class="cf-dot unk" title=${t('Used from a background segment: its Depth section, “Depth from: ComfyUI”')}><${Icon} name="info" size=${13} /></span>`
          : html`<button class="btn icon ghost sm" title=${t('Run it…')} onClick=${() => onRun(f.id)}><${Icon} name="play" size=${13} /></button>`}
        <button class="btn icon ghost sm" title=${t('Edit its roles')} onClick=${() => setDlg({ edit: f.id })}><${Icon} name="pencil" size=${13} /></button>
        <button class="btn icon ghost sm" title=${t('Remove')} onClick=${() => del(f)}><${Icon} name="trash-2" size=${13} /></button></div></div>`)}
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
  const where = (r, i, want, tail, head) => {
    const n = nodeOf(r.node);
    const ns = nodes.filter((x) => x.id === r.node || (want === 'output' ? x.output : inputsOf(x, want).length));
    const val = n && r.input ? n.inputs[r.input] : undefined;
    const sv = val === undefined ? '' : String(val);
    const k = r.node + '|' + r.input;
    return html`${head}<select class="cf-node" value=${r.node} onChange=${(e) => {
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
      <div class="cf-sw">${where(r, i, want, html`<button class="btn icon ghost sm" title=${t('Remove')} onClick=${() => del(i)}><${Icon} name="x" size=${13} /></button>`,
        kind === 'image' && html`<select class="cf-media" title=${t('What file it takes')} value=${r.media || 'image'} onChange=${(e) => setRole(i, { media: e.target.value })}>
          ${MEDIA.map(([k, l]) => html`<option value=${k}>${t(l)}</option>`)}</select>`)}</div></div>`)}
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
        ${many('image', 'any', 'Input files', 'The images, videos or audio you choose go to these loaders, in order.', 'Input file', 'Its name')}
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
  const anyFiles = imgRoles.some((r) => r.media && r.media !== 'image');
  const params = (flow.roles || []).filter((r) => r.kind === 'param');
  const set = (k, v) => setValues((o) => ({ ...o, [k]: v }));
  const roleText = (k) => { const r = (flow.roles || []).find((x) => x.kind === k); return r && typeof r.value === 'string' ? r.value : ''; };
  return html`<div class="cf-form">
    ${has('prompt') && html`<label class="ig-l">${t('Prompt')}</label>
      <textarea class="ig-prompt" rows="6" value=${values.prompt || ''} placeholder=${roleText('prompt').slice(0, 400)}
        title=${t('Empty: the flow’s own')} onInput=${(e) => set('prompt', e.target.value)}></textarea>`}
    ${has('negative') && html`<label class="ig-l">${t('Negative prompt')}</label>
      <textarea class="ig-neg" rows="2" value=${values.negative ?? ''} placeholder=${roleText('negative').slice(0, 300)}
        title=${t('Empty: the flow’s own')} onInput=${(e) => set('negative', e.target.value)}></textarea>`}
    ${imgRoles.length > 0 && html`<label class="ig-l">${t(anyFiles ? 'Input files' : 'Input images')}</label><div class="ig-refs">
      ${imgRoles.map((r, i) => {
        const p = (images || [])[i];
        const media = r.media || 'image';
        return html`<div class="cf-in" key=${i}><span class="faint">${fileLabel(flow.roles, r)}</span>
          ${p ? html`<div class="ig-ref" title=${p}>${media === 'audio' ? html`<div class="cf-file-in"><${Icon} name="music" size=${18} /><span>${p.split('/').pop()}</span></div>`
              : html`<img src=${'/api/thumb?s=320&path=' + encodeURIComponent(p)} alt="" />`}
            <button class="btn icon sm" onClick=${() => pickImage(i, '', media)}><${Icon} name="x" size=${12} /></button></div>`
            : html`<button class="ig-add" title=${t({ video: 'Choose a video', audio: 'Choose an audio file' }[media] || 'Choose an image')} onClick=${() => pickImage(i, null, media)}>
              <${Icon} name=${{ video: 'film', audio: 'music' }[media] || 'plus'} size=${18} /></button>`}</div>`;
      })}</div>`}
    ${has('width') && size}
    ${has('seed') && html`<div class="fld"><label>${t('Seed')}</label><div class="ctl"><input type="number" min="0" value=${values.seed ?? ''} placeholder=${t('Random')}
      onInput=${(e) => set('seed', e.target.value)} /></div></div>`}
    ${params.map((r) => {
      const k = r.node + '|' + r.input;
      const cur = (values.params || {})[k];
      const v = cur ?? r.value;
      const num = typeof r.value === 'number';
      const setP = (x) => setValues((o) => ({ ...o, params: { ...(o.params || {}), [k]: x } }));
      const long = typeof r.value === 'string' && (r.input === 'lyrics' || r.value.length > 60 || r.value.includes('\n'));
      if (long) {
        return html`<div key=${k}><label class="ig-l">${t(r.label || r.input)}</label>
          <textarea class="ig-neg" rows="5" value=${v ?? ''} onInput=${(e) => setP(e.target.value)}></textarea></div>`;
      }
      return html`<div class="fld" key=${k}><label>${t(r.label || r.input)}</label><div class="ctl">
        ${typeof r.value === 'boolean' ? html`<input type="checkbox" checked=${!!v} onChange=${(e) => setP(e.target.checked)} />`
          : html`<input type=${num ? 'number' : 'text'} step="any" value=${v ?? ''} onInput=${(e) => setP(num ? +e.target.value : e.target.value)} />`}</div></div>`;
    })}
  </div>`;
}

// ---- running a flow (any kind but depth): its form, its progress, its result (an image, a
// video or audio kept in the project), and what to do with it
const extOf = (p) => (p.match(/\.([a-z0-9]+)$/i) || [])[1]?.toLowerCase() || '';
const mediaOf = (p) => (['mp4', 'webm', 'mov', 'mkv', 'avi', 'm4v'].includes(extOf(p)) ? 'video'
  : ['flac', 'wav', 'mp3', 'ogg', 'opus', 'm4a'].includes(extOf(p)) ? 'audio' : 'image'); // (a GIF shows as an image)
const mediaURL = (p) => '/api/media?path=' + encodeURIComponent(p);

export function ComfyRunDialog({ id, job, onClose, onCancel, onRun, onUseBackground, onUseAudio, pickFile }) {
  const flows = useStore(flowsStore);
  const flow = flows.find((f) => f.id === id);
  const [vals, setVals] = useState({});
  const [files, setFiles] = useState([]);
  const [err, setErr] = useState('');
  useEffect(() => { loadFlows(true); }, []);
  if (!flow) return html`<${Modal} title=${t('ComfyUI')} icon="play" onClose=${onClose}><p class="faint">${t('Asking ComfyUI…')}</p></${Modal}>`;
  const mine = job && job.kind === 'comfy' && job.ref === id ? job : null;
  const running = mine && mine.status === 'running';
  const roleVal = (k) => (flow.roles.find((r) => r.kind === k) || {}).value;
  const has = (k) => flow.roles.some((r) => r.kind === k);
  const blocked = !!(flow.issues && flow.issues.length);
  const setFile = (i, p) => { const a = [...files]; a[i] = p; setFiles(a); };
  const pick = (i, p, media) => {
    if (p !== null) { setFile(i, p); return; }
    pickFile(PICK_KIND[media] || 'image', (x) => setFile(i, x));
  };
  const run = () => {
    setErr('');
    // (an empty prompt: the flow's own)
    const v = { params: vals.params || {} };
    if ((vals.prompt || '').trim()) v.prompt = vals.prompt;
    if ((vals.negative || '').trim()) v.negative = vals.negative;
    if (has('width')) { v.width = +(vals.width ?? roleVal('width')) || 0; v.height = +(vals.height ?? roleVal('height')) || 0; }
    if (vals.seed !== undefined && vals.seed !== '') v.seed = +vals.seed;
    onRun({ id, values: v, images: files }).catch((e) => setErr(e.message));
  };
  const size = has('width') && html`<div class="fld"><label>${t('Size')}</label><div class="ctl cf-size">
    <input type="number" min="64" step="8" title=${t('Width')} value=${vals.width ?? roleVal('width') ?? ''} onInput=${(e) => { const x = e.target.value; setVals((o) => ({ ...o, width: x })); }} />
    <span class="faint">×</span>
    <input type="number" min="64" step="8" title=${t('Height')} value=${vals.height ?? roleVal('height') ?? ''} onInput=${(e) => { const x = e.target.value; setVals((o) => ({ ...o, height: x })); }} /></div></div>`;
  const out = mine && mine.status === 'done' && mine.output;
  const m = out && mediaOf(out);
  let side;
  if (mine && mine.status !== 'done') {
    side = html`<div class="ig-stage">${mine.preview ? html`<img class="pv" src=${mine.preview} alt="" />`
      : html`<div class="faint ig-wait"><${Icon} name=${running ? 'loader-circle' : 'triangle-alert'} size=${28} cls=${running ? 'spin' : ''} /></div>`}</div>
      <div class="gen"><div class="gen-row"><span class="gen-l">${t('Total')}</span>
        <div class="progress"><i style=${{ width: ((mine.progress || 0) * 100).toFixed(1) + '%' }}></i></div>
        <span class="gen-v mono">${Math.round((mine.progress || 0) * 100)}%</span><span class="gen-t mono">${fmtDur(mine.elapsed || 0)}</span></div>
        <div class=${mine.status === 'error' ? 'st-err' : 'faint'} style="font-size:12px;margin-top:6px">${mine.message}</div></div>`;
  } else if (out) {
    side = html`<div class="ig-stage">${m === 'video' ? html`<video src=${mediaURL(out)} controls autoplay loop muted playsinline></video>`
        : m === 'audio' ? html`<div class="cf-audio"><${Icon} name="music" size=${40} cls="muted" /><audio src=${mediaURL(out)} controls></audio></div>`
          : html`<img src=${mediaURL(out)} alt="" />`}</div>
      <p class="faint" style="font-size:12px">${mine.message} · <span class="mono">${out}</span></p>
      <div class="row" style="gap:6px">
        ${m !== 'audio' && html`<button class="btn sm" onClick=${() => onUseBackground(out)}><${Icon} name="image" size=${13} /> ${t('Use as background')}</button>`}
        ${m === 'audio' && html`<button class="btn sm" onClick=${() => onUseAudio(out)}><${Icon} name="music" size=${13} /> ${t('Use as the project’s audio')}</button>`}
        <a class="btn sm ghost" href=${mediaURL(out) + '&download=1'}><${Icon} name="download" size=${13} /> ${t('Download')}</a></div>`;
  } else {
    side = html`<div class="ig-stage"><div class="faint ig-wait"><${Icon} name="play" size=${28} />
      <p style="max-width:340px;text-align:center">${t('It runs in ComfyUI and what it makes is kept in the project.')}</p></div></div>`;
  }
  return html`<${Modal} title=${'ComfyUI · ' + flow.name} icon="play" onClose=${onClose} width="min(1000px,96vw)"
    footer=${html`<span class="spacer"></span>
      <button class="btn" onClick=${onClose}>${t(running ? 'Keep running in the background' : 'Close')}</button>
      ${running ? html`<button class="btn danger" onClick=${onCancel}><${Icon} name="x" size=${15} /> ${t('Cancel')}</button>`
        : html`<button class="btn primary" disabled=${blocked || (job && job.status === 'running')} onClick=${run}><${Icon} name="play" size=${15} /> ${t('Run')}</button>`}`}>
    ${err && html`<div class="alert err" style="margin-bottom:10px"><${Icon} name="triangle-alert" size=${15} />${err}</div>`}
    <div class="ig"><div class="ig-form">
      <p class="faint ig-desc">${t(Object.fromEntries(KINDS)[flow.kind] || flow.kind)}</p>
      ${running && mine.config ? html`<${JobConfig} job=${mine} />` : html`
        <${Issues} issues=${flow.issues} />
        <${FlowForm} flow=${flow} values=${vals} setValues=${setVals} images=${files} size=${size} pickImage=${pick} />`}
    </div><div class="ig-side">${!running && mine && mine.config && html`<${JobConfig} job=${mine} onUse=${() => {
        const v = mine.config.values || {};
        setVals({ prompt: v.prompt, negative: v.negative, seed: v.seed, width: v.width, height: v.height, params: v.params });
        setFiles(mine.config.images || []);
      }} />`}${side}</div></div>
  </${Modal}>`;
}

// ---- what a job was started with, and by whom (a person, an agent): shown in its dialog
const base = (p) => String(p || '').split('/').pop();
export function JobConfig({ job, onUse }) {
  const c = job && job.config;
  if (!c) return null;
  const who = job.byId && job.byId === clientId() ? t('you') : job.by || '';
  const rows = [];
  const add = (k, v) => { if (v !== undefined && v !== null && v !== '') rows.push([k, v]); };
  if (job.kind === 'comfy') {
    const v = c.values || {};
    add('Flow', c.name);
    add('Prompt', v.prompt);
    add('Negative prompt', v.negative);
    add('Size', v.width && v.height ? `${v.width} × ${v.height}` : '');
    add('Seed', v.seed ?? t('Random'));
    add('Input files', (c.images || []).filter(Boolean).map(base).join(', '));
    Object.entries(v.params || {}).forEach(([k, x]) => add(k.split('|').pop(), String(x)));
  } else {
    add('Model', c.model);
    add('Prompt', c.prompt);
    add('Negative prompt', c.negative);
    add('Size', c.width && c.height ? `${c.width} × ${c.height}` : '');
    add('Steps', c.steps || '');
    add('Seed', c.seed >= 0 ? c.seed : t('Random'));
    add('CFG', c.cfg || '');
    add('Reference images', (c.refs || []).map(base).join(', '));
    add('Initial image', c.init ? `${base(c.init)} · ${Math.round((c.strength || 0) * 100)} %` : '');
  }
  return html`<div class="jobcfg">
    <div class="jobcfg-h"><${Icon} name="info" size=${13} /><span>${who ? t('Started by {who}', { who }) : t('Started with')}</span><span class="spacer"></span>
      ${onUse && html`<button class="btn sm ghost" title=${t('Copy it to the form, to run it again or vary it')} onClick=${onUse}><${Icon} name="copy" size=${12} /> ${t('Use this configuration')}</button>`}</div>
    <dl>${rows.map(([k, v]) => html`<dt>${t(k)}</dt><dd>${v}</dd>`)}</dl></div>`;
}
