// Downloaded models (internal/studio/models.go): every file in AudioViz's models folder, its size
// and what uses it, to free disk space; a deleted model downloads again when a feature needs it.

import { html, useEffect, useState } from './vendor/preact-htm.js';
import { Icon } from './icons.js';
import { t } from './i18n.js';
import { api } from './util.js';
import { fmtSize } from './dialogs.js';

// "Images · Z-Image Turbo": the whole text translated, else its first part
const useLabel = (u) => {
  const whole = t(u);
  if (whole !== u) return whole;
  const i = u.indexOf(' · ');
  return i > 0 ? t(u.slice(0, i)) + u.slice(i) : u;
};

export function ModelsSection({ Section, showErr }) {
  const [st, setSt] = useState(null);
  const [busy, setBusy] = useState('');
  const load = () => api('GET', '/api/models').then(setSt).catch(showErr);
  useEffect(() => { load(); }, []);
  const remove = async (f) => {
    const what = f.label || f.file;
    const msg = f.known
      ? t('Delete “{n}” ({s})? It downloads again the next time it is needed.', { n: what, s: fmtSize(f.size) })
      : t('Delete “{n}” ({s})? No feature of AudioViz uses it.', { n: what, s: fmtSize(f.size) });
    if (!confirm(msg)) return;
    setBusy(f.file);
    try { setSt(await api('DELETE', '/api/models?file=' + encodeURIComponent(f.file))); } catch (e) { showErr(e); }
    setBusy('');
  };
  return html`<${Section} title=${t('Downloaded models')} id="models"
      help=${'The AI models AudioViz has downloaded (images, lyrics, depth…), shared by every project. Delete the ones you do not use to free disk space: they download again when you use them.'}>
    ${!st ? html`<p class="faint">…</p>` : html`
      <p class="faint md-total">${t('{s} in {n} files', { s: fmtSize(st.total), n: (st.files || []).length })}
        <button class="btn icon ghost sm" title=${t('Refresh')} onClick=${load}><${Icon} name="rotate-ccw" size=${12} /></button></p>
      <div class="pl-list">
        ${!(st.files || []).length && html`<p class="faint">${t('No models downloaded yet.')}</p>`}
        ${(st.files || []).map((f) => html`<div class="pl-row" key=${f.file}>
          <div class="pl-name"><b title=${f.file}>${f.label || f.file}</b><span class="faint">${fmtSize(f.size)}</span></div>
          <div class="pl-line">
            <span class="faint md-use">${f.partial ? t('Unfinished download')
              : f.known ? f.usedBy.map(useLabel).join(' · ') : t('Not used by AudioViz')}</span>
            <span class="spacer"></span>
            <button class="btn icon ghost sm" title=${t('Delete')} disabled=${busy === f.file} onClick=${() => remove(f)}>
              <${Icon} name="trash-2" size=${13} /></button>
          </div></div>`)}
      </div>`}
  </${Section}>`;
}
