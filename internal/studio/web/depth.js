import { html, useEffect, useState } from './vendor/preact-htm.js';
import { Form, Section } from './fields.js';
import { Icon } from './icons.js';
import { t } from './i18n.js';
import { api } from './util.js';
import { useStore } from './store.js';
import { flowsStore, loadFlows } from './comfy.js';
import { DepthEditor } from './depthedit.js';

// The depth map of a background segment (config.DepthConfig): where it comes from, working it
// out (a job: Depth Anything 3 on the GPU, the model downloaded the first time) and a look at it.

export const DEPTH_FIELDS = [
  { key: 'source', label: 'Depth from', type: 'enum', def: 'none', anim: false, options: [['none', 'None'],
    ['ai', 'AI (Depth Anything 3)'], ['comfy', 'ComfyUI (a depth flow of the project)'], ['brightness', 'Brightness (quick, without AI)'],
    ['file', 'A depth map of my own']] },
  { key: 'model', label: 'Model', type: 'enum', def: 'small', anim: false, showIf: (c) => c.source === 'ai',
    options: [['small', 'DA3 Small (fast, 105 MB)'], ['base', 'DA3 Base (149 MB)'],
      ['mono', 'DA3 Mono Large: made for one image, the sky at the back (1.3 GB)'], ['metric', 'DA3 Metric Large (1.3 GB)'],
      ['large', 'DA3 Large (1.4 GB, non-commercial)'], ['giant', 'DA3 Giant (4.9 GB, slow, non-commercial)'],
      ['nested', 'DA3 Nested Giant + Large (6.2 GB, the slowest, non-commercial)'],
      ['video', 'Video Depth Anything Small: made for videos, the depth steady from frame to frame (116 MB)'],
      ['da2-small', 'DA2 Small (99 MB)'], ['da2-base', 'DA2 Base (140 MB, non-commercial)'],
      ['da2-large', 'DA2 Large: fine edges (454 MB, non-commercial)'],
      ['da2-indoor', 'DA2 Metric indoors (454 MB, non-commercial)'], ['da2-outdoor', 'DA2 Metric outdoors (454 MB, non-commercial)']],
    help: 'Downloaded the first time. Non-commercial: their license only allows non-commercial use. Video Depth Anything looks at 32 frames at once: with "Every frame of the video" it gives the steadiest depth' },
  { key: 'detail', label: 'Detail', type: 'enum', def: 'high', anim: false, showIf: (c) => c.source === 'ai',
    options: [['normal', 'Normal (504 px, fastest)'], ['high', 'High (1008 px)'], ['max', 'Very high (1512 px)'],
      ['native', 'The image\'s own size (by tiles, slowest)']],
    help: 'The size it works at: higher keeps finer shapes. Above 504 px it goes over the image in tiles fitted to one look at the whole (DA3 Small, Base, Large and Giant do High and Very high in one go)' },
  { key: 'path', label: 'Depth map', type: 'file', kind: 'bg', showIf: (c) => c.source === 'file', help: 'White = near, black = far' },
  { key: 'every_frame', label: 'Every frame of the video', type: 'bool', anim: false, video: true, showIf: (c) => c.source && c.source !== 'none' && c.source !== 'file',
    help: 'Off: one depth map from its first frame, for all of it. On: the depth of each frame, so it follows what moves in the video (worked out once, frame by frame: slower)' },
  { key: 'relief', label: 'Relief of the surfaces', type: 'num', min: 0.2, max: 3, step: 0.05, def: 1, anim: false, showIf: (c) => c.source && c.source !== 'none',
    help: 'For the lights with depth: how round the shapes are. More = the light wraps them more (strong where they face it, fading as they turn away)' },
];

const VIDEO_RE = /\.(mp4|mkv|mov|webm|avi|m4v|ts)$/i;
// every frame: only for a video and a source that has frames
const allFrames = (image, d) => !!(d.every_frame && VIDEO_RE.test(image || '') && d.source !== 'file');

const viewURL = (image, d, v) => '/api/depth/view?' + new URLSearchParams({ image, source: d.source || '', model: d.model || 'small',
  detail: d.detail || 'high', file: d.path || '', flow: d.flow || '', all: allFrames(image, d) ? '1' : '', v }).toString();

// a job's progress: a bar by the percentage; over a video's frames, the whole and this frame's
function DepthProgress({ job, onCancel }) {
  const f = job.frames;
  const pct = (v) => ((v || 0) * 100).toFixed(1) + '%';
  return html`<div class="depth-prog">
    <div class="row"><div class="progress"><i style=${{ width: pct(job.progress) }}></i></div>
      <span class="faint">${Math.round((job.progress || 0) * 100)} %</span></div>
    ${f && f.total > 0 && html`<div class="row"><div class="progress sub"><i style=${{ width: pct(f.frame) }}></i></div>
      <span class="faint">${Math.round((f.frame || 0) * 100)} %</span></div>`}
    <div class="faint depth-prog-msg">${f && f.total > 0 ? `${t('Frames')} ${Math.min(f.done + 1, f.total)} / ${f.total}` : job.message}</div>
    <div class="row depth-prog-act"><button class="btn sm ghost" title=${t('Cancel it (the frames done so far are kept: working it out again goes on from there)')} onClick=${onCancel}>
      <${Icon} name="x" size=${13} /> ${t('Cancel')}</button></div>
  </div>`;
}

// the fields with the project's depth flows to choose from (with source ComfyUI)
const depthFields = (flows) => {
  const opts = flows.filter((f) => f.kind === 'depth').map((f) => [f.id, f.name]);
  const i = DEPTH_FIELDS.findIndex((f) => f.key === 'model');
  return [...DEPTH_FIELDS.slice(0, i), { key: 'flow', label: 'Flow', type: 'enum', def: '', anim: false, showIf: (c) => c.source === 'comfy',
    options: [['', opts.length ? '—' : 'No depth flows: import one (Project › ComfyUI)'], ...opts],
    help: 'It gets the segment’s image as its first input image; its result (white = near) is the depth map. ComfyUI must be running' },
  ...DEPTH_FIELDS.slice(i)];
};

export function DepthSection({ cfg, onChange, ctx, at = 0 }) {
  const d = cfg.depth || {};
  const flows = useStore(flowsStore);
  useEffect(() => { if (d.source === 'comfy') loadFlows(); }, [d.source]);
  const [ver, setVer] = useState(0);
  const [missing, setMissing] = useState(false);
  const job = ctx.job && ctx.job.kind === 'depth' ? ctx.job : null;
  const running = job && job.status === 'running';
  // a finished job: look at the map again
  const doneKey = job && job.status === 'done' ? job.id : '';
  useEffect(() => { if (doneKey) { setVer((x) => x + 1); setMissing(false); } }, [doneKey]);
  // another source or model: look again
  useEffect(() => { setMissing(false); }, [cfg.path, d.source, d.model, d.detail, d.path, d.flow, d.every_frame]);
  const on = d.source && d.source !== 'none';
  // frames left by a stopped run (every frame of a video): go on from them, or throw them away
  const [partial, setPartial] = useState(0);
  const all = allFrames(cfg.path, d);
  const body = () => ({ image: cfg.path, source: d.source, model: d.model || 'small', detail: d.detail || 'high',
    file: d.path || '', flow: d.flow || '', all });
  useEffect(() => {
    if (!all || running || !cfg.path) { setPartial(0); return; }
    api('GET', '/api/depth/partial?' + new URLSearchParams({ ...body(), all: '1' }).toString()).then((r) => setPartial(r.frames || 0)).catch(() => setPartial(0));
  }, [cfg.path, d.source, d.model, d.detail, d.path, d.flow, all, running, ver]);
  const discard = () => api('POST', '/api/depth/discard', body()).then(() => setPartial(0)).catch(ctx.showErr);
  const make = (force) => api('POST', '/api/depth', { image: cfg.path, source: d.source, model: d.model || 'small', detail: d.detail || 'high',
    file: d.path || '', flow: d.flow || '', all: allFrames(cfg.path, d), force: !!force }).catch(ctx.showErr);
  const isVideo = VIDEO_RE.test(cfg.path || '');
  // the editor: by hand over the picture (a video's every frame: one by one, opening at the frame
  // under the cursor)
  const [editing, setEditing] = useState(null);
  const [nEdited, setNEdited] = useState(0);
  useEffect(() => {
    if (!on || !cfg.path || missing) { setNEdited(0); return; }
    api('GET', '/api/depth/edit/info?' + new URLSearchParams({ ...body(), all: all ? '1' : '' }).toString())
      .then((r) => setNEdited(r.ready ? (r.edited || []).length : 0)).catch(() => setNEdited(0));
  }, [cfg.path, d.source, d.model, d.detail, d.path, d.flow, all, missing, ver]);
  const openEditor = async () => {
    let f = 0;
    if (all) {
      try {
        const r = await api('GET', '/api/depth/edit/info?' + new URLSearchParams({ ...body(), all: '1' }).toString());
        if (r.ready && r.fps > 0) {
          let tv = Math.max(0, (ctx.time || 0) - at);
          if (cfg.loop_video && r.frames > 0) tv %= r.frames / r.fps;
          f = Math.max(0, Math.min(r.frames - 1, Math.round(tv * r.fps)));
        }
      } catch (e) { /* (the first frame) */ }
    }
    setEditing({ req: body(), frame: f });
  };
  const dropEdits = () => {
    if (!confirm(t('Throw away the hand edits of this depth?'))) return;
    api('DELETE', '/api/depth/edit/map?' + new URLSearchParams({ ...body(), all: all ? '1' : '', frame: 'all' }).toString())
      .then(() => setVer((x) => x + 1)).catch(ctx.showErr);
  };
  const fields = depthFields(flows).filter((f) => !f.video || isVideo);
  return html`<${Section} id="depth" title=${t('Depth')} help=${'How near each point of the image is: the light of the emitters follows it, and things can go behind what is nearer.'}>
    <${Form} fields=${fields} value=${d} ctx=${ctx} onChange=${(nd) => onChange({ ...cfg, depth: nd })} />
    ${on && html`<div class="depth-box">
      ${!missing && cfg.path ? html`<img class="depth-img" src=${viewURL(cfg.path, d, ver)} onError=${() => setMissing(true)} onLoad=${() => setMissing(false)} />`
        : html`<div class="depth-none faint">${t(cfg.path ? 'Not worked out yet' : 'The segment has no image')}</div>`}
      <div class="row depth-ready" style="margin-top:6px">
        ${running ? html`<${DepthProgress} job=${job} onCancel=${() => api('POST', '/api/job/cancel').catch(ctx.showErr)} />`
          : !missing && html`<span class="faint" style="font-size:12px"><${Icon} name="check" size=${12} /> ${t('Ready (kept in the project\'s cache)')}</span>`}
        ${job && job.status === 'error' && html`<span class="danger" style="font-size:12px">${job.message}</span>`}
        ${running ? null : missing ? html`<button class="btn sm primary" disabled=${!cfg.path} onClick=${() => make(false)}>
          <${Icon} name="wand-sparkles" size=${13} /> ${t(partial ? 'Go on working out the depth' : 'Work out the depth')}</button>`
          : html`<button class="btn sm ghost" disabled=${ctx.job && ctx.job.status === 'running'} title=${t('Work it out again (another model or flow version, a better result…)')}
              onClick=${() => make(true)}><${Icon} name="rotate-ccw" size=${13} /> ${t('Work out again')}</button>`}
      </div>
      ${!missing && !running && cfg.path && html`<div class="row depth-edit">
        <button class="btn sm" title=${t('Paint the depth by hand over the picture (a video: frame by frame)')} onClick=${openEditor}>
          <${Icon} name="brush" size=${13} /> ${t('Edit the depth…')}</button>
        ${nEdited > 0 && html`<span class="faint" title=${t('Edited by hand: they win over the worked out depth (also after working it out again)')}>${t(all ? '{n} frames edited' : 'Edited by hand', { n: nEdited })}</span>
          <button class="btn sm ghost" title=${t('Throw away the hand edits: back to the worked out depth')} onClick=${dropEdits}><${Icon} name="trash-2" size=${13} /></button>`}
      </div>`}
      ${editing && html`<${DepthEditor} req=${editing.req} startFrame=${editing.frame} showErr=${ctx.showErr}
        onClose=${() => { setEditing(null); setVer((x) => x + 1); }} />`}
      ${partial > 0 && !running && html`<div class="row depth-partial">
        <span class="faint">${t('{n} frames kept from a stopped run', { n: partial })}</span>
        <button class="btn sm ghost" title=${t('Throw them away: the next run starts from the first frame')} onClick=${discard}>
          <${Icon} name="trash-2" size=${13} /> ${t('Discard')}</button></div>`}
    </div>`}
  </${Section}>`;
}
