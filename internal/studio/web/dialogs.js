import { html, useEffect, useState, useRef } from './vendor/preact-htm.js';
import { api, fmtTime, basename } from './util.js';
import { Icon } from './icons.js';
import { FILE_ICON, Select } from './fields.js';
import { t } from './i18n.js';

const KIND_LABEL = {
  audio: 'Choose audio', bg: 'Choose background', lyrics: 'Choose lyrics', txt: 'Choose lyrics text',
  font: 'Choose font', project: 'Open project', dir: 'Choose folder',
};
const KIND_HINT = {
  audio: 'FLAC, WAV, MP3 or OGG', bg: 'Image, GIF or video (MP4, MOV, MKV, WebM, AVI)', lyrics: '.lrc or .srt',
  txt: '.txt (Suno format works)', font: '.ttf or .otf', project: 'project.yaml',
  dir: 'Browse to the folder and press “Use this folder”',
};

export function Modal({ title, icon, onClose, children, footer, wide }) {
  const ref = useRef();
  useEffect(() => {
    const k = (e) => {
      if (e.key !== 'Escape') return;
      const all = document.querySelectorAll('.modal-bg');
      if (all[all.length - 1] === ref.current) onClose(); // only the topmost dialog
    };
    addEventListener('keydown', k);
    return () => removeEventListener('keydown', k);
  }, []);
  return html`<div class="modal-bg" ref=${ref} onMouseDown=${(e) => e.target === e.currentTarget && onClose()}>
    <div class="modal" style=${wide ? { width: 'min(820px,96vw)' } : null}>
      <div class="modal-h">${icon && html`<${Icon} name=${icon} size=${16} cls="muted" />`}${title}<span class="spacer"></span>
        <button class="btn icon ghost" title=${t('Close (Esc)')} onClick=${onClose}><${Icon} name="x" size=${16} /></button></div>
      <div class="mb">${children}</div>
      ${footer && html`<div class="mf">${footer}</div>`}
    </div>
  </div>`;
}

// File browser of the machine running the studio (+ upload into the project)
const VIDEO_EXT = /\.(mp4|mov|mkv|webm|avi|m4v)$/i;

export function FilePicker({ kind, start, onPick, onClose, hasProject }) {
  const [dir, setDir] = useState(null);
  // Images and videos: thumbnail grid (or list), remembered by the browser
  const canGrid = kind === 'bg';
  const [view, setViewState] = useState(() => { try { return localStorage.getItem('audioviz.pickerView') || 'grid'; } catch (e) { return 'grid'; } });
  const setView = (v) => { setViewState(v); try { localStorage.setItem('audioviz.pickerView', v); } catch (e) { /* ignore */ } };
  const grid = canGrid && view === 'grid';
  const [err, setErr] = useState('');
  const [busy, setBusy] = useState(false);
  const fileRef = useRef();
  const load = async (path) => {
    try {
      setErr('');
      const q = new URLSearchParams({ kind: kind === 'dir' ? 'none-' : kind });
      if (path) q.set('path', path);
      setDir(await api('GET', '/api/fs?' + q));
    } catch (e) { setErr(e.message); }
  };
  useEffect(() => { load(start ? (kind === 'dir' ? start : start.replace(/[\\/][^\\/]*$/, '')) : ''); }, []);
  const upload = async (file) => {
    setBusy(true);
    try {
      const fd = new FormData();
      fd.append('file', file);
      const r = await fetch('/api/upload', { method: 'POST', body: fd });
      const d = await r.json();
      if (!r.ok) throw new Error(d.error);
      onPick(d.path);
      onClose();
    } catch (e) { setErr(e.message); }
    setBusy(false);
  };
  const entries = dir ? dir.entries.filter((e) => kind !== 'dir' || e.dir) : [];
  return html`<${Modal} title=${t(KIND_LABEL[kind] || 'Choose file')} icon="folder-open" onClose=${onClose} wide=${true}
    footer=${html`
      ${hasProject && kind !== 'dir' && kind !== 'project' && html`
        <input type="file" ref=${fileRef} style="display:none" onChange=${(e) => e.target.files[0] && upload(e.target.files[0])} />
        <button class="btn" disabled=${busy} onClick=${() => fileRef.current.click()}><${Icon} name="upload" size=${15} /> ${t('Upload from this computer…')}</button>`}
      <span class="spacer"></span>
      <button class="btn" onClick=${onClose}>${t('Cancel')}</button>
      ${kind === 'dir' && dir && html`<button class="btn primary" onClick=${() => { onPick(dir.path); onClose(); }}>${t('Use this folder')}</button>`}`}>
    <p class="faint" style="margin-top:0">${t(KIND_HINT[kind] || '')}</p>
    ${err && html`<div class="alert err" style="margin-bottom:10px"><${Icon} name="triangle-alert" size=${15} />${err}</div>`}
    ${dir && html`<div class="row" style="margin-bottom:10px">
      <div class="btngroup">
        <button class="btn icon" title=${t('Parent folder')} onClick=${() => load(dir.parent)}><${Icon} name="arrow-up" size=${15} /></button>
        <button class="btn icon" title=${t('Home folder')} onClick=${() => load('~')}><${Icon} name="house" size=${15} /></button>
      </div>
      <div class="crumb grow">${dir.path}</div>
      ${canGrid && html`<div class="btngroup">
        <button class=${'btn icon' + (grid ? ' on' : '')} title=${t('Thumbnails')} onClick=${() => setView('grid')}><${Icon} name="layout-grid" size=${15} /></button>
        <button class=${'btn icon' + (grid ? '' : ' on')} title=${t('List')} onClick=${() => setView('list')}><${Icon} name="list" size=${15} /></button>
      </div>`}
    </div>
    ${grid ? html`<div class="tiles">
      ${entries.length === 0 && html`<div class="faint" style="grid-column:1/-1;padding:8px">${t('No files of this type here')}</div>`}
      ${entries.map((e) => html`<div class=${'tile' + (e.dir ? ' dir' : '')} key=${e.path} title=${e.name}
          onClick=${() => (e.dir ? load(e.path) : (onPick(e.path), onClose()))}>
        <div class="th">
          <${Icon} name=${e.dir ? 'folder' : VIDEO_EXT.test(e.name) ? 'film' : 'image'} size=${e.dir ? 30 : 22} cls="faint" />
          ${!e.dir && html`<img loading="lazy" alt="" src=${'/api/thumb?s=240&path=' + encodeURIComponent(e.path)}
            onError=${(ev) => { ev.target.style.display = 'none'; }} />`}
          ${!e.dir && VIDEO_EXT.test(e.name) && html`<span class="vid"><${Icon} name="play" size=${10} /></span>`}
        </div>
        <div class="nm">${e.name}</div>
        ${!e.dir && html`<div class="sz">${(e.size / 1048576).toFixed(1)} MB</div>`}
      </div>`)}
    </div>` : html`<div class="files">
      ${entries.length === 0 && html`<div class="f faint">${t(kind === 'dir' ? 'No folders here' : 'No files of this type here')}</div>`}
      ${entries.map((e) => html`<div class=${'f' + (e.dir ? ' dir' : '')} onClick=${() => (e.dir ? load(e.path) : (onPick(e.path), onClose()))}>
        <${Icon} name=${e.dir ? 'folder' : FILE_ICON[kind] || 'file'} size=${15} cls=${e.dir ? 'muted' : 'faint'} /><span>${e.name}</span>
        ${!e.dir && html`<span class="sz">${(e.size / 1048576).toFixed(1)} MB</span>`}
      </div>`)}
    </div>`}`}
  </${Modal}>`;
}

export function NewProject({ start, onCreate, onClose, pickFile }) {
  const [dir, setDir] = useState(start || '');
  const [name, setName] = useState('my-video');
  const [err, setErr] = useState('');
  return html`<${Modal} title=${t('New project')} icon="plus" onClose=${onClose}
    footer=${html`<button class="btn" onClick=${onClose}>${t('Cancel')}</button>
      <button class="btn primary" onClick=${async () => { try { await onCreate(dir, name); onClose(); } catch (e) { setErr(e.message); } }}>${t('Create project')}</button>`}>
    <div class="fld"><label>${t('Location')}</label><div class="ctl filectl">
      <div class="fn"><${Icon} name="folder" size=${14} cls="faint" /><span>${dir}</span></div>
      <button class="btn icon" title=${t('Choose folder')} onClick=${() => pickFile('dir', setDir, dir)}><${Icon} name="folder-open" size=${15} /></button></div></div>
    <div class="fld"><label>${t('Name')}</label><div class="ctl"><input type="text" class="grow" value=${name} onInput=${(e) => setName(e.target.value)} /></div></div>
    <p class="faint">${t('This creates {p} and an {a} folder for the files you upload.', { p: `${dir}/${name}/project.yaml`, a: 'assets/' })}</p>
    ${err && html`<div class="alert err"><${Icon} name="triangle-alert" size=${15} />${err}</div>`}
  </${Modal}>`;
}

export function ExportDialog({ proj, meta, job, onStart, onCancel, onClose }) {
  const [path, setPath] = useState(proj.output.path || 'output.mp4');
  const [range, setRange] = useState('all');
  const [from, setFrom] = useState(0);
  const [to, setTo] = useState(meta.duration);
  const running = job && job.kind === 'export' && job.status === 'running';
  return html`<${Modal} title=${t('Export video')} icon="clapperboard" onClose=${onClose}
    footer=${html`
      <button class="btn" onClick=${onClose}>${t('Close')}</button>
      ${running ? html`<button class="btn danger" onClick=${onCancel}><${Icon} name="x" size=${15} /> ${t('Cancel export')}</button>`
        : html`<button class="btn primary" disabled=${!meta.hasAudio} onClick=${() => onStart({ path, start: range === 'all' ? 0 : from, end: range === 'all' ? 0 : to })}>
          <${Icon} name="film" size=${15} /> ${t('Export')}</button>`}`}>
    <div class="fld"><label>${t('Output file')}</label><div class="ctl"><input type="text" class="grow mono" value=${path} onInput=${(e) => setPath(e.target.value)} /></div></div>
    <div class="fld"><label>${t('Range')}</label><div class="ctl">
      <${Select} value=${range} translate=${false} onChange=${setRange}
        options=${[['all', t('Whole song ({d})', { d: fmtTime(meta.duration, false) })], ['part', t('Only a part')]]} />
    </div></div>
    ${range === 'part' && html`<div class="fld"><label>${t('From / to (s)')}</label><div class="ctl">
      <input type="number" step="0.1" value=${from} onInput=${(e) => setFrom(+e.target.value)} />
      <input type="number" step="0.1" value=${to} onInput=${(e) => setTo(+e.target.value)} /></div></div>`}
    <p class="faint">${meta.width}×${meta.height} · ${meta.fps} fps · H.264 (${proj.output.preset}, CRF ${proj.output.crf}) · ${t('lossless FLAC audio.')}
      ${' '}${t('A copy of the project as it is now is exported, so you can keep editing.')}</p>
    ${!meta.hasAudio && html`<div class="alert"><${Icon} name="triangle-alert" size=${15} />${t('The project has no audio yet.')}</div>`}
    ${job && job.kind === 'export' && html`<${JobView} job=${job} />`}
  </${Modal}>`;
}

export function JobView({ job }) {
  const cls = { done: 'st-ok', error: 'st-err', canceled: 'st-warn' }[job.status] || '';
  const icon = { done: 'check', error: 'triangle-alert', canceled: 'x' }[job.status] || 'loader-circle';
  return html`<div style="margin-top:12px">
    <div class="row"><div class="progress"><i style=${{ width: (job.progress * 100).toFixed(1) + '%' }}></i></div>
      <span class="mono faint" style="font-size:11px;width:36px;text-align:right">${(job.progress * 100).toFixed(0)}%</span></div>
    <p class=${'row ' + cls} style="font-size:12.5px"><${Icon} name=${icon} size=${15} />${job.message}</p>
    ${job.log && job.log.length > 0 && html`<div class="logbox">${job.log.join('\n')}</div>`}
  </div>`;
}

export const fmtDur = (s) => {
  if (!(s >= 0)) return '–';
  s = Math.round(s);
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`;
};

export const STEP_LABEL = { models: 'Models', prepare: 'Prepare audio', separate: 'Separate the vocals', recognize: 'Recognize the vocals', align: 'Align with the lyrics' };

// Lyrics generation steps: overall bar + current step bar + list of steps
export function GenSteps({ gen, status }) {
  if (!gen) return null;
  const cur = gen.steps[gen.current] || gen.steps[0];
  const icon = { done: 'check', skipped: 'minus', error: 'triangle-alert', running: 'loader-circle', pending: 'circle' };
  return html`<div class="gen">
    <div class="gen-row">
      <span class="gen-l">${t('Total')}</span>
      <div class="progress"><i style=${{ width: (gen.progress * 100).toFixed(1) + '%' }}></i></div>
      <span class="gen-v mono">${Math.round(gen.progress * 100)}%</span>
      <span class="gen-t mono">${fmtDur(gen.elapsed)}</span>
    </div>
    ${status === 'running' && cur && html`<div class="gen-row">
      <span class="gen-l">${t(STEP_LABEL[cur.id] || cur.label)}</span>
      <div class="progress sub"><i style=${{ width: (cur.progress * 100).toFixed(1) + '%' }}></i></div>
      <span class="gen-v mono">${Math.round(cur.progress * 100)}%</span>
      <span class="gen-t mono" title=${t('Elapsed / remaining')}>${fmtDur(cur.elapsed)}${cur.eta > 0 ? ' · −' + fmtDur(cur.eta) : ''}</span>
    </div>`}
    <div class="gen-steps">
      ${gen.steps.map((st, i) => html`<div class=${'gen-step ' + st.state}>
        <${Icon} name=${icon[st.state] || 'circle'} size=${14} cls=${st.state === 'running' ? 'spin' : ''} />
        <span class="n">${i + 1}. ${t(STEP_LABEL[st.id] || st.label)}</span>
        <span class="d">${st.detail}</span>
        <span class="t mono">${st.state === 'done' || st.state === 'running' ? fmtDur(st.elapsed) : ''}</span>
      </div>`)}
    </div>
    ${gen.device && html`<div class="faint" style="font-size:11.5px;margin-top:6px">${t('Recognition running on: {d}', { d: gen.device })}</div>`}
  </div>`;
}

export const LYRIC_LANGS = [['en', 'English'], ['es', 'Spanish'], ['fr', 'French'], ['de', 'German'], ['it', 'Italian'], ['pt', 'Portuguese'], ['ja', 'Japanese']];

export function GenLyricsDialog({ meta, job, models, pickFile, onStart, onCancel, onClose, initialTxt }) {
  const [txt, setTxt] = useState(initialTxt || '');
  const [lang, setLang] = useState('en');
  const [mode, setMode] = useState('precise');
  const mine = job && job.kind === 'lyrics' ? job : null;
  const running = mine && mine.status === 'running';
  const toDownload = (models && !models.whisper ? 574 : 0) + (models && !models.demucs && mode === 'precise' ? 84 : 0);
  return html`<${Modal} title=${t('Generate synced lyrics')} icon="wand-sparkles" onClose=${onClose}
    footer=${html`
      <button class="btn" onClick=${onClose}>${t(running ? 'Keep running in the background' : 'Close')}</button>
      ${running ? html`<button class="btn danger" onClick=${onCancel}><${Icon} name="x" size=${15} /> ${t('Cancel')}</button>`
        : html`<button class="btn primary" disabled=${!txt || !meta.hasAudio} onClick=${() => onStart(txt, lang, mode === 'fast')}>
          <${Icon} name="wand-sparkles" size=${15} /> ${t(mine && mine.status === 'done' ? 'Generate again' : 'Generate')}</button>`}`}>
    ${!running && html`
      <p class="faint" style="margin-top:0">${t('From the lyrics as text (Suno format works: [ ] tags are ignored) it works out when every word is sung. Everything runs inside AudioViz; recognition uses the GPU when available.')}</p>
      <div class="fld"><label>${t('Lyrics text')}</label><div class="ctl filectl">
        <div class="fn"><${Icon} name="file-text" size=${14} cls="faint" /><span>${txt ? basename(txt) : html`<span class="faint">${t('None')}</span>`}</span></div>
        <button class="btn icon" title=${t('Choose file')} onClick=${() => pickFile('txt', setTxt, txt)}><${Icon} name="folder-open" size=${15} /></button></div></div>
      <div class="fld"><label>${t('Language')}</label><div class="ctl"><${Select} value=${lang} options=${LYRIC_LANGS} onChange=${setLang} /></div></div>
      <div class="fld"><label>${t('Mode')}</label><div class="ctl"><${Select} value=${mode} onChange=${setMode}
        options=${[['precise', 'Precise: separates the vocals first (≈ song length)'], ['fast', 'Fast: on the full mix (seconds, less reliable)']]} /></div></div>
      ${toDownload > 0 && html`<div class="alert" style="margin-top:8px"><${Icon} name="info" size=${15} />
        ${t('The first time the models are downloaded (≈ {mb} MB) to the cache; after that it works offline.', { mb: toDownload })}</div>`}
      ${!meta.hasAudio && html`<div class="alert" style="margin-top:8px"><${Icon} name="triangle-alert" size=${15} />${t('The project has no audio yet.')}</div>`}`}
    ${mine && mine.gen && html`<${GenSteps} gen=${mine.gen} status=${mine.status} />`}
    ${mine && mine.status !== 'running' && html`<p class=${'row ' + ({ done: 'st-ok', error: 'st-err', canceled: 'st-warn' }[mine.status] || '')} style="font-size:12.5px">
      <${Icon} name=${{ done: 'check', error: 'triangle-alert', canceled: 'x' }[mine.status] || 'info'} size=${15} />${mine.message}</p>`}
  </${Modal}>`;
}
