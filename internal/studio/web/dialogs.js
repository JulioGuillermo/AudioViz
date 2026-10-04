import { html, useEffect, useState, useRef } from './vendor/preact-htm.js';
import { api, fmtTime, basename } from './util.js';
import { Icon } from './icons.js';
import { FILE_ICON, Select, Switch } from './fields.js';
import { t } from './i18n.js';

const KIND_LABEL = {
  audio: 'Choose audio', bg: 'Choose background', lyrics: 'Choose lyrics', txt: 'Choose lyrics text',
  font: 'Choose font', project: 'Open project', dir: 'Choose folder', zip: 'Import project package',
};
const KIND_HINT = {
  audio: 'FLAC, WAV, MP3 or OGG', bg: 'Image, GIF or video (MP4, MOV, MKV, WebM, AVI)', lyrics: '.lrc or .srt',
  txt: '.txt (Suno format works)', font: '.ttf or .otf', project: 'project.yaml', zip: 'A project exported as .zip',
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

// New exports go to the project's exports/ folder by default, with the date in the name
const stamp = () => { const d = new Date(), p = (n) => String(n).padStart(2, '0'); return `${d.getFullYear()}${p(d.getMonth() + 1)}${p(d.getDate())}-${p(d.getHours())}${p(d.getMinutes())}`; };

export function ExportDialog({ proj, meta, job, onStart, onCancel, onClose, onShowExports }) {
  const [path, setPath] = useState(() => `${meta.dir}/exports/${meta.name}-${stamp()}.mp4`);
  const [range, setRange] = useState('all');
  const [from, setFrom] = useState(0);
  const [to, setTo] = useState(meta.duration);
  const running = job && job.kind === 'export' && job.status === 'running';
  return html`<${Modal} title=${t('Export video')} icon="clapperboard" onClose=${onClose}
    footer=${html`
      ${job && job.kind === 'export' && job.status === 'done' && html`<button class="btn ghost" onClick=${onShowExports}><${Icon} name="film" size=${15} /> ${t('See exports')}</button>`}
      <span class="spacer"></span>
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
    ${job && job.kind === 'export' && html`<${ExportProgress} job=${job} fps=${meta.fps} />`}
  </${Modal}>`;
}

// Export progress: stage, bar, and frames / speed / elapsed / remaining, formatted
export function ExportProgress({ job, fps }) {
  const e = job.export;
  const st = job.status;
  const pct = Math.round(job.progress * 100);
  const final = { done: ['check', 'st-ok'], error: ['triangle-alert', 'st-err'], canceled: ['x', 'st-warn'] }[st];
  const stage = st === 'done' ? t('Done') : st !== 'running' ? t('Stopped') : e && e.stage === 'mux' ? t('Writing the MP4') : t('Rendering frames');
  // While rendering: current speed; once finished: the average of the whole export
  const speed = !e ? 0 : st === 'running' ? e.fps : e.elapsed > 0 ? e.done / e.elapsed : 0;
  const realtime = fps > 0 && speed > 0 ? speed / fps : 0;
  return html`<div class="gen">
    <div class="gen-row">
      <span class="gen-l">${stage}</span>
      <div class="progress"><i style=${{ width: (job.progress * 100).toFixed(1) + '%' }}></i></div>
      <span class="gen-v mono">${pct}%</span>
      <span class="gen-t mono">${fmtDur(e ? e.elapsed : job.elapsed)}</span>
    </div>
    ${e && html`<div class="exp-stats">
      <div><span class="k">${t('Frames')}</span><span class="v mono">${e.done.toLocaleString()} / ${e.total.toLocaleString()}</span></div>
      <div><span class="k">${t(st === 'running' ? 'Speed' : 'Average speed')}</span><span class="v mono">${speed > 0 ? speed.toFixed(1) + ' fps' : '–'}</span>
        ${realtime > 0 && html`<span class="v2">${t('{x}× real time', { x: realtime.toFixed(1) })}</span>`}</div>
      <div><span class="k">${t('Elapsed')}</span><span class="v mono">${fmtDur(e.elapsed)}</span></div>
      <div><span class="k">${t('Remaining')}</span><span class="v mono">${st === 'running' && e.stage === 'render' && e.eta > 0 ? fmtDur(e.eta) : '–'}</span></div>
    </div>`}
    ${final && html`<p class=${'row ' + final[1]} style="font-size:12.5px;margin:10px 0 0"><${Icon} name=${final[0]} size=${15} />
      ${st === 'done' && job.output ? t('Exported to {p}', { p: job.output }) : job.message}</p>`}
    ${job.log && job.log.length > 0 && html`<div class="logbox">${job.log.join('\n')}</div>`}
  </div>`;
}

export const fmtDur = (s) => {
  if (!(s >= 0)) return '–';
  s = Math.round(s);
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`;
};

export const STEP_LABEL = { models: 'Models', prepare: 'Prepare audio', separate: 'Separate the vocals', recognize: 'Recognize the vocals', align: 'Align with the lyrics', lines: 'Build the lines' };

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

const GEN_LANGS = [['auto', 'Detect automatically'], ...LYRIC_LANGS];

// Lyrics as plain text, one line per row: correct the words (a transcription, typos), add or
// remove lines, then align the text again. The recognition of the song is cached, so this
// takes seconds.
export function LyricsTextDialog({ lines, lang: lang0, onAlign, onClose }) {
  const [text, setText] = useState(() => (lines || []).map((l) => l.words.map((w) => w.text).join(' ')).join('\n'));
  const [lang, setLang] = useState(lang0 || 'en');
  const n = text.split('\n').filter((s) => s.trim()).length;
  return html`<${Modal} wide title=${t('Edit the lyrics text')} icon="pencil" onClose=${onClose}
    footer=${html`<span class="faint" style="font-size:12px">${n} ${t('lines')}</span><span class="spacer"></span>
      <button class="btn" onClick=${onClose}>${t('Cancel')}</button>
      <button class="btn primary" disabled=${!n} onClick=${() => onAlign(text, lang)}><${Icon} name="wand-sparkles" size=${15} /> ${t('Align again')}</button>`}>
    <p class="faint" style="margin-top:0">${t('One line of the lyrics per row. Fix misheard words, split or join lines, then align: the words get their times again from the song (the recognition is reused, so it takes seconds). Manual timing changes are replaced.')}</p>
    <textarea class="lyrtext" spellcheck="false" value=${text} onInput=${(e) => setText(e.target.value)}></textarea>
    <div class="fld"><label>${t('Language')}</label><div class="ctl"><${Select} value=${lang} options=${GEN_LANGS} onChange=${setLang} /></div></div>
  </${Modal}>`;
}

export function GenLyricsDialog({ meta, job, models, pickFile, onStart, onCancel, onClose, initialTxt, onEditText }) {
  const [txt, setTxt] = useState(initialTxt || '');
  // Source: the lyrics as text (aligned) or nothing (the audio is transcribed)
  const [src, setSrc] = useState('text');
  const [lang, setLang] = useState('en');
  const [mode, setMode] = useState('precise');
  const mine = job && job.kind === 'lyrics' ? job : null;
  const running = mine && mine.status === 'running';
  const toDownload = (models && !models.whisper ? 574 : 0) + (models && !models.demucs && mode === 'precise' ? 84 : 0);
  return html`<${Modal} title=${t('Generate synced lyrics')} icon="wand-sparkles" onClose=${onClose}
    footer=${html`
      <button class="btn" onClick=${onClose}>${t(running ? 'Keep running in the background' : 'Close')}</button>
      ${running ? html`<button class="btn danger" onClick=${onCancel}><${Icon} name="x" size=${15} /> ${t('Cancel')}</button>`
        : html`<button class="btn primary" disabled=${(src === 'text' && !txt) || !meta.hasAudio}
            onClick=${() => onStart({ txt: src === 'text' ? txt : '', transcribe: src === 'transcribe', lang, fast: mode === 'fast' })}>
          <${Icon} name="wand-sparkles" size=${15} /> ${t(mine && mine.status === 'done' ? 'Generate again' : 'Generate')}</button>`}`}>
    ${!running && html`
      <div class="fld"><label>${t('Source')}</label><div class="ctl"><${Select} value=${src} onChange=${setSrc}
        options=${[['text', 'The lyrics as text (.txt)'], ['transcribe', 'Transcribe the audio (no text needed)']]} /></div></div>
      <p class="faint" style="margin-top:0">${src === 'text'
        ? t('From the lyrics as text (Suno format works: [ ] tags are ignored) it works out when every word is sung. Everything runs inside AudioViz; recognition uses the GPU when available.')
        : t('The vocals are recognized and become the lyrics, split into lines at the pauses. Then correct the words with “Edit text”: the corrected text is aligned again in seconds.')}</p>
      ${src === 'text' && html`<div class="fld"><label>${t('Lyrics text')}</label><div class="ctl filectl">
        <div class="fn"><${Icon} name="file-text" size=${14} cls="faint" /><span>${txt ? basename(txt) : html`<span class="faint">${t('None')}</span>`}</span></div>
        <button class="btn icon" title=${t('Choose file')} onClick=${() => pickFile('txt', setTxt, txt)}><${Icon} name="folder-open" size=${15} /></button></div></div>`}
      <div class="fld"><label>${t('Language')}</label><div class="ctl"><${Select} value=${lang} options=${src === 'transcribe' ? GEN_LANGS : LYRIC_LANGS} onChange=${setLang} /></div></div>
      <div class="fld"><label>${t('Mode')}</label><div class="ctl"><${Select} value=${mode} onChange=${setMode}
        options=${[['precise', 'Precise: separates the vocals first (≈ song length)'], ['fast', 'Fast: on the full mix (seconds, less reliable)']]} /></div></div>
      ${toDownload > 0 && html`<div class="alert" style="margin-top:8px"><${Icon} name="info" size=${15} />
        ${t('The first time the models are downloaded (≈ {mb} MB) to the cache; after that it works offline.', { mb: toDownload })}</div>`}
      ${!meta.hasAudio && html`<div class="alert" style="margin-top:8px"><${Icon} name="triangle-alert" size=${15} />${t('The project has no audio yet.')}</div>`}`}
    ${mine && mine.gen && html`<${GenSteps} gen=${mine.gen} status=${mine.status} />`}
    ${mine && mine.status !== 'running' && html`<p class=${'row ' + ({ done: 'st-ok', error: 'st-err', canceled: 'st-warn' }[mine.status] || '')} style="font-size:12.5px">
      <${Icon} name=${{ done: 'check', error: 'triangle-alert', canceled: 'x' }[mine.status] || 'info'} size=${15} />${mine.message}</p>`}
    ${mine && mine.status === 'done' && onEditText && html`<div class="row" style="margin-top:6px">
      <span class="faint" style="font-size:12px">${t('Check the words and fix what was misheard:')}</span>
      <button class="btn sm" onClick=${onEditText}><${Icon} name="pencil" size=${13} /> ${t('Edit text')}</button></div>`}
  </${Modal}>`;
}

// ------------------------------------------------------------------ projects, packages, exports

const fmtSize = (b) => (b >= 1073741824 ? (b / 1073741824).toFixed(1) + ' GB' : b >= 1048576 ? (b / 1048576).toFixed(1) + ' MB' : Math.max(1, Math.round(b / 1024)) + ' KB');
const fmtDate = (d) => new Date(d).toLocaleString([], { dateStyle: 'medium', timeStyle: 'short' });
const thumbURL = (p) => '/api/thumb?s=320&path=' + encodeURIComponent(p);

// Upload a project package (zip) with progress; resolves with the new state
export function uploadPackage(file, onProgress) {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    const fd = new FormData();
    fd.append('file', file);
    xhr.open('POST', '/api/project/import');
    xhr.upload.onprogress = (e) => e.lengthComputable && onProgress && onProgress(e.loaded / e.total);
    xhr.onload = () => {
      let d = null;
      try { d = JSON.parse(xhr.responseText); } catch (e) { /* not JSON */ }
      if (xhr.status >= 200 && xhr.status < 300) resolve(d);
      else reject(new Error((d && d.error) || xhr.statusText || 'import failed'));
    };
    xhr.onerror = () => reject(new Error('network error'));
    xhr.send(fd);
  });
}

// Grid of the workspace projects (thumbnail of the background, name, date, exports)
export function WorkspaceGrid({ current, onOpen }) {
  const [ws, setWs] = useState(null);
  useEffect(() => { api('GET', '/api/workspace').then(setWs).catch(() => setWs({ projects: [] })); }, []);
  if (!ws) return html`<p class="faint">${t('Loading…')}</p>`;
  return html`<div>
    <div class="faint mono" style="font-size:11px;margin-bottom:8px" title=${t('Workspace folder')}><${Icon} name="folder" size=${12} /> ${ws.workspace}</div>
    ${ws.projects.length === 0 ? html`<p class="faint">${t('No projects in the workspace yet.')}</p>` : html`<div class="tiles proj">
      ${ws.projects.map((p) => html`<div class=${'tile' + (p.path === current ? ' cur' : '')} key=${p.path} title=${p.path} onClick=${() => onOpen(p.path)}>
        <div class="th">
          <${Icon} name="clapperboard" size=${22} cls="faint" />
          ${p.thumb && html`<img loading="lazy" alt="" src=${thumbURL(p.thumb)} onError=${(e) => { e.target.style.display = 'none'; }} />`}
          ${p.exports > 0 && html`<span class="vid" title=${t('Exported videos')}><${Icon} name="film" size=${10} /> ${p.exports}</span>`}
        </div>
        <div class="nm">${p.name}</div>
        <div class="sz">${fmtDate(p.modified)}</div>
      </div>`)}
    </div>`}
  </div>`;
}

// Projects: the workspace, new, import (upload or from this machine), open any project.yaml
export function ProjectsDialog({ current, onOpen, onNew, onImported, pickFile, onClose }) {
  const fileRef = useRef();
  const [busy, setBusy] = useState(null); // upload progress 0..1
  const [err, setErr] = useState('');
  const doUpload = async (f) => {
    setErr(''); setBusy(0);
    try { await uploadPackage(f, setBusy); setBusy(null); onImported(); onClose(); } catch (e) { setBusy(null); setErr(e.message); }
  };
  const fromHost = () => pickFile('zip', async (p) => {
    setErr(''); setBusy(1);
    const fd = new FormData();
    fd.append('path', p);
    try {
      const r = await fetch('/api/project/import', { method: 'POST', body: fd });
      const d = await r.json();
      if (!r.ok) throw new Error(d.error);
      setBusy(null); onImported(); onClose();
    } catch (e) { setBusy(null); setErr(e.message); }
  });
  return html`<${Modal} title=${t('Projects')} icon="folder-open" onClose=${onClose} wide=${true}
    footer=${html`
      <input type="file" accept=".zip" ref=${fileRef} style="display:none" onChange=${(e) => e.target.files[0] && doUpload(e.target.files[0])} />
      <button class="btn" disabled=${busy !== null} onClick=${() => fileRef.current.click()} title=${t('Upload a project .zip from this computer')}><${Icon} name="upload" size=${15} /> ${t('Import…')}</button>
      <button class="btn ghost" disabled=${busy !== null} onClick=${fromHost} title=${t('Import a .zip that is on the machine running the studio')}>${t('From the server…')}</button>
      <span class="spacer"></span>
      <button class="btn ghost" onClick=${() => pickFile('project', onOpen)}>${t('Open other…')}</button>
      <button class="btn primary" onClick=${onNew}><${Icon} name="plus" size=${15} /> ${t('New project')}</button>`}>
    ${busy !== null && html`<div class="row" style="margin-bottom:10px"><div class="progress"><i style=${{ width: (busy * 100).toFixed(1) + '%' }}></i></div>
      <span class="mono faint" style="font-size:11px">${busy < 1 ? Math.round(busy * 100) + '%' : t('Unpacking…')}</span></div>`}
    ${err && html`<div class="alert err" style="margin-bottom:10px"><${Icon} name="triangle-alert" size=${15} />${err}</div>`}
    <${WorkspaceGrid} current=${current} onOpen=${(p) => { onOpen(p); onClose(); }} />
  </${Modal}>`;
}

const LEVELS = [['0', 'None (fastest, largest)'], ['1', 'Fast'], ['6', 'Normal'], ['9', 'Maximum (slowest)']];

// Package: download the project as a zip, or collect its external files into its folder
export function PackageDialog({ meta, onCollected, onClose }) {
  const [level, setLevel] = useState('1');
  const [videos, setVideos] = useState(false);
  const [res, setRes] = useState(null);
  const [busy, setBusy] = useState(false);
  const collect = async () => {
    setBusy(true);
    try { const r = await api('POST', '/api/project/collect'); setRes(r.copied || []); onCollected(); } catch (e) { setRes(e.message); }
    setBusy(false);
  };
  return html`<${Modal} title=${t('Project package')} icon="layers" onClose=${onClose}
    footer=${html`<span class="spacer"></span><button class="btn" onClick=${onClose}>${t('Close')}</button>
      <a class="btn primary" href=${'/api/project/package?level=' + level + '&videos=' + (videos ? 1 : 0)} download=${meta.name + '.zip'}>
        <${Icon} name="download" size=${15} /> ${t('Download .zip')}</a>`}>
    <p class="faint" style="margin-top:0">${t('Everything the project uses goes into one .zip: project, lyrics, audio, backgrounds, fonts and assets/. Files from outside the project are put in assets/ and the project inside the zip points to them. Import it from Projects on any machine.')}</p>
    <div class="fld"><label>${t('Compression')}</label><div class="ctl">
      <${Select} value=${level} options=${LEVELS} onChange=${setLevel} /></div></div>
    <p class="faint" style="font-size:11.5px;margin:-2px 0 8px">${t('Audio, images and videos are already compressed and are stored as they are.')}</p>
    <div class="fld"><label>${t('Exported videos')}</label><div class="ctl">
      <${Switch} checked=${videos} onChange=${setVideos} /><span class="faint" style="font-size:12px">${t('Include the videos of exports/')}</span></div></div>
    <div class="card" style="margin-top:14px">
      <div class="row"><b style="font-size:13px">${t('Collect files into the project')}</b><span class="spacer"></span>
        <button class="btn sm" disabled=${busy} onClick=${collect}>${t('Collect')}</button></div>
      <div class="card-note">${t('Copies the files the project uses from outside its folder into assets/ and points the project to the copies, so the folder works on its own.')}</div>
      ${res !== null && (typeof res === 'string' ? html`<div class="alert err" style="margin-top:8px">${res}</div>`
        : html`<div class="st-ok" style="font-size:12.5px;margin-top:8px">${res.length ? t('Copied {n} files: {f}', { n: res.length, f: res.join(', ') }) : t('Everything was already inside the project.')}</div>`)}
    </div>
  </${Modal}>`;
}

// Exports: the videos of exports/ — play, download, delete
export function ExportsDialog({ meta, onClose }) {
  const [list, setList] = useState(null);
  const [cur, setCur] = useState(null);
  const [err, setErr] = useState('');
  const load = () => api('GET', '/api/exports').then(setList).catch((e) => setErr(e.message));
  useEffect(() => { load(); }, []);
  const del = async (f) => {
    if (!confirm(t('Delete {f}? This cannot be undone.', { f: f.name }))) return;
    try { await api('DELETE', '/api/exports?name=' + encodeURIComponent(f.name)); setCur(null); load(); } catch (e) { setErr(e.message); }
  };
  const media = (f, dl) => '/api/media?path=' + encodeURIComponent(f.path) + (dl ? '&download=1' : '');
  return html`<${Modal} title=${t('Exports')} icon="film" onClose=${onClose} wide=${true}
    footer=${html`${cur && html`<button class="btn ghost" onClick=${() => setCur(null)}><${Icon} name="chevron-left" size=${15} /> ${t('All exports')}</button>`}
      <span class="spacer"></span>
      ${cur && html`<button class="btn ghost danger" onClick=${() => del(cur)}><${Icon} name="trash-2" size=${15} /> ${t('Delete')}</button>
        <a class="btn primary" href=${media(cur, true)} download=${cur.name}><${Icon} name="download" size=${15} /> ${t('Download')}</a>`}
      ${!cur && html`<button class="btn" onClick=${onClose}>${t('Close')}</button>`}`}>
    ${err && html`<div class="alert err" style="margin-bottom:10px"><${Icon} name="triangle-alert" size=${15} />${err}</div>`}
    ${!list ? html`<p class="faint">${t('Loading…')}</p>` : cur ? html`<div class="player">
        <video src=${media(cur)} controls autoplay preload="metadata"></video>
        <div class="row" style="margin-top:8px"><b>${cur.name}</b><span class="spacer"></span>
          <span class="faint mono" style="font-size:11.5px">${cur.width}×${cur.height} · ${fmtDur(cur.duration)} · ${fmtSize(cur.size)} · ${fmtDate(cur.modified)}</span></div>
      </div>` : html`
      <div class="faint mono" style="font-size:11px;margin-bottom:8px"><${Icon} name="folder" size=${12} /> ${list.dir}</div>
      ${list.files.length === 0 ? html`<p class="faint">${t('No exported videos yet. Export one and it will appear here.')}</p>` : html`<div class="tiles proj">
        ${list.files.map((f) => html`<div class="tile" key=${f.path} title=${f.name} onClick=${() => setCur(f)}>
          <div class="th">
            <${Icon} name="film" size=${22} cls="faint" />
            <img loading="lazy" alt="" src=${thumbURL(f.path)} onError=${(e) => { e.target.style.display = 'none'; }} />
            <span class="vid"><${Icon} name="play" size=${10} /> ${fmtDur(f.duration)}</span>
          </div>
          <div class="nm">${f.name}</div>
          <div class="sz">${fmtSize(f.size)} · ${fmtDate(f.modified)}</div>
        </div>`)}
      </div>`}`}
  </${Modal}>`;
}
