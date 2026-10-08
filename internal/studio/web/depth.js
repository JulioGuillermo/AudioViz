import { html, useEffect, useState } from './vendor/preact-htm.js';
import { Form, Section } from './fields.js';
import { Icon } from './icons.js';
import { t } from './i18n.js';
import { api } from './util.js';

// The depth map of a background segment (config.DepthConfig): where it comes from, working it
// out (a job: Depth Anything 3 on the GPU, the model downloaded the first time) and a look at it.

export const DEPTH_FIELDS = [
  { key: 'source', label: 'Depth from', type: 'enum', def: 'none', anim: false, options: [['none', 'None'],
    ['ai', 'AI (Depth Anything 3)'], ['brightness', 'Brightness (quick, without AI)'], ['file', 'A depth map of my own']] },
  { key: 'model', label: 'Model', type: 'enum', def: 'small', anim: false, showIf: (c) => c.source === 'ai',
    options: [['small', 'Small (fast, 105 MB)'], ['base', 'Base (more detail, 149 MB)']] },
  { key: 'detail', label: 'Detail', type: 'enum', def: 'high', anim: false, showIf: (c) => c.source === 'ai',
    options: [['normal', 'Normal (504 px, fastest)'], ['high', 'High (1008 px, ~1 s)'], ['max', 'Very high (1512 px, ~3 s)']],
    help: 'The size it works at: higher keeps finer shapes' },
  { key: 'path', label: 'Depth map', type: 'file', kind: 'bg', showIf: (c) => c.source === 'file', help: 'White = near, black = far' },
];

const viewURL = (image, d, v) => '/api/depth/view?' + new URLSearchParams({ image, source: d.source || '', model: d.model || 'small',
  detail: d.detail || 'high', file: d.path || '', v }).toString();

export function DepthSection({ cfg, onChange, ctx }) {
  const d = cfg.depth || {};
  const [ver, setVer] = useState(0);
  const [missing, setMissing] = useState(false);
  const job = ctx.job && ctx.job.kind === 'depth' ? ctx.job : null;
  const running = job && job.status === 'running';
  // a finished job: look at the map again
  const doneKey = job && job.status === 'done' ? job.id : '';
  useEffect(() => { if (doneKey) { setVer((x) => x + 1); setMissing(false); } }, [doneKey]);
  // another source or model: look again
  useEffect(() => { setMissing(false); }, [cfg.path, d.source, d.model, d.detail, d.path]);
  const on = d.source && d.source !== 'none';
  const make = () => api('POST', '/api/depth', { image: cfg.path, source: d.source, model: d.model || 'small', detail: d.detail || 'high', file: d.path || '' }).catch(ctx.showErr);
  return html`<${Section} id="depth" title=${t('Depth')}>
    <p class="faint" style="margin:2px 0 8px;font-size:12px">${t('How near each point of the image is: the light of the emitters follows it, and things can go behind what is nearer.')}</p>
    <${Form} fields=${DEPTH_FIELDS} value=${d} ctx=${ctx} onChange=${(nd) => onChange({ ...cfg, depth: nd })} />
    ${on && html`<div class="depth-box">
      ${!missing && cfg.path ? html`<img class="depth-img" src=${viewURL(cfg.path, d, ver)} onError=${() => setMissing(true)} onLoad=${() => setMissing(false)} />`
        : html`<div class="depth-none faint">${t(cfg.path ? 'Not worked out yet' : 'The segment has no image')}</div>`}
      <div class="row" style="margin-top:6px">
        ${missing || running ? html`<button class="btn sm primary" disabled=${running || !cfg.path} onClick=${make}>
          <${Icon} name=${running ? 'loader-circle' : 'wand-sparkles'} size=${13} cls=${running ? 'spin' : ''} /> ${t('Work out the depth')}</button>`
          : html`<span class="faint" style="font-size:12px"><${Icon} name="check" size=${12} /> ${t('Ready (kept in the project\'s cache)')}</span>`}
        ${running && html`<span class="faint" style="font-size:12px">${job.message} · ${Math.round((job.progress || 0) * 100)} %</span>`}
        ${job && job.status === 'error' && html`<span class="danger" style="font-size:12px">${job.message}</span>`}
      </div>
    </div>`}
  </${Section}>`;
}
