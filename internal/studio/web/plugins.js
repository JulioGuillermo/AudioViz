// Plugins (internal/studio/plugins.go): installed from a .zip into AudioViz's own folder, listed,
// removed; and the engine image generation runs on (the built-in Vulkan, or a plugin's: Intel
// GPUs' SYCL).

import { html, useEffect, useState } from './vendor/preact-htm.js';
import { Icon } from './icons.js';
import { t } from './i18n.js';
import { api } from './util.js';

const ENGINES = [['auto', 'Automatic'], ['vulkan', 'Vulkan (built in)'], ['sycl', 'Intel GPU (SYCL plugin)']];

export function PluginsSection({ Section, showErr }) {
  const [st, setSt] = useState(null);
  const [busy, setBusy] = useState('');
  const [note, setNote] = useState('');
  const load = () => api('GET', '/api/plugins').then(setSt).catch(showErr);
  useEffect(() => { load(); }, []);
  const install = async (file) => {
    if (!file) return;
    setBusy('install'); setNote('');
    try {
      const fd = new FormData();
      fd.append('file', file);
      const r = await fetch('/api/plugins/install', { method: 'POST', body: fd });
      const d = await r.json();
      if (!r.ok) throw new Error(d.error || r.statusText);
      setNote(d.restart ? t('Installed: restart AudioViz to use the new version.') : t('Installed.'));
      await load();
    } catch (e) { showErr(e); }
    setBusy('');
  };
  const remove = async (p) => {
    if (!confirm(t('Remove the plugin “{n}”?', { n: p.name }))) return;
    try {
      const d = await api('DELETE', '/api/plugins?id=' + encodeURIComponent(p.id));
      setNote(d.restart ? t('It goes when AudioViz restarts (it is in use).') : '');
      await load();
    } catch (e) { showErr(e); }
  };
  const engine = async (e) => {
    setBusy('engine');
    try { setSt(await api('PUT', '/api/plugins/engine', { engine: e })); } catch (x) { showErr(x); await load(); }
    setBusy('');
  };
  return html`<${Section} title=${t('Plugins')} id="plugins" help=${'Extra engines and abilities, installed from a .zip into AudioViz’s own folder (they work for every project).'}>
    ${!st ? html`<p class="faint">…</p>` : html`
      <div class="fld"><label>${t('Image engine')}</label><div class="ctl">
        <select value=${st.engine} disabled=${busy === 'engine'} onChange=${(e) => engine(e.target.value)}>
          ${ENGINES.map(([k, l]) => html`<option value=${k}>${t(l)}</option>`)}</select></div></div>
      <p class="faint pl-dev">${t('Running on: {d}', { d: st.device })}</p>
      <div class="pl-list">
        ${!st.plugins.length && html`<p class="faint">${t(st.intel ? 'No plugins. On this Intel GPU, the SYCL plugin makes image generation about 1.7× faster.' : 'No plugins installed.')}</p>`}
        ${st.plugins.map((p) => html`<div class="pl-row" key=${p.id}>
          <div class="pl-name"><b>${p.name}</b><span class="faint">${p.version}</span></div>
          <div class="pl-line">
            ${p.loaded ? html`<span class="st-ok"><${Icon} name="check" size=${12} /> ${t('In use')}</span>`
              : p.error ? html`<span class="st-err" title=${p.error}><${Icon} name="triangle-alert" size=${12} /> ${p.error}</span>`
                : html`<span class="faint">${t('Not loaded (engine: Vulkan, or restart AudioViz)')}</span>`}
            <span class="spacer"></span>
            <button class="btn icon ghost sm" title=${t('Remove')} onClick=${() => remove(p)}><${Icon} name="trash-2" size=${13} /></button>
          </div></div>`)}
      </div>
      ${note && html`<p class="faint pl-note">${note}</p>`}
      <label class=${'btn sm' + (busy === 'install' ? ' disabled' : '')}>
        <${Icon} name=${busy === 'install' ? 'loader-circle' : 'upload'} size=${13} cls=${busy === 'install' ? 'spin' : ''} />
        ${t(busy === 'install' ? 'Installing…' : 'Install a plugin…')}
        <input type="file" accept=".zip,application/zip" hidden disabled=${!!busy} onChange=${(e) => { install(e.target.files[0]); e.target.value = ''; }} /></label>`}
  </${Section}>`;
}
